package container

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/neogointernal"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
	cst "github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
	"github.com/nspcc-dev/neofs-contract/contracts/nns/recordtype"
)

type (
	StorageNode struct {
		Info []byte
	}

	Container struct {
		Value []byte
		Sig   interop.Signature
		Pub   interop.PublicKey
		Token []byte
	}

	ExtendedACL struct {
		Value []byte
		Sig   interop.Signature
		Pub   interop.PublicKey
		Token []byte
	}

	// Estimation contains the public key of the estimator (storage node) and
	// the size this estimator has for the container.
	Estimation struct {
		From interop.PublicKey
		Size int
	}

	// NodeReport is a report by a certain storage node about its storage
	// engine's volume it uses for a certain container.
	NodeReport struct {
		PublicKey       interop.PublicKey
		ContainerSize   int
		NumberOfObjects int
		NumberOfReports int
		LastUpdateEpoch int
	}

	// NodeReportSummary is the summary of all [NodeReport] claimed by all
	// storage nodes.
	NodeReportSummary struct {
		ContainerSize   int
		NumberOfObjects int
	}

	ContainerSizes struct {
		CID         []byte
		Estimations []Estimation
	}

	// Quota describes size limitation for a container or
	// for a user as a sum of all objects in all his
	// containers.
	Quota struct {
		SoftLimit int
		HardLimit int
	}
)

const (
	balanceContractKey = "balanceScriptHash"
	netmapContractKey  = "netmapScriptHash"
	proxyContractKey   = "proxyScriptHash"
	nnsContractKey     = "nnsScriptHash"
	nnsRootKey         = "nnsRoot"
	nnsHasAliasKey     = "nnsHasAlias"

	// nolint:deadcode,unused
	nnsDefaultTLD = "container"

	// V2 format.
	containerIDSize = interop.Hash256Len // SHA256 size

	reportsSummary             = 's'
	maxNumberOfReportsPerEpoch = 3
	reportersPrefix            = 'i'
	estimateKeyPrefix          = "cnr"
	containersWithMetaPrefix   = 'm'
	containerKeyPrefix         = 'x'
	ownerKeyPrefix             = 'o'
	deletedKeyPrefix           = 'd'
	nodesPrefix                = 'n'
	replicasNumberPrefix       = 'r'
	nextEpochNodesPrefix       = 'u'
	containerQuotaKeyPrefix    = 'a'
	userQuotaKeyPrefix         = 'b'
	userLoadKeyPrefix          = 'e'

	// default SOA record field values.
	defaultRefresh = 3600                 // 1 hour
	defaultRetry   = 600                  // 10 min
	defaultExpire  = 3600 * 24 * 365 * 10 // 10 years
	defaultTTL     = 3600                 // 1 hour
)

var (
	eACLPrefix = []byte("eACL")
)

// OnNEP11Payment is needed for registration with contract as the owner to work.
func OnNEP11Payment(a interop.Hash160, b int, c []byte, d any) {
}

// nolint:deadcode,unused
func _deploy(data any, isUpdate bool) {
	ctx := storage.GetContext()
	args := data.([]any)
	if isUpdate {
		version := args[len(args)-1].(int)
		common.CheckVersion(version)

		if version < 22_000 {
			storage.Delete(ctx, "identityScriptHash") // neofsIDContractKey

			it := storage.Find(ctx, []byte{containerKeyPrefix}, storage.DeserializeValues|storage.PickField0) // Container.Value field
			for iterator.Next(it) {
				val := iterator.Value(it).(struct{ key, cnr []byte })
				storage.Put(ctx, val.key, val.cnr)
			}

			it = storage.Find(ctx, eACLPrefix, storage.DeserializeValues|storage.PickField0) // ExtendedACL.Value field
			for iterator.Next(it) {
				val := iterator.Value(it).(struct{ key, cnr []byte })
				storage.Put(ctx, val.key, val.cnr)
			}
		}

		if version < 23_000 {
			addrNNS := storage.Get(ctx, nnsContractKey).(interop.Hash160)
			if len(addrNNS) != interop.Hash160Len {
				panic("do not know NNS address")
			}
			addrProxy := common.ResolveFSContractWithNNS(addrNNS, "proxy")
			if len(addrProxy) != interop.Hash160Len {
				panic("NNS does not know Proxy address")
			}
			storage.Put(ctx, proxyContractKey, addrProxy)
		}

		if version < 24_000 {
			it := storage.Find(ctx, []byte(estimateKeyPrefix), storage.KeysOnly)
			for iterator.Next(it) {
				k := iterator.Value(it).([]byte)
				storage.Delete(ctx, k)
			}
		}

		//if version < 25_000 {
		//	common.UnsubscribeFromNewEpoch()
		//}

		return
	}

	var (
		addrNetmap  interop.Hash160
		addrBalance interop.Hash160
		addrNNS     interop.Hash160
		addrProxy   interop.Hash160
		nnsRoot     string
	)
	// args[0] is notaryDisabled flag

	// Do this out of order since NNS has to be present anyway and it can
	// be used to resolve other contracts.
	if len(args) >= 5 && len(args[4].(interop.Hash160)) == interop.Hash160Len {
		addrNNS = args[4].(interop.Hash160)
	} else {
		addrNNS = common.InferNNSHash()
	}

	if len(args) >= 2 && len(args[1].(interop.Hash160)) == interop.Hash160Len {
		addrNetmap = args[1].(interop.Hash160)
	} else {
		addrNetmap = common.ResolveFSContractWithNNS(addrNNS, "netmap")
	}

	if len(args) >= 3 && len(args[2].(interop.Hash160)) == interop.Hash160Len {
		addrBalance = args[2].(interop.Hash160)
	} else {
		addrBalance = common.ResolveFSContractWithNNS(addrNNS, "balance")
	}

	// args[3] is neofsid hash, no longer used

	addrProxy = common.ResolveFSContractWithNNS(addrNNS, "proxy")

	if len(args) >= 6 && len(args[5].(string)) > 0 {
		nnsRoot = args[5].(string)
	} else {
		nnsRoot = nnsDefaultTLD
	}

	storage.Put(ctx, netmapContractKey, addrNetmap)
	storage.Put(ctx, balanceContractKey, addrBalance)
	storage.Put(ctx, nnsContractKey, addrNNS)
	storage.Put(ctx, proxyContractKey, addrProxy)
	storage.Put(ctx, nnsRootKey, nnsRoot)

	// add NNS root for container alias domains
	registerNiceNameTLD(addrNNS, nnsRoot)

	runtime.Log("container contract initialized")
}

// nolint:deadcode,unused
func registerNiceNameTLD(addrNNS interop.Hash160, nnsRoot string) {
	isAvail := contract.Call(addrNNS, "isAvailable", contract.AllowCall|contract.ReadStates,
		nnsRoot).(bool)
	if !isAvail {
		return
	}

	contract.Call(addrNNS, "registerTLD", contract.All,
		nnsRoot, "ops@nspcc.ru",
		defaultRefresh, defaultRetry, defaultExpire, defaultTTL)
}

// Update method updates contract source code and manifest. It can be invoked
// by committee only.
func Update(nefFile, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nefFile, manifest, common.AppendVersion(data))
	runtime.Log("container contract updated")
}

// SubmitObjectPut registers successful object PUT operation and notifies about
// it. metaInformation must be signed by container nodes according to
// container's placement, see [VerifyPlacementSignatures]. metaInformation
// must contain information about an object placed to a container that was
// created using [Put] ([PutMeta]) with enabled meta-on-chain option.
func SubmitObjectPut(metaInformation []byte, sigs [][]interop.Signature) {
	ctx := storage.GetContext()
	proxyH := storage.Get(ctx, proxyContractKey).(interop.Hash160)
	if !runtime.CurrentSigners()[0].Account.Equals(proxyH) {
		panic("not signed by Proxy contract")
	}

	metaMap := std.Deserialize(metaInformation).(map[string]any)

	// required

	cID := requireMapValue(metaMap, "cid").(interop.Hash256)
	if len(cID) != interop.Hash256Len {
		panic("incorrect container ID")
	}
	if storage.Get(ctx, append([]byte{containersWithMetaPrefix}, cID...)) == nil {
		panic("container does not support meta-on-chain")
	}
	oID := requireMapValue(metaMap, "oid").(interop.Hash256)
	if len(oID) != interop.Hash256Len {
		panic("incorrect object ID")
	}
	_ = requireMapValue(metaMap, "size").(int)
	vub := requireMapValue(metaMap, "validUntil").(int)
	if vub <= ledger.CurrentIndex() {
		panic("incorrect vub: exceeded")
	}
	magic := requireMapValue(metaMap, "network").(int)
	if magic != runtime.GetNetwork() {
		panic("incorrect network magic")
	}

	// optional

	if v, ok := getFromMap(metaMap, "type"); ok {
		typ := v.(int)
		switch typ {
		case 0, 1, 2, 3, 4: // regular, tombstone, storage group, lock, link
		default:
			panic("incorrect object type")
		}
	}
	if v, ok := getFromMap(metaMap, "firstPart"); ok {
		firstPart := v.(interop.Hash256)
		if len(firstPart) != interop.Hash256Len {
			panic("incorrect first part object ID")
		}
	}
	if v, ok := getFromMap(metaMap, "previousPart"); ok {
		previousPart := v.(interop.Hash256)
		if len(previousPart) != interop.Hash256Len {
			panic("incorrect previous part object ID")
		}
	}
	if v, ok := getFromMap(metaMap, "locked"); ok {
		locked := v.([]interop.Hash256)
		for i, l := range locked {
			if len(l) != interop.Hash256Len {
				panic("incorrect " + std.Itoa10(i) + " locked object")
			}
		}
	}
	if v, ok := getFromMap(metaMap, "deleted"); ok {
		deleted := v.([]interop.Hash256)
		for i, d := range deleted {
			if len(d) != interop.Hash256Len {
				panic("incorrect " + std.Itoa10(i) + " deleted object")
			}
		}
	}

	runtime.Notify("ObjectPut", cID, oID, metaMap)
}

func requireMapValue(m map[string]any, key string) any {
	v, ok := getFromMap(m, key)
	if !ok {
		panic("'" + key + "'" + " not found")
	}

	return v
}

func getFromMap(m map[string]any, key string) (any, bool) {
	if neogointernal.Opcode2("HASKEY", m, key).(bool) { // https://github.com/nspcc-dev/neo-go/issues/3716
		return m[key], true
	}

	return nil, false
}

// PutMeta is the same as [Put] and [PutNamed] (and exposed as put from
// the contract via overload), but allows named containers and container's
// meta-information be handled and notified using the chain. If name and
// zone are non-empty strings, it behaves the same as [PutNamed]; empty
// strings make a regular [Put] call.
// Deprecated: use [Create] instead.
func PutMeta(container []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte, name, zone string, metaOnChain bool) {
	if metaOnChain {
		ctx := storage.GetContext()
		cID := crypto.Sha256(container)
		storage.Put(ctx, append([]byte{containersWithMetaPrefix}, cID...), []byte{})
	}
	PutNamed(container, signature, publicKey, token, name, zone)
}

// Put method creates a new container if it has been invoked by Alphabet nodes
// of the Inner Ring. Otherwise, it produces containerPut notification.
//
// Container should be a stable marshaled Container structure from API.
// Signature is a RFC6979 signature of the Container.
// PublicKey contains the public key of the signer.
// Token is optional and should be a stable marshaled SessionToken structure from
// API.
// Deprecated: use [Create] instead.
func Put(container []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte) {
	PutNamed(container, signature, publicKey, token, "", "")
}

// PutNamedOverloaded is the same as [Put] (and exposed as put from the contract via
// overload), but allows named container creation via NNS contract.
// Deprecated: use [Create] instead.
func PutNamedOverloaded(container []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte, name, zone string) {
	PutNamed(container, signature, publicKey, token, name, zone)
}

// PutNamed is similar to put but also sets a TXT record in nns contract.
// Note that zone must exist.
// DEPRECATED: use [Create] instead.
func PutNamed(container []byte, signature interop.Signature,
	publicKey interop.PublicKey, token []byte,
	name, zone string) {
	ctx := storage.GetContext()

	ownerID := ownerFromBinaryContainer(container)
	containerID := crypto.Sha256(container)
	if storage.Get(ctx, append([]byte{deletedKeyPrefix}, []byte(containerID)...)) != nil {
		panic(cst.ErrorDeleted)
	}

	var (
		needRegister    bool
		nnsContractAddr interop.Hash160
		domain          string
	)
	if name != "" {
		if zone == "" {
			zone = storage.Get(ctx, nnsRootKey).(string)
		}
		nnsContractAddr = storage.Get(ctx, nnsContractKey).(interop.Hash160)
		domain = name + "." + zone
		needRegister = checkNiceNameAvailable(nnsContractAddr, domain)
	}

	alphabet := common.AlphabetNodes()
	from := common.WalletToScriptHash(ownerID)
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	balanceContractAddr := storage.Get(ctx, balanceContractKey).(interop.Hash160)
	containerFee := contract.Call(netmapContractAddr, "config", contract.ReadOnly, cst.RegistrationFeeKey).(int)
	balance := contract.Call(balanceContractAddr, "balanceOf", contract.ReadOnly, from).(int)
	if name != "" {
		aliasFee := contract.Call(netmapContractAddr, "config", contract.ReadOnly, cst.AliasFeeKey).(int)
		containerFee += aliasFee
	}

	if balance < containerFee*len(alphabet) {
		panic("insufficient balance to create container")
	}

	common.CheckAlphabetWitness()

	details := common.ContainerFeeTransferDetails(containerID)

	for _, node := range alphabet {
		to := contract.CreateStandardAccount(node)

		contract.Call(balanceContractAddr, "transferX",
			contract.All,
			from,
			to,
			containerFee,
			details,
		)
	}

	addContainer(ctx, containerID, ownerID, container)

	if name != "" {
		if needRegister {
			res := contract.Call(nnsContractAddr, "register", contract.All,
				domain, runtime.GetExecutingScriptHash(), "ops@nspcc.ru",
				defaultRefresh, defaultRetry, defaultExpire, defaultTTL).(bool)
			if !res {
				panic("can't register the domain " + domain)
			}
		}
		contract.Call(nnsContractAddr, "addRecord", contract.All,
			domain, recordtype.TXT, std.Base58Encode(containerID))

		key := append([]byte(nnsHasAliasKey), containerID...)
		storage.Put(ctx, key, domain)
	}

	runtime.Log("added new container")
	runtime.Notify("PutSuccess", containerID, publicKey)
}

// Create saves container descriptor serialized according to the NeoFS API
// binary protocol. Created containers are content-addressed: they may be
// accessed by SHA-256 checksum of their data. On success, Create throws
// 'Created' notification event.
//
// Created containers are disposable: if they are deleted, they cannot be
// created again. Create throws [cst.ErrorDeleted] exception on recreation
// attempts.
//
// Domain name is optional. If specified, it is used to register 'name.zone'
// domain for given container. Domain zone is optional: it defaults to the 6th
// contract deployment parameter which itself defaults to 'container'.
//
// Meta-on-chain boolean flag specifies whether meta information about objects
// from this container can be collected for it.
//
// The operation is paid. Container owner pays per-container fee (global chain
// configuration) to each committee member. If domain registration is requested,
// additional alias fee (also a configuration) is added to each payment.
//
// Create must have chain's committee multi-signature witness. Invocation
// script, verification script and session token parameters are owner
// credentials. They are transmitted in notary transactions carrying original
// users' requests. IR verifies requests and approves them via multi-signature.
// Once creation is approved, container is persisted and becomes accessible.
// Credentials are disposable and do not persist in the chain.
func Create(cnr []byte, invocScript, verifScript, sessionToken []byte, name, zone string, metaOnChain bool) {
	owner := ownerFromBinaryContainer(cnr)
	alphabet := common.AlphabetNodes()
	if !runtime.CheckWitness(common.Multiaddress(alphabet, false)) {
		panic(common.ErrAlphabetWitnessFailed)
	}

	ctx := storage.GetContext()
	id := crypto.Sha256(cnr)
	if storage.Get(ctx, append([]byte{deletedKeyPrefix}, id...)) != nil {
		panic(cst.ErrorDeleted)
	}

	if name != "" {
		if zone == "" {
			zone = storage.Get(ctx, nnsRootKey).(string)
		}
		nnsContract := storage.Get(ctx, nnsContractKey).(interop.Hash160)
		domain := name + "." + zone

		if checkNiceNameAvailable(nnsContract, domain) {
			if !contract.Call(nnsContract, "register", contract.All, domain, runtime.GetExecutingScriptHash(),
				"ops@nspcc.ru", defaultRefresh, defaultRetry, defaultExpire, defaultTTL).(bool) {
				panic("domain registration failed")
			}
		}

		contract.Call(nnsContract, "addRecord", contract.All, domain, recordtype.TXT, std.Base58Encode(id))

		storage.Put(ctx, append([]byte(nnsHasAliasKey), id...), domain)
	}

	netmapContract := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	fee := contract.Call(netmapContract, "config", contract.ReadOnly, cst.RegistrationFeeKey).(int)
	if name != "" {
		fee += contract.Call(netmapContract, "config", contract.ReadOnly, cst.AliasFeeKey).(int)
	}
	if fee > 0 {
		ownerAcc := common.WalletToScriptHash(owner)
		balanceContract := storage.Get(ctx, balanceContractKey).(interop.Hash160)
		ownerBalance := contract.Call(balanceContract, "balanceOf", contract.ReadOnly, ownerAcc).(int)
		if ownerBalance < fee*len(alphabet) {
			panic("insufficient balance to create container")
		}

		transferDetails := common.ContainerFeeTransferDetails(id)
		for i := range alphabet {
			contract.Call(balanceContract, "transferX", contract.All,
				ownerAcc, contract.CreateStandardAccount(alphabet[i]), fee, transferDetails)
		}
	}

	storage.Put(ctx, append(append([]byte{ownerKeyPrefix}, owner...), id...), id)
	storage.Put(ctx, append([]byte{containerKeyPrefix}, id...), cnr)
	if metaOnChain {
		storage.Put(ctx, append([]byte{containersWithMetaPrefix}, id...), []byte{})
	}

	runtime.Notify("Created", id, owner)
}

// checkNiceNameAvailable checks if the nice name is available for the container.
// It panics if the name is taken. Returned value specifies if new domain registration is needed.
func checkNiceNameAvailable(nnsContractAddr interop.Hash160, domain string) bool {
	isAvail := contract.Call(nnsContractAddr, "isAvailable",
		contract.ReadStates|contract.AllowCall, domain).(bool)
	if isAvail {
		return true
	}

	owner := contract.Call(nnsContractAddr, "ownerOf",
		contract.ReadStates|contract.AllowCall, domain).(string)
	if owner != string(common.CommitteeAddress()) && owner != string(runtime.GetExecutingScriptHash()) {
		panic("committee or container contract must own registered domain")
	}

	res := contract.Call(nnsContractAddr, "getRecords",
		contract.ReadStates|contract.AllowCall, domain, recordtype.TXT).([]string)
	if len(res) > 0 {
		panic("name is already taken")
	}

	return false
}

// Delete method removes a container from the contract storage if it has been
// invoked by Alphabet nodes of the Inner Ring. Otherwise, it produces
// containerDelete notification.
//
// Signature is a RFC6979 signature of the container ID.
// Token is optional and should be a stable marshaled SessionToken structure from
// API.
//
// If the container doesn't exist, it panics with NotFoundError.
// Deprecated: use [Remove] instead.
func Delete(containerID []byte, signature interop.Signature, token []byte) {
	ctx := storage.GetContext()

	ownerID := getOwnerByID(ctx, containerID)
	if ownerID == nil {
		return
	}

	common.CheckAlphabetWitness()

	key := append([]byte(nnsHasAliasKey), containerID...)
	domain := storage.Get(ctx, key).(string)
	if len(domain) != 0 {
		storage.Delete(ctx, key)
		deleteNNSRecords(ctx, domain)
	}
	removeContainer(ctx, containerID, ownerID)
	runtime.Log("remove container")
	runtime.Notify("DeleteSuccess", containerID)
}

// Remove removes all data for the referenced container. Remove is no-op if
// container does not exist. On success, Remove throws 'Removed' notification
// event.
//
// See [Create] for details.
func Remove(id []byte, invocScript, verifScript, sessionToken []byte) {
	common.CheckAlphabetWitness()

	if len(id) != interop.Hash256Len {
		panic("invalid container ID length")
	}

	ctx := storage.GetContext()
	cnrItemKey := append([]byte{containerKeyPrefix}, id...)
	cnrItem := storage.Get(ctx, cnrItemKey)
	if cnrItem == nil {
		return
	}

	owner := ownerFromBinaryContainer(cnrItem.([]byte))

	removeContainer(ctx, id, owner)

	domainKey := append([]byte(nnsHasAliasKey), id...)
	if domain := storage.Get(ctx, domainKey).(string); len(domain) != 0 { // != "" not working
		storage.Delete(ctx, domainKey)
		deleteNNSRecords(ctx, domain)
	}

	runtime.Notify("Removed", interop.Hash256(id), owner)
}

func deleteNNSRecords(ctx storage.Context, domain string) {
	defer func() {
		// Exception happened.
		if r := recover(); r != nil {
			var msg = r.([]byte)
			// Expired or deleted entries are OK.
			if std.MemorySearch(msg, []byte("has expired")) == -1 &&
				std.MemorySearch(msg, []byte("not found")) == -1 {
				panic("unable to delete NNS record: " + string(msg))
			}
		}
	}()

	nnsContractAddr := storage.Get(ctx, nnsContractKey).(interop.Hash160)
	contract.Call(nnsContractAddr, "deleteRecords", contract.All, domain, recordtype.TXT)
}

// Get method returns a structure that contains a stable marshaled Container structure,
// the signature, the public key of the container creator and a stable marshaled SessionToken
// structure if it was provided.
//
// If the container doesn't exist, it panics with NotFoundError.
// Deprecated: use [GetContainerData] instead.
func Get(containerID []byte) Container {
	ctx := storage.GetReadOnlyContext()
	cnt := getContainer(ctx, containerID)
	if len(cnt.Value) == 0 {
		panic(cst.NotFoundError)
	}
	return cnt
}

// GetContainerData returns binary of the container it was created with by ID.
//
// If the container is missing, GetContainerData throws [cst.NotFoundError]
// exception.
func GetContainerData(id []byte) []byte {
	cnr := storage.Get(storage.GetReadOnlyContext(), append([]byte{containerKeyPrefix}, id...))
	if cnr == nil {
		panic(cst.NotFoundError)
	}
	return cnr.([]byte)
}

// Owner method returns a 25 byte Owner ID of the container.
//
// If the container doesn't exist, it panics with NotFoundError.
func Owner(containerID []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	owner := getOwnerByID(ctx, containerID)
	if owner == nil {
		panic(cst.NotFoundError)
	}
	return owner
}

// Alias method returns a string with an alias of the container if it's set
// (Null otherwise).
//
// If the container doesn't exist, it panics with NotFoundError.
func Alias(cid []byte) string {
	ctx := storage.GetReadOnlyContext()
	owner := getOwnerByID(ctx, cid)
	if owner == nil {
		panic(cst.NotFoundError)
	}
	return storage.Get(ctx, append([]byte(nnsHasAliasKey), cid...)).(string)
}

// Count method returns the number of registered containers.
func Count() int {
	count := 0
	ctx := storage.GetReadOnlyContext()
	it := storage.Find(ctx, []byte{containerKeyPrefix}, storage.KeysOnly)
	for iterator.Next(it) {
		count++
	}
	return count
}

// ContainersOf iterates over all container IDs owned by the specified owner.
// If owner is nil, it iterates over all containers.
func ContainersOf(owner []byte) iterator.Iterator {
	ctx := storage.GetReadOnlyContext()
	key := []byte{ownerKeyPrefix}
	if len(owner) != 0 {
		key = append(key, owner...)
	}
	return storage.Find(ctx, key, storage.ValuesOnly)
}

// maxNumOfREPs is a max supported number of REP value in container's policy
// and also a max number of REPs.
const maxNumOfREPs = 255

// AddNextEpochNodes accumulates passed nodes as container members for the next
// epoch to be committed using [CommitContainerListUpdate]. Nodes must be
// grouped by selector index from placement policy (SELECT clauses). Results of
// the call operation can be received via [Nodes]. This method must be called
// only when a container list is changed, otherwise nothing should be done.
// Call must be signed by the Alphabet nodes.
func AddNextEpochNodes(cID interop.Hash256, placementVector uint8, publicKeys []interop.PublicKey) {
	if len(cID) != interop.Hash256Len {
		panic(cst.ErrorInvalidContainerID + ": length: " + std.Itoa10(len(cID)))
	}
	// nolint:staticcheck
	if placementVector >= maxNumOfREPs {
		panic(cst.ErrorTooBigNumberOfNodes + ": " + std.Itoa10(int(placementVector)))
	}
	ctx := storage.GetContext()
	validatePlacementIndex(ctx, cID, placementVector)

	common.CheckAlphabetWitness()

	commonPrefix := append([]byte{nextEpochNodesPrefix}, cID...)
	commonPrefix = append(commonPrefix, placementVector)

	for _, publicKey := range publicKeys {
		if len(publicKey) != interop.PublicKeyCompressedLen {
			panic(cst.ErrorInvalidPublicKey + ": length: " + std.Itoa10(len(publicKey)))
		}

		account := contract.CreateStandardAccount(publicKey)
		storageKey := append(commonPrefix, account...)
		storage.Put(ctx, storageKey, publicKey)
	}
}

func validatePlacementIndex(ctx storage.Context, cID interop.Hash256, inx uint8) {
	if inx == 0 {
		return
	}

	commonPrefix := append([]byte{nextEpochNodesPrefix}, cID...)
	iter := storage.Find(ctx, append(commonPrefix, inx-1), storage.None)
	if !iterator.Next(iter) {
		panic("invalid placement vector: " + std.Itoa10(int(inx-1)) + " index not found but " + std.Itoa10(int(inx)) + " requested")
	}
}

// VerifyPlacementSignatures verifies that message has been signed by container
// members according to container's placement policy: there should be at least
// REP number of signatures for every placement vector. sigs must be container's
// number of SELECTs length.
func VerifyPlacementSignatures(cid interop.Hash256, msg []byte, sigs [][]interop.Signature) bool {
	sigsLen := len(sigs)
	var i int
	repsI := ReplicasNumbers(cid)
repsLoop:
	for iterator.Next(repsI) {
		if sigsLen == i {
			return false
		}

		m := iterator.Value(repsI).(int)
		if len(sigs[i]) < m {
			return false
		}

		var counter int
		for _, sig := range sigs[i] {
			pubsI := Nodes(cid, uint8(i))
			for iterator.Next(pubsI) {
				pub := iterator.Value(pubsI).(interop.PublicKey)
				if crypto.VerifyWithECDsa(msg, pub, sig, crypto.Secp256r1Sha256) {
					counter++
					break
				}
			}

			if counter == m {
				i++
				continue repsLoop
			}
		}

		return false
	}

	return true
}

// CommitContainerListUpdate commits container list changes made by
// [AddNextEpochNodes] calls in advance. Replicas must correspond to
// ordered placement policy (REP clauses). If no [AddNextEpochNodes]
// have been made, it clears container list. Makes "ContainerUpdate"
// notification with container ID after successful list change.
// Call must be signed by the Alphabet nodes.
func CommitContainerListUpdate(cID interop.Hash256, replicas []uint8) {
	if len(cID) != interop.Hash256Len {
		panic(cst.ErrorInvalidContainerID + ": length: " + std.Itoa10(len(cID)))
	}

	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	oldNodesPrefix := append([]byte{nodesPrefix}, cID...)
	newNodesPrefix := append([]byte{nextEpochNodesPrefix}, cID...)
	replicasPrefix := append([]byte{replicasNumberPrefix}, cID...)

	var (
		spaceDiff int
		objsDiff  int
	)
	oldNodes := storage.Find(ctx, oldNodesPrefix, storage.None)
	for iterator.Next(oldNodes) {
		oldNode := iterator.Value(oldNodes).(struct{ k, v []byte })

		// check if node left network map (it does not belong to
		// any placement vector)
		var stillInNetmap bool
		newNodeKey := append([]byte{nextEpochNodesPrefix}, oldNode.k[1:]...)
		// nolint:intrange // Not supported by NeoGo
		for i := 0; i < len(replicas); i++ {
			newNodeKey[1+interop.Hash256Len] = uint8(i)
			stillInNetmap = storage.Get(ctx, newNodeKey) != nil
			if stillInNetmap {
				break
			}
		}

		if !stillInNetmap {
			reportKey := append([]byte{reportersPrefix}, oldNode.k[1:1+interop.Hash256Len]...)
			reportKey = append(reportKey, oldNode.k[1+interop.Hash256Len+1:]...)
			reportRaw := storage.Get(ctx, reportKey)
			if reportRaw != nil {
				report := std.Deserialize(reportRaw.([]byte)).(NodeReport)

				spaceDiff -= report.ContainerSize
				objsDiff -= report.NumberOfObjects

				storage.Delete(ctx, reportKey)
			}
		}

		storage.Delete(ctx, oldNode.k)
	}

	newNodes := storage.Find(ctx, newNodesPrefix, storage.None)
	for iterator.Next(newNodes) {
		newNode := iterator.Value(newNodes).(struct {
			key []byte
			val []byte
		})

		storage.Delete(ctx, newNode.key)

		newKey := append([]byte{nodesPrefix}, newNode.key[1:]...)
		storage.Put(ctx, newKey, newNode.val)
	}

	rr := storage.Find(ctx, replicasPrefix, storage.KeysOnly)
	for iterator.Next(rr) {
		oldReplicasNumber := iterator.Value(rr).([]byte)
		storage.Delete(ctx, oldReplicasNumber)
	}

	// nolint:gosimple // https://github.com/nspcc-dev/neo-go/issues/3608
	if replicas != nil {
		for i, replica := range replicas {
			if replica > maxNumOfREPs {
				panic(cst.ErrorTooBigNumberOfNodes + ": " + std.Itoa10(int(replica)))
			}

			storage.Put(ctx, append(replicasPrefix, uint8(i)), replica)
		}
	}

	// update reports stat after netmap change

	if spaceDiff != 0 || objsDiff != 0 {
		// summary update

		summaryKey := append([]byte{reportsSummary}, cID...)
		rSummary := std.Deserialize(storage.Get(ctx, summaryKey).([]byte)).(NodeReportSummary)
		rSummary.ContainerSize += spaceDiff
		rSummary.NumberOfObjects += objsDiff
		storage.Put(ctx, summaryKey, std.Serialize(rSummary))

		// user stat update

		owner := getOwnerByID(ctx, cID)
		totalUserKey := userTotalStorageKey(owner)
		oldVal := storage.Get(ctx, totalUserKey).(int)
		storage.Put(ctx, totalUserKey, oldVal+spaceDiff)
	}

	runtime.Notify("NodesUpdate", cID)
}

// ReplicasNumbers returns iterator over saved by [CommitContainerListUpdate]
// container's replicas from placement policy.
func ReplicasNumbers(cID interop.Hash256) iterator.Iterator {
	if len(cID) != interop.Hash256Len {
		panic(cst.ErrorInvalidContainerID + ": length: " + std.Itoa10(len(cID)))
	}

	ctx := storage.GetReadOnlyContext()

	return storage.Find(ctx, append([]byte{replicasNumberPrefix}, cID...), storage.ValuesOnly)
}

// Nodes returns iterator over members of the container. The list is handled
// by the Alphabet nodes and must be updated via [AddNextEpochNodes] and
// [CommitContainerListUpdate] calls.
func Nodes(cID interop.Hash256, placementVector uint8) iterator.Iterator {
	if len(cID) != interop.Hash256Len {
		panic(cst.ErrorInvalidContainerID + ": length: " + std.Itoa10(len(cID)))
	}

	ctx := storage.GetReadOnlyContext()
	key := append([]byte{nodesPrefix}, cID...)
	key = append(key, placementVector)

	return storage.Find(ctx, key, storage.ValuesOnly)
}

// SetEACL method sets a new extended ACL table related to the contract
// if it was invoked by Alphabet nodes of the Inner Ring. Otherwise, it produces
// setEACL notification.
//
// EACL should be a stable marshaled EACLTable structure from API. Protocol
// version and container reference must be set in 'version' and 'container_id'
// fields respectively.
// Signature is a RFC6979 signature of the Container.
// PublicKey contains the public key of the signer.
// Token is optional and should be a stable marshaled SessionToken structure from
// API.
//
// If the container doesn't exist, it panics with NotFoundError.
// Deprecated: use [PutEACL] instead.
func SetEACL(eACL []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte) {
	ctx := storage.GetContext()

	// V2 format
	// get container ID
	lnEACL := len(eACL)
	if lnEACL < 2 {
		panic("missing version field in eACL BLOB")
	}
	offset := int(eACL[1])
	offset = 2 + offset + 4
	if lnEACL < offset+containerIDSize {
		panic("missing container ID field in eACL BLOB")
	}
	containerID := eACL[offset : offset+containerIDSize]

	ownerID := getOwnerByID(ctx, containerID)
	if ownerID == nil {
		panic(cst.NotFoundError)
	}

	common.CheckAlphabetWitness()

	key := append(eACLPrefix, containerID...)

	storage.Put(ctx, key, eACL)

	runtime.Log("success")
	runtime.Notify("SetEACLSuccess", containerID, publicKey)
}

// PutEACL puts given eACL serialized according to the NeoFS API binary protocol
// for the container it is referenced to. Operation must be allowed in the
// container's basic ACL. If container does not exist, PutEACL throws
// [cst.NotFoundError] exception. On success, PutEACL throws 'EACLChanged'
// notification event.
//
// See [Create] for details.
func PutEACL(eACL []byte, invocScript, verifScript, sessionToken []byte) {
	common.CheckAlphabetWitness()

	if len(eACL) < 2 {
		panic("missing version field in eACL BLOB")
	}

	idOff := 2 + int(eACL[1]) + 4
	if len(eACL) < idOff+containerIDSize {
		panic("missing container ID field in eACL BLOB")
	}

	id := eACL[idOff : idOff+containerIDSize]
	ctx := storage.GetContext()

	if storage.Get(ctx, append([]byte{containerKeyPrefix}, id...)) == nil {
		panic(cst.NotFoundError)
	}

	storage.Put(ctx, append(eACLPrefix, id...), eACL)

	runtime.Notify("EACLChanged", interop.Hash256(id))
}

// EACL method returns a structure that contains a stable marshaled EACLTable structure,
// the signature, the public key of the extended ACL setter and a stable marshaled SessionToken
// structure if it was provided.
//
// If the container doesn't exist, it panics with NotFoundError.
// Deprecated: use [GetEACLData] instead.
func EACL(containerID []byte) ExtendedACL {
	ctx := storage.GetReadOnlyContext()

	ownerID := getOwnerByID(ctx, containerID)
	if ownerID == nil {
		panic(cst.NotFoundError)
	}

	return getEACL(ctx, containerID)
}

// GetEACLData returns binary of container eACL it was put with by the container
// ID.
//
// If the container is missing, GetEACLData throws [cst.NotFoundError]
// exception.
func GetEACLData(id []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	if storage.Get(ctx, append([]byte{containerKeyPrefix}, id...)) == nil {
		panic(cst.NotFoundError)
	}

	return storage.Get(ctx, append(eACLPrefix, id...)).([]byte)
}

// PutReport method saves container's state report in contract memory.
// It must be invoked only by Storage nodes that belong to reported container.
// This method checks witness based on the provided public key of the Storage
// node. sizeBytes is a total storage that is used by storage node for storing
// all marshaled objects that belong to the specified container. objsNumber is
// a total number of container's object storage node have.
//
// If the container doesn't exist, it panics with [cst.NotFoundError].
func PutReport(cid interop.Hash256, sizeBytes, objsNumber int, pubKey interop.PublicKey) {
	common.CheckWitness(pubKey)
	ctx := storage.GetContext()
	owner := getOwnerByID(ctx, cid)
	if owner == nil {
		panic(cst.NotFoundError)
	}

	if !nodeFromContainer(ctx, cid, pubKey) {
		panic("method must be invoked by storage node from container")
	}

	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	currEpoch := contract.Call(netmapContractAddr, "epoch", contract.ReadOnly).(int)

	var (
		nodeAcc           = contract.CreateStandardAccount(pubKey)
		storageDiff       int
		summaryKey        = append([]byte{reportsSummary}, cid...)
		reportsSummaryRaw = storage.Get(ctx, summaryKey)
		summary           NodeReportSummary
	)
	reportKey := append([]byte{reportersPrefix}, cid...)
	reportKey = append(reportKey, nodeAcc...)
	if reportsSummaryRaw != nil {
		summary = std.Deserialize(reportsSummaryRaw.([]byte)).(NodeReportSummary)
		reportRaw := storage.Get(ctx, reportKey)
		if reportRaw == nil {
			storageDiff = sizeBytes
			summary.ContainerSize += sizeBytes
			summary.NumberOfObjects += objsNumber

			report := NodeReport{
				PublicKey:       pubKey,
				NumberOfReports: 1,
				ContainerSize:   sizeBytes,
				NumberOfObjects: objsNumber,
				LastUpdateEpoch: currEpoch,
			}
			storage.Put(ctx, reportKey, std.Serialize(report))
		} else {
			report := std.Deserialize(reportRaw.([]byte)).(NodeReport)
			if report.LastUpdateEpoch == currEpoch && report.NumberOfReports == maxNumberOfReportsPerEpoch {
				panic("max number of reports (" + std.Itoa10(maxNumberOfReportsPerEpoch) + ") reached")
			}

			storageDiff = sizeBytes - report.ContainerSize

			summary.ContainerSize = summary.ContainerSize - report.ContainerSize + sizeBytes
			summary.NumberOfObjects = summary.NumberOfObjects - report.NumberOfObjects + objsNumber

			if report.LastUpdateEpoch == currEpoch {
				report.NumberOfReports += 1
			} else {
				report.LastUpdateEpoch = currEpoch
				report.NumberOfReports = 1
			}
			report.ContainerSize = sizeBytes
			report.NumberOfObjects = objsNumber

			storage.Put(ctx, reportKey, std.Serialize(report))
		}
	} else {
		storageDiff = sizeBytes
		summary = NodeReportSummary{
			ContainerSize:   sizeBytes,
			NumberOfObjects: objsNumber,
		}
		report := NodeReport{
			PublicKey:       pubKey,
			NumberOfReports: 1,
			ContainerSize:   sizeBytes,
			NumberOfObjects: objsNumber,
			LastUpdateEpoch: currEpoch,
		}
		storage.Put(ctx, reportKey, std.Serialize(report))
	}

	userSummaryKey := userTotalStorageKey(owner)
	v := storage.Get(ctx, userSummaryKey)
	if v == nil {
		storage.Put(ctx, userSummaryKey, storageDiff)
	} else {
		oldSize := v.(int)
		storage.Put(ctx, userSummaryKey, oldSize+storageDiff)
	}

	storage.Put(ctx, summaryKey, std.Serialize(summary))
	runtime.Log("saved container size report")
}

// GetTakenSpaceByUser returns total load space in all containers user owns.
// If user have no containers, it returns 0.
func GetTakenSpaceByUser(user []byte) int {
	ctx := storage.GetReadOnlyContext()
	v := storage.Get(ctx, userTotalStorageKey(user))
	if v == nil {
		return 0
	}

	return v.(int)
}

// GetNodeReportSummary method returns a sum of object count and occupied
// volume as reported by storage nodes provided via [PutReport].
//
// If the container doesn't exist, it panics with [cst.NotFoundError]. If no
// reports were claimed, it returns zero values.
func GetNodeReportSummary(cid interop.Hash256) NodeReportSummary {
	ctx := storage.GetReadOnlyContext()
	if getOwnerByID(ctx, cid) == nil {
		panic(cst.NotFoundError)
	}

	key := append([]byte{reportsSummary}, cid...)
	cnrSummaryRaw := storage.Get(ctx, key).([]byte)
	if cnrSummaryRaw == nil {
		return NodeReportSummary{}
	}

	return std.Deserialize(cnrSummaryRaw).(NodeReportSummary)
}

// GetReportByNode method returns the latest report, that node with pubKey
// reported for the specified container via [PutReport].
//
// If the container doesn't exist, it panics with [cst.NotFoundError]. If no
// reports were claimed, it returns zero values.
func GetReportByNode(cid interop.Hash256, pubKey interop.PublicKey) NodeReport {
	ctx := storage.GetReadOnlyContext()
	if getOwnerByID(ctx, cid) == nil {
		panic(cst.NotFoundError)
	}

	key := append([]byte{reportersPrefix}, cid...)
	key = append(key, contract.CreateStandardAccount(pubKey)...)
	reportRaw := storage.Get(ctx, key)
	if reportRaw != nil {
		return std.Deserialize(reportRaw.([]byte)).(NodeReport)
	}

	return NodeReport{}
}

// IterateReports method returns iterator over nodes' reports
// that were claimed for specified epoch and container.
func IterateReports(cid interop.Hash256) iterator.Iterator {
	if len(cid) != interop.Hash256Len {
		panic("invalid container id")
	}

	ctx := storage.GetReadOnlyContext()
	key := append([]byte{reportersPrefix}, cid...)

	return storage.Find(ctx, key, storage.ValuesOnly|storage.DeserializeValues)
}

// IterateAllReportSummaries method returns iterator over all total container sizes
// that have been registered for the specified epoch. Items returned from this iterator
// are key-value pairs with keys having container ID as a prefix and values being
// [NodeReportSummary] structures.
func IterateAllReportSummaries() iterator.Iterator {
	return storage.Find(storage.GetReadOnlyContext(), []byte{reportsSummary}, storage.RemovePrefix|storage.DeserializeValues)
}

// PutContainerSize method saves container size estimation in contract
// memory. It can be invoked only by Storage nodes from the network map. This method
// checks witness based on the provided public key of the Storage node.
//
// If the container doesn't exist, it panics with NotFoundError.
//
// Deprecated: method is no-op, use [PutReport] instead.
func PutContainerSize(epoch int, cid []byte, usedSize int, pubKey interop.PublicKey) {
	ctx := storage.GetContext()

	if getOwnerByID(ctx, cid) == nil {
		panic(cst.NotFoundError)
	}

	common.CheckWitness(pubKey)

	if !isStorageNode(ctx, epoch, pubKey) {
		panic("method must be invoked by storage node from network map")
	}

	runtime.Log("deprecated estimations call")
}

// GetContainerSize method returns the container ID and a slice of container
// estimations. Container estimation includes the public key of the Storage Node
// that registered estimation and value of estimation.
//
// Use the ID obtained from ListContainerSizes method. Estimations are removed
// from contract storage every epoch, see NewEpoch method; therefore, this method
// can return different results during different epochs.
//
// Deprecated: please use IterateContainerSizes API, this one is not convenient
// to use and limited in the number of items it can return. It will be removed in
// future versions.
//
// Deprecated: method always returns zero values, use [GetNodeReportSummary]
// or [GetReportByNode] instead.
func GetContainerSize(id []byte) ContainerSizes {
	if len(id) < len(estimateKeyPrefix)+containerIDSize ||
		string(id[:len(estimateKeyPrefix)]) != estimateKeyPrefix {
		panic("wrong estimation prefix")
	}

	return ContainerSizes{}
}

// ListContainerSizes method returns the IDs of container size estimations
// that have been registered for the specified epoch.
//
// Deprecated: please use IterateAllContainerSizes API, this one is not convenient
// to use and limited in the number of items it can return. It will be removed in
// future versions.
//
// Deprecated: method always returns empty array. Use [IterateAllEstimations] instead.
func ListContainerSizes(epoch int) [][]byte {
	return [][]byte{}
}

// IterateContainerSizes method returns iterator over specific container size
// estimations that have been registered for the specified epoch. The iterator items
// are Estimation structures.
//
// Deprecated: method iterates nothing, use [IterateReports] instead.
func IterateContainerSizes(epoch int, cid interop.Hash256) iterator.Iterator {
	if len(cid) != interop.Hash256Len {
		panic("wrong container id")
	}

	ctx := storage.GetReadOnlyContext()

	var buf any = epoch

	key := []byte(estimateKeyPrefix)
	key = append(key, buf.([]byte)...)
	key = append(key, cid...)

	return storage.Find(ctx, key, storage.ValuesOnly|storage.DeserializeValues)
}

// IterateAllContainerSizes method returns iterator over all container size estimations
// that have been registered for the specified epoch. Items returned from this iterator
// are key-value pairs with keys having container ID as a prefix and values being Estimation
// structures.
//
// Deprecated: method iterates nothing, use [IterateAllEstimations] instead.
func IterateAllContainerSizes(epoch int) iterator.Iterator {
	ctx := storage.GetReadOnlyContext()

	var buf any = epoch

	key := []byte(estimateKeyPrefix)
	key = append(key, buf.([]byte)...)

	return storage.Find(ctx, key, storage.RemovePrefix|storage.DeserializeValues)
}

// NewEpoch method does nothing.
func NewEpoch(epochNum int) {}

// SetSoftContainerQuota sets soft size quota that limits all space used for
// storing objects in cID (including object replicas). Non-positive size sets
// no limitation. After exceeding the limit nodes are instructed to warn only,
// without denial of service. Call must be signed by cID's owner. Limit can be
// changed with a repeated call. See also [SetHardContainerQuota].
// Panics if cID is incorrect or container does not exist.
func SetSoftContainerQuota(cID interop.Hash256, size int) {
	setContainerQuota(cID, size, false)
}

// SetHardContainerQuota sets hard size quota that limits all space used for
// storing objects in cID (including object replicas). Non-positive size sets
// no limitation. After exceeding the limit nodes will refuse any further PUTs.
// Call must be signed by cID's owner. Limit can be changed with a repeated
// call. See also [SetSoftContainerQuota].
// Panics if cID is incorrect or container does not exist.
func SetHardContainerQuota(cID interop.Hash256, size int) {
	setContainerQuota(cID, size, true)
}

func checkQuotaSigner(ctx storage.Context, userScriptHash []byte) {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	vRaw := contract.Call(netmapContractAddr, "config", contract.ReadOnly, cst.AlphabetManagesQuotasKey)
	if vRaw == nil || !vRaw.(bool) {
		common.CheckOwnerWitness(userScriptHash)
	} else {
		common.CheckAlphabetWitness()
	}
}

func setContainerQuota(cID interop.Hash256, size int, hard bool) {
	if len(cID) != interop.Hash256Len {
		panic("invalid container id")
	}
	ctx := storage.GetContext()
	ownerID := getOwnerByID(ctx, cID)
	if ownerID == nil {
		panic(cst.NotFoundError)
	}

	checkQuotaSigner(ctx, common.WalletToScriptHash(ownerID))

	var q Quota
	v := storage.Get(ctx, containerQuotaKey(cID))
	if v != nil {
		q = std.Deserialize(v.([]byte)).(Quota)
	}
	if hard {
		q.HardLimit = size
	} else {
		q.SoftLimit = size
	}

	storage.Put(ctx, containerQuotaKey(cID), std.Serialize(q))
	runtime.Notify("ContainerQuotaSet", cID, size, hard)
}

// ContainerQuota returns container size quota set by cID's owner. If no
// limitation has been set, returns empty values (both limitations are set
// to 0). Non-positive limitation must be treated as no limits.
// Panics if cID is incorrect or container does not exist.
func ContainerQuota(cID interop.Hash256) Quota {
	if len(cID) != interop.Hash256Len {
		panic("invalid container id")
	}
	ctx := storage.GetReadOnlyContext()
	ownerID := getOwnerByID(ctx, cID)
	if ownerID == nil {
		panic(cst.NotFoundError)
	}

	v := storage.Get(ctx, containerQuotaKey(cID))
	if v == nil {
		return Quota{}
	}

	q := std.Deserialize(v.([]byte))

	return q.(Quota)
}

// SetSoftUserQuota sets size quota that limits all space used for storing
// objects in all containers that belong to user (including object replicas).
// Non-positive size sets no limitation. After exceeding the limit nodes are
// instructed to warn only, without denial of service. Call must be signed
// by user. Limit can be changed with a repeated call. See also
// [SetHardUserQuota].
// Panics if user is incorrect.
func SetSoftUserQuota(user []byte, size int) {
	setUserQuota(user, size, false)
}

// SetHardUserQuota sets size quota that limits all space used for storing
// objects in all containers that belong to user (including object replicas).
// Non-positive size sets no limitation. After exceeding the limit nodes will
// refuse any further PUTs. Call must be signed by user. Limit can be changed
// with a repeated call. See also [SetSoftUserQuota].
// Panics if user is incorrect.
func SetHardUserQuota(user []byte, size int) {
	setUserQuota(user, size, true)
}

func setUserQuota(user []byte, size int, hard bool) {
	if len(user) != common.NeoFSUserAccountLength {
		panic("invalid user id")
	}
	ctx := storage.GetContext()

	checkQuotaSigner(ctx, common.WalletToScriptHash(user))

	var q Quota
	v := storage.Get(ctx, userQuotaKey(user))
	if v != nil {
		q = std.Deserialize(v.([]byte)).(Quota)
	}
	if hard {
		q.HardLimit = size
	} else {
		q.SoftLimit = size
	}

	storage.Put(ctx, userQuotaKey(user), std.Serialize(q))
	runtime.Notify("UserQuotaSet", user, size, hard)
}

// UserQuota returns user size quota for every container he owns. If no
// limitation has been set, returns empty values (both limitations are set
// to 0). Non-positive limitation must be treated as no limits.
// Panics if user is incorrect.
func UserQuota(user []byte) Quota {
	if len(user) != common.NeoFSUserAccountLength {
		panic("invalid user id")
	}

	ctx := storage.GetReadOnlyContext()
	v := storage.Get(ctx, userQuotaKey(user))
	if v == nil {
		return Quota{}
	}

	q := std.Deserialize(v.([]byte))

	return q.(Quota)
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

func addContainer(ctx storage.Context, id, owner, container []byte) {
	containerListKey := append([]byte{ownerKeyPrefix}, owner...)
	containerListKey = append(containerListKey, id...)
	storage.Put(ctx, containerListKey, id)

	idKey := append([]byte{containerKeyPrefix}, id...)
	storage.Put(ctx, idKey, container)
}

func removeContainer(ctx storage.Context, id []byte, owner []byte) {
	// reports stat update

	summaryKey := append([]byte{reportsSummary}, id...)
	summaryRaw := storage.Get(ctx, summaryKey)
	if summaryRaw != nil {
		rSummary := std.Deserialize(summaryRaw.([]byte)).(NodeReportSummary)
		diff := -rSummary.ContainerSize

		totalUserKey := userTotalStorageKey(owner)
		oldVal := storage.Get(ctx, totalUserKey).(int)
		if oldVal <= 0 {
			storage.Delete(ctx, totalUserKey)
		} else {
			storage.Put(ctx, totalUserKey, oldVal+diff)
		}
	}

	// indexes clean up

	containerListKey := append([]byte{ownerKeyPrefix}, owner...)
	containerListKey = append(containerListKey, id...)
	storage.Delete(ctx, containerListKey)

	deleteByPrefix(ctx, append([]byte{nodesPrefix}, id...))
	deleteByPrefix(ctx, append([]byte{nextEpochNodesPrefix}, id...))
	deleteByPrefix(ctx, append([]byte{replicasNumberPrefix}, id...))
	deleteByPrefix(ctx, append([]byte{reportersPrefix}, id...))
	storage.Delete(ctx, containerQuotaKey(id))
	storage.Delete(ctx, summaryKey)

	storage.Delete(ctx, append([]byte{containerKeyPrefix}, id...))
	storage.Delete(ctx, append([]byte{containersWithMetaPrefix}, id...))
	storage.Delete(ctx, append(eACLPrefix, id...))
	storage.Put(ctx, append([]byte{deletedKeyPrefix}, id...), []byte{})
}

func getEACL(ctx storage.Context, cid []byte) ExtendedACL {
	key := append(eACLPrefix, cid...)
	data := storage.Get(ctx, key)
	if data != nil {
		return ExtendedACL{Value: data.([]byte), Sig: interop.Signature{}, Pub: interop.PublicKey{}, Token: []byte{}}
	}

	return ExtendedACL{Value: []byte{}, Sig: interop.Signature{}, Pub: interop.PublicKey{}, Token: []byte{}}
}

func getContainer(ctx storage.Context, cid []byte) Container {
	data := storage.Get(ctx, append([]byte{containerKeyPrefix}, cid...))
	if data != nil {
		return Container{Value: data.([]byte), Sig: interop.Signature{}, Pub: interop.PublicKey{}, Token: []byte{}}
	}

	return Container{Value: []byte{}, Sig: interop.Signature{}, Pub: interop.PublicKey{}, Token: []byte{}}
}

func getOwnerByID(ctx storage.Context, cid []byte) []byte {
	container := getContainer(ctx, cid)
	if len(container.Value) == 0 {
		return nil
	}

	return ownerFromBinaryContainer(container.Value)
}

func ownerFromBinaryContainer(container []byte) []byte {
	// V2 format
	offset := int(container[1])
	offset = 2 + offset + 4              // version prefix + version size + owner prefix
	return container[offset : offset+25] // offset + size of owner
}

// isStorageNode looks into _previous_ epoch network map, because storage node
// announces container size estimation of the previous epoch.
func isStorageNode(ctx storage.Context, epoch int, key interop.PublicKey) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	return contract.Call(netmapContractAddr, "isStorageNode", contract.ReadOnly, key, epoch-1).(bool)
}

func nodeFromContainer(ctx storage.Context, cID interop.Hash256, key interop.PublicKey) bool {
	k := []byte{nodesPrefix}
	k = append(k, cID[:]...)

	it := storage.Find(ctx, k, storage.ValuesOnly)
	for iterator.Next(it) {
		pk := iterator.Value(it).(interop.PublicKey)
		if key.Equals(pk) {
			return true
		}
	}

	return false
}

func deleteByPrefix(ctx storage.Context, prefix []byte) {
	it := storage.Find(ctx, prefix, storage.KeysOnly)
	for iterator.Next(it) {
		storage.Delete(ctx, iterator.Value(it).([]byte))
	}
}

func containerQuotaKey(cID interop.Hash256) []byte {
	key := []byte{containerQuotaKeyPrefix}
	key = append(key, cID...)

	return key
}

func userQuotaKey(user []byte) []byte {
	key := []byte{userQuotaKeyPrefix}
	key = append(key, user...)

	return key
}

func userTotalStorageKey(user []byte) []byte {
	key := append([]byte{userLoadKeyPrefix}, user...)

	return key
}
