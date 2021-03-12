package netmapcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
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
)

// Init function sets up initial list of inner ring public keys and should
// be invoked once at neofs infrastructure setup.
func Init(owner interop.Hash160, keys []interop.PublicKey) {
	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	var irList []common.IRNode

	for i := 0; i < len(keys); i++ {
		key := keys[i]
		irList = append(irList, common.IRNode{PublicKey: key})
	}

	storage.Put(ctx, common.OwnerKey, owner)

	common.SetSerialized(ctx, innerRingKey, irList)

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.Put(ctx, snapshotEpoch, 0)

	// simplified: this used for const sysfee in AddPeer method
	common.SetSerialized(ctx, netmapKey, []netmapNode{})
	common.SetSerialized(ctx, snapshot0Key, []netmapNode{})
	common.SetSerialized(ctx, snapshot1Key, []netmapNode{})

	runtime.Log("netmap contract initialized")
}

func Migrate(script []byte, manifest []byte) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("netmap contract updated")

	return true
}

func InnerRingList() []common.IRNode {
	ctx := storage.GetReadOnlyContext()
	return getIRNodes(ctx)
}

func Multiaddress() []byte {
	ctx := storage.GetReadOnlyContext()
	return multiaddress(getIRNodes(ctx), false)
}

func Committee() []byte {
	ctx := storage.GetReadOnlyContext()
	return multiaddress(getIRNodes(ctx), true)
}

func UpdateInnerRing(keys []interop.PublicKey) bool {
	ctx := storage.GetContext()

	multiaddr := Multiaddress()
	if !runtime.CheckWitness(multiaddr) {
		panic("updateInnerRing: this method must be invoked by inner ring nodes")
	}

	var irList []common.IRNode

	for i := 0; i < len(keys); i++ {
		key := keys[i]
		irList = append(irList, common.IRNode{PublicKey: key})
	}

	runtime.Log("updateInnerRing: inner ring list updated")
	common.SetSerialized(ctx, innerRingKey, irList)

	return true
}

func AddPeer(nodeInfo []byte) bool {
	ctx := storage.GetContext()

	multiaddr := Multiaddress()
	if !runtime.CheckWitness(multiaddr) {
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

	nm := addToNetmap(ctx, candidate)

	if nm == nil {
		runtime.Log("addPeer: storage node already in the netmap")
	} else {
		common.SetSerialized(ctx, netmapKey, nm)
		runtime.Log("addPeer: add storage node to the network map")
	}

	return true
}

func UpdateState(state int, publicKey interop.PublicKey) bool {
	if len(publicKey) != 33 {
		panic("updateState: incorrect public key")
	}

	ctx := storage.GetContext()

	multiaddr := Multiaddress()
	if !runtime.CheckWitness(multiaddr) {
		if !runtime.CheckWitness(publicKey) {
			panic("updateState: witness check failed")
		}

		runtime.Notify("UpdateState", state, publicKey)

		return true
	}

	switch nodeState(state) {
	case offlineState:
		newNetmap := removeFromNetmap(ctx, publicKey)
		runtime.Log("updateState: remove storage node from the network map")
		common.SetSerialized(ctx, netmapKey, newNetmap)
	default:
		panic("updateState: unsupported state")
	}

	return true
}

func NewEpoch(epochNum int) bool {
	ctx := storage.GetContext()

	multiaddr := Multiaddress()
	if !runtime.CheckWitness(multiaddr) {
		panic("newEpoch: this method must be invoked by inner ring nodes")
	}

	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)
	if epochNum <= currentEpoch {
		return false // ignore invocations with invalid epoch
	}

	data0snapshot := getSnapshot(ctx, snapshot0Key)
	dataOnlineState := filterNetmap(ctx, onlineState)

	runtime.Log("newEpoch: process new epoch")

	// todo: check if provided epoch number is bigger than current
	storage.Put(ctx, snapshotEpoch, epochNum)

	// put actual snapshot into previous snapshot
	common.SetSerialized(ctx, snapshot1Key, data0snapshot)

	// put netmap into actual snapshot
	common.SetSerialized(ctx, snapshot0Key, dataOnlineState)

	runtime.Notify("NewEpoch", epochNum)

	return true
}

func Epoch() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, snapshotEpoch).(int)
}

func Netmap() []storageNode {
	ctx := storage.GetReadOnlyContext()
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

	ctx := storage.GetReadOnlyContext()
	return getSnapshot(ctx, key)
}

func SnapshotByEpoch(epoch int) []storageNode {
	ctx := storage.GetReadOnlyContext()
	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)

	return Snapshot(currentEpoch - epoch)
}

func Config(key []byte) interface{} {
	ctx := storage.GetReadOnlyContext()
	return getConfig(ctx, key)
}

func SetConfig(id, key, val []byte) bool {
	multiaddr := Multiaddress()
	if !runtime.CheckWitness(multiaddr) {
		panic("setConfig: invoked by non inner ring node")
	}

	ctx := storage.GetContext()

	setConfig(ctx, key, val)

	runtime.Log("setConfig: configuration has been updated")

	return true
}

func InitConfig(args [][]byte) bool {
	ctx := storage.GetContext()

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
	ctx := storage.GetReadOnlyContext()

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

func removeFromNetmap(ctx storage.Context, key interop.PublicKey) []netmapNode {
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
		return std.Deserialize(data.([]byte)).([]common.IRNode)
	}

	return []common.IRNode{}
}

func getNetmapNodes(ctx storage.Context) []netmapNode {
	data := storage.Get(ctx, netmapKey)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]netmapNode)
	}

	return []netmapNode{}
}

func getSnapshot(ctx storage.Context, key string) []storageNode {
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]storageNode)
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

func multiaddress(n []common.IRNode, committee bool) []byte {
	threshold := len(n)/3*2 + 1
	if committee {
		threshold = len(n)/2 + 1
	}

	var result = []byte{0x10 + uint8(threshold)} // m value =  5

	sortedNodes := insertSort(n)

	for _, node := range sortedNodes {
		key := node.PublicKey

		result = append(result, []byte{0x0C, 0x21}...) // 33 byte array
		result = append(result, key...)                // public key
	}

	ln := 0x10 + uint8(len(sortedNodes))
	result = append(result, ln) // n value = 7

	result = append(result, 0x0B) // PUSHNULL

	result = append(result, []byte{0x41, 0x13, 0x8D, 0xEF, 0xAF}...) // NeoCryptoCheckMultisigWithECDsaSecp256r1

	shaHash := crypto.Sha256(result)

	return crypto.Ripemd160(shaHash)
}

func insertSort(nodes []common.IRNode) []common.IRNode {
	for i := 1; i < len(nodes); i++ {
		v := nodes[i]
		j := i - 1
		for j >= 0 && more(nodes[j], v) {
			nodes[j+1] = nodes[j]
			j--
		}
		nodes[j+1] = v
	}

	return nodes
}

func more(a, b common.IRNode) bool {
	keyA := a.PublicKey
	keyB := b.PublicKey

	for i := 1; i < len(keyA); i++ { // start from 1 because we sort based on Xcoord of public key
		if keyA[i] == keyB[i] {
			continue
		}

		return keyA[i] > keyB[i]
	}

	return false
}
