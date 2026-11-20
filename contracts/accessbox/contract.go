package accessbox

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/accessbox/accessboxconst"
)

const (
	// KindAny selects records of every kind when listing records.
	KindAny = iota
	// KindS3 identifies an S3 access box.
	KindS3
	// KindSession identifies a Console login-session access box.
	KindSession
	// KindPAT identifies a Console personal-access-token access box.
	KindPAT
)

const (
	payloadPrefix     = 'p'
	recordPrefix      = 'r'
	ownerIndexPrefix  = 'o'
	revokerPrefix     = 'v'
	revokerCountKey   = 'V'
	expiryIndexPrefix = 'u'

	idSize              = 16
	maxPayloadSize      = 65_535
	maxMetadataSize     = 4 * 1024
	maxNamespaceSize    = 255
	maxPageSize         = 100
	maxRevokers         = 100
	maxLifetimeMillisec = 365 * 24 * 60 * 60 * 1000 // 1 year

	netmapContractKey  = "netmapContractHash"
	balanceContractKey = "balanceContractHash"
)

// Record describes public metadata of an access box. Payload is stored
// separately and returned by [Get].
//
// ExpiresAt and CreatedAt are Unix timestamps in milliseconds.
// Metadata is consumer-defined public data. The contract does not interpret it.
type Record struct {
	ID        []byte
	Owner     interop.Hash160
	Namespace string
	Kind      int
	CreatedAt int
	ExpiresAt int
	Metadata  []byte
}

// nolint:unused
func _deploy(data any, isUpdate bool) {
	args := data.([]any)
	if isUpdate {
		version := args[len(args)-1].(int)

		common.CheckVersion(version)

		return
	}

	var netmap, balance interop.Hash160
	if len(args) > 0 && len(args[0].(interop.Hash160)) == interop.Hash160Len {
		netmap = args[0].(interop.Hash160)
	} else {
		netmap = common.ResolveFSContract("netmap")
	}
	if len(args) > 1 && len(args[1].(interop.Hash160)) == interop.Hash160Len {
		balance = args[1].(interop.Hash160)
	} else {
		balance = common.ResolveFSContract("balance")
	}

	storage.LocalPut([]byte(netmapContractKey), netmap)
	storage.LocalPut([]byte(balanceContractKey), balance)

	runtime.Log("access box contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(nefFile, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nefFile, manifest, common.AppendVersion(data))
	runtime.Log("access box contract updated")
}

// Put creates an access box, stores its opaque payload and public metadata, and
// returns the generated 16-byte ID. Owner must witness the invocation.
// ExpiresAt must be in the future and no more than one year from the current
// block time. When AccessBoxFee is non-zero, the owner's signer must also
// allow this contract to call Balance. Clients should use CalledByEntry with
// a rule for that nested call; CalledByEntry alone is not sufficient.
func Put(owner interop.Hash160, kind int, namespace string, expiresAt int, metadata, payload []byte) []byte {
	now := runtime.GetTime()
	if owner == nil || len(owner) != interop.Hash160Len {
		panic("invalid owner")
	}
	common.CheckOwnerWitness(owner)
	checkKind(kind, false)
	if len(namespace) > maxNamespaceSize {
		panic("namespace is too long")
	}
	if metadata == nil {
		metadata = []byte{}
	}
	if len(metadata) > maxMetadataSize {
		panic("metadata is too large")
	}
	if len(payload) == 0 || len(payload) > maxPayloadSize {
		panic("invalid payload size")
	}

	if expiresAt <= now {
		panic("access box is already expired")
	}
	if expiresAt-now > maxLifetimeMillisec {
		panic("access box lifetime exceeds one year")
	}
	id := newID()
	chargeFee(owner)

	record := Record{
		ID:        copyBytes(id),
		Owner:     owner,
		Namespace: namespace,
		Kind:      kind,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		Metadata:  copyBytes(metadata),
	}

	storage.LocalPut(payloadKey(id), payload)
	storage.LocalPut(recordKey(id), std.Serialize(record))
	storage.LocalPut(ownerIndexKey(owner, KindAny, id), id)
	storage.LocalPut(ownerIndexKey(owner, kind, id), id)
	storage.LocalPut(expiryIndexKey(id), convert.ToBytes(expiresAt))

	return id
}

// Get returns the original opaque access-box payload. It returns nil if the
// record does not exist or has expired.
func Get(id []byte) []byte {
	if id == nil || len(id) != idSize {
		return nil
	}

	record, ok := getRecord(id)
	if !ok || isExpired(record) {
		return nil
	}

	return storage.LocalGet(payloadKey(id))
}

// GetInfo returns public metadata of an access box. It returns an empty record
// if no record with ID exists. Metadata remains available after expiry until
// the record is revoked or pruned.
func GetInfo(id []byte) Record {
	if id == nil || len(id) != idSize {
		return Record{}
	}

	record, _ := getRecord(id)
	return record
}

// List returns at most limit records owned by owner and having kind. KindAny
// lists records of every kind. Cursor is the ID of the final record from the
// previous page; an empty cursor starts from the beginning.
func List(owner interop.Hash160, kind int, cursor []byte, limit int) []Record {
	if owner == nil || len(owner) != interop.Hash160Len {
		panic("invalid owner")
	}
	if cursor == nil {
		cursor = []byte{}
	}
	checkKind(kind, true)
	checkCursor(cursor)
	checkPageLimit(limit)

	prefix := ownerIndexPrefixKey(owner, kind)
	it := storage.LocalFind(prefix, storage.KeysOnly)
	var records []Record

	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // it MUST BE storage.KeysOnly
		id := key[len(prefix):]
		if len(cursor) != 0 && std.MemoryCompare(id, cursor) <= 0 {
			continue
		}

		record, ok := getRecord(id)
		if !ok {
			panic("owner index points to missing access box")
		}

		records = append(records, record)
		if len(records) == limit {
			break
		}
	}

	return records
}

// Revoke removes the access box with ID. It can be invoked by the record owner
// or by a registered revoker.
func Revoke(id []byte) {
	checkID(id)
	record, ok := getRecord(id)
	if !ok {
		panic("access box not found")
	}
	checkRevokeWitness(record.Owner)
	removeRecord(record)
}

// RevokeOwner removes one page of records owned by owner and having kind.
// KindAny removes records of every kind. It can be invoked by the owner or by
// a registered revoker. It returns the ID of the final removed record when
// another page may remain, or nil otherwise.
func RevokeOwner(owner interop.Hash160, kind int, cursor []byte, limit int) []byte {
	if owner == nil || len(owner) != interop.Hash160Len {
		panic("invalid owner")
	}
	if cursor == nil {
		cursor = []byte{}
	}
	checkKind(kind, true)
	checkCursor(cursor)
	checkPageLimit(limit)
	checkRevokeWitness(owner)

	prefix := ownerIndexPrefixKey(owner, kind)
	it := storage.LocalFind(prefix, storage.KeysOnly)
	var (
		last    []byte
		removed int
	)

	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // it MUST BE storage.KeysOnly
		id := key[len(prefix):]
		if len(cursor) != 0 && std.MemoryCompare(id, cursor) <= 0 {
			continue
		}
		if removed == limit {
			return last
		}

		record, ok := getRecord(id)
		if !ok {
			panic("owner index points to missing access box")
		}

		last = copyBytes(id)
		removeRecord(record)
		removed++
	}

	return nil
}

// PruneExpired scans one page of records and removes the expired ones. Anyone
// may invoke it. It returns the final scanned ID when another page may remain,
// or nil otherwise.
func PruneExpired(cursor []byte, limit int) []byte {
	if cursor == nil {
		cursor = []byte{}
	}
	checkCursor(cursor)
	checkPageLimit(limit)

	it := storage.LocalFind([]byte{expiryIndexPrefix}, storage.KeysOnly)
	var (
		last    []byte
		scanned int
	)

	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // it MUST BE storage.KeysOnly
		id := key[1:]
		if len(cursor) != 0 && std.MemoryCompare(id, cursor) <= 0 {
			continue
		}
		if scanned == limit {
			return last
		}

		last = copyBytes(id)
		scanned++
		if convert.ToInteger(storage.LocalGet(key)) <= runtime.GetTime() {
			record, ok := getRecord(id)
			if !ok {
				panic("expiry index points to missing access box")
			}
			removeRecord(record)
		}
	}

	return nil
}

// AddRevoker adds account to the set of accounts that may revoke any access
// box. It can be invoked only by committee. Adding an already registered
// account does nothing.
func AddRevoker(account interop.Hash160) {
	if !common.HasUpdateAccess() {
		panic("only committee can add revoker")
	}
	checkRevokerAccount(account)

	key := revokerKey(account)
	if storage.LocalGet(key) != nil {
		return
	}

	count := revokerCount()
	if count >= maxRevokers {
		panic("too many access box revokers")
	}

	storage.LocalPut(key, []byte{1})
	storage.LocalPut([]byte{revokerCountKey}, convert.ToBytes(count+1))
}

// RemoveRevoker removes account from the set of accounts that may revoke any
// access box. It can be invoked only by committee. Removing an account that is
// not registered does nothing.
func RemoveRevoker(account interop.Hash160) {
	if !common.HasUpdateAccess() {
		panic("only committee can remove revoker")
	}
	checkRevokerAccount(account)

	key := revokerKey(account)
	if storage.LocalGet(key) == nil {
		return
	}

	storage.LocalDelete(key)
	count := revokerCount()
	if count <= 1 {
		storage.LocalDelete([]byte{revokerCountKey})
		return
	}
	storage.LocalPut([]byte{revokerCountKey}, convert.ToBytes(count-1))
}

// IsRevoker reports whether account is registered as an access-box revoker.
// It returns false for an invalid account.
func IsRevoker(account interop.Hash160) bool {
	return account != nil && len(account) == interop.Hash160Len && storage.LocalGet(revokerKey(account)) != nil
}

// ListRevokers returns an iterator over the accounts registered as access-box
// revokers.
func ListRevokers() iterator.Iterator {
	return storage.LocalFind([]byte{revokerPrefix}, storage.KeysOnly|storage.RemovePrefix)
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

func checkID(id []byte) {
	if id == nil || len(id) != idSize {
		panic("invalid access box id")
	}
}

func newID() []byte {
	// The transaction hash and invocation counter make an ID unique even if the
	// same transaction calls Put more than once.
	seed := copyBytes(runtime.GetScriptContainer().Hash)
	seed = append(seed, convert.ToBytes(runtime.GetInvocationCounter())...)
	return copyBytes(crypto.Sha256(seed)[:idSize])
}

func chargeFee(owner interop.Hash160) {
	netmap := interop.Hash160(storage.LocalGet([]byte(netmapContractKey)))
	feeRaw := contract.Call(netmap, "config", contract.ReadOnly, []byte(accessboxconst.FeeConfigKey)).([]byte)
	if feeRaw == nil {
		return
	}

	fee := convert.ToInteger(feeRaw)
	if fee == 0 {
		return
	}
	if fee < 0 {
		panic("invalid access box fee")
	}

	alphabet := common.AlphabetNodes()
	total := fee * len(alphabet)
	balance := interop.Hash160(storage.LocalGet([]byte(balanceContractKey)))
	ownerBalance := contract.Call(balance, "balanceOf", contract.ReadOnly, owner).(int)
	if ownerBalance < total {
		panic("insufficient balance to create access box")
	}

	for i := range alphabet {
		transferred := contract.Call(balance, "transfer", contract.All,
			owner, contract.CreateStandardAccount(alphabet[i]), fee, []byte{}).(bool)
		if !transferred {
			panic("failed to transfer access box fee")
		}
	}
}

func checkCursor(cursor []byte) {
	if len(cursor) != 0 && len(cursor) != idSize {
		panic("invalid cursor")
	}
}

func checkKind(kind int, allowAny bool) {
	if allowAny && kind == KindAny {
		return
	}
	if kind < KindS3 || kind > KindPAT {
		panic("invalid access box kind")
	}
}

func checkPageLimit(limit int) {
	if limit <= 0 || limit > maxPageSize {
		panic("invalid page limit")
	}
}

func checkRevokeWitness(owner interop.Hash160) {
	if runtime.CheckWitness(owner) {
		return
	}

	it := ListRevokers()
	for iterator.Next(it) {
		if runtime.CheckWitness(iterator.Value(it).([]byte)) {
			return
		}
	}

	panic(common.ErrOwnerWitnessFailed)
}

func checkRevokerAccount(account interop.Hash160) {
	if account == nil || len(account) != interop.Hash160Len {
		panic("invalid revoker")
	}
}

func revokerCount() int {
	count := storage.LocalGet([]byte{revokerCountKey})
	if count == nil {
		return 0
	}
	return convert.ToInteger(count)
}

func getRecord(id []byte) (Record, bool) {
	raw := storage.LocalGet(recordKey(id))
	if raw == nil {
		return Record{}, false
	}

	return std.Deserialize(raw).(Record), true
}

func isExpired(record Record) bool {
	return record.ExpiresAt <= runtime.GetTime()
}

func removeRecord(record Record) {
	storage.LocalDelete(payloadKey(record.ID))
	storage.LocalDelete(recordKey(record.ID))
	storage.LocalDelete(ownerIndexKey(record.Owner, KindAny, record.ID))
	storage.LocalDelete(ownerIndexKey(record.Owner, record.Kind, record.ID))
	storage.LocalDelete(expiryIndexKey(record.ID))
}

func payloadKey(id []byte) []byte {
	return append([]byte{payloadPrefix}, id...)
}

func recordKey(id []byte) []byte {
	return append([]byte{recordPrefix}, id...)
}

func expiryIndexKey(id []byte) []byte {
	return append([]byte{expiryIndexPrefix}, id...)
}

func ownerIndexPrefixKey(owner interop.Hash160, kind int) []byte {
	key := append([]byte{ownerIndexPrefix}, owner...)
	return append(key, byte(kind))
}

func ownerIndexKey(owner interop.Hash160, kind int, id []byte) []byte {
	return append(ownerIndexPrefixKey(owner, kind), id...)
}

func revokerKey(account interop.Hash160) []byte {
	return append([]byte{revokerPrefix}, account...)
}

func copyBytes(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}
