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

	newEpochSubscribersPrefix = "e"
	cleanupEpochMethod        = "newEpoch"

	// Node2 storage.
	node2CandidatePrefix = "2"
	node2NetmapPrefix    = "p"

	defaultCleanupThreshold = 3
	cleanupThresholdKey     = "t"

	epochIndexKey           = 'i'
	netmapVersionKey        = 'j'
	epochToNetmapVersionKey = 'k'
	netmapChangedFlagKey    = 'l'
)

var (
	configPrefix = []byte("config")
)

// _deploy function sets up initial list of inner ring public keys.
// nolint:unused
func _deploy(data any, isUpdate bool) {
	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)
		common.CheckVersion(version)

		if version < 26_000 {
			const (
				obsoleteAuditFeeKey          = "AuditFee"
				obsoleteCandidatePrefix      = "candidate"
				obsoleteMaintenanceKey       = "MaintenanceModeAllowed"
				obsoleteNodeV2Key            = "UseNodeV2"
				obsoleteSnapshotKeyPrefix    = "snapshot_"
				obsoleteSnapshotCurrentIDKey = "snapshotCurrent"
			)
			storage.LocalDelete(append(configPrefix, []byte(obsoleteNodeV2Key)...))
			storage.LocalDelete(append(configPrefix, []byte(obsoleteAuditFeeKey)...))
			storage.LocalDelete(append(configPrefix, []byte(obsoleteMaintenanceKey)...))

			it := storage.LocalFind([]byte(obsoleteSnapshotKeyPrefix), storage.KeysOnly)
			for iterator.Next(it) {
				storage.LocalDelete(iterator.Value(it).([]byte))
			}
			it = storage.LocalFind([]byte(obsoleteCandidatePrefix), storage.KeysOnly)
			for iterator.Next(it) {
				storage.LocalDelete(iterator.Value(it).([]byte))
			}

			storage.LocalDelete([]byte(obsoleteSnapshotCurrentIDKey))
		}

		if version < 27_000 {
			// homomorphic hashing was deprecated starting from API 2.23, see
			// * https://github.com/nspcc-dev/neofs-api/pull/385
			// * https://github.com/nspcc-dev/neofs-node/issues/3847
			storage.LocalDelete(append(configPrefix, []byte("HomomorphicHashingDisabled")...))

			migrateToVersionedNetmap()
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

		setConfig(key, val)
	}

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.LocalPut([]byte(snapshotCountKey), convert.ToBytes(DefaultSnapshotCount))
	storage.LocalPut([]byte(snapshotEpoch), convert.ToBytes(0))
	storage.LocalPut([]byte(cleanupThresholdKey), convert.ToBytes(defaultCleanupThreshold))
	storage.LocalPut([]byte{netmapVersionKey}, convert.ToBytes(0))
	storage.LocalPut(append([]byte{epochToNetmapVersionKey}, make([]byte, 4)...), convert.ToBytes(0))

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

// AddNode adds a new node into the candidate list for the next epoch. Node
// must have [nodestate.Online] state to be considered and the request must be
// signed by both the node and Alphabet. AddNode event is emitted upon success.
func AddNode(n Node2) {
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
	storage.LocalPut(key, std.Serialize(c))
	// consider any `AddNode` call as a new network map, a node may change
	// its addresses, attributes, etc. in the worst scenario it just triggers
	// new network map version without changes
	setNetmapChangedFlag(true)
	runtime.Notify("AddNode", n.Key, n.Addresses, n.Attributes)

	numOfContainers := contract.Call(common.ResolveFSContract("container"), "totalSupply", contract.ReadOnly).(int)
	if numOfContainers == 0 {
		curr := Epoch()
		NewEpoch(curr + 1)
	}
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
	updateCandidateState(pkey, nodestate.Offline)
}

// updates state of the network map candidate by its public key in the contract
// storage, and throws UpdateStateSuccess notification after this.
//
// State MUST be from the [nodestate.Type] enum.
func updateCandidateState(publicKey interop.PublicKey, state nodestate.Type) {
	switch state {
	case nodestate.Offline:
		removeFromNetmap(publicKey)
		setNetmapChangedFlag(true)
		runtime.Log("remove storage node from the network map")
	case nodestate.Online, nodestate.Maintenance:
		updateNetmapState(publicKey, state)
		runtime.Log("update state of the network map candidate")
	default:
		panic("unsupported state")
	}

	runtime.Notify("UpdateStateSuccess", publicKey, state)
}

func setNetmapChangedFlag(flag bool) {
	storage.LocalPut([]byte{netmapChangedFlagKey}, convert.ToBytes(flag))
}

func netmapChangedFlag() bool {
	return convert.ToBool(storage.LocalGet([]byte{netmapChangedFlagKey}))
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

	common.CheckWitness(publicKey)
	common.CheckAlphabetWitness()

	updateCandidateState(publicKey, state)
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
	common.CheckAlphabetWitness()

	currentEpoch := convert.ToInteger(storage.LocalGet([]byte(snapshotEpoch)))
	if epochNum <= currentEpoch {
		// ignore invocations with invalid epoch
		panic("invalid epoch, current: " + std.Itoa(currentEpoch, 10) + ", received: " + std.Itoa(epochNum, 10))
	}

	runtime.Log("process new epoch")

	storage.LocalPut([]byte(snapshotEpoch), convert.ToBytes(epochNum))
	updateNetmap(epochNum)

	dropNetmaps()

	// make clean up routines in other contracts
	cleanup(epochNum)

	storage.LocalPut(append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(epochNum))...), std.Serialize(epochItem{
		height: ledger.CurrentIndex(),
		time:   runtime.GetTime(),
	}))

	runtime.Notify("NewEpoch", epochNum)
}

// Epoch method returns the current epoch number.
func Epoch() int {
	return convert.ToInteger(storage.LocalGet([]byte(snapshotEpoch)))
}

// LastEpochBlock method returns the block number when the current epoch was
// applied. Do not confuse with [LastEpochTime].
//
// Use [GetEpochBlock] for specific epoch.
func LastEpochBlock() int {
	curEpoch := Epoch()
	val := storage.LocalGet(append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(curEpoch))...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val).(epochItem).height
}

// LastEpochTime method returns the block time when the current epoch was
// applied. Do not confuse with [LastEpochBlock].
//
// Use [GetEpochTime] for specific epoch.
func LastEpochTime() int {
	curEpoch := Epoch()
	val := storage.LocalGet(append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(curEpoch))...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val).(epochItem).time
}

// ListNodes provides an iterator to walk over current (as in corresponding
// to the current epoch network map) node set. Iterator values are [Node2]
// structures.
func ListNodes() iterator.Iterator {
	return ListNodesVersion(NetworkMapVersion())
}

// ListNodesVersion provides an iterator to walk over node set at the given
// version. It's the same as [ListNodes], but allows to query a particular
// version data if it's still stored. If this version is already removed
// returns an empty iterator.
func ListNodesVersion(version int) iterator.Iterator {
	return storage.LocalFind(
		append([]byte(node2NetmapPrefix), convert.Uint32ToBytesBE(uint32(version))...),
		storage.ValuesOnly|storage.DeserializeValues)
}

// ListNodesEpoch provides an iterator to walk over node set at the given epoch.
// It's the same as [ListNodes] (and exposed as listNodes from the contract via
// overload), but allows to query a particular epoch data if it's still stored.
// If this epoch is already expired (or not happened yet) returns an empty
// iterator.
//
// Deprecated: netmwork map based on epochs should not be used.
// use [ListNodes] or [ListNodesVersion].
func ListNodesEpoch(epoch int) iterator.Iterator {
	return storage.LocalFind(
		append([]byte(node2NetmapPrefix), convert.Uint32ToBytesBE(nmVersionForEpoch(epoch))...),
		storage.ValuesOnly|storage.DeserializeValues)
}

// ListCandidates returns an iterator for a set of current candidate nodes.
// Iterator values are [Candidate] structures.
func ListCandidates() iterator.Iterator {
	return storage.LocalFind(
		[]byte(node2CandidatePrefix),
		storage.ValuesOnly|storage.DeserializeValues)
}

// IsStorageNode allows to check for the given key presence in the current
// network map.
func IsStorageNode(key interop.PublicKey) bool {
	return getStorageNodeInEpoch(key, Epoch()) != nil
}

// IsStorageNodeInEpoch is the same as [IsStorageNode], but allows to do
// the check for previous epochs if they're still stored in the contract.
// If this epoch is no longer stored (or too new) it will return false.
func IsStorageNodeInEpoch(key interop.PublicKey, epoch int) bool {
	if epoch > Epoch() {
		return false
	}
	return getStorageNodeInEpoch(key, epoch) != nil
}

func nmVersionForEpoch(epoch int) uint32 {
	var (
		it          = storage.LocalFind([]byte{epochToNetmapVersionKey}, storage.RemovePrefix|storage.Backwards)
		verForEpoch int
	)
	for iterator.Next(it) {
		kv := iterator.Value(it).(storage.KeyValue)
		if convert.BytesBEToUint32(kv.Key) <= uint32(epoch) {
			return uint32(convert.ToInteger(kv.Value))
		}
	}

	return uint32(verForEpoch)
}

func getStorageNodeInEpoch(key interop.PublicKey, epoch int) []byte {
	dbkey := append(append([]byte(node2NetmapPrefix), convert.Uint32ToBytesBE(nmVersionForEpoch(epoch))...), key...)
	return storage.LocalGet(dbkey)
}

// IsStorageNodeStatus checks if the given status matches for the given
// node in the specified epoch.
func IsStorageNodeStatus(key interop.PublicKey, epoch int, status nodestate.Type) bool {
	if epoch > Epoch() {
		return false
	}
	var nodeRaw = getStorageNodeInEpoch(key, epoch)
	if nodeRaw == nil {
		return status == nodestate.Offline // Offline is missing node.
	}
	var n2 = std.Deserialize(nodeRaw).(Node2)
	return n2.State == status
}

func getSnapshotCount() int {
	return convert.ToInteger(storage.LocalGet([]byte(snapshotCountKey)))
}

// UpdateSnapshotCount updates the number of the stored snapshots.
// If a new number is less than the old one, old snapshots are removed.
// Otherwise, history is extended with empty snapshots, so
// `Snapshot` method can return invalid results for `diff = new-old` versions
// until `diff` versions have passed.
//
// Count MUST NOT be negative.
func UpdateSnapshotCount(count int) {
	common.CheckAlphabetWitness()
	if count < 0 {
		panic("count must be positive")
	}
	oldCount := getSnapshotCount()
	if oldCount == count {
		panic("count has not changed")
	}
	storage.LocalPut([]byte(snapshotCountKey), convert.ToBytes(count))
	dropNetmaps()
}

// Config returns configuration value of NeoFS configuration. If key does
// not exist, returns nil.
func Config(key []byte) []byte {
	return storage.LocalGet(append(configPrefix, key...))
}

// SetConfig key-value pair as a NeoFS runtime configuration value. It can be invoked
// only by Alphabet nodes.
func SetConfig(id, key, val []byte) {
	common.CheckAlphabetWitness()

	setConfig(key, val)

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
	var config []ConfigRecord

	it := storage.LocalFind(configPrefix, storage.RemovePrefix)
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

	it := storage.LocalFind([]byte(newEpochSubscribersPrefix), storage.KeysOnly)
	for iterator.Next(it) {
		k := iterator.Value(it).([]byte)
		contractHash := k[2:] // prefix and 1-byte index
		if contract.Equals(contractHash) {
			storage.LocalDelete(k)
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

	var num byte
	it := storage.LocalFind([]byte(newEpochSubscribersPrefix), storage.KeysOnly|storage.RemovePrefix)
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

	storage.LocalPut(key, []byte{})

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
	storage.LocalPut([]byte(cleanupThresholdKey), convert.ToBytes(val))
}

// CleanupThreshold returns the cleanup threshold configuration. Nodes that
// do not update their state for this number of epochs get kicked out of the
// network map. Zero means cleanups are disabled.
func CleanupThreshold() int {
	return convert.ToInteger(storage.LocalGet([]byte(cleanupThresholdKey)))
}

// UnusedCandidate does nothing except marking Candidate structure as used one
// thereby making RPC binding generator produce code for it. It is a temporary
// solution until we have proper iterator types. Never use it.
func UnusedCandidate() Candidate {
	return Candidate{}
}

// NetworkMapVersion return current network map version. Version monotonically
// increases with each change to the network map.
func NetworkMapVersion() int {
	return convert.ToInteger(storage.LocalGet([]byte{netmapVersionKey}))
}

// GetEpochBlock returns block index when given epoch came. Returns 0 if the
// epoch is missing. Do not confuse with [GetEpochTime].
//
// Use [LastEpochBlock] if you are interested in the current epoch.
func GetEpochBlock(epoch int) int {
	val := storage.LocalGet(append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(epoch))...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val).(epochItem).height
}

// GetEpochTime returns block time when given epoch came. Returns 0 if the epoch
// is missing. Do not confuse with [GetEpochBlock].
//
// Use [LastEpochTime] if you are interested in the current epoch.
func GetEpochTime(epoch int) int {
	val := storage.LocalGet(append([]byte{epochIndexKey}, convert.Uint32ToBytesBE(uint32(epoch))...))
	if val == nil {
		return 0
	}
	return std.Deserialize(val).(epochItem).time
}

// GetEpochBlockByTime returns the block index when the latest epoch that
// started not later than the provided block time came. Returns 0 if the time
// precedes any known epoch.
func GetEpochBlockByTime(ts int) int {
	minEpoch := 0
	maxEpoch := Epoch()

	var res int
	lo, hi := minEpoch, maxEpoch
	for lo <= hi {
		mid := (lo + hi) / 2
		t := GetEpochTime(mid)
		if t == 0 {
			lo = mid + 1
			continue
		}
		if t > ts {
			hi = mid - 1
			continue
		}
		res = GetEpochBlock(mid)
		lo = mid + 1
	}

	return res
}

func removeFromNetmap(key interop.PublicKey) {
	storageKey := append([]byte(node2CandidatePrefix), key...)
	storage.LocalDelete(storageKey)
}

func updateNetmapState(key interop.PublicKey, state nodestate.Type) {
	storageKey := append([]byte(node2CandidatePrefix), key...)
	raw := storage.LocalGet(storageKey)
	if raw == nil {
		panic("peer is missing")
	}
	cand := std.Deserialize(raw).(Candidate)
	if cand.State != state {
		setNetmapChangedFlag(true)
	}
	cand.State = state
	cand.LastActiveEpoch = Epoch()
	storage.LocalPut(storageKey, std.Serialize(cand))
}

func updateNetmap(epoch int) {
	var (
		netmapChanged    = netmapChangedFlag()
		cleanupThreshold = CleanupThreshold()
		it               = storage.LocalFind([]byte(node2CandidatePrefix), storage.RemovePrefix)
	)
	for iterator.Next(it) {
		kv := iterator.Value(it).(storage.KeyValue)

		cand := std.Deserialize(kv.Value).(Candidate)
		if cleanupThreshold > 0 && cand.LastActiveEpoch < epoch-cleanupThreshold {
			// Forget about stale nodes.
			updateCandidateState(interop.PublicKey(kv.Key), nodestate.Offline)
			netmapChanged = true
		}
	}

	if !netmapChanged {
		return
	}

	var (
		newVersion    = incNetmapVersion()
		versionPrefix = append([]byte(node2NetmapPrefix), convert.Uint32ToBytesBE(uint32(newVersion))...)
	)

	it = storage.LocalFind([]byte(node2CandidatePrefix), storage.RemovePrefix)
	for iterator.Next(it) {
		kv := iterator.Value(it).(storage.KeyValue)
		cand := std.Deserialize(kv.Value).(Candidate)
		var n2 = Node2{
			Addresses:  cand.Addresses,
			Attributes: cand.Attributes,
			Key:        cand.Key,
			State:      cand.State,
		}
		// Offline nodes are just deleted, so we can omit state check.
		storage.LocalPut(append(versionPrefix, kv.Key...), std.Serialize(n2))
	}

	storage.LocalPut(append([]byte{epochToNetmapVersionKey}, convert.Uint32ToBytesBE(uint32(epoch))...), convert.ToBytes(newVersion))
	setNetmapChangedFlag(false)
	runtime.Notify("NewNetmap", newVersion)
}

func incNetmapVersion() int {
	var (
		k      = []byte{netmapVersionKey}
		ver    int
		verRaw = storage.LocalGet(k)
	)
	if verRaw == nil {
		ver = 0
	} else {
		ver = convert.ToInteger(verRaw)
	}

	ver++
	storage.LocalPut(k, convert.ToBytes(ver))
	return ver
}

func dropNetmaps() {
	var latestValidVersion = NetworkMapVersion() - getSnapshotCount()
	if latestValidVersion <= 0 {
		return
	}
	var nmIt = storage.LocalFind([]byte(node2NetmapPrefix), storage.KeysOnly)
	for iterator.Next(nmIt) {
		ver := convert.BytesBEToUint32(iterator.Value(nmIt).([]byte)[1:5])
		if int(ver) >= latestValidVersion {
			return
		}

		storage.LocalDelete(iterator.Value(nmIt).([]byte))
	}
}

func setConfig(key, val []byte) {
	storageKey := append(configPrefix, key...)

	storage.LocalPut(storageKey, val)
}

func cleanup(epoch int) {
	it := storage.LocalFind([]byte(newEpochSubscribersPrefix), storage.RemovePrefix|storage.KeysOnly)
	for iterator.Next(it) {
		contractHash := interop.Hash160(iterator.Value(it).([]byte)[1:]) // one byte is for number prefix
		contract.Call(contractHash, cleanupEpochMethod, contract.All, epoch)
	}
}

// nolint:unused
func migrateToVersionedNetmap() {
	const (
		prevVersion = 1
		currVersion = 2
	)
	var (
		currEpoch = Epoch()
		nmIt      = storage.LocalFind([]byte(node2NetmapPrefix), storage.None)

		prevEpochToVersionKey = append([]byte{epochToNetmapVersionKey}, convert.Uint32ToBytesBE(uint32(currEpoch-1))...)
		currEpochToVersionKey = append([]byte{epochToNetmapVersionKey}, convert.Uint32ToBytesBE(uint32(currEpoch))...)
	)

	storage.LocalPut([]byte{netmapVersionKey}, convert.ToBytes(currVersion))
	storage.LocalPut(prevEpochToVersionKey, convert.ToBytes(prevVersion))
	storage.LocalPut(currEpochToVersionKey, convert.ToBytes(currVersion))
	storage.LocalPut([]byte{netmapChangedFlagKey}, []byte{0})

	for iterator.Next(nmIt) {
		var (
			kv     = iterator.Value(nmIt).(storage.KeyValue)
			e      = int(convert.BytesBEToUint32(kv.Key[1:5]))
			newKey = []byte(node2NetmapPrefix)
		)

		switch e {
		case currEpoch - 1:
			newKey = append(newKey, convert.Uint32ToBytesBE(prevVersion)...)
		case currEpoch:
			newKey = append(newKey, convert.Uint32ToBytesBE(currVersion)...)
		default:
			storage.LocalDelete(kv.Key)
			continue
		}

		newKey = append(newKey, kv.Key[1+4:]...)

		storage.LocalPut(newKey, kv.Value)
		storage.LocalDelete(kv.Key)
	}
}
