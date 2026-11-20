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
	recordPrefix             = 'r'
	entryPrefix              = 'e'
	ownerIndexPrefix         = 'o'
	expiryIndexPrefix        = 'u'
	ownerAllIndexMarker      = 0
	ownerCategoryIndexMarker = 1

	idSize = 16
	// 65,000 bytes leaves room for the invocation arguments in a normal
	// transaction, whose script itself is limited to 65,535 bytes.
	maxEntrySize = 65_000
	// Neo storage keys may not exceed 64 bytes. Entry keys include the
	// 16-byte access-box ID.
	maxEntryKeySize     = 47
	maxCategorySize     = 16
	maxEntriesPerBox    = 100
	maxTotalEntrySize   = 256 * 1024
	maxPageSize         = 100
	maxEntryDeletes     = 256
	maxLifetimeMillisec = 365 * 24 * 60 * 60 * 1000 // 1 year
	expiryIndexTimeSize = 8

	netmapContractKey  = "netmapContractHash"
	balanceContractKey = "balanceContractHash"
)

// Record describes an access box. Its entries are stored separately and are
// returned by [Get].
//
// CreatedAt and ExpiresAt are Unix timestamps in milliseconds.
type Record struct {
	ID         []byte
	Owner      interop.Hash160
	Category   string
	CreatedAt  int
	ExpiresAt  int
	EntryCount int
	TotalSize  int
}

// nolint:unused
func _deploy(data any, isUpdate bool) {
	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)

		common.CheckVersion(version)

		return
	}

	storage.LocalPut([]byte(netmapContractKey), common.ResolveFSContract("netmap"))
	storage.LocalPut([]byte(balanceContractKey), common.ResolveFSContract("balance"))

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

// Put creates an access box with opaque entries and returns its generated
// 16-byte ID. Owner must witness the invocation. Category identifies the
// caller-defined type of the box. Each key must be unique within the new box.
// Put initializes every key with the same value. ExpiresAt must be in the
// future and no more than one year from the current block time.
//
// When AccessBoxFee is non-zero, the owner's signer must also allow this
// contract to call Balance. Clients should use CalledByEntry with a rule for
// that nested call, CalledByEntry alone is not sufficient.
func Put(owner interop.Hash160, category string, expiresAt int, keys [][]byte, value []byte) []byte {
	now := runtime.GetTime()
	if owner == nil || len(owner) != interop.Hash160Len {
		panic("invalid owner")
	}
	common.CheckOwnerWitness(owner)
	checkCategory(category, false)
	totalSize := checkEntries(keys, value)
	if expiresAt <= now {
		panic("access box is already expired")
	}
	if expiresAt-now > maxLifetimeMillisec {
		panic("access box lifetime exceeds one year")
	}

	id := newID()
	chargeFee(owner)

	record := Record{
		ID:         copyBytes(id),
		Owner:      owner,
		Category:   category,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		EntryCount: len(keys),
		TotalSize:  totalSize,
	}

	putRecord(record)
	storage.LocalPut(expiryIndexKey(expiresAt, id), []byte{})
	for i := range keys {
		putEntry(id, keys[i], value)
	}
	runtime.Notify("AccessBoxCreated", id)

	return id
}

// Append adds an opaque entry to the access box. Owner must witness the
// invocation. It fails when an entry with key already exists.
func Append(id, key, value []byte) {
	checkID(id)
	record, ok := getRecord(id)
	if !ok {
		panic("access box not found")
	}
	common.CheckOwnerWitness(record.Owner)
	if isExpired(record) {
		panic("access box is expired")
	}
	checkEntry(key, value)

	entry := checkNewEntry(id, key)
	checkAppendCapacity(record, len(value))
	storage.LocalPut(entry, value)
	record.EntryCount++
	record.TotalSize += len(value)
	putRecord(record)
}

// Get returns the opaque entry stored under key in the access box. It returns
// nil if the ID or key does not exist.
func Get(id, key []byte) []byte {
	if id == nil || len(id) != idSize || key == nil || len(key) == 0 || len(key) > maxEntryKeySize {
		return nil
	}
	record, ok := getRecord(id)
	if !ok || isExpired(record) {
		return nil
	}

	return storage.LocalGet(entryKey(id, key))
}

// GetInfo returns public information about an access box. It returns nil if
// no record with ID exists.
func GetInfo(id []byte) *Record {
	if id == nil || len(id) != idSize {
		return nil
	}

	record, ok := getRecord(id)
	if !ok {
		return nil
	}
	return &Record{
		ID:         copyBytes(record.ID),
		Owner:      record.Owner,
		Category:   record.Category,
		CreatedAt:  record.CreatedAt,
		ExpiresAt:  record.ExpiresAt,
		EntryCount: record.EntryCount,
		TotalSize:  record.TotalSize,
	}
}

// List returns an iterator over records owned by owner. An empty category
// selects records in every category; otherwise it selects only the given
// category.
func List(owner interop.Hash160, category string) iterator.Iterator {
	if owner == nil || len(owner) != interop.Hash160Len {
		panic("invalid owner")
	}
	checkCategory(category, true)

	return storage.LocalFind(ownerIndexPrefixKey(owner, category),
		storage.ValuesOnly|storage.DeserializeValues)
}

// Revoke removes the access box with ID and all of its entries. It can be
// invoked by the record owner or by committee.
func Revoke(id []byte) {
	checkID(id)
	record, ok := getRecord(id)
	if !ok {
		panic("access box not found")
	}
	checkOwnerOrCommitteeWitness(record.Owner)
	removeRecord(record)
}

// Remove deletes one entry from the access box. It can be invoked by the
// record owner or by committee. Removing an entry does not revoke the access
// box itself.
func Remove(id, key []byte) {
	checkID(id)
	checkEntryKey(key)
	record, ok := getRecord(id)
	if !ok {
		panic("access box not found")
	}
	checkOwnerOrCommitteeWitness(record.Owner)

	entry := entryKey(id, key)
	value := storage.LocalGet(entry)
	if value == nil {
		panic("access box entry not found")
	}
	checkRecordStats(record)
	if record.EntryCount == 0 || len(value) > record.TotalSize {
		panic("invalid access box record")
	}
	storage.LocalDelete(entry)
	record.EntryCount--
	record.TotalSize -= len(value)
	putRecord(record)
}

// RevokeOwner removes at most limit access boxes owned by owner and having
// category. An empty category removes records in every category. It can be
// invoked by the owner or by committee. The total number of entries removed
// in one invocation is also bounded. It always starts from the first remaining
// record and returns the number of removed records.
func RevokeOwner(owner interop.Hash160, category string, limit int) int {
	if owner == nil || len(owner) != interop.Hash160Len {
		panic("invalid owner")
	}
	checkCategory(category, true)
	checkPageLimit(limit)
	checkOwnerOrCommitteeWitness(owner)

	// Delete after scanning so the iterator is never changed while it is used.
	records := []Record{}
	entryDeletes := 0
	it := List(owner, category)
	for len(records) < limit && iterator.Next(it) {
		record := iterator.Value(it).(Record)
		if !canDeleteRecord(record, entryDeletes) {
			break
		}
		records = append(records, record)
		entryDeletes += record.EntryCount
	}
	for i := range records {
		removeRecord(records[i])
	}

	return len(records)
}

// PruneExpired removes at most limit expired access boxes. Anyone may invoke
// it. It starts at the earliest expiry and stops at the first still-valid box.
// The total number of entries removed in one invocation is also bounded. It
// returns the number of removed boxes.
func PruneExpired(limit int) int {
	checkPageLimit(limit)

	it := storage.LocalFind([]byte{expiryIndexPrefix}, storage.KeysOnly)
	records := []Record{}
	entryDeletes := 0
	now := runtime.GetTime()
	for len(records) < limit && iterator.Next(it) {
		key := iterator.Value(it).([]byte) // it MUST BE storage.KeysOnly
		expiresAt, id := parseExpiryIndexKey(key)
		if expiresAt > now {
			break
		}

		record, ok := getRecord(id)
		if !ok {
			panic("expiry index points to missing access box")
		}
		if record.ExpiresAt != expiresAt {
			panic("expiry index points to wrong access box")
		}
		if !canDeleteRecord(record, entryDeletes) {
			break
		}
		records = append(records, record)
		entryDeletes += record.EntryCount
	}
	for i := range records {
		removeRecord(records[i])
	}

	return len(records)
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

func checkCategory(category string, allowEmpty bool) {
	if (category == "" && !allowEmpty) || len(category) > maxCategorySize {
		panic("invalid access box category")
	}
}

func checkEntries(keys [][]byte, value []byte) int {
	if len(keys) == 0 {
		panic("invalid access box entries")
	}
	if len(keys) > maxEntriesPerBox {
		panic("too many access box entries")
	}
	if len(value) == 0 {
		panic("invalid access box entry size")
	}
	if len(value) > maxEntrySize {
		panic("access box entry is too large")
	}

	totalSize := 0
	for i := range keys {
		checkEntryKey(keys[i])
		if len(value) > maxTotalEntrySize-totalSize {
			panic("access box is too large")
		}
		totalSize += len(value)
		for j := range i {
			if std.MemoryCompare(keys[i], keys[j]) == 0 {
				panic("duplicate access box entry key")
			}
		}
	}

	return totalSize
}

func checkEntry(key, value []byte) {
	checkEntryKey(key)
	if len(value) == 0 {
		panic("invalid access box entry size")
	}
	if len(value) > maxEntrySize {
		panic("access box entry is too large")
	}
}

func checkEntryKey(key []byte) {
	if len(key) == 0 || len(key) > maxEntryKeySize {
		panic("invalid access box entry key")
	}
}

func checkPageLimit(limit int) {
	if limit <= 0 || limit > maxPageSize {
		panic("invalid page limit")
	}
}

func checkAppendCapacity(record Record, valueSize int) {
	checkRecordStats(record)
	if record.EntryCount == maxEntriesPerBox {
		panic("too many access box entries")
	}
	if valueSize > maxTotalEntrySize-record.TotalSize {
		panic("access box is too large")
	}
}

func checkRecordStats(record Record) {
	if record.EntryCount < 0 || record.EntryCount > maxEntriesPerBox ||
		record.TotalSize < 0 || record.TotalSize > maxTotalEntrySize {
		panic("invalid access box record")
	}
}

func canDeleteRecord(record Record, entryDeletes int) bool {
	checkRecordStats(record)
	return record.EntryCount <= maxEntryDeletes-entryDeletes
}

func checkOwnerOrCommitteeWitness(owner interop.Hash160) {
	if runtime.CheckWitness(owner) || common.HasUpdateAccess() {
		return
	}
	panic(common.ErrOwnerWitnessFailed)
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

func putEntry(id, key, value []byte) {
	storage.LocalPut(checkNewEntry(id, key), value)
}

func checkNewEntry(id, key []byte) []byte {
	entry := entryKey(id, key)
	if storage.LocalGet(entry) != nil {
		panic("access box entry already exists")
	}
	return entry
}

func putRecord(record Record) {
	checkRecordStats(record)
	storage.LocalPut(recordKey(record.ID), std.Serialize(record))
	putOwnerIndex(record, "")
	putOwnerIndex(record, record.Category)
}

func removeRecord(record Record) {
	it := storage.LocalFind(entryPrefixKey(record.ID), storage.KeysOnly)
	entryKeys := [][]byte{}
	for iterator.Next(it) {
		if len(entryKeys) == maxEntriesPerBox {
			panic("access box has too many entries")
		}
		entryKeys = append(entryKeys, copyBytes(iterator.Value(it).([]byte)))
	}
	if len(entryKeys) != record.EntryCount {
		panic("invalid access box record")
	}
	for i := range entryKeys {
		storage.LocalDelete(entryKeys[i])
	}
	storage.LocalDelete(recordKey(record.ID))
	removeOwnerIndex(record.Owner, "", record.ID)
	removeOwnerIndex(record.Owner, record.Category, record.ID)
	storage.LocalDelete(expiryIndexKey(record.ExpiresAt, record.ID))
}

func recordKey(id []byte) []byte {
	return append([]byte{recordPrefix}, id...)
}

func entryPrefixKey(id []byte) []byte {
	return append([]byte{entryPrefix}, id...)
}

func entryKey(id, key []byte) []byte {
	return append(entryPrefixKey(id), key...)
}

func expiryIndexKey(expiresAt int, id []byte) []byte {
	key := append([]byte{expiryIndexPrefix}, convert.Uint64ToBytesBE(uint64(expiresAt))...)
	return append(key, id...)
}

func parseExpiryIndexKey(key []byte) (int, []byte) {
	if len(key) != 1+expiryIndexTimeSize+idSize || key[0] != expiryIndexPrefix {
		panic("invalid expiry index key")
	}

	expiresAt := int(convert.BytesBEToUint64(key[1 : 1+expiryIndexTimeSize]))
	return expiresAt, copyBytes(key[1+expiryIndexTimeSize:])
}

func ownerIndexPrefixKey(owner interop.Hash160, category string) []byte {
	key := append([]byte{ownerIndexPrefix}, owner...)
	if category == "" {
		return append(key, ownerAllIndexMarker)
	}

	key = append(key, ownerCategoryIndexMarker, byte(len(category)))
	return append(key, []byte(category)...)
}

func ownerIndexKey(owner interop.Hash160, category string, id []byte) []byte {
	return append(ownerIndexPrefixKey(owner, category), id...)
}

func putOwnerIndex(record Record, category string) {
	storage.LocalPut(ownerIndexKey(record.Owner, category, record.ID), std.Serialize(record))
}

func removeOwnerIndex(owner interop.Hash160, category string, id []byte) {
	key := ownerIndexKey(owner, category, id)
	if storage.LocalGet(key) == nil {
		panic("access box is missing from owner index")
	}
	storage.LocalDelete(key)
}

func copyBytes(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}
