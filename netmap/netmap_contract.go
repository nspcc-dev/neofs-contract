package netmapcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

type (
	storageNode struct {
		info []byte
	}

	netmapNode struct {
		node  storageNode
		state nodeState
	}

	nodeState int

	record struct {
		key []byte
		val []byte
	}
)

const (
	version = 1

	netmapKey     = "netmap"
	innerRingKey  = "innerring"
	configuredKey = "initconfig"

	snapshot0Key  = "snapshotCurrent"
	snapshot1Key  = "snapshotPrevious"
	snapshotEpoch = "snapshotEpoch"
)

const (
	_ nodeState = iota
	onlineState
	offlineState
)

var (
	configPrefix = []byte("config")

	ctx storage.Context
)

func init() {
	ctx = storage.GetContext()
}

// Init function sets up initial list of inner ring public keys and should
// be invoked once at neofs infrastructure setup.
func Init(keys [][]byte) {
	if storage.Get(ctx, innerRingKey) != nil {
		panic("netmap: contract already initialized")
	}

	var irList []common.IRNode

	for i := 0; i < len(keys); i++ {
		key := keys[i]
		irList = append(irList, common.IRNode{PublicKey: key})
	}

	common.SetSerialized(ctx, innerRingKey, irList)

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.Put(ctx, snapshotEpoch, 0)

	// simplified: this used for const sysfee in AddPeer method
	common.SetSerialized(ctx, netmapKey, []netmapNode{})
	common.SetSerialized(ctx, snapshot0Key, []netmapNode{})
	common.SetSerialized(ctx, snapshot1Key, []netmapNode{})
	common.InitVote(ctx)

	runtime.Log("netmap contract initialized")
}

func InnerRingList() []common.IRNode {
	return getIRNodes(ctx)
}

func UpdateInnerRing(keys [][]byte) bool {
	innerRing := getIRNodes(ctx)
	threshold := len(innerRing)/3*2 + 1

	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		panic("updateInnerRing: this method must be invoked by inner ring nodes")
	}

	var irList []common.IRNode

	for i := 0; i < len(keys); i++ {
		key := keys[i]
		irList = append(irList, common.IRNode{PublicKey: key})
	}

	rawIRList := binary.Serialize(irList)
	hashIRList := crypto.SHA256(rawIRList)

	n := common.Vote(ctx, hashIRList, irKey)
	if n >= threshold {
		runtime.Log("updateInnerRing: inner ring list updated")
		common.SetSerialized(ctx, innerRingKey, irList)
		common.RemoveVotes(ctx, hashIRList)
	} else {
		runtime.Log("updateInnerRing: processed invoke from inner ring")
	}

	return true
}

func AddPeer(nodeInfo []byte) bool {
	innerRing := getIRNodes(ctx)
	threshold := len(innerRing)/3*2 + 1

	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		publicKey := nodeInfo[2:35] // offset:2, len:33
		if !runtime.CheckWitness(publicKey) {
			panic("addPeer: witness check failed")
		}
		runtime.Notify("AddPeer", nodeInfo)
		return true
	}

	candidate := storageNode{
		info: nodeInfo,
	}
	rawCandidate := binary.Serialize(candidate)
	hashCandidate := crypto.SHA256(rawCandidate)

	nm := addToNetmap(ctx, candidate)

	n := common.Vote(ctx, hashCandidate, irKey)
	if n >= threshold {
		if nm == nil {
			runtime.Log("addPeer: storage node already in the netmap")
		} else {
			common.SetSerialized(ctx, netmapKey, nm)
			runtime.Log("addPeer: add storage node to the network map")
		}
		common.RemoveVotes(ctx, hashCandidate)
	} else {
		runtime.Log("addPeer: processed invoke from inner ring")
	}

	return true
}

func UpdateState(state int, publicKey []byte) bool {
	if len(publicKey) != 33 {
		panic("updateState: incorrect public key")
	}

	innerRing := getIRNodes(ctx)
	threshold := len(innerRing)/3*2 + 1

	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		if !runtime.CheckWitness(publicKey) {
			panic("updateState: witness check failed")
		}
		runtime.Notify("UpdateState", state, publicKey)
		return true
	}

	switch nodeState(state) {
	case offlineState:
		newNetmap := removeFromNetmap(ctx, publicKey)

		hashID := common.InvokeID([]interface{}{publicKey}, []byte("delete"))
		n := common.Vote(ctx, hashID, irKey)
		if n >= threshold {
			runtime.Log("updateState: remove storage node from the network map")
			common.SetSerialized(ctx, netmapKey, newNetmap)
			common.RemoveVotes(ctx, hashID)
		} else {
			runtime.Log("updateState: processed invoke from inner ring")
		}
	default:
		panic("updateState: unsupported state")
	}

	return true
}

func NewEpoch(epochNum int) bool {
	innerRing := getIRNodes(ctx)
	threshold := len(innerRing)/3*2 + 1

	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		panic("newEpoch: this method must be invoked by inner ring nodes")
	}

	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)
	if epochNum <= currentEpoch {
		return false // ignore invocations with invalid epoch
	}

	data0snapshot := getSnapshot(ctx, snapshot0Key)
	dataOnlineState := filterNetmap(ctx, onlineState)

	hashID := common.InvokeID([]interface{}{epochNum}, []byte("epoch"))

	n := common.Vote(ctx, hashID, irKey)
	if n >= threshold {
		runtime.Log("newEpoch: process new epoch")

		// todo: check if provided epoch number is bigger than current
		storage.Put(ctx, snapshotEpoch, epochNum)

		// put actual snapshot into previous snapshot
		common.SetSerialized(ctx, snapshot1Key, data0snapshot)

		// put netmap into actual snapshot
		common.SetSerialized(ctx, snapshot0Key, dataOnlineState)

		common.RemoveVotes(ctx, hashID)
		runtime.Notify("NewEpoch", epochNum)
	} else {
		runtime.Log("newEpoch: processed invoke from inner ring")
	}

	return true
}

func Epoch() int {
	return storage.Get(ctx, snapshotEpoch).(int)
}

func Netmap() []storageNode {
	return getSnapshot(ctx, snapshot0Key)
}

func Snapshot(diff int) []storageNode {
	var key string
	switch diff {
	case 0:
		key = snapshot0Key
	case 1:
		key = snapshot1Key
	default:
		panic("snapshot: incorrect diff")
	}

	return getSnapshot(ctx, key)
}

func SnapshotByEpoch(epoch int) []storageNode {
	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)

	return Snapshot(currentEpoch - epoch)
}

func Config(key []byte) interface{} {
	return getConfig(ctx, key)
}

func SetConfig(id, key, val []byte) bool {
	// check if it is inner ring invocation
	innerRing := getIRNodes(ctx)
	threshold := len(innerRing)/3*2 + 1

	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		panic("setConfig: invoked by non inner ring node")
	}

	// check unique id of the operation
	hashID := common.InvokeID([]interface{}{id, key, val}, []byte("config"))
	n := common.Vote(ctx, hashID, irKey)

	if n >= threshold {
		common.RemoveVotes(ctx, hashID)
		setConfig(ctx, key, val)

		runtime.Log("setConfig: configuration has been updated")
	}

	return true
}

func InitConfig(args [][]byte) bool {
	if storage.Get(ctx, configuredKey) != nil {
		panic("netmap: configuration already installed")
	}

	ln := len(args)
	if ln%2 != 0 {
		panic("initConfig: bad arguments")
	}

	for i := 0; i < ln/2; i++ {
		key := args[i*2]
		val := args[i*2+1]

		setConfig(ctx, key, val)
	}

	storage.Put(ctx, configuredKey, true)
	runtime.Log("netmap: config has been installed")

	return true
}

func ListConfig() []record {
	var config []record

	it := storage.Find(ctx, configPrefix, storage.None)
	for iterator.Next(it) {
		pair := iterator.Value(it).([]interface{})
		key := pair[0].([]byte)
		val := pair[1].([]byte)
		r := record{key: key[len(configPrefix):], val: val}

		config = append(config, r)
	}

	return config
}

func Version() int {
	return version
}

func addToNetmap(ctx storage.Context, n storageNode) []netmapNode {
	var (
		newNode    = n.info
		newNodeKey = newNode[2:35]

		netmap = getNetmapNodes(ctx)
		node   = netmapNode{
			node:  n,
			state: onlineState,
		}
	)

	for i := range netmap {
		netmapNode := netmap[i].node.info
		netmapNodeKey := netmapNode[2:35]

		if common.BytesEqual(newNodeKey, netmapNodeKey) {
			return nil
		}
	}

	netmap = append(netmap, node)

	return netmap
}

func removeFromNetmap(ctx storage.Context, key []byte) []netmapNode {
	var (
		netmap    = getNetmapNodes(ctx)
		newNetmap = []netmapNode{}
	)

	for i := 0; i < len(netmap); i++ {
		item := netmap[i]
		node := item.node.info
		publicKey := node[2:35] // offset:2, len:33

		if !common.BytesEqual(publicKey, key) {
			newNetmap = append(newNetmap, item)
		}
	}

	return newNetmap
}

func filterNetmap(ctx storage.Context, st nodeState) []storageNode {
	var (
		netmap = getNetmapNodes(ctx)
		result = []storageNode{}
	)

	for i := 0; i < len(netmap); i++ {
		item := netmap[i]
		if item.state == st {
			result = append(result, item.node)
		}
	}

	return result
}

func getIRNodes(ctx storage.Context) []common.IRNode {
	data := storage.Get(ctx, innerRingKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]common.IRNode)
	}

	return []common.IRNode{}
}

func getNetmapNodes(ctx storage.Context) []netmapNode {
	data := storage.Get(ctx, netmapKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]netmapNode)
	}

	return []netmapNode{}
}

func getSnapshot(ctx storage.Context, key string) []storageNode {
	data := storage.Get(ctx, key)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]storageNode)
	}

	return []storageNode{}
}

func getConfig(ctx storage.Context, key interface{}) interface{} {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	return storage.Get(ctx, storageKey)
}

func setConfig(ctx storage.Context, key, val interface{}) {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	storage.Put(ctx, storageKey, val)
}
