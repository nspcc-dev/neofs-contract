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
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/netmap/nodestate"
)

// Node2 stores data related to a single NeoFS storage node. It follows
// the model defined in NeoFS API specification.
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

type epochItem struct {
	height int
	time   int
}

const (
	// DefaultSnapshotCount contains the number of previous snapshots stored by this contract.
	// Must be less than 255.
	DefaultSnapshotCount = 10
	snapshotCountKey     = "snapshotCount"
	snapshotEpoch        = "snapshotEpoch"
	snapshotBlockKey     = "snapshotBlock"

	newEpochSubscribersPrefix = "e"
	cleanupEpochMethod        = "newEpoch"

	// Node2 storage.
	node2CandidatePrefix = "2"
	node2NetmapPrefix    = "p"

	defaultCleanupThreshold = 3
	cleanupThresholdKey     = "t"

	epochIndexKey = 'i'
)

var (
	configPrefix = []byte("config")
)

// _deploy function sets up initial list of inner ring public keys.
// nolint:unused
func _deploy(data any, isUpdate bool) {
	ctx := storage.GetContext()

	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)
		common.CheckVersion(version)

		if version < 22_000 {
			curEpoch := storage.Get(ctx, snapshotEpoch).(int)
			curEpochHeight := storage.Get(ctx, snapshotBlockKey).(int)
			storage.Put(ctx, append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(curEpoch))...), std.Serialize(epochItem{height: curEpochHeight}))
			storage.Delete(ctx, snapshotBlockKey)
		}

		if version < 24_000 {
			// For whatever reason this was also stored here, not just in neofs contract.
			const candidateFeeConfigKey = "InnerRingCandidateFee"
			storage.Delete(ctx, append(configPrefix, []byte(candidateFeeConfigKey)...))
		}

		if version < 26_000 {
			const (
				obsoleteAuditFeeKey          = "AuditFee"
				obsoleteCandidatePrefix      = "candidate"
				obsoleteMaintenanceKey       = "MaintenanceModeAllowed"
				obsoleteNodeV2Key            = "UseNodeV2"
				obsoleteSnapshotKeyPrefix    = "snapshot_"
				obsoleteSnapshotCurrentIDKey = "snapshotCurrent"
			)
			storage.Delete(ctx, append(configPrefix, []byte(obsoleteNodeV2Key)...))
			storage.Delete(ctx, append(configPrefix, []byte(obsoleteAuditFeeKey)...))
			storage.Delete(ctx, append(configPrefix, []byte(obsoleteMaintenanceKey)...))

			it := storage.Find(ctx, obsoleteSnapshotKeyPrefix, storage.KeysOnly)
			for iterator.Next(it) {
				storage.Delete(ctx, iterator.Value(it))
			}
			it = storage.Find(ctx, obsoleteCandidatePrefix, storage.KeysOnly)
			for iterator.Next(it) {
				storage.Delete(ctx, iterator.Value(it))
			}

			storage.Delete(ctx, obsoleteSnapshotCurrentIDKey)
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

	for i := range ln / 2 {
		key := args.config[i*2]
		val := args.config[i*2+1]

		setConfig(ctx, key, val)
	}

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.Put(ctx, snapshotCountKey, DefaultSnapshotCount)
	storage.Put(ctx, snapshotEpoch, 0)
	storage.Put(ctx, []byte(cleanupThresholdKey), defaultCleanupThreshold)

	runtime.Log("netmap contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(nefFile, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nefFile, manifest, common.AppendVersion(data))
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

	runtime.Log("process new epoch")

	// todo: check if provided epoch number is bigger than current
	storage.Put(ctx, snapshotEpoch, epochNum)
	fillNetmap(ctx, epochNum)

	var snapCount = getSnapshotCount(ctx)

	if epochNum > snapCount {
		dropNetmap(ctx, epochNum-snapCount)
	}

	// make clean up routines in other contracts
	cleanup(ctx, epochNum)

	storage.Put(ctx, append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(epochNum))...), std.Serialize(epochItem{
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
	val := storage.Get(ctx, append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(curEpoch))...))
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
	val := storage.Get(ctx, append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(curEpoch))...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val.([]byte)).(epochItem).time
}

// ListNodes provides an iterator to walk over current (as in corresponding
// to the current epoch network map) node set. Iterator values are [Node2]
// structures.
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
		append([]byte(node2NetmapPrefix), convert.Uint32ToBytesBE(uint32(epoch))...),
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
	dbkey := append(append([]byte(node2NetmapPrefix), convert.Uint32ToBytesBE(uint32(epoch))...), key...)
	return storage.Get(ctx, dbkey) != nil
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

	var curEpoch = Epoch()
	for k := curEpoch - oldCount + 1; k < curEpoch-count; k++ {
		dropNetmap(ctx, k)
	}
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

// UnsubscribeFromNewEpoch removes new epoch subscription made by
// [SubscribeForNewEpoch] beforehand. Does nothing if subscription has not
// been made. Transactions that call UnsubscribeFromNewEpoch must be witnessed
// by the Alphabet.
// Produces `NewEpochUnsubscription` notification event if any unsubscription
// was made.
func UnsubscribeFromNewEpoch(contract interop.Hash160) {
	common.CheckAlphabetWitness()

	ctx := storage.GetContext()
	it := storage.Find(ctx, []byte(newEpochSubscribersPrefix), storage.KeysOnly)
	for iterator.Next(it) {
		k := iterator.Value(it).([]byte)
		contractHash := k[2:] // prefix and 1-byte index
		if contract.Equals(contractHash) {
			storage.Delete(ctx, k)
			runtime.Notify("NewEpochUnsubscription", contract)

			return
		}
	}
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
	val := storage.Get(storage.GetReadOnlyContext(), append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(epoch))...))
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
	val := storage.Get(storage.GetReadOnlyContext(), append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(epoch))...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val.([]byte)).(epochItem).time
}

func removeFromNetmap(ctx storage.Context, key interop.PublicKey) {
	storageKey := append([]byte(node2CandidatePrefix), key...)
	storage.Delete(ctx, storageKey)
}

func updateNetmapState(ctx storage.Context, key interop.PublicKey, state nodestate.Type) {
	storageKey := append([]byte(node2CandidatePrefix), key...)
	raw := storage.Get(ctx, storageKey).([]byte)
	if raw == nil {
		panic("peer is missing")
	}
	cand := std.Deserialize(raw).(Candidate)
	cand.State = state
	cand.LastActiveEpoch = Epoch()
	storage.Put(ctx, storageKey, std.Serialize(cand))
}

func fillNetmap(ctx storage.Context, epoch int) {
	var (
		cleanupThreshold = CleanupThreshold()
		epochPrefix      = append([]byte(node2NetmapPrefix), convert.Uint32ToBytesBE(uint32(epoch))...)
		it               = storage.Find(ctx, []byte(node2CandidatePrefix), storage.RemovePrefix)
	)
	for iterator.Next(it) {
		kv := iterator.Value(it).(storage.KeyValue)

		cand := std.Deserialize(kv.Value).(Candidate)
		if cleanupThreshold > 0 && cand.LastActiveEpoch < epoch-cleanupThreshold {
			// Forget about stale nodes.
			updateCandidateState(ctx, interop.PublicKey(kv.Key), nodestate.Offline)
		} else {
			var n2 = Node2{
				Addresses:  cand.Addresses,
				Attributes: cand.Attributes,
				Key:        cand.Key,
				State:      cand.State,
			}
			// Offline nodes are just deleted, so we can omit state check.
			storage.Put(ctx, append(epochPrefix, kv.Key...), std.Serialize(n2))
		}
	}
}

func dropNetmap(ctx storage.Context, epoch int) {
	var it = storage.Find(ctx, append([]byte(node2NetmapPrefix), convert.Uint32ToBytesBE(uint32(epoch))...), storage.KeysOnly)
	for iterator.Next(it) {
		storage.Delete(ctx, iterator.Value(it).([]byte))
	}
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
