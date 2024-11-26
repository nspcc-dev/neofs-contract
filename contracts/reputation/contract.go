package reputation

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	reputationValuePrefix = 'r'
	reputationCountPrefix = 'c'
)

// nolint:deadcode,unused
func _deploy(data any, isUpdate bool) {
	ctx := storage.GetContext()

	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)

		common.CheckVersion(version)

		// switch to notary mode if version of the current contract deployment is
		// earlier than v0.17.0 (initial version when non-notary mode was taken out of
		// use)
		// TODO: avoid number magic, add function for version comparison to common package
		if version < 17_000 {
			switchToNotary(ctx)
		}

		return
	}

	runtime.Log("reputation contract initialized")
}

// re-initializes contract from non-notary to notary mode. Does nothing if
// action has already been done. The function is called on contract update with
// storage.Context from _deploy.
//
// If contract stores non-empty value by 'ballots' key, switchToNotary panics.
// Otherwise, existing value is removed.
//
// switchToNotary removes value stored by 'notary' key.
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
	runtime.Log("reputation contract updated")
}

// Put method saves global trust data in contract storage. It can be invoked only by
// storage nodes with Alphabet assistance (multisignature witness).
//
// Epoch is the epoch number when GlobalTrust structure was generated.
// PeerID contains public key of the storage node that is the subject of the GlobalTrust.
// Value contains a stable marshaled structure of GlobalTrust.
func Put(epoch int, peerID []byte, value []byte) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	id := storageID(epoch, peerID)

	key := getReputationKey(reputationCountPrefix, id)
	rawCnt := storage.Get(ctx, key)
	cnt := 0
	if rawCnt != nil {
		cnt = rawCnt.(int)
	}
	cnt++
	storage.Put(ctx, key, cnt)

	key[0] = reputationValuePrefix
	key = append(key, convert.ToBytes(cnt)...)
	storage.Put(ctx, key, value)
}

// Get method returns a list of all stable marshaled GlobalTrust structures
// known for the given peer during the specified epoch.
func Get(epoch int, peerID []byte) [][]byte {
	id := storageID(epoch, peerID)
	return GetByID(id)
}

// GetByID method returns a list of all stable marshaled GlobalTrust with
// the specified id. Use ListByEpoch method to obtain the id.
func GetByID(id []byte) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var data [][]byte

	it := storage.Find(ctx, getReputationKey(reputationValuePrefix, id), storage.ValuesOnly)
	for iterator.Next(it) {
		data = append(data, iterator.Value(it).([]byte))
	}
	return data
}

func getReputationKey(prefix byte, id []byte) []byte {
	return append([]byte{prefix}, id...)
}

// ListByEpoch returns a list of IDs that may be used to get reputation data
// with GetByID method.
func ListByEpoch(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()
	key := getReputationKey(reputationCountPrefix, convert.ToBytes(epoch))
	it := storage.Find(ctx, key, storage.KeysOnly)

	var result [][]byte

	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // iterator MUST BE `storage.KeysOnly`
		result = append(result, key[1:])
	}

	return result
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

func storageID(epoch int, peerID []byte) []byte {
	var buf any = epoch

	return append(buf.([]byte), peerID...)
}
