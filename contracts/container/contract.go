package container

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
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

	ContainerSizes struct {
		CID         []byte
		Estimations []Estimation
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

	singleEstimatePrefix     = "est"
	estimateKeyPrefix        = "cnr"
	containersWithMetaPrefix = 'm'
	containerKeyPrefix       = 'x'
	ownerKeyPrefix           = 'o'
	deletedKeyPrefix         = 'd'
	nodesPrefix              = 'n'
	replicasNumberPrefix     = 'r'
	nextEpochNodesPrefix     = 'u'
	estimatePostfixSize      = 10

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

	common.SubscribeForNewEpoch()

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

	storage.Delete(ctx, cnrItemKey)
	storage.Delete(ctx, append(append([]byte{ownerKeyPrefix}, owner...), id...))
	storage.Delete(ctx, append(eACLPrefix, id...))
	storage.Delete(ctx, append([]byte{containersWithMetaPrefix}, id...))

	deleteByPrefix(ctx, append([]byte{nodesPrefix}, id...))
	deleteByPrefix(ctx, append([]byte{nextEpochNodesPrefix}, id...))
	deleteByPrefix(ctx, append([]byte{replicasNumberPrefix}, id...))

	domainKey := append([]byte(nnsHasAliasKey), id...)
	if domain := storage.Get(ctx, domainKey).(string); len(domain) != 0 { // != "" not working
		storage.Delete(ctx, domainKey)
		deleteNNSRecords(ctx, domain)
	}

	storage.Put(ctx, append([]byte{deletedKeyPrefix}, id...), []byte{})

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

	counter := 0
	c := storage.Find(ctx, commonPrefix, storage.RemovePrefix|storage.KeysOnly|storage.Backwards)
	if iterator.Next(c) {
		counter = counterFromBytes(iterator.Value(c).([]byte))
	}

	for _, publicKey := range publicKeys {
		if len(publicKey) != interop.PublicKeyCompressedLen {
			panic(cst.ErrorInvalidPublicKey + ": length: " + std.Itoa10(len(publicKey)))
		}

		counter++

		storageKey := append(commonPrefix, counterToBytes(counter)...)
		storage.Put(ctx, storageKey, publicKey)
	}
}

func counterToBytes(counter int) []byte {
	var anyCounter any = counter
	res := anyCounter.([]byte)

	switch len(res) {
	case 0:
		return []byte{0, 0}
	case 1:
		return []byte{0, res[0]}
	default:
		// only 2 bytes are expected, it should be ensured on the upper levels
	}

	// BE for correct sorting
	first := res[0]
	res[0] = res[1]
	res[1] = first

	return res
}

func counterFromBytes(counter []byte) int {
	first := counter[0]
	counter[0] = counter[1]
	counter[1] = first

	var anyCounter any = counter

	return anyCounter.(int)
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

	oldNodes := storage.Find(ctx, oldNodesPrefix, storage.KeysOnly)
	for iterator.Next(oldNodes) {
		oldNode := iterator.Value(oldNodes).(string)
		storage.Delete(ctx, oldNode)
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

// PutContainerSize method saves container size estimation in contract
// memory. It can be invoked only by Storage nodes from the network map. This method
// checks witness based on the provided public key of the Storage node.
//
// If the container doesn't exist, it panics with NotFoundError.
func PutContainerSize(epoch int, cid []byte, usedSize int, pubKey interop.PublicKey) {
	ctx := storage.GetContext()

	if getOwnerByID(ctx, cid) == nil {
		panic(cst.NotFoundError)
	}

	common.CheckWitness(pubKey)

	if !isStorageNode(ctx, pubKey) {
		panic("method must be invoked by storage node from network map")
	}

	key := estimationKey(epoch, cid, pubKey)

	s := Estimation{
		From: pubKey,
		Size: usedSize,
	}

	storage.Put(ctx, key, std.Serialize(s))
	updateEstimations(ctx, epoch, cid, pubKey, false)

	runtime.Log("saved container size estimation")
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
func GetContainerSize(id []byte) ContainerSizes {
	ctx := storage.GetReadOnlyContext()

	if len(id) < len(estimateKeyPrefix)+containerIDSize ||
		string(id[:len(estimateKeyPrefix)]) != estimateKeyPrefix {
		panic("wrong estimation prefix")
	}

	// V2 format
	// this `id` expected to be from `ListContainerSizes`
	// therefore it is not contains postfix, we ignore it in the cut.
	ln := len(id)
	cid := id[ln-containerIDSize : ln]

	return getContainerSizeEstimation(ctx, id, cid)
}

// ListContainerSizes method returns the IDs of container size estimations
// that have been registered for the specified epoch.
//
// Deprecated: please use IterateAllContainerSizes API, this one is not convenient
// to use and limited in the number of items it can return. It will be removed in
// future versions.
func ListContainerSizes(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var buf any = epoch

	key := []byte(estimateKeyPrefix)
	key = append(key, buf.([]byte)...)

	it := storage.Find(ctx, key, storage.KeysOnly)

	uniq := map[string]struct{}{}

	for iterator.Next(it) {
		storageKey := iterator.Value(it).([]byte)

		ln := len(storageKey)
		storageKey = storageKey[:ln-estimatePostfixSize]

		uniq[string(storageKey)] = struct{}{}
	}

	var result [][]byte

	for k := range uniq {
		result = append(result, []byte(k))
	}

	return result
}

// IterateContainerSizes method returns iterator over specific container size
// estimations that have been registered for the specified epoch. The iterator items
// are Estimation structures.
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
func IterateAllContainerSizes(epoch int) iterator.Iterator {
	ctx := storage.GetReadOnlyContext()

	var buf any = epoch

	key := []byte(estimateKeyPrefix)
	key = append(key, buf.([]byte)...)

	return storage.Find(ctx, key, storage.RemovePrefix|storage.DeserializeValues)
}

// NewEpoch method removes all container size estimations from epoch older than
// epochNum + 3. It can be invoked only by NewEpoch method of the Netmap contract.
func NewEpoch(epochNum int) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	cleanupContainers(ctx, epochNum)
}

// StartContainerEstimation method produces StartEstimation notification.
// It can be invoked only by Alphabet nodes of the Inner Ring.
func StartContainerEstimation(epoch int) {
	common.CheckAlphabetWitness()

	runtime.Notify("StartEstimation", epoch)
	runtime.Log("notification has been produced")
}

// StopContainerEstimation method produces StopEstimation notification.
// It can be invoked only by Alphabet nodes of the Inner Ring.
func StopContainerEstimation(epoch int) {
	common.CheckAlphabetWitness()

	runtime.Notify("StopEstimation", epoch)
	runtime.Log("notification has been produced")
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
	containerListKey := append([]byte{ownerKeyPrefix}, owner...)
	containerListKey = append(containerListKey, id...)
	storage.Delete(ctx, containerListKey)

	deleteByPrefix(ctx, append([]byte{nodesPrefix}, id...))
	deleteByPrefix(ctx, append([]byte{nextEpochNodesPrefix}, id...))
	deleteByPrefix(ctx, append([]byte{replicasNumberPrefix}, id...))

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

func estimationKey(epoch int, cid []byte, key interop.PublicKey) []byte {
	var buf any = epoch

	hash := crypto.Ripemd160(key)

	result := []byte(estimateKeyPrefix)
	result = append(result, buf.([]byte)...)
	result = append(result, cid...)

	return append(result, hash[:estimatePostfixSize]...)
}

func getContainerSizeEstimation(ctx storage.Context, key, cid []byte) ContainerSizes {
	var estimations []Estimation

	it := storage.Find(ctx, key, storage.ValuesOnly|storage.DeserializeValues)
	for iterator.Next(it) {
		est := iterator.Value(it).(Estimation)
		estimations = append(estimations, est)
	}

	return ContainerSizes{
		CID:         cid,
		Estimations: estimations,
	}
}

// isStorageNode looks into _previous_ epoch network map, because storage node
// announces container size estimation of the previous epoch.
func isStorageNode(ctx storage.Context, key interop.PublicKey) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	epoch := contract.Call(netmapContractAddr, "epoch", contract.ReadOnly).(int)
	return contract.Call(netmapContractAddr, "isStorageNode", contract.ReadOnly, key, epoch-1).(bool)
}

func updateEstimations(ctx storage.Context, epoch int, cid []byte, pub interop.PublicKey, isUpdate bool) {
	h := crypto.Ripemd160(pub)
	estKey := append([]byte(singleEstimatePrefix), cid...)
	estKey = append(estKey, h...)

	var newEpochs []int
	rawList := storage.Get(ctx, estKey).([]byte)

	if rawList != nil {
		epochs := std.Deserialize(rawList).([]int)
		for _, oldEpoch := range epochs {
			if !isUpdate && epoch-oldEpoch > cst.CleanupDelta {
				key := append([]byte(estimateKeyPrefix), convert.ToBytes(oldEpoch)...)
				key = append(key, cid...)
				key = append(key, h[:estimatePostfixSize]...)
				storage.Delete(ctx, key)
			} else {
				newEpochs = append(newEpochs, oldEpoch)
			}
		}
	}

	newEpochs = append(newEpochs, epoch)
	common.SetSerialized(ctx, estKey, newEpochs)
}

func cleanupContainers(ctx storage.Context, epoch int) {
	it := storage.Find(ctx, []byte(estimateKeyPrefix), storage.KeysOnly)
	for iterator.Next(it) {
		k := iterator.Value(it).([]byte)
		// V2 format
		nbytes := k[len(estimateKeyPrefix) : len(k)-containerIDSize-estimatePostfixSize]

		var n any = nbytes

		if epoch-n.(int) > cst.TotalCleanupDelta {
			storage.Delete(ctx, k)
		}
	}
}

func deleteByPrefix(ctx storage.Context, prefix []byte) {
	it := storage.Find(ctx, prefix, storage.KeysOnly)
	for iterator.Next(it) {
		storage.Delete(ctx, iterator.Value(it).([]byte))
	}
}
