package netmap

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
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
	notaryDisabledKey = "notary"
	innerRingKey      = "innerring"

	snapshot0Key     = "snapshotCurrent"
	snapshot1Key     = "snapshotPrevious"
	snapshotEpoch    = "snapshotEpoch"
	snapshotBlockKey = "snapshotBlock"

	containerContractKey = "containerScriptHash"
	balanceContractKey   = "balanceScriptHash"

	cleanupEpochMethod = "newEpoch"
)

const (
	// V2 format
	_ nodeState = iota
	onlineState
	offlineState
)

var (
	configPrefix    = []byte("config")
	candidatePrefix = []byte("candidate")
)

// _deploy function sets up initial list of inner ring public keys.
func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	args := data.([]interface{})
	notaryDisabled := args[0].(bool)
	addrBalance := args[1].(interop.Hash160)
	addrContainer := args[2].(interop.Hash160)
	keys := args[3].([]interop.PublicKey)
	config := args[4].([][]byte)

	ln := len(config)
	if ln%2 != 0 {
		panic("bad configuration")
	}

	for i := 0; i < ln/2; i++ {
		key := config[i*2]
		val := config[i*2+1]

		setConfig(ctx, key, val)
	}

	if isUpdate {
		return
	}

	if len(addrBalance) != 20 || len(addrContainer) != 20 {
		panic("incorrect length of contract script hash")
	}

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.Put(ctx, snapshotEpoch, 0)
	storage.Put(ctx, snapshotBlockKey, 0)

	common.SetSerialized(ctx, snapshot0Key, []netmapNode{})
	common.SetSerialized(ctx, snapshot1Key, []netmapNode{})

	storage.Put(ctx, balanceContractKey, addrBalance)
	storage.Put(ctx, containerContractKey, addrContainer)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, notaryDisabled)
	if notaryDisabled {
		var irList []common.IRNode

		for i := 0; i < len(keys); i++ {
			key := keys[i]
			irList = append(irList, common.IRNode{PublicKey: key})
		}

		common.SetSerialized(ctx, innerRingKey, irList)
		common.InitVote(ctx)
		runtime.Log("netmap contract notary disabled")
	}

	runtime.Log("netmap contract initialized")
}

// Update method updates contract source code and manifest. Can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("netmap contract updated")
}

// InnerRingList method returns slice of structures that contains public key of
// Inner Ring node. Should be used only in notary disabled environment.
//
// If notary enabled, then look to NeoFSAlphabet role in native RoleManagement
// contract of the side chain.
func InnerRingList() []common.IRNode {
	ctx := storage.GetReadOnlyContext()
	return getIRNodes(ctx)
}

// UpdateInnerRing method updates list of Inner Ring node keys. Should be used
// only in notary disabled environment. Can be invoked only by Alphabet nodes.
//
// If notary enabled, then update NeoFSAlphabet role in native RoleManagement
// contract of the side chain. Use notary service to collect multi signature.
func UpdateInnerRing(keys []interop.PublicKey) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked by alphabet nodes")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	var irList []common.IRNode

	for i := 0; i < len(keys); i++ {
		key := keys[i]
		irList = append(irList, common.IRNode{PublicKey: key})
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := keysID(keys, []byte("updateIR"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	runtime.Log("inner ring list updated")
	common.SetSerialized(ctx, innerRingKey, irList)
}

// AddPeer method adds new candidate to the next network map if it was invoked
// by Alphabet node. If it was invoked by node candidate, it produces AddPeer
// notification. Otherwise method throws panic.
//
// If the candidate already exists, it's info is updated.
// NodeInfo argument contains stable marshaled version of netmap.NodeInfo
// structure.
func AddPeer(nodeInfo []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet     []common.IRNode
		nodeKey      []byte
		alphabetCall bool
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		alphabetCall = len(nodeKey) != 0
	} else {
		multiaddr := common.AlphabetAddress()
		alphabetCall = runtime.CheckWitness(multiaddr)
	}

	if !alphabetCall {
		// V2 format
		publicKey := nodeInfo[2:35] // offset:2, len:33
		common.CheckWitness(publicKey)

		runtime.Notify("AddPeer", nodeInfo)
		return
	}

	candidate := storageNode{
		info: nodeInfo,
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		rawCandidate := std.Serialize(candidate)
		id := crypto.Sha256(rawCandidate)

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	addToNetmap(ctx, candidate)
}

// UpdateState method updates state of node from the network map candidate list
// if it was invoked by Alphabet node. If it was invoked by public key owner,
// then it produces UpdateState notification. Otherwise method throws panic.
//
// State argument defines node state. The only supported state now is (2) --
// offline state. Node is removed from network map candidate list.
//
// Method panics when invoked with unsupported states.
func UpdateState(state int, publicKey interop.PublicKey) {
	if len(publicKey) != 33 {
		panic("incorrect public key")
	}

	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			common.CheckWitness(publicKey)

			runtime.Notify("UpdateState", state, publicKey)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{state, publicKey}, []byte("update"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckWitness(publicKey)
		common.CheckAlphabetWitness(multiaddr)
	}

	switch nodeState(state) {
	case offlineState:
		removeFromNetmap(ctx, publicKey)
		runtime.Log("remove storage node from the network map")
	default:
		panic("unsupported state")
	}
}

// NewEpoch method changes epoch number up to provided epochNum argument. Can
// be invoked only by Alphabet nodes. If provided epoch number is less or equal
// current epoch number, method throws panic.
//
// When epoch number updated, contract sets storage node candidates as current
// network map. Also contract invokes NewEpoch method on Balance and Container
// contracts.
//
// Produces NewEpoch notification.
func NewEpoch(epochNum int) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked by inner ring nodes")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{epochNum}, []byte("epoch"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)
	if epochNum <= currentEpoch {
		panic("invalid epoch") // ignore invocations with invalid epoch
	}

	data0snapshot := getSnapshot(ctx, snapshot0Key)
	dataOnlineState := filterNetmap(ctx, onlineState)

	runtime.Log("process new epoch")

	// todo: check if provided epoch number is bigger than current
	storage.Put(ctx, snapshotEpoch, epochNum)
	storage.Put(ctx, snapshotBlockKey, ledger.CurrentIndex())

	// put actual snapshot into previous snapshot
	common.SetSerialized(ctx, snapshot1Key, data0snapshot)

	// put netmap into actual snapshot
	common.SetSerialized(ctx, snapshot0Key, dataOnlineState)

	// make clean up routines in other contracts
	cleanup(ctx, epochNum)

	runtime.Notify("NewEpoch", epochNum)
}

// Epoch method returns current epoch number.
func Epoch() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, snapshotEpoch).(int)
}

// LastEpochBlock method returns block number when current epoch was applied.
func LastEpochBlock() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, snapshotBlockKey).(int)
}

// Netmap method returns list of structures that contain byte array of stable
// marshalled netmap.NodeInfo structure. These structure contain Storage nodes
// of current epoch.
func Netmap() []storageNode {
	ctx := storage.GetReadOnlyContext()
	return getSnapshot(ctx, snapshot0Key)
}

// Snapshot method returns list of structures that contain node state
// and byte array of stable marshalled netmap.NodeInfo structure.
// These structure contain Storage node candidates for next epoch.
func NetmapCandidates() []netmapNode {
	ctx := storage.GetReadOnlyContext()
	return getNetmapNodes(ctx)
}

// Snapshot method returns list of structures that contain node state
// (online: 1) and byte array of stable marshalled netmap.NodeInfo structure.
// These structure contain Storage nodes of specified epoch.
//
// Netmap contract contains only two recent network map snapshot: current and
// previous epoch. For diff bigger than 1 or less than 0 method throws panic.
func Snapshot(diff int) []storageNode {
	var key string

	switch diff {
	case 0:
		key = snapshot0Key
	case 1:
		key = snapshot1Key
	default:
		panic("incorrect diff")
	}

	ctx := storage.GetReadOnlyContext()
	return getSnapshot(ctx, key)
}

// SnapshotByEpoch method returns list of structures that contain node state
// (online: 1) and byte array of stable marshalled netmap.NodeInfo structure.
// These structure contain Storage nodes of specified epoch.
//
// Netmap contract contains only two recent network map snapshot: current and
// previous epoch. For all others epoch method throws panic.
func SnapshotByEpoch(epoch int) []storageNode {
	ctx := storage.GetReadOnlyContext()
	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)

	return Snapshot(currentEpoch - epoch)
}

// Config returns configuration value of NeoFS configuration. If key does
// not exists, returns nil.
func Config(key []byte) interface{} {
	ctx := storage.GetReadOnlyContext()
	return getConfig(ctx, key)
}

// SetConfig key-value pair as a NeoFS runtime configuration value. Can be invoked
// only by Alphabet nodes.
func SetConfig(id, key, val []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("invoked by non inner ring node")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	setConfig(ctx, key, val)

	runtime.Log("configuration has been updated")
}

// ListConfig returns array of structures that contain key and value of all
// NeoFS configuration records. Key and value are both byte arrays.
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

// Version returns version of the contract.
func Version() int {
	return common.Version
}

func addToNetmap(ctx storage.Context, n storageNode) {
	var (
		newNode    = n.info
		newNodeKey = newNode[2:35]
		storageKey = append(candidatePrefix, newNodeKey...)

		node = netmapNode{
			node:  n,
			state: onlineState,
		}
	)

	storage.Put(ctx, storageKey, std.Serialize(node))
}

func removeFromNetmap(ctx storage.Context, key interop.PublicKey) {
	storageKey := append(candidatePrefix, key...)
	storage.Delete(ctx, storageKey)
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
	result := []netmapNode{}

	it := storage.Find(ctx, candidatePrefix, storage.ValuesOnly)
	for iterator.Next(it) {
		rawNode := iterator.Value(it).([]byte)
		node := std.Deserialize(rawNode).(netmapNode)
		result = append(result, node)
	}

	return result
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

func getIRNodes(ctx storage.Context) []common.IRNode {
	data := storage.Get(ctx, innerRingKey)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]common.IRNode)
	}

	return []common.IRNode{}
}

func keysID(args []interop.PublicKey, prefix []byte) []byte {
	var (
		result []byte
	)

	result = append(result, prefix...)

	for i := range args {
		result = append(result, args[i]...)
	}

	return crypto.Sha256(result)
}
