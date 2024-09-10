package netmap

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
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

// Node groups data related to NeoFS storage nodes registered in the NeoFS
// network. The information is stored in the current contract.
type Node struct {
	// Information about the node encoded according to the NeoFS binary
	// protocol.
	BLOB []byte

	// Current node state.
	State nodestate.Type
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

		// switch to notary mode if version of the current contract deployment is
		// earlier than v0.17.0 (initial version when non-notary mode was taken out of
		// use)
		// TODO: avoid number magic, add function for version comparison to common package
		if version < 17_000 {
			switchToNotary(ctx)
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
	storage.Put(ctx, snapshotBlockKey, 0)

	prefix := []byte(snapshotKeyPrefix)
	for i := 0; i < DefaultSnapshotCount; i++ { //nolint:intrange // Not supported by NeoGo
		common.SetSerialized(ctx, append(prefix, byte(i)), []Node{})
	}
	storage.Put(ctx, snapshotCurrentIDKey, 0)

	runtime.Log("netmap contract initialized")
}

// re-initializes contract from non-notary to notary mode. Does nothing if
// action has already been done. The function is called on contract update with
// storage.Context from _deploy.
//
// If contract stores non-empty value by 'ballots' key, switchToNotary panics.
// Otherwise, existing value is removed.
//
// switchToNotary removes values stored by 'innerring' and 'notary' keys.
//
// nolint:unused
func switchToNotary(ctx storage.Context) {
	const notaryDisabledKey = "notary" // non-notary legacy

	notaryVal := storage.Get(ctx, notaryDisabledKey)
	if notaryVal == nil {
		runtime.Log("contract is already notarized")
		return
	} else if notaryVal.(bool) && !common.TryPurgeVotes(ctx) {
		panic("pending vote detected")
	}

	storage.Delete(ctx, notaryDisabledKey)
	storage.Delete(ctx, "innerring")

	if notaryVal.(bool) {
		runtime.Log("contract successfully notarized")
	}
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
// contract of the sidechain.
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
func AddPeerIR(nodeInfo []byte) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness(common.AlphabetAddress())

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
// Note that if the Alphabet needs to complete information about the candidate,
// it will be added with AddPeerIR.
func AddPeer(nodeInfo []byte) {
	ctx := storage.GetContext()

	publicKey := nodeInfo[nodeKeyOffset:nodeKeyEndOffset]

	common.CheckWitness(publicKey)

	// TODO: is it good approach? We could always call AddPeerIR by the Alphabet,
	//  but for unchanged candidates it would require new transaction, which seems
	//  rather redundant. At the same time, doing this check every time here will
	//  sometimes waste more GAS. Maybe we can somehow cheaply precede the check by
	//  determining the presence of signatures (for example, is there a second
	//  signature).
	if runtime.CheckWitness(common.AlphabetAddress()) {
		addToNetmap(ctx, publicKey, Node{
			BLOB:  nodeInfo,
			State: nodestate.Online,
		})
	}
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

	// TODO: see same place in AddPeer
	if runtime.CheckWitness(common.AlphabetAddress()) {
		updateCandidateState(ctx, publicKey, state)
	}
}

// UpdateStateIR is called by the NeoFS Alphabet instead of UpdateState when
// signature of the network candidate is inaccessible. In such cases, a new
// transaction will be required and therefore the candidate's signature is not
// verified by UpdateStateIR. Besides this, the behavior is similar.
func UpdateStateIR(state nodestate.Type, publicKey interop.PublicKey) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness(common.AlphabetAddress())

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

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)
	if epochNum <= currentEpoch {
		panic("invalid epoch") // ignore invocations with invalid epoch
	}

	dataOnlineState := filterNetmap(ctx)

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

// Epoch method returns the current epoch number.
func Epoch() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, snapshotEpoch).(int)
}

// LastEpochBlock method returns the block number when the current epoch was applied.
func LastEpochBlock() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, snapshotBlockKey).(int)
}

// Netmap returns set of information about the storage nodes representing a network
// map in the current epoch.
//
// Current state of each node is represented in the State field. It MAY differ
// with the state encoded into BLOB field, in this case binary encoded state
// MUST NOT be processed.
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
func NetmapCandidates() []Node {
	ctx := storage.GetReadOnlyContext()
	return getNetmapNodes(ctx)
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

// SnapshotByEpoch returns set of information about the storage nodes representing
// a network map in the given epoch.
//
// Behaves like Snapshot: it is called after difference with the current epoch is
// calculated.
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

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	setConfig(ctx, key, val)

	runtime.Log("configuration has been updated")
}

type record struct {
	key []byte
	val []byte
}

// ListConfig returns an array of structures that contain key and value of all
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

// SubscribeForNewEpoch registers passed contract as a NewEpoch event
// subscriber. Such a contract must have a `NewEpoch` method with a single
// numeric parameter. Transactions that call SubscribeForNewEpoch must be
// witnessed by the Alphabet.
// Produces `NewEpochSubscription` notification event with a just
// registered recipient in a success case.
func SubscribeForNewEpoch(contract interop.Hash160) {
	common.CheckAlphabetWitness(common.AlphabetAddress())

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
}

func updateNetmapState(ctx storage.Context, key interop.PublicKey, state nodestate.Type) {
	storageKey := append(candidatePrefix, key...)
	raw := storage.Get(ctx, storageKey).([]byte)
	if raw == nil {
		panic("peer is missing")
	}
	node := std.Deserialize(raw).(Node)
	node.State = state
	storage.Put(ctx, storageKey, std.Serialize(node))
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
