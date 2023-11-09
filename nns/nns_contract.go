/*
Package nns contains non-divisible non-fungible NEP11-compatible token
implementation. This token is a compatible analogue of C# Neo Name Service
token and is aimed to serve as a domain name service for Neo smart-contracts,
thus it's NeoNameService. This token can be minted with new domain name
registration, the domain name itself is your NFT. Corresponding domain root
must be added by committee before a new domain name can be registered.
*/
package nns

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
	"github.com/nspcc-dev/neofs-contract/common"
)

// Prefixes used for contract data storage.
const (
	// prefixTotalSupply contains total supply of minted domains.
	prefixTotalSupply byte = 0x00
	// prefixBalance contains map from the owner to their balance.
	prefixBalance byte = 0x01
	// prefixAccountToken contains map from (owner + token key) to token ID,
	// where token key = hash160(token ID) and token ID = domain name.
	prefixAccountToken byte = 0x02
	// prefixRegisterPrice contains price for new domain name registration.
	prefixRegisterPrice byte = 0x10
	// prefixRoot contains set of roots (map from root to 0).
	prefixRoot byte = 0x20
	// prefixName contains map from token key to token where token is domain
	// NameState structure.
	prefixName byte = 0x21
	// prefixRecord contains map from (token key + hash160(token name) + record type)
	// to record.
	prefixRecord byte = 0x22
)

// Values constraints.
const (
	// maxRegisterPrice is the maximum price of register method.
	maxRegisterPrice = int64(1_0000_0000_0000)
	// maxRootLength is the maximum domain root length.
	maxRootLength = 16
	// maxDomainNameFragmentLength is the maximum length of the domain name fragment.
	maxDomainNameFragmentLength = 63
	// minDomainNameLength is minimum domain length.
	minDomainNameLength = 3
	// maxDomainNameLength is maximum domain length.
	maxDomainNameLength = 255
	// maxTXTRecordLength is the maximum length of the TXT domain record.
	maxTXTRecordLength = 255
)

// Other constants.
const (
	// defaultRegisterPrice is the default price for new domain registration.
	// nolint:unused
	defaultRegisterPrice = 10_0000_0000
	// millisecondsInYear is amount of milliseconds per year.
	millisecondsInYear = int64(365 * 24 * 3600 * 1000)
)

// RecordState is a type that registered entities are saved to.
type RecordState struct {
	Name string
	Type RecordType
	Data string
	ID   byte
}

// nsIteratorValue is used for upgrade from 0.17.0 to 0.18.0.
// nolint:deadcode,unused
type nsIteratorValue struct {
	k []byte
	v NameState
}

// Update updates NameService contract.
func Update(nef []byte, manifest string, data interface{}) {
	checkCommittee()
	// Calculating keys and serializing requires calling
	// std and crypto contracts. This can be helpful on update
	// thus we provide `AllowCall` to management.Update.
	// management.Update(nef, []byte(manifest))
	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nef, manifest, common.AppendVersion(data))
	runtime.Log("nns contract updated")
}

// _deploy initializes defaults (total supply and registration price) on contract deploy.
// nolint:deadcode,unused
func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		args := data.([]interface{})
		version := args[len(args)-1].(int)
		common.CheckVersion(version)

		if version >= 18_000 {
			return
		}

		// From 0.17.0 to 0.18.0.
		// Upgrade TLD owners to nil value, they're committee-owned.
		var (
			ctx  = storage.GetContext()
			iter = storage.Find(ctx, []byte{prefixName}, storage.DeserializeValues)
			rec  nsIteratorValue
			ser  []byte
		)
		for iterator.Next(iter) {
			rec = iterator.Value(iter).(nsIteratorValue)
			if std.MemorySearch([]byte(rec.v.Name), []byte{'.'}) != -1 { // Non-TLD
				continue
			}
			updateBalance(ctx, []byte(rec.v.Name), rec.v.Owner, -1)
			rec.v.Owner = nil
			ser = std.Serialize(rec.v)
			storage.Put(ctx, rec.k, ser)
		}

		return
	}

	ctx := storage.GetContext()
	storage.Put(ctx, []byte{prefixTotalSupply}, 0)
	storage.Put(ctx, []byte{prefixRegisterPrice}, defaultRegisterPrice)

	if data != nil { // for backward compatibility
		args := data.(struct {
			tldSet []struct {
				name  string
				email string
			}
		})

		for i := range args.tldSet {
			const (
				refresh = 3600
				retry   = 600
				expire  = 10 * 365 * 24 * 60 * 60 // 10 years
				ttl     = 3600
			)
			saveCommitteeDomain(ctx, args.tldSet[i].name, args.tldSet[i].email, refresh, retry, expire, ttl)
			runtime.Log("registered committee domain " + args.tldSet[i].name)
		}
	}
}

// Symbol returns NeoNameService symbol.
func Symbol() string {
	return "NNS"
}

// Decimals returns NeoNameService decimals.
func Decimals() int {
	return 0
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

// TotalSupply returns the overall number of domains minted by NeoNameService contract.
func TotalSupply() int {
	ctx := storage.GetReadOnlyContext()
	return getTotalSupply(ctx)
}

// OwnerOf returns the owner of the specified domain. The tokenID domain MUST
// NOT be a TLD.
func OwnerOf(tokenID []byte) interop.Hash160 {
	fragments := std.StringSplit(string(tokenID), ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	ctx := storage.GetReadOnlyContext()
	ns := getFragmentedNameState(ctx, tokenID, fragments)
	return ns.Owner
}

// Properties returns a domain name and an expiration date of the specified domain.
// The tokenID MUST NOT be a TLD.
func Properties(tokenID []byte) map[string]interface{} {
	fragments := std.StringSplit(string(tokenID), ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	ctx := storage.GetReadOnlyContext()
	ns := getFragmentedNameState(ctx, tokenID, fragments)
	return map[string]interface{}{
		"name":       ns.Name,
		"expiration": ns.Expiration,
	}
}

// BalanceOf returns the overall number of domains owned by the specified owner.
func BalanceOf(owner interop.Hash160) int {
	if !isValid(owner) {
		panic(`invalid owner`)
	}
	ctx := storage.GetReadOnlyContext()
	balance := storage.Get(ctx, append([]byte{prefixBalance}, owner...))
	if balance == nil {
		return 0
	}
	return balance.(int)
}

// Tokens returns iterator over a set of all registered domain names.
func Tokens() iterator.Iterator {
	ctx := storage.GetReadOnlyContext()
	return storage.Find(ctx, []byte{prefixName}, storage.ValuesOnly|storage.DeserializeValues|storage.PickField1)
}

// TokensOf returns iterator over minted domains owned by the specified owner.
func TokensOf(owner interop.Hash160) iterator.Iterator {
	if !isValid(owner) {
		panic(`invalid owner`)
	}
	ctx := storage.GetReadOnlyContext()
	return storage.Find(ctx, append([]byte{prefixAccountToken}, owner...), storage.ValuesOnly)
}

// Transfer transfers the domain with the specified name to a new owner.
// The tokenID MUST NOT be a TLD.
func Transfer(to interop.Hash160, tokenID []byte, data interface{}) bool {
	if !isValid(to) {
		panic(`invalid receiver`)
	}

	fragments := std.StringSplit(string(tokenID), ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	var (
		tokenKey = getTokenKey(tokenID)
		ctx      = storage.GetContext()
	)
	ns := getNameStateWithKey(ctx, tokenKey)
	from := ns.Owner
	if !runtime.CheckWitness(from) {
		return false
	}
	if !util.Equals(from, to) {
		// update token info
		ns.Owner = to
		ns.Admin = nil
		putNameStateWithKey(ctx, tokenKey, ns)

		// update `from` balance
		updateBalance(ctx, tokenID, from, -1)

		// update `to` balance
		updateBalance(ctx, tokenID, to, +1)
	}
	postTransfer(from, to, tokenID, data)
	return true
}

// Roots returns iterator over a set of NameService roots.
func Roots() iterator.Iterator {
	ctx := storage.GetReadOnlyContext()
	return storage.Find(ctx, []byte{prefixRoot}, storage.KeysOnly|storage.RemovePrefix)
}

// SetPrice sets the domain registration price.
func SetPrice(price int64) {
	checkCommittee()
	if price < 0 || price > maxRegisterPrice {
		panic("The price is out of range.")
	}
	ctx := storage.GetContext()
	storage.Put(ctx, []byte{prefixRegisterPrice}, price)
}

// GetPrice returns the domain registration price.
func GetPrice() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, []byte{prefixRegisterPrice}).(int)
}

// IsAvailable checks whether the provided domain name is available. Notice that
// TLD is available for the committee only.
func IsAvailable(name string) bool {
	fragments := splitAndCheck(name, false)
	if fragments == nil {
		panic("invalid domain name format")
	}
	ctx := storage.GetReadOnlyContext()
	l := len(fragments)
	if storage.Get(ctx, append([]byte{prefixRoot}, []byte(fragments[l-1])...)) == nil {
		if l != 1 {
			panic("TLD not found")
		}
		return true
	}
	return parentExpired(ctx, 0, fragments)
}

// parentExpired returns true if any domain from fragments doesn't exist or is expired.
// first denotes the deepest subdomain to check.
func parentExpired(ctx storage.Context, first int, fragments []string) bool {
	now := int64(runtime.GetTime())
	last := len(fragments) - 1
	name := fragments[last]
	for i := last; i >= first; i-- {
		if i != last {
			name = fragments[i] + "." + name
		}
		nsBytes := storage.Get(ctx, append([]byte{prefixName}, getTokenKey([]byte(name))...))
		if nsBytes == nil {
			return true
		}
		ns := std.Deserialize(nsBytes.([]byte)).(NameState)
		if now >= ns.Expiration {
			return true
		}
	}
	return false
}

// Register registers a new domain with the specified owner and name if it's
// available. Top-level domains MUST NOT be registered via Register, use
// RegisterTLD for this.
//
// Access rules::
//   - 2nd-level domain can be registered by anyone
//   - starting from the 3rd level, the domain can only be registered by the
//     owner or administrator (if any) of the previous level domain
func Register(name string, owner interop.Hash160, email string, refresh, retry, expire, ttl int) bool {
	fragments := splitAndCheck(name, true)
	if fragments == nil {
		panic("invalid domain name format")
	}

	l := len(fragments)
	if l == 1 {
		panic("TLD denied")
	}

	ctx := storage.GetContext()

	if storage.Get(ctx, makeTLDKey(fragments[l-1])) == nil {
		panic("TLD not found")
	}
	if parentExpired(ctx, 1, fragments) {
		panic("one of the parent domains is not registered")
	}
	parentKey := getTokenKey([]byte(name[len(fragments[0])+1:]))

	if l > 2 {
		nsBytes := storage.Get(ctx, append([]byte{prefixName}, parentKey...))
		ns := std.Deserialize(nsBytes.([]byte)).(NameState)
		ns.checkAdmin()
	}

	parentRecKey := append([]byte{prefixRecord}, parentKey...)
	it := storage.Find(ctx, parentRecKey, storage.ValuesOnly|storage.DeserializeValues)
	suffix := []byte(name)
	for iterator.Next(it) {
		r := iterator.Value(it).(RecordState)
		ind := std.MemorySearchLastIndex([]byte(r.Name), suffix, len(r.Name))
		if ind > 0 && ind+len(suffix) == len(r.Name) {
			panic("parent domain has conflicting records: " + r.Name)
		}
	}

	if !isValid(owner) {
		panic("invalid owner")
	}
	common.CheckOwnerWitness(owner)
	runtime.BurnGas(GetPrice())
	var (
		tokenKey = getTokenKey([]byte(name))
		oldOwner interop.Hash160
	)
	nsBytes := storage.Get(ctx, append([]byte{prefixName}, tokenKey...))
	if nsBytes != nil {
		ns := std.Deserialize(nsBytes.([]byte)).(NameState)
		if int64(runtime.GetTime()) < ns.Expiration {
			return false
		}
		oldOwner = ns.Owner
		updateBalance(ctx, []byte(name), oldOwner, -1)
	} else {
		updateTotalSupply(ctx, +1)
	}
	saveDomain(ctx, name, email, refresh, retry, expire, ttl, owner)
	updateBalance(ctx, []byte(name), owner, +1)
	postTransfer(oldOwner, owner, []byte(name), nil)
	return true
}

// RegisterTLD registers new top-level domain. RegisterTLD MUST be called by the
// committee only. Name MUST be a valid TLD.
//
// RegisterTLD panics with 'TLD already exists' if domain already exists.
func RegisterTLD(name, email string, refresh, retry, expire, ttl int) {
	checkCommittee()
	saveCommitteeDomain(storage.GetContext(), name, email, refresh, retry, expire, ttl)
}

// saveCommitteeDomain marks TLD as registered via prefixRoot<name> storage
// record and saves domain state calling saveDomain with given parameters and
// empty owner. The name MUST be a valid TLD name.
func saveCommitteeDomain(ctx storage.Context, name, email string, refresh, retry, expire, ttl int) {
	fragments := splitAndCheck(name, false)
	if len(fragments) != 1 {
		panic("invalid domain name format")
	}

	tldKey := makeTLDKey(name)
	if storage.Get(ctx, tldKey) != nil && !parentExpired(ctx, 0, fragments) {
		panic("TLD already exists")
	}

	storage.Put(ctx, tldKey, 0)

	var committeeOwner interop.Hash160
	saveDomain(ctx, name, email, refresh, retry, expire, ttl, committeeOwner)
}

// saveDomain constructs NameState and RecordState of SOA type for the domain
// based on parameters and saves these descriptors in the contract storage.
// Empty owner parameter corresponds to owner-by-committee domains.
//
// Provided storage.Context MUST be read-write.
func saveDomain(ctx storage.Context, name, email string, refresh, retry, expire, ttl int, owner interop.Hash160) {
	putNameStateWithKey(ctx, getTokenKey([]byte(name)), NameState{
		Owner: owner,
		Name:  name,
		// NNS expiration is in milliseconds
		Expiration: int64(runtime.GetTime() + expire*1000),
	})
	putSoaRecord(ctx, name, email, refresh, retry, expire, ttl)
}

// Renew increases domain expiration date.
func Renew(name string) int64 {
	if len(name) > maxDomainNameLength {
		panic("invalid domain name format")
	}
	runtime.BurnGas(GetPrice())
	ctx := storage.GetContext()
	ns := getNameState(ctx, []byte(name))
	ns.checkAdmin()
	ns.Expiration += millisecondsInYear
	putNameState(ctx, ns)
	return ns.Expiration
}

// UpdateSOA updates soa record.
func UpdateSOA(name, email string, refresh, retry, expire, ttl int) {
	if len(name) > maxDomainNameLength {
		panic("invalid domain name format")
	}
	ctx := storage.GetContext()
	ns := getNameState(ctx, []byte(name))
	ns.checkAdmin()
	putSoaRecord(ctx, name, email, refresh, retry, expire, ttl)
}

// SetAdmin updates domain admin. The name MUST NOT be a TLD.
func SetAdmin(name string, admin interop.Hash160) {
	if len(name) > maxDomainNameLength {
		panic("invalid domain name format")
	}

	fragments := std.StringSplit(name, ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	if admin != nil && !runtime.CheckWitness(admin) {
		panic("not witnessed by admin")
	}
	ctx := storage.GetContext()
	ns := getFragmentedNameState(ctx, []byte(name), fragments)
	common.CheckOwnerWitness(ns.Owner)
	ns.Admin = admin
	putNameState(ctx, ns)
}

// SetRecord adds a new record of the specified type to the provided domain.
// The name MUST NOT be a TLD.
func SetRecord(name string, typ RecordType, id byte, data string) {
	tokenID := []byte(tokenIDFromName(name))
	if !checkBaseRecords(typ, data) {
		panic("invalid record data")
	}

	fragments := std.StringSplit(name, ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	ctx := storage.GetContext()
	ns := getFragmentedNameState(ctx, tokenID, fragments)
	ns.checkAdmin()
	putRecord(ctx, tokenID, name, typ, id, data)
	updateSoaSerial(ctx, tokenID)
}

func checkBaseRecords(typ RecordType, data string) bool {
	switch typ {
	case A:
		return checkIPv4(data)
	case CNAME:
		return splitAndCheck(data, true) != nil
	case TXT:
		return len(data) <= maxTXTRecordLength
	case AAAA:
		return checkIPv6(data)
	default:
		panic("unsupported record type")
	}
}

// AddRecord adds a new record of the specified type to the provided domain.
// The name MUST NOT be a TLD.
func AddRecord(name string, typ RecordType, data string) {
	tokenID := []byte(tokenIDFromName(name))
	if !checkBaseRecords(typ, data) {
		panic("invalid record data")
	}

	fragments := std.StringSplit(string(tokenID), ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	ctx := storage.GetContext()
	ns := getFragmentedNameState(ctx, tokenID, fragments)
	ns.checkAdmin()
	addRecord(ctx, tokenID, name, typ, data)
	updateSoaSerial(ctx, tokenID)
}

// GetRecords returns domain record of the specified type if it exists or an empty
// string if not. The name MUST NOT be a TLD.
func GetRecords(name string, typ RecordType) []string {
	fragments := std.StringSplit(name, ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	tokenID := []byte(tokenIDFromName(name))
	ctx := storage.GetReadOnlyContext()
	_ = getFragmentedNameState(ctx, tokenID, fragments) // ensure not expired
	return getRecordsByType(ctx, tokenID, name, typ)
}

// DeleteRecords removes domain records with the specified type. The name MUST
// NOT be a TLD.
func DeleteRecords(name string, typ RecordType) {
	if typ == SOA {
		panic("you cannot delete soa record")
	}

	tokenID := []byte(tokenIDFromName(name))

	fragments := std.StringSplit(string(tokenID), ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	ctx := storage.GetContext()
	ns := getFragmentedNameState(ctx, tokenID, fragments)
	ns.checkAdmin()
	recordsKey := getRecordsKeyByType(tokenID, name, typ)
	records := storage.Find(ctx, recordsKey, storage.KeysOnly)
	for iterator.Next(records) {
		r := iterator.Value(records).(string)
		storage.Delete(ctx, r)
	}
	updateSoaSerial(ctx, tokenID)
}

// Resolve resolves given name (not more then three redirects are allowed).
// The name MUST NOT be a TLD.
func Resolve(name string, typ RecordType) []string {
	fragments := std.StringSplit(name, ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	ctx := storage.GetReadOnlyContext()
	return resolve(ctx, nil, name, typ, 2)
}

// GetAllRecords returns an Iterator with RecordState items for the given name.
// The name MUST NOT be a TLD.
func GetAllRecords(name string) iterator.Iterator {
	fragments := std.StringSplit(name, ".")
	if len(fragments) == 1 {
		panic("token not found")
	}

	tokenID := []byte(tokenIDFromName(name))
	ctx := storage.GetReadOnlyContext()
	_ = getFragmentedNameState(ctx, tokenID, fragments) // ensure not expired
	recordsKey := getRecordsKey(tokenID, name)
	return storage.Find(ctx, recordsKey, storage.ValuesOnly|storage.DeserializeValues)
}

// updateBalance updates account's balance and account's tokens.
func updateBalance(ctx storage.Context, tokenId []byte, acc interop.Hash160, diff int) {
	balanceKey := append([]byte{prefixBalance}, acc...)
	var balance int
	if b := storage.Get(ctx, balanceKey); b != nil {
		balance = b.(int)
	}
	balance += diff
	if balance == 0 {
		storage.Delete(ctx, balanceKey)
	} else {
		storage.Put(ctx, balanceKey, balance)
	}

	tokenKey := getTokenKey(tokenId)
	accountTokenKey := append(append([]byte{prefixAccountToken}, acc...), tokenKey...)
	if diff < 0 {
		storage.Delete(ctx, accountTokenKey)
	} else {
		storage.Put(ctx, accountTokenKey, tokenId)
	}
}

// postTransfer sends Transfer notification to the network and calls onNEP11Payment
// method.
func postTransfer(from, to interop.Hash160, tokenID []byte, data interface{}) {
	runtime.Notify("Transfer", from, to, 1, tokenID)
	if management.GetContract(to) != nil {
		contract.Call(to, "onNEP11Payment", contract.All, from, 1, tokenID, data)
	}
}

// getTotalSupply returns total supply from storage.
func getTotalSupply(ctx storage.Context) int {
	val := storage.Get(ctx, []byte{prefixTotalSupply})
	return val.(int)
}

// updateTotalSupply adds the specified diff to the total supply.
func updateTotalSupply(ctx storage.Context, diff int) {
	tsKey := []byte{prefixTotalSupply}
	ts := getTotalSupply(ctx)
	storage.Put(ctx, tsKey, ts+diff)
}

// getTokenKey computes hash160 from the given tokenID.
func getTokenKey(tokenID []byte) []byte {
	return crypto.Ripemd160(tokenID)
}

// getNameState returns domain name state by the specified tokenID.
func getNameState(ctx storage.Context, tokenID []byte) NameState {
	return getFragmentedNameState(ctx, tokenID, nil)
}

// getFragmentedNameState returns domain name state by the specified tokenID.
// Optional fragments parameter allows to pass pre-calculated elements of the
// domain name path: if empty, getFragmentedNameState splits name on its own.
func getFragmentedNameState(ctx storage.Context, tokenID []byte, fragments []string) NameState {
	tokenKey := getTokenKey(tokenID)
	ns := getNameStateWithKey(ctx, tokenKey)
	if len(fragments) == 0 {
		fragments = std.StringSplit(string(tokenID), ".")
	}
	if parentExpired(ctx, 1, fragments) {
		panic("parent domain has expired")
	}
	return ns
}

// getNameStateWithKey returns domain name state by the specified token key.
func getNameStateWithKey(ctx storage.Context, tokenKey []byte) NameState {
	nameKey := append([]byte{prefixName}, tokenKey...)
	nsBytes := storage.Get(ctx, nameKey)
	if nsBytes == nil {
		panic("token not found")
	}
	ns := std.Deserialize(nsBytes.([]byte)).(NameState)
	ns.ensureNotExpired()
	return ns
}

// putNameState stores domain name state.
func putNameState(ctx storage.Context, ns NameState) {
	tokenKey := getTokenKey([]byte(ns.Name))
	putNameStateWithKey(ctx, tokenKey, ns)
}

// putNameStateWithKey stores domain name state with the specified token key.
func putNameStateWithKey(ctx storage.Context, tokenKey []byte, ns NameState) {
	nameKey := append([]byte{prefixName}, tokenKey...)
	nsBytes := std.Serialize(ns)
	storage.Put(ctx, nameKey, nsBytes)
}

// getRecordsByType returns domain record.
func getRecordsByType(ctx storage.Context, tokenId []byte, name string, typ RecordType) []string {
	recordsKey := getRecordsKeyByType(tokenId, name, typ)

	var result []string
	records := storage.Find(ctx, recordsKey, storage.ValuesOnly|storage.DeserializeValues)
	for iterator.Next(records) {
		r := iterator.Value(records).(RecordState)
		if r.Type == typ {
			result = append(result, r.Data)
		}
	}
	return result
}

// putRecord stores domain record.
func putRecord(ctx storage.Context, tokenId []byte, name string, typ RecordType, id byte, data string) {
	recordKey := getIdRecordKey(tokenId, name, typ, id)
	recBytes := storage.Get(ctx, recordKey)
	if recBytes == nil {
		panic("invalid record id")
	}

	storeRecord(ctx, recordKey, name, typ, id, data)
}

// addRecord stores domain record.
func addRecord(ctx storage.Context, tokenId []byte, name string, typ RecordType, data string) {
	recordsKey := getRecordsKeyByType(tokenId, name, typ)

	var id byte
	records := storage.Find(ctx, recordsKey, storage.ValuesOnly|storage.DeserializeValues)
	for iterator.Next(records) {
		id++

		r := iterator.Value(records).(RecordState)
		if r.Name == name && r.Type == typ && r.Data == data {
			panic("record already exists")
		}
	}

	if typ == CNAME && id != 0 {
		panic("you shouldn't have more than one CNAME record")
	}

	recordKey := append(recordsKey, id) // the same as getIdRecordKey
	storeRecord(ctx, recordKey, name, typ, id, data)
}

// storeRecord puts record to storage.
func storeRecord(ctx storage.Context, recordKey []byte, name string, typ RecordType, id byte, data string) {
	rs := RecordState{
		Name: name,
		Type: typ,
		Data: data,
		ID:   id,
	}
	recBytes := std.Serialize(rs)
	storage.Put(ctx, recordKey, recBytes)
}

// putSoaRecord stores soa domain record.
func putSoaRecord(ctx storage.Context, name, email string, refresh, retry, expire, ttl int) {
	var id byte
	tokenId := []byte(tokenIDFromName(name))
	recordKey := getIdRecordKey(tokenId, name, SOA, id)
	rs := RecordState{
		Name: name,
		Type: SOA,
		ID:   id,
		Data: name + " " + email + " " +
			std.Itoa(runtime.GetTime(), 10) + " " +
			std.Itoa(refresh, 10) + " " +
			std.Itoa(retry, 10) + " " +
			std.Itoa(expire, 10) + " " +
			std.Itoa(ttl, 10),
	}
	recBytes := std.Serialize(rs)
	storage.Put(ctx, recordKey, recBytes)
}

// updateSoaSerial stores soa domain record.
func updateSoaSerial(ctx storage.Context, tokenId []byte) {
	var id byte
	recordKey := getIdRecordKey(tokenId, string(tokenId), SOA, id)

	recBytes := storage.Get(ctx, recordKey)
	if recBytes == nil {
		panic("not found soa record")
	}
	rec := std.Deserialize(recBytes.([]byte)).(RecordState)

	split := std.StringSplitNonEmpty(rec.Data, " ")
	if len(split) != 7 {
		panic("invalid soa record")
	}
	split[2] = std.Itoa(runtime.GetTime(), 10) // update serial
	rec.Data = split[0] + " " + split[1] + " " +
		split[2] + " " + split[3] + " " +
		split[4] + " " + split[5] + " " +
		split[6]

	recBytes = std.Serialize(rec)
	storage.Put(ctx, recordKey, recBytes)
}

// getRecordsKey returns the prefix used to store domain records of different types.
func getRecordsKey(tokenId []byte, name string) []byte {
	recordKey := append([]byte{prefixRecord}, getTokenKey(tokenId)...)
	return append(recordKey, getTokenKey([]byte(name))...)
}

// getRecordsKeyByType returns the key used to store domain records.
func getRecordsKeyByType(tokenId []byte, name string, typ RecordType) []byte {
	recordKey := getRecordsKey(tokenId, name)
	return append(recordKey, byte(typ))
}

// getIdRecordKey returns the key used to store domain records.
func getIdRecordKey(tokenId []byte, name string, typ RecordType, id byte) []byte {
	recordKey := getRecordsKey(tokenId, name)
	return append(recordKey, byte(typ), id)
}

// isValid returns true if the provided address is a valid Uint160.
func isValid(address interop.Hash160) bool {
	return address != nil && len(address) == interop.Hash160Len
}

// checkCommittee panics if the script container is not signed by the committee.
func checkCommittee() {
	committee := neo.GetCommittee()
	if committee == nil {
		panic("failed to get committee")
	}
	l := len(committee)
	committeeMultisig := contract.CreateMultisigAccount(l-(l-1)/2, committee)
	if committeeMultisig == nil || !runtime.CheckWitness(committeeMultisig) {
		panic("not witnessed by committee")
	}
}

// checkFragment validates root or a part of domain name.
// 1. Root domain must start with a letter.
// 2. All other fragments must start and end with a letter or a digit.
func checkFragment(v string, isRoot bool) bool {
	maxLength := maxDomainNameFragmentLength
	if isRoot {
		maxLength = maxRootLength
	}
	if len(v) == 0 || len(v) > maxLength {
		return false
	}
	c := v[0]
	if isRoot {
		if !(c >= 'a' && c <= 'z') {
			return false
		}
	} else {
		if !isAlNum(c) {
			return false
		}
	}
	for i := 1; i < len(v)-1; i++ {
		if v[i] != '-' && !isAlNum(v[i]) {
			return false
		}
	}
	return isAlNum(v[len(v)-1])
}

// isAlNum checks whether provided char is a lowercase letter or a number.
func isAlNum(c uint8) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// splitAndCheck splits domain name into parts and validates it.
func splitAndCheck(name string, allowMultipleFragments bool) []string {
	l := len(name)
	if l < minDomainNameLength || maxDomainNameLength < l {
		return nil
	}
	fragments := std.StringSplit(name, ".")
	l = len(fragments)
	if l > 2 && !allowMultipleFragments {
		return nil
	}
	for i := 0; i < l; i++ {
		if !checkFragment(fragments[i], i == l-1) {
			return nil
		}
	}
	return fragments
}

// checkIPv4 checks record on IPv4 compliance.
func checkIPv4(data string) bool {
	l := len(data)
	if l < 7 || 15 < l {
		return false
	}
	fragments := std.StringSplit(data, ".")
	if len(fragments) != 4 {
		return false
	}
	numbers := make([]int, 4)
	for i, f := range fragments {
		if len(f) == 0 {
			return false
		}
		number := std.Atoi10(f)
		if number < 0 || 255 < number {
			panic("not a byte")
		}
		if number > 0 && f[0] == '0' {
			return false
		}
		if number == 0 && len(f) > 1 {
			return false
		}
		numbers[i] = number
	}
	n0 := numbers[0]
	n1 := numbers[1]
	n3 := numbers[3]
	if n0 == 0 ||
		n0 == 10 ||
		n0 == 127 ||
		n0 >= 224 ||
		(n0 == 169 && n1 == 254) ||
		(n0 == 172 && 16 <= n1 && n1 <= 31) ||
		(n0 == 192 && n1 == 168) ||
		n3 == 0 ||
		n3 == 255 {
		return false
	}
	return true
}

// checkIPv6 checks record on IPv6 compliance.
func checkIPv6(data string) bool {
	l := len(data)
	if l < 2 || 39 < l {
		return false
	}
	fragments := std.StringSplit(data, ":")
	l = len(fragments)
	if l < 3 || 8 < l {
		return false
	}
	var hasEmpty bool
	nums := make([]int, 8)
	for i, f := range fragments {
		if len(f) == 0 {
			if i == 0 {
				if len(fragments[1]) != 0 {
					return false
				}
				nums[i] = 0
			} else if i == l-1 {
				if len(fragments[i-1]) != 0 {
					return false
				}
				nums[7] = 0
			} else if hasEmpty {
				return false
			} else {
				hasEmpty = true
				endIndex := 9 - l + i
				for j := i; j < endIndex; j++ {
					nums[j] = 0
				}
			}
		} else {
			if len(f) > 4 {
				return false
			}
			n := std.Atoi(f, 16)
			if 65535 < n {
				panic("fragment overflows uint16: " + f)
			}
			idx := i
			if hasEmpty {
				idx = i + 8 - l
			}
			nums[idx] = n
		}
	}
	if l < 8 && !hasEmpty {
		return false
	}

	f0 := nums[0]
	if f0 < 0x2000 || f0 == 0x2002 || f0 == 0x3ffe || f0 > 0x3fff { // IPv6 Global Unicast https://www.iana.org/assignments/ipv6-address-space/ipv6-address-space.xhtml
		return false
	}
	if f0 == 0x2001 {
		f1 := nums[1]
		if f1 < 0x200 || f1 == 0xdb8 {
			return false
		}
	}
	return true
}

// tokenIDFromName returns token ID (domain.root) from the provided name.
func tokenIDFromName(name string) string {
	fragments := splitAndCheck(name, true)
	if fragments == nil {
		panic("invalid domain name format")
	}

	ctx := storage.GetReadOnlyContext()
	sum := 0
	l := len(fragments) - 1
	for i := 0; i < l; i++ {
		tokenKey := getTokenKey([]byte(name[sum:]))
		nameKey := append([]byte{prefixName}, tokenKey...)
		nsBytes := storage.Get(ctx, nameKey)
		if nsBytes != nil {
			ns := std.Deserialize(nsBytes.([]byte)).(NameState)
			if int64(runtime.GetTime()) < ns.Expiration {
				return name[sum:]
			}
		}
		sum += len(fragments[i]) + 1
	}
	return name
}

// resolve resolves the provided name using record with the specified type and given
// maximum redirections constraint.
func resolve(ctx storage.Context, res []string, name string, typ RecordType, redirect int) []string {
	if redirect < 0 {
		panic("invalid redirect")
	}
	if len(name) == 0 {
		panic("invalid name")
	}
	if name[len(name)-1] == '.' {
		name = name[:len(name)-1]
	}
	records := getAllRecords(ctx, name)
	cname := ""
	for iterator.Next(records) {
		r := iterator.Value(records).(RecordState)
		if r.Type == typ {
			res = append(res, r.Data)
		}
		if r.Type == CNAME {
			cname = r.Data
		}
	}
	if cname == "" || typ == CNAME {
		return res
	}

	res = append(res, cname)
	return resolve(ctx, res, cname, typ, redirect-1)
}

// getAllRecords returns iterator over the set of records corresponded with the
// specified name.
func getAllRecords(ctx storage.Context, name string) iterator.Iterator {
	tokenID := []byte(tokenIDFromName(name))
	_ = getNameState(ctx, tokenID)
	recordsKey := getRecordsKey(tokenID, name)
	return storage.Find(ctx, recordsKey, storage.ValuesOnly|storage.DeserializeValues)
}

func makeTLDKey(name string) []byte {
	return append([]byte{prefixRoot}, name...)
}
