package netmap

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/lib/address"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/neogointernal"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/netmap/nodestate"
)

// Node groups data related to NeoFS storage nodes registered in the NeoFS
// network. The information is stored in the current contract.
type Node struct {
	// Information about the node encoded according to the NeoFS binary
	// protocol.
	BLOB []byte

	// Current node state.
	State nodestate.Type
}

// Node2 is a more modern version of [Node] stored in the contract. It exposes
// all of the node structure to contract.
type Node2 struct {
	// Addresses are to be used to connect to the node.
	Addresses []string

	// Attributes are KV pairs with node metadata.
	Attributes map[string]string

	// Key is a node public key.
	Key interop.PublicKey

	// State is a current node state.
	State nodestate.Type
}

// Candidate is an extended version of [Node2] that also knows about the last
// active epoch for the node.
type Candidate struct {
	// Addresses are to be used to connect to the node.
	Addresses []string

	// Attributes are KV pairs with node metadata.
	Attributes map[string]string

	// Key is a node public key.
	Key interop.PublicKey

	// State is a current node state.
	State nodestate.Type

	// LastActiveEpoch is the last epoch this node was active in.
	LastActiveEpoch int
}

// Temporary migration-related types.
// nolint:deadcode,unused
type oldNode struct {
	BLOB []byte
}

// nolint:deadcode,unused
type oldCandidate struct {
	f1 oldNode
	f2 nodestate.Type
}

// nolint:deadcode,unused
type kv struct {
	k []byte
	v []byte
}

type epochItem struct {
	height int
	time   int
}

const (
	// DefaultSnapshotCount contains the number of previous snapshots stored by this contract.
	// Must be less than 255.
	DefaultSnapshotCount = 10
	snapshotCountKey     = "snapshotCount"
	snapshotKeyPrefix    = "snapshot_"
	snapshotCurrentIDKey = "snapshotCurrent"
	snapshotEpoch        = "snapshotEpoch"
	snapshotBlockKey     = "snapshotBlock"

	// nolint:unused // used only in _deploy func which is nolinted too
	containerContractKey = "containerScriptHash"
	// nolint:unused // used only in _deploy func which is nolinted too
	balanceContractKey = "balanceScriptHash"

	newEpochSubscribersPrefix = "e"
	cleanupEpochMethod        = "newEpoch"

	// Node2 storage.
	node2CandidatePrefix = "2"
	node2NetmapPrefix    = "p"

	defaultCleanupThreshold = 3
	cleanupThresholdKey     = "t"

	epochIndexKey = 'i'

	// nodeKeyOffset is an offset in a serialized node info representation (V2 format)
	// marking the start of the node's public key.
	nodeKeyOffset = 2
	// nodeKeyEndOffset is an offset in a serialized node info representation (V2 format)
	// marking the end of the node's public key.
	nodeKeyEndOffset = nodeKeyOffset + interop.PublicKeyCompressedLen
)

var (
	configPrefix    = []byte("config")
	candidatePrefix = []byte("candidate")
)

// _deploy function sets up initial list of inner ring public keys.
// nolint:deadcode,unused
func _deploy(data any, isUpdate bool) {
	ctx := storage.GetContext()

	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)
		common.CheckVersion(version)

		if version < 16*1_000 {
			count := getSnapshotCount(ctx)
			prefix := []byte(snapshotKeyPrefix)
			for i := 0; i < count; i++ { //nolint:intrange // Not supported by NeoGo
				key := append(prefix, byte(i))
				data := storage.Get(ctx, key)
				if data != nil {
					nodes := std.Deserialize(data.([]byte)).([]oldNode)
					var newnodes []Node
					for j := range nodes {
						// Old structure contains only the first field,
						// second is implicitly assumed to be Online.
						newnodes = append(newnodes, Node{
							BLOB:  nodes[j].BLOB,
							State: nodestate.Online,
						})
					}
					common.SetSerialized(ctx, key, newnodes)
				}
			}

			it := storage.Find(ctx, candidatePrefix, storage.None)
			for iterator.Next(it) {
				cand := iterator.Value(it).(kv)
				oldcan := std.Deserialize(cand.v).(oldCandidate)
				newcan := Node{
					BLOB:  oldcan.f1.BLOB,
					State: oldcan.f2,
				}
				common.SetSerialized(ctx, cand.k, newcan)
			}
		}

		if version < 19_000 {
			balanceContract := storage.Get(ctx, balanceContractKey).(interop.Hash160)
			key := append([]byte(newEpochSubscribersPrefix), append([]byte{byte(0)}, balanceContract...)...)
			storage.Put(ctx, key, []byte{})
			storage.Delete(ctx, balanceContractKey)

			containerContract := storage.Get(ctx, containerContractKey).(interop.Hash160)
			key = append([]byte(newEpochSubscribersPrefix), append([]byte{byte(1)}, containerContract...)...)
			storage.Put(ctx, key, []byte{})
			storage.Delete(ctx, containerContractKey)
		}

		if version < 21_000 {
			storage.Put(ctx, []byte(cleanupThresholdKey), defaultCleanupThreshold)
			setConfig(ctx, "UseNodeV2", []byte{0})
		}

		if version < 22_000 {
			curEpoch := storage.Get(ctx, snapshotEpoch).(int)
			curEpochHeight := storage.Get(ctx, snapshotBlockKey).(int)
			storage.Put(ctx, append([]byte{epochIndexKey}, fourBytesBE(curEpoch)...), std.Serialize(epochItem{height: curEpochHeight}))
			storage.Delete(ctx, snapshotBlockKey)
		}

		return
	}

	var args = data.(struct {
		_       bool                // notaryDisabled
		_       interop.Hash160     // Balance contract not used legacy
		_       interop.Hash160     // Container contract not used legacy
		_       []interop.PublicKey // keys
		config  [][]byte
		version int
	})

	ln := len(args.config)
	if ln%2 != 0 {
		panic("bad configuration")
	}

	for i := 0; i < ln/2; i++ { //nolint:intrange // Not supported by NeoGo
		key := args.config[i*2]
		val := args.config[i*2+1]

		setConfig(ctx, key, val)
	}

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.Put(ctx, snapshotCountKey, DefaultSnapshotCount)
	storage.Put(ctx, snapshotEpoch, 0)
	storage.Put(ctx, []byte(cleanupThresholdKey), defaultCleanupThreshold)
	setConfig(ctx, "UseNodeV2", []byte{1})

	prefix := []byte(snapshotKeyPrefix)
	for i := 0; i < DefaultSnapshotCount; i++ { //nolint:intrange // Not supported by NeoGo
		common.SetSerialized(ctx, append(prefix, byte(i)), []Node{})
	}
	storage.Put(ctx, snapshotCurrentIDKey, 0)

	runtime.Log("netmap contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("netmap contract updated")
}

// InnerRingList method returns a slice of structures that contains the public key of
// an Inner Ring node. It should be used in notary disabled environment only.
//
// If notary is enabled, look to NeoFSAlphabet role in native RoleManagement
// contract of FS chain.
//
// Deprecated: since non-notary settings are no longer supported, refer only to
// the RoleManagement contract only. The method will be removed in one of the
// future releases.
func InnerRingList() []common.IRNode {
	pubs := common.InnerRingNodes()
	nodes := []common.IRNode{}
	for i := range pubs {
		nodes = append(nodes, common.IRNode{PublicKey: pubs[i]})
	}
	return nodes
}

// AddPeerIR is called by the NeoFS Alphabet instead of AddPeer when signature
// of the network candidate is inaccessible. For example, when information about
// the candidate proposed via AddPeer needs to be supplemented. In such cases, a
// new transaction will be required and therefore the candidate's signature is
// not verified by AddPeerIR. Besides this, the behavior is similar.
//
// Deprecated: currently unused, to be removed in future.
func AddPeerIR(nodeInfo []byte) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	publicKey := nodeInfo[nodeKeyOffset:nodeKeyEndOffset]

	addToNetmap(ctx, publicKey, Node{
		BLOB:  nodeInfo,
		State: nodestate.Online,
	})
}

// AddPeer proposes a node for consideration as a candidate for the next-epoch
// network map. Information about the node is accepted in NeoFS API binary
// format. Call transaction MUST be signed by the public key sewn into the
// parameter (compressed 33-byte array starting from 3rd byte), i.e. by
// candidate itself. If the signature is correct, the Notary service will submit
// a request for signature by the NeoFS Alphabet. After collecting a sufficient
// number of signatures, the node will be added to the list of candidates for
// the next-epoch network map ('AddPeerSuccess' notification is thrown after
// that).
//
// Deprecated: migrate to [AddNode].
func AddPeer(nodeInfo []byte) {
	ctx := storage.GetContext()

	publicKey := nodeInfo[nodeKeyOffset:nodeKeyEndOffset]

	common.CheckWitness(publicKey)
	common.CheckAlphabetWitness()

	addToNetmap(ctx, publicKey, Node{
		BLOB:  nodeInfo,
		State: nodestate.Online,
	})
}

// AddNode adds a new node into the candidate list for the next epoch. Node
// must have [nodestate.Online] state to be considered and the request must be
// signed by both the node and Alphabet. AddNode event is emitted upon success.
func AddNode(n Node2) {
	ctx := storage.GetContext()

	if n.State != nodestate.Online {
		panic("can't add non-online node")
	}

	if len(n.Key) != interop.PublicKeyCompressedLen {
		panic("incorrect public key")
	}
	common.CheckWitness(n.Key)
	common.CheckAlphabetWitness()

	var (
		c = Candidate{
			Addresses:       n.Addresses,
			Attributes:      n.Attributes,
			Key:             n.Key,
			State:           n.State,
			LastActiveEpoch: Epoch(),
		}
		key = append([]byte(node2CandidatePrefix), n.Key...)
	)
	storage.Put(ctx, key, std.Serialize(c))
	runtime.Notify("AddNode", n.Key, n.Addresses, n.Attributes)
}

// DeleteNode removes a node with the given public key from candidate list.
// It must be signed by Alphabet nodes and doesn't require node witness. See
// [UpdateState] as well, this method emits UpdateStateSuccess upon success
// with state [nodestate.Offline].
func DeleteNode(pkey interop.PublicKey) {
	if len(pkey) != interop.PublicKeyCompressedLen {
		panic("incorrect public key")
	}

	common.CheckAlphabetWitness()
	updateCandidateState(storage.GetContext(), pkey, nodestate.Offline)
}

// updates state of the network map candidate by its public key in the contract
// storage, and throws UpdateStateSuccess notification after this.
//
// State MUST be from the [nodestate.Type] enum.
func updateCandidateState(ctx storage.Context, publicKey interop.PublicKey, state nodestate.Type) {
	switch state {
	case nodestate.Offline:
		removeFromNetmap(ctx, publicKey)
		runtime.Log("remove storage node from the network map")
	case nodestate.Online, nodestate.Maintenance:
		updateNetmapState(ctx, publicKey, state)
		runtime.Log("update state of the network map candidate")
	default:
		panic("unsupported state")
	}

	runtime.Notify("UpdateStateSuccess", publicKey, state)
}

// UpdateState proposes a new state of candidate for the next-epoch network map.
// The candidate is identified by the given public key. Call transaction MUST be
// signed by the provided public key, i.e. by node itself. If the signature is
// correct, the Notary service will submit a request for signature by the NeoFS
// Alphabet. After collecting a sufficient number of signatures, the candidate's
// state will be switched to the given one ('UpdateStateSuccess' notification is
// thrown after that).
//
// UpdateState panics if requested candidate is missing in the current candidate
// set. UpdateState drops candidate from the candidate set if it is switched to
// [nodestate.Offline].
//
// State MUST be from the [nodestate.Type] enum. Public key MUST be
// interop.PublicKeyCompressedLen bytes.
func UpdateState(state nodestate.Type, publicKey interop.PublicKey) {
	if len(publicKey) != interop.PublicKeyCompressedLen {
		panic("incorrect public key")
	}

	ctx := storage.GetContext()

	common.CheckWitness(publicKey)
	common.CheckAlphabetWitness()

	updateCandidateState(ctx, publicKey, state)
}

// UpdateStateIR is called by the NeoFS Alphabet instead of UpdateState when
// signature of the network candidate is inaccessible. In such cases, a new
// transaction will be required and therefore the candidate's signature is not
// verified by UpdateStateIR. Besides this, the behavior is similar.
//
// Deprecated: migrate to [UpdateState] and [DeleteNode].
func UpdateStateIR(state nodestate.Type, publicKey interop.PublicKey) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	updateCandidateState(ctx, publicKey, state)
}

// NewEpoch method changes the epoch number up to the provided epochNum argument. It can
// be invoked only by Alphabet nodes. If provided epoch number is less than the
// current epoch number or equals it, the method throws panic.
//
// When epoch number is updated, the contract sets storage node candidates as the current
// network map. The contract also invokes NewEpoch method on Balance and Container
// contracts.
//
// It produces NewEpoch notification.
func NewEpoch(epochNum int) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)
	if epochNum <= currentEpoch {
		panic("invalid epoch") // ignore invocations with invalid epoch
	}

	dataOnlineState := filterNetmap(ctx)

	runtime.Log("process new epoch")

	// todo: check if provided epoch number is bigger than current
	storage.Put(ctx, snapshotEpoch, epochNum)
	fillNetmap(ctx, epochNum)

	var (
		snapCount = getSnapshotCount(ctx)
		id        = storage.Get(ctx, snapshotCurrentIDKey).(int)
	)
	id = (id + 1) % snapCount
	storage.Put(ctx, snapshotCurrentIDKey, id)

	// put netmap into actual snapshot
	common.SetSerialized(ctx, snapshotKeyPrefix+string([]byte{byte(id)}), dataOnlineState)

	if epochNum > snapCount {
		dropNetmap(ctx, epochNum-snapCount)
	}

	// make clean up routines in other contracts
	cleanup(ctx, epochNum)

	storage.Put(ctx, append([]byte{epochIndexKey}, fourBytesBE(epochNum)...), std.Serialize(epochItem{
		height: ledger.CurrentIndex(),
		time:   runtime.GetTime(),
	}))

	runtime.Notify("NewEpoch", epochNum)
}

// Epoch method returns the current epoch number.
func Epoch() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, snapshotEpoch).(int)
}

// LastEpochBlock method returns the block number when the current epoch was
// applied. Do not confuse with [LastEpochTime].
//
// Use [GetEpochBlock] for specific epoch.
func LastEpochBlock() int {
	ctx := storage.GetReadOnlyContext()
	curEpoch := storage.Get(ctx, snapshotEpoch).(int)
	val := storage.Get(ctx, append([]byte{epochIndexKey}, fourBytesBE(curEpoch)...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val.([]byte)).(epochItem).height
}

// LastEpochTime method returns the block time when the current epoch was
// applied. Do not confuse with [LastEpochBlock].
//
// Use [GetEpochTime] for specific epoch.
func LastEpochTime() int {
	ctx := storage.GetReadOnlyContext()
	curEpoch := storage.Get(ctx, snapshotEpoch).(int)
	val := storage.Get(ctx, append([]byte{epochIndexKey}, fourBytesBE(curEpoch)...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val.([]byte)).(epochItem).time
}

// Netmap returns set of information about the storage nodes representing a network
// map in the current epoch.
//
// Current state of each node is represented in the State field. It MAY differ
// with the state encoded into BLOB field, in this case binary encoded state
// MUST NOT be processed.
//
// Deprecated: migrate to [ListNodes].
func Netmap() []Node {
	ctx := storage.GetReadOnlyContext()
	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	return getSnapshot(ctx, snapshotKeyPrefix+string([]byte{byte(id)}))
}

// NetmapCandidates returns set of information about the storage nodes
// representing candidates for the network map in the coming epoch.
//
// Current state of each node is represented in the State field. It MAY differ
// with the state encoded into BLOB field, in this case binary encoded state
// MUST NOT be processed.
//
// Deprecated: migrate to [ListCandidates].
func NetmapCandidates() []Node {
	ctx := storage.GetReadOnlyContext()
	return getNetmapNodes(ctx)
}

// ListNodes provides an iterator to walk over current node set. It is similar
// to [Netmap] method, iterator values are [Node2] structures.
func ListNodes() iterator.Iterator {
	return ListNodesEpoch(Epoch())
}

// ListNodesEpoch provides an iterator to walk over node set at the given epoch.
// It's the same as [ListNodes] (and exposed as listNodes from the contract via
// overload), but allows to query a particular epoch data if it's still stored.
// If this epoch is already expired (or not happened yet) returns an empty
// iterator.
func ListNodesEpoch(epoch int) iterator.Iterator {
	return storage.Find(storage.GetReadOnlyContext(),
		append([]byte(node2NetmapPrefix), fourBytesBE(epoch)...),
		storage.ValuesOnly|storage.DeserializeValues)
}

// ListCandidates returns an iterator for a set of current candidate nodes.
// Iterator values are [Candidate] structures.
func ListCandidates() iterator.Iterator {
	return storage.Find(storage.GetReadOnlyContext(),
		[]byte(node2CandidatePrefix),
		storage.ValuesOnly|storage.DeserializeValues)
}

// IsStorageNode allows to check for the given key presence in the current
// network map.
func IsStorageNode(key interop.PublicKey) bool {
	return IsStorageNodeInEpoch(key, Epoch())
}

// IsStorageNodeInEpoch is the same as [IsStorageNode], but allows to do
// the check for previous epochs if they're still stored in the contract.
// If this epoch is no longer stored (or too new) it will return false.
func IsStorageNodeInEpoch(key interop.PublicKey, epoch int) bool {
	if epoch > Epoch() {
		return false
	}

	ctx := storage.GetReadOnlyContext()

	// v2 check is rather trivial.
	key2 := append(append([]byte(node2NetmapPrefix), fourBytesBE(epoch)...), key...)
	v := storage.Get(ctx, key2)
	if v != nil {
		return true
	}

	// v1 is more involved.
	count := getSnapshotCount(ctx)
	diff := Epoch() - epoch
	if count <= diff {
		return false
	}

	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	needID := (id - diff + count) % count
	snapshot := getSnapshot(ctx, snapshotKeyPrefix+string([]byte{byte(needID)}))

	for i := range snapshot {
		nodeInfo := snapshot[i].BLOB
		nodeKey := nodeInfo[nodeKeyOffset:nodeKeyEndOffset]

		if key.Equals(nodeKey) {
			return true
		}
	}
	return false
}

// Snapshot returns set of information about the storage nodes representing a network
// map in (current-diff)-th epoch.
//
// Diff MUST NOT be negative. Diff MUST be less than maximum number of network
// map snapshots stored in the contract. The limit is a contract setting,
// DefaultSnapshotCount by default. See UpdateSnapshotCount for details.
//
// Current state of each node is represented in the State field. It MAY differ
// with the state encoded into BLOB field, in this case binary encoded state
// MUST NOT be processed.
//
// Deprecated: migrate to [ListNodesEpoch].
func Snapshot(diff int) []Node {
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

// UpdateSnapshotCount updates the number of the stored snapshots.
// If a new number is less than the old one, old snapshots are removed.
// Otherwise, history is extended with empty snapshots, so
// `Snapshot` method can return invalid results for `diff = new-old` epochs
// until `diff` epochs have passed.
//
// Count MUST NOT be negative.
func UpdateSnapshotCount(count int) {
	common.CheckAlphabetWitness()
	if count < 0 {
		panic("count must be positive")
	}
	ctx := storage.GetContext()
	oldCount := getSnapshotCount(ctx)
	if oldCount == count {
		panic("count has not changed")
	}
	storage.Put(ctx, snapshotCountKey, count)

	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	var delStart, delFinish int
	if oldCount < count {
		// Increase history size.
		//
		// Old state (N = count, K = oldCount, E = current index, C = current epoch)
		// KEY INDEX: 0   | 1     | ... | E | E+1   | ... | K-1   | ... | N-1
		// EPOCH    : C-E | C-E+1 | ... | C | C-K+1 | ... | C-E-1 |
		//
		// New state:
		// KEY INDEX: 0   | 1     | ... | E | E+1 | ... | K-1 | ... | N-1
		// EPOCH    : C-E | C-E+1 | ... | C | nil | ... | .   | ... | C-E-1
		//
		// So we need to move tail snapshots N-K keys forward,
		// i.e. from E+1 .. K to N-K+E+1 .. N
		diff := count - oldCount
		lower := diff + id + 1
		for k := count - 1; k >= lower; k-- {
			moveSnapshot(ctx, k-diff, k)
		}
		delStart, delFinish = id+1, id+1+diff
		if oldCount < delFinish {
			delFinish = oldCount
		}
	} else {
		// Decrease history size.
		//
		// Old state (N = oldCount, K = count)
		// KEY INDEX: 0   | 1     | ... K1 ... | E | E+1   | ... K2-1 ... | N-1
		// EPOCH    : C-E | C-E+1 | ... .. ... | C | C-N+1 | ... ...  ... | C-E-1
		var step, start int
		if id < count {
			// K2 case, move snapshots from E+1+N-K .. N-1 range to E+1 .. K-1
			// New state:
			// KEY INDEX: 0   | 1     | ... | E | E+1   | ... | K-1
			// EPOCH    : C-E | C-E+1 | ... | C | C-K+1 | ... | C-E-1
			step = oldCount - count
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
		delStart, delFinish = count, oldCount
	}
	for k := delStart; k < delFinish; k++ {
		key := snapshotKeyPrefix + string([]byte{byte(k)})
		storage.Delete(ctx, key)
	}
	var curEpoch = Epoch()
	for k := curEpoch - oldCount + 1; k < curEpoch-count; k++ {
		dropNetmap(ctx, k)
	}
}

func moveSnapshot(ctx storage.Context, from, to int) {
	keyFrom := snapshotKeyPrefix + string([]byte{byte(from)})
	keyTo := snapshotKeyPrefix + string([]byte{byte(to)})
	data := storage.Get(ctx, keyFrom)
	storage.Put(ctx, keyTo, data)
}

// SnapshotByEpoch returns set of information about the storage nodes representing
// a network map in the given epoch.
//
// Behaves like Snapshot: it is called after difference with the current epoch is
// calculated.
//
// Deprecated: migrate to [ListNodesEpoch].
func SnapshotByEpoch(epoch int) []Node {
	ctx := storage.GetReadOnlyContext()
	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)

	return Snapshot(currentEpoch - epoch)
}

// Config returns configuration value of NeoFS configuration. If key does
// not exist, returns nil.
func Config(key []byte) any {
	ctx := storage.GetReadOnlyContext()
	return getConfig(ctx, key)
}

// SetConfig key-value pair as a NeoFS runtime configuration value. It can be invoked
// only by Alphabet nodes.
func SetConfig(id, key, val []byte) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	setConfig(ctx, key, val)

	runtime.Log("configuration has been updated")
}

// ConfigRecord represent a config Key/Value pair.
type ConfigRecord struct {
	Key   []byte
	Value []byte
}

// ListConfig returns an array of structures that contain key and value of all
// NeoFS configuration records. Key and value are both byte arrays.
func ListConfig() []ConfigRecord {
	ctx := storage.GetReadOnlyContext()

	var config []ConfigRecord

	it := storage.Find(ctx, configPrefix, storage.RemovePrefix)
	for iterator.Next(it) {
		config = append(config, iterator.Value(it).(ConfigRecord))
	}

	return config
}

// SubscribeForNewEpoch registers passed contract as a NewEpoch event
// subscriber. Such a contract must have a `NewEpoch` method with a single
// numeric parameter. Transactions that call SubscribeForNewEpoch must be
// witnessed by the Alphabet.
// Produces `NewEpochSubscription` notification event with a just
// registered recipient in a success case.
func SubscribeForNewEpoch(contract interop.Hash160) {
	common.CheckAlphabetWitness()

	if !management.HasMethod(contract, "newEpoch", 1) {
		panic(address.FromHash160(contract) + " contract does not have `newEpoch(epoch)` method")
	}

	ctx := storage.GetContext()
	var num byte
	it := storage.Find(ctx, []byte(newEpochSubscribersPrefix), storage.KeysOnly|storage.RemovePrefix)
	for iterator.Next(it) {
		raw := iterator.Value(it).([]byte)[1:] // 1 byte is an index
		if contract.Equals(raw) {
			return
		}

		num += 1
	}

	var key []byte
	key = append(key, []byte(newEpochSubscribersPrefix)...)
	key = append(key, num)
	key = append(key, contract...)

	storage.Put(ctx, key, []byte{})

	runtime.Notify("NewEpochSubscription", contract)
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

// SetCleanupThreshold sets cleanup threshold configuration. Negative values
// are not allowed. Zero disables stale node cleanups on epoch change.
func SetCleanupThreshold(val int) {
	if val < 0 {
		panic("negative value")
	}

	common.CheckAlphabetWitness()
	storage.Put(storage.GetContext(), []byte(cleanupThresholdKey), val)
}

// CleanupThreshold returns the cleanup threshold configuration. Nodes that
// do not update their state for this number of epochs get kicked out of the
// network map. Zero means cleanups are disabled.
func CleanupThreshold() int {
	return storage.Get(storage.GetReadOnlyContext(), []byte(cleanupThresholdKey)).(int)
}

// UnusedCandidate does nothing except marking Candidate structure as used one
// thereby making RPC binding generator produce code for it. It is a temporary
// solution until we have proper iterator types. Never use it.
func UnusedCandidate() Candidate {
	return Candidate{}
}

// GetEpochBlock returns block index when given epoch came. Returns 0 if the
// epoch is missing. Do not confuse with [GetEpochTime].
//
// Use [LastEpochBlock] if you are interested in the current epoch.
func GetEpochBlock(epoch int) int {
	val := storage.Get(storage.GetReadOnlyContext(), append([]byte{epochIndexKey}, fourBytesBE(epoch)...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val.([]byte)).(epochItem).height
}

// GetEpochTime returns block time when given epoch came. Returns 0 if the epoch
// is missing. Do not confuse with [GetEpochBlock].
//
// Use [LastEpochTime] if you are interested in the current epoch.
func GetEpochTime(epoch int) int {
	val := storage.Get(storage.GetReadOnlyContext(), append([]byte{epochIndexKey}, fourBytesBE(epoch)...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val.([]byte)).(epochItem).time
}

// serializes and stores the given Node by its public key in the contract storage,
// and throws AddPeerSuccess notification after this.
//
// Public key MUST match the one encoded in BLOB field.
func addToNetmap(ctx storage.Context, publicKey []byte, node Node) {
	storageKey := append(candidatePrefix, publicKey...)
	storage.Put(ctx, storageKey, std.Serialize(node))

	runtime.Notify("AddPeerSuccess", interop.PublicKey(publicKey))
}

func removeFromNetmap(ctx storage.Context, key interop.PublicKey) {
	storageKey := append(candidatePrefix, key...)
	storage.Delete(ctx, storageKey)
	storageKey = append([]byte(node2CandidatePrefix), key...)
	storage.Delete(ctx, storageKey)
}

func updateNetmapState(ctx storage.Context, key interop.PublicKey, state nodestate.Type) {
	var present bool

	storageKey := append(candidatePrefix, key...)
	raw := storage.Get(ctx, storageKey).([]byte)
	if raw != nil {
		present = true
		node := std.Deserialize(raw).(Node)
		node.State = state
		storage.Put(ctx, storageKey, std.Serialize(node))
	}
	storageKey = append([]byte(node2CandidatePrefix), key...)
	raw = storage.Get(ctx, storageKey).([]byte)
	if raw != nil {
		present = true
		cand := std.Deserialize(raw).(Candidate)
		cand.State = state
		cand.LastActiveEpoch = Epoch()
		storage.Put(ctx, storageKey, std.Serialize(cand))
	}
	if !present {
		panic("peer is missing")
	}
}

func filterNetmap(ctx storage.Context) []Node {
	var (
		netmap = getNetmapNodes(ctx)
		result = []Node{}
	)

	for _, item := range netmap {
		if item.State != nodestate.Offline {
			result = append(result, item)
		}
	}

	return result
}

func fourBytesBE(num int) []byte {
	var res = make([]byte, 4)
	copy(res, convert.ToBytes(num))                    // LE
	neogointernal.Opcode1NoReturn("REVERSEITEMS", res) // BE
	return res
}

func fillNetmap(ctx storage.Context, epoch int) {
	var (
		cleanupThreshold = CleanupThreshold()
		epochPrefix      = append([]byte(node2NetmapPrefix), fourBytesBE(epoch)...)
		it               = storage.Find(ctx, []byte(node2CandidatePrefix), storage.RemovePrefix)
	)
	for iterator.Next(it) {
		kv := iterator.Value(it).(kv)

		cand := std.Deserialize(kv.v).(Candidate)
		if cleanupThreshold > 0 && cand.LastActiveEpoch < epoch-cleanupThreshold {
			// Forget about stale nodes.
			updateCandidateState(ctx, interop.PublicKey(kv.k), nodestate.Offline)
		} else {
			var n2 = Node2{
				Addresses:  cand.Addresses,
				Attributes: cand.Attributes,
				Key:        cand.Key,
				State:      cand.State,
			}
			// Offline nodes are just deleted, so we can omit state check.
			storage.Put(ctx, append(epochPrefix, kv.k...), std.Serialize(n2))
		}
	}
}

func dropNetmap(ctx storage.Context, epoch int) {
	var it = storage.Find(ctx, append([]byte(node2NetmapPrefix), fourBytesBE(epoch)...), storage.KeysOnly)
	for iterator.Next(it) {
		storage.Delete(ctx, iterator.Value(it).([]byte))
	}
}

func getNetmapNodes(ctx storage.Context) []Node {
	result := []Node{}

	it := storage.Find(ctx, candidatePrefix, storage.ValuesOnly|storage.DeserializeValues)
	for iterator.Next(it) {
		node := iterator.Value(it).(Node)
		result = append(result, node)
	}

	return result
}

func getSnapshot(ctx storage.Context, key string) []Node {
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]Node)
	}

	return []Node{}
}

func getConfig(ctx storage.Context, key any) any {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	return storage.Get(ctx, storageKey)
}

func setConfig(ctx storage.Context, key, val any) {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	storage.Put(ctx, storageKey, val)
}

func cleanup(ctx storage.Context, epoch int) {
	it := storage.Find(ctx, newEpochSubscribersPrefix, storage.RemovePrefix|storage.KeysOnly)
	for iterator.Next(it) {
		contractHash := interop.Hash160(iterator.Value(it).([]byte)[1:]) // one byte is for number prefix
		contract.Call(contractHash, cleanupEpochMethod, contract.All, epoch)
	}
}
