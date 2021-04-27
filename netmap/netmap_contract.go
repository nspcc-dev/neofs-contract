package netmapcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
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

	netmapKey         = "netmap"
	configuredKey     = "initconfig"
	notaryDisabledKey = "notary"

	snapshot0Key  = "snapshotCurrent"
	snapshot1Key  = "snapshotPrevious"
	snapshotEpoch = "snapshotEpoch"

	containerContractKey = "containerScriptHash"
	balanceContractKey   = "balanceScriptHash"

	cleanupEpochMethod = "newEpoch"
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
func Init(notaryDisabled bool, owner, addrBalance, addrContainer interop.Hash160) {
	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	if len(addrBalance) != 20 || len(addrContainer) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, common.OwnerKey, owner)

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.Put(ctx, snapshotEpoch, 0)

	// simplified: this used for const sysfee in AddPeer method
	common.SetSerialized(ctx, netmapKey, []netmapNode{})
	common.SetSerialized(ctx, snapshot0Key, []netmapNode{})
	common.SetSerialized(ctx, snapshot1Key, []netmapNode{})

	storage.Put(ctx, balanceContractKey, addrBalance)
	storage.Put(ctx, containerContractKey, addrContainer)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, notaryDisabled)
	if notaryDisabled {
		common.InitVote(ctx)
	}

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

func AddPeer(nodeInfo []byte) bool {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
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

	multiaddr := common.AlphabetAddress()
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

	multiaddr := common.AlphabetAddress()
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

	// make clean up routines in other contracts
	cleanup(ctx, epochNum)

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
	multiaddr := common.AlphabetAddress()
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

func cleanup(ctx storage.Context, epoch int) {
	balanceContractAddr := storage.Get(ctx, balanceContractKey).(interop.Hash160)
	contract.Call(balanceContractAddr, cleanupEpochMethod, contract.All, epoch)

	containerContractAddr := storage.Get(ctx, containerContractKey).(interop.Hash160)
	contract.Call(containerContractAddr, cleanupEpochMethod, contract.All, epoch)
}
