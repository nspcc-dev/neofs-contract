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

	// DefaultSnapshotCount contains the number of previous snapshots stored by this contract.
	// Must be less than 255.
	DefaultSnapshotCount = 10
	snapshotCountKey     = "snapshotCount"
	snapshotKeyPrefix    = "snapshot_"
	snapshotCurrentIDKey = "snapshotCurrent"
	snapshotEpoch        = "snapshotEpoch"
	snapshotBlockKey     = "snapshotBlock"

	containerContractKey = "containerScriptHash"
	balanceContractKey   = "balanceScriptHash"

	cleanupEpochMethod = "newEpoch"
)

const (
	// V2 format
	_ nodeState = iota
	OnlineState
	OfflineState
)

var (
	configPrefix    = []byte("config")
	candidatePrefix = []byte("candidate")
)

// _deploy function sets up initial list of inner ring public keys.
func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	var args = data.(struct {
		notaryDisabled bool
		addrBalance    interop.Hash160
		addrContainer  interop.Hash160
		keys           []interop.PublicKey
		config         [][]byte
		version        int
	})

	ln := len(args.config)
	if ln%2 != 0 {
		panic("bad configuration")
	}

	for i := 0; i < ln/2; i++ {
		key := args.config[i*2]
		val := args.config[i*2+1]

		setConfig(ctx, key, val)
	}

	if isUpdate {
		common.CheckVersion(args.version)
		storage.Put(ctx, snapshotCountKey, DefaultSnapshotCount)
		return
	}

	if len(args.addrBalance) != interop.Hash160Len || len(args.addrContainer) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.Put(ctx, snapshotCountKey, DefaultSnapshotCount)
	storage.Put(ctx, snapshotEpoch, 0)
	storage.Put(ctx, snapshotBlockKey, 0)

	prefix := []byte(snapshotKeyPrefix)
	for i := 0; i < DefaultSnapshotCount; i++ {
		common.SetSerialized(ctx, append(prefix, byte(i)), []storageNode{})
	}
	storage.Put(ctx, snapshotCurrentIDKey, 0)

	storage.Put(ctx, balanceContractKey, args.addrBalance)
	storage.Put(ctx, containerContractKey, args.addrContainer)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, args.notaryDisabled)
	if args.notaryDisabled {
		common.SetSerialized(ctx, innerRingKey, args.keys)
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
	pubs := getIRNodes(ctx)
	nodes := []common.IRNode{}
	for i := range pubs {
		nodes = append(nodes, common.IRNode{PublicKey: pubs[i]})
	}
	return nodes
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
		alphabet []interop.PublicKey
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
	common.SetSerialized(ctx, innerRingKey, keys)
}

// AddPeerIR method tries to add new candidate to the network map.
// Should only be invoked in notary-enabled environment by the alphabet.
func AddPeerIR(nodeInfo []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)
	if notaryDisabled {
		panic("AddPeerIR should only be called in notary-enabled environment")
	}

	common.CheckAlphabetWitness(common.AlphabetAddress())

	addToNetmap(ctx, storageNode{info: nodeInfo})
	return
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
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
	}

	// If notary is enabled or caller is not an alphabet node,
	// just emit the notification for alphabet.
	if !notaryDisabled || len(nodeKey) == 0 {
		// V2 format
		publicKey := nodeInfo[2:35] // offset:2, len:33

		common.CheckWitness(publicKey)
		if notaryDisabled {
			runtime.Notify("AddPeer", nodeInfo)
		}
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

// UpdateState method updates state of node from the network map candidate list.
// For notary-ENABLED environment tx must be signed by both storage node and the alphabet.
// To force update without storage node signature, see `UpdateStateIR`.
//
// For notary-DISABLED environment the behaviour depends on who signed the transaction:
// 1. If it was signed by alphabet, go into voting.
// 2. If it was signed by a storage node, emit `UpdateState` notification.
// 2. Fail in any other case.
//
// The behaviour can be summarized in the following table:
// | notary \ Signer | Storage node | Alphabet | Both                  |
// | ENABLED         | FAIL         | FAIL     | OK                    |
// | DISABLED        | NOTIFICATION | OK       | OK (same as alphabet) |
// State argument defines node state. The only supported state now is (2) --
// offline state. Node is removed from network map candidate list.
//
// Method panics when invoked with unsupported states.
func UpdateState(state int, publicKey interop.PublicKey) {
	if len(publicKey) != interop.PublicKeyCompressedLen {
		panic("incorrect public key")
	}

	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	if notaryDisabled {
		alphabet := common.AlphabetNodes()
		nodeKey := common.InnerRingInvoker(alphabet)

		// If caller is not an alphabet node,
		// just emit the notification for alphabet.
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
		common.CheckWitness(publicKey)
		common.CheckAlphabetWitness(common.AlphabetAddress())
	}

	switch nodeState(state) {
	case OfflineState:
		removeFromNetmap(ctx, publicKey)
		runtime.Log("remove storage node from the network map")
	default:
		panic("unsupported state")
	}
}

// UpdateStateIR method tries to change node state in the network map.
// Should only be invoked in notary-enabled environment by the alphabet.
func UpdateStateIR(state nodeState, publicKey interop.PublicKey) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)
	if notaryDisabled {
		panic("UpdateStateIR should only be called in notary-enabled environment")
	}

	common.CheckAlphabetWitness(common.AlphabetAddress())

	switch state {
	case OfflineState:
		removeFromNetmap(ctx, publicKey)
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
		alphabet []interop.PublicKey
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

	dataOnlineState := filterNetmap(ctx, OnlineState)

	runtime.Log("process new epoch")

	// todo: check if provided epoch number is bigger than current
	storage.Put(ctx, snapshotEpoch, epochNum)
	storage.Put(ctx, snapshotBlockKey, ledger.CurrentIndex())

	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	id = (id + 1) % getSnapshotCount(ctx)
	storage.Put(ctx, snapshotCurrentIDKey, id)

	// put netmap into actual snapshot
	common.SetSerialized(ctx, snapshotKeyPrefix+string([]byte{byte(id)}), dataOnlineState)

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
	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	return getSnapshot(ctx, snapshotKeyPrefix+string([]byte{byte(id)}))
}

// NetmapCandidates method returns list of structures that contain node state
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
	ctx := storage.GetReadOnlyContext()
	count := getSnapshotCount(ctx)
	if diff < 0 || count <= diff {
		panic("incorrect diff")
	}

	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	needID := (id - diff + count) % count
	key := snapshotKeyPrefix + string([]byte{byte(needID)})
	return getSnapshot(ctx, key)
}

func getSnapshotCount(ctx storage.Context) int {
	return storage.Get(ctx, snapshotCountKey).(int)
}

// UpdateSnapshotCount updates number of stored snapshots.
// If new number is less than the old one, old snapshots are removed.
// Otherwise, history is extended to with empty snapshots, so
// `Snapshot` method can return invalid results for `diff = new-old` epochs
// until `diff` epochs have passed.
func UpdateSnapshotCount(count int) {
	common.CheckAlphabetWitness(common.AlphabetAddress())
	if count < 0 {
		panic("count must be positive")
	}
	ctx := storage.GetContext()
	curr := getSnapshotCount(ctx)
	if curr == count {
		panic("count has not changed")
	}
	storage.Put(ctx, snapshotCountKey, count)

	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	var delStart, delFinish int
	if curr < count {
		// Increase history size.
		//
		// Old state (N = count, K = curr, E = current index, C = current epoch)
		// KEY INDEX: 0   | 1     | ... | E | E+1   | ... | K-1   | ... | N-1
		// EPOCH    : C-E | C-E+1 | ... | C | C-K+1 | ... | C-E-1 |
		//
		// New state:
		// KEY INDEX: 0   | 1     | ... | E | E+1 | ... | K-1 | ... | N-1
		// EPOCH    : C-E | C-E+1 | ... | C | nil | ... | .   | ... | C-E-1
		//
		// So we need to move tail snapshots N-K keys forward,
		// i.e. from E+1 .. K to N-K+E+1 .. N
		diff := count - curr
		lower := diff + id + 1
		for k := count - 1; k >= lower; k-- {
			moveSnapshot(ctx, k-diff, k)
		}
		delStart, delFinish = id+1, id+1+diff
		if curr < delFinish {
			delFinish = curr
		}
	} else {
		// Decrease history size.
		//
		// Old state (N = curr, K = count)
		// KEY INDEX: 0   | 1     | ... K1 ... | E | E+1   | ... K2-1 ... | N-1
		// EPOCH    : C-E | C-E+1 | ... .. ... | C | C-N+1 | ... ...  ... | C-E-1
		var step, start int
		if id < count {
			// K2 case, move snapshots from E+1+N-K .. N-1 range to E+1 .. K-1
			// New state:
			// KEY INDEX: 0   | 1     | ... | E | E+1   | ... | K-1
			// EPOCH    : C-E | C-E+1 | ... | C | C-K+1 | ... | C-E-1
			step = curr - count
			start = id + 1
		} else {
			// New state:
			// KEY INDEX: 0     | 1     | ... | K-1
			// EPOCH    : C-K+1 | C-K+2 | ... | C
			// K1 case, move snapshots from E-K+1 .. E range to 0 .. K-1
			// AND replace current id with K-1
			step = id - count + 1
			storage.Put(ctx, snapshotCurrentIDKey, count-1)
		}
		for k := start; k < count; k++ {
			moveSnapshot(ctx, k+step, k)
		}
		delStart, delFinish = count, curr
	}
	for k := delStart; k < delFinish; k++ {
		key := snapshotKeyPrefix + string([]byte{byte(k)})
		storage.Delete(ctx, key)
	}
}

func moveSnapshot(ctx storage.Context, from, to int) {
	keyFrom := snapshotKeyPrefix + string([]byte{byte(from)})
	keyTo := snapshotKeyPrefix + string([]byte{byte(to)})
	data := storage.Get(ctx, keyFrom)
	storage.Put(ctx, keyTo, data)
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
		alphabet []interop.PublicKey
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
		pair := iterator.Value(it).(struct {
			key []byte
			val []byte
		})
		r := record{key: pair.key[len(configPrefix):], val: pair.val}

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
			state: OnlineState,
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

	it := storage.Find(ctx, candidatePrefix, storage.ValuesOnly|storage.DeserializeValues)
	for iterator.Next(it) {
		node := iterator.Value(it).(netmapNode)
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

func getIRNodes(ctx storage.Context) []interop.PublicKey {
	data := storage.Get(ctx, innerRingKey)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]interop.PublicKey)
	}

	return []interop.PublicKey{}
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
