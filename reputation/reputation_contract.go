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
	notaryDisabledKey     = "notary"
	reputationValuePrefix = 'r'
	reputationCountPrefix = 'c'
)

func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	args := data.(struct {
		notaryDisabled bool
	})

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, args.notaryDisabled)
	if args.notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("reputation contract notary disabled")
	}

	runtime.Log("reputation contract initialized")
}

// Update method updates contract source code and manifest. Can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("reputation contract updated")
}

// Put method saves DataAuditResult in contract storage. Can be invoked only by
// Inner Ring nodes. Does not require multi signature invocations.
//
// Epoch is an epoch number when DataAuditResult structure was generated.
// PeerID contains public keys of Inner Ring node that produced DataAuditResult.
// Value contains stable marshaled structure of DataAuditResult.
func Put(epoch int, peerID []byte, value []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet     []interop.PublicKey
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
		runtime.Notify("reputationPut", epoch, peerID, value)
		return
	}

	id := storageID(epoch, peerID)
	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

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

// Get method returns list of all stable marshaled DataAuditResult structures
// produced by specified Inner Ring node in specified epoch.
func Get(epoch int, peerID []byte) [][]byte {
	id := storageID(epoch, peerID)
	return GetByID(id)
}

// GetByID method returns list of all stable marshaled DataAuditResult with
// specified id. Use ListByEpoch method to obtain id.
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

// ListByEpoch returns list of IDs that may be used to get reputation data
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

// Version returns version of the contract.
func Version() int {
	return common.Version
}

func storageID(epoch int, peerID []byte) []byte {
	var buf interface{} = epoch

	return append(buf.([]byte), peerID...)
}
