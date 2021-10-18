package container

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

type (
	storageNode struct {
		info []byte
	}

	Container struct {
		value []byte
		sig   interop.Signature
		pub   interop.PublicKey
		token []byte
	}

	ExtendedACL struct {
		value []byte
		sig   interop.Signature
		pub   interop.PublicKey
		token []byte
	}

	estimation struct {
		from interop.PublicKey
		size int
	}

	containerSizes struct {
		cid         []byte
		estimations []estimation
	}
)

const (
	neofsIDContractKey = "identityScriptHash"
	balanceContractKey = "balanceScriptHash"
	netmapContractKey  = "netmapScriptHash"
	nnsContractKey     = "nnsScriptHash"
	nnsRootKey         = "nnsRoot"
	nnsHasAliasKey     = "nnsHasAlias"
	notaryDisabledKey  = "notary"

	// RegistrationFeeKey is a key in netmap config which contains fee for container registration.
	RegistrationFeeKey = "ContainerFee"
	// AliasFeeKey is a key in netmap config which contains fee for nice-name registration.
	AliasFeeKey = "ContainerAliasFee"

	containerIDSize = 32 // SHA256 size

	estimateKeyPrefix   = "cnr"
	estimatePostfixSize = 10
	cleanupDelta        = 3
)

var (
	eACLPrefix = []byte("eACL")
)

// OnNEP11Payment is needed for registration with contract as owner to work.
func OnNEP11Payment(a interop.Hash160, b int, c []byte, d interface{}) {
}

func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	args := data.([]interface{})
	notaryDisabled := args[0].(bool)
	addrNetmap := args[1].(interop.Hash160)
	addrBalance := args[2].(interop.Hash160)
	addrID := args[3].(interop.Hash160)
	addrNNS := args[4].(interop.Hash160)
	nnsRoot := args[5].(string)

	if isUpdate {
		registerNiceNameTLD(addrNNS, nnsRoot)
		return
	}

	if len(addrNetmap) != 20 || len(addrBalance) != 20 || len(addrID) != 20 {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, addrNetmap)
	storage.Put(ctx, balanceContractKey, addrBalance)
	storage.Put(ctx, neofsIDContractKey, addrID)
	storage.Put(ctx, nnsContractKey, addrNNS)
	storage.Put(ctx, nnsRootKey, nnsRoot)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, notaryDisabled)
	if notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("container contract notary disabled")
	}

	// add NNS root for container alias domains
	registerNiceNameTLD(addrNNS, nnsRoot)

	runtime.Log("container contract initialized")
}

func registerNiceNameTLD(addrNNS interop.Hash160, nnsRoot string) {
	iter := contract.Call(addrNNS, "roots", contract.ReadStates).(iterator.Iterator)
	for iterator.Next(iter) {
		if iterator.Value(iter).(string) == nnsRoot {
			runtime.Log("NNS root is already registered: " + nnsRoot)
			return
		}
	}
	contract.Call(addrNNS, "addRoot", contract.All, nnsRoot)
}

// Update method updates contract source code and manifest. Can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update", contract.All, script, manifest, data)
	runtime.Log("container contract updated")
}

// Put method creates new container if it was invoked by Alphabet nodes
// of the Inner Ring. Otherwise it produces containerPut notification.
//
// Container should be stable marshaled Container structure from API.
// Signature is a RFC6979 signature of Container.
// PublicKey contains public key of the signer.
// Token is optional and should be stable marshaled SessionToken structure from
// API.
func Put(container []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte) {
	PutNamed(container, signature, publicKey, token, "", "")
}

// PutNamed is similar to put but also sets a TXT record in nns contract.
// Note that zone must exist.
func PutNamed(container []byte, signature interop.Signature,
	publicKey interop.PublicKey, token []byte,
	name, zone string) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	ownerID := ownerFromBinaryContainer(container)
	containerID := crypto.Sha256(container)
	neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).(interop.Hash160)
	cnr := Container{
		value: container,
		sig:   signature,
		pub:   publicKey,
		token: token,
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
	containerFee := contract.Call(netmapContractAddr, "config", contract.ReadOnly, RegistrationFeeKey).(int)
	balance := contract.Call(balanceContractAddr, "balanceOf", contract.ReadOnly, from).(int)
	if name != "" {
		aliasFee := contract.Call(netmapContractAddr, "config", contract.ReadOnly, AliasFeeKey).(int)
		containerFee += aliasFee
	}

	if balance < containerFee*len(alphabet) {
		panic("insufficient balance to create container")
	}

	if notaryDisabled {
		nodeKey := common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			runtime.Notify("containerPut", container, signature, publicKey, token)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{container, signature, publicKey}, []byte("put"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("put: alphabet witness check failed")
		}
	}
	// todo: check if new container with unique container id

	details := common.ContainerFeeTransferDetails(containerID)

	for i := 0; i < len(alphabet); i++ {
		node := alphabet[i]
		to := contract.CreateStandardAccount(node.PublicKey)

		contract.Call(balanceContractAddr, "transferX",
			contract.All,
			from,
			to,
			containerFee,
			details,
		)
	}

	addContainer(ctx, containerID, ownerID, cnr)

	if name != "" {
		if needRegister {
			const (
				defaultRefresh = 3600   // 1 hour
				defaultRetry   = 600    // 10 min
				defaultExpire  = 604800 // 1 week
				defaultTTL     = 3600   // 1 hour
			)
			res := contract.Call(nnsContractAddr, "register", contract.All,
				domain, runtime.GetExecutingScriptHash(), "ops@nspcc.ru",
				defaultRefresh, defaultRetry, defaultExpire, defaultTTL).(bool)
			if !res {
				panic("can't register the domain " + domain)
			}
		}
		contract.Call(nnsContractAddr, "addRecord", contract.All,
			domain, 16 /* TXT */, std.Base58Encode(containerID))

		key := append([]byte(nnsHasAliasKey), containerID...)
		storage.Put(ctx, key, domain)
	}

	if len(token) == 0 { // if container created directly without session
		contract.Call(neofsIDContractAddr, "addKey", contract.All, ownerID, [][]byte{publicKey})
	}

	runtime.Log("put: added new container")
}

// checkNiceNameAvailable checks if nice name is available for the container.
// It panics if the name is taken. Returned value specifies if new domain registration is needed.
func checkNiceNameAvailable(nnsContractAddr interop.Hash160, domain string) bool {
	isAvail := contract.Call(nnsContractAddr, "isAvailable",
		contract.ReadStates|contract.AllowCall, domain).(bool)
	if isAvail {
		return true
	}

	owner := contract.Call(nnsContractAddr, "ownerOf",
		contract.ReadStates|contract.AllowCall, domain).(string)
	if string(owner) != string(common.CommitteeAddress()) {
		panic("committee must own registered domain")
	}

	res := contract.Call(nnsContractAddr, "getRecords",
		contract.ReadStates|contract.AllowCall, domain, 16 /* TXT */)
	if res != nil {
		panic("name is already taken")
	}

	return false
}

// Delete method removes container from contract storage if it was
// invoked by Alphabet nodes of the Inner Ring. Otherwise it produces
// containerDelete notification.
//
// Signature is a RFC6979 signature of container ID.
// Token is optional and should be stable marshaled SessionToken structure from
// API.
func Delete(containerID []byte, signature interop.Signature, token []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		return
	}

	if notaryDisabled {
		alphabet := common.AlphabetNodes()
		nodeKey := common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			runtime.Notify("containerDelete", containerID, signature, token)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{containerID, signature}, []byte("delete"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("delete: alphabet witness check failed")
		}
	}

	key := append([]byte(nnsHasAliasKey), containerID...)
	domain := storage.Get(ctx, key).(string)
	if len(domain) != 0 {
		storage.Delete(ctx, key)
		// We should do `getRecord` first because NNS record could be deleted
		// by other means (expiration, manual) thus leading to failing `deleteRecord`
		// and inability to delete container. We should also check that we own the record in case.
		nnsContractAddr := storage.Get(ctx, nnsContractKey).(interop.Hash160)
		res := contract.Call(nnsContractAddr, "getRecords", contract.ReadStates|contract.AllowCall, domain, 16 /* TXT */)
		if res != nil && std.Base58Encode(containerID) == string(res.([]interface{})[0].(string)) {
			contract.Call(nnsContractAddr, "deleteRecords", contract.All, domain, 16 /* TXT */)
		}
	}
	removeContainer(ctx, containerID, ownerID)
	runtime.Log("delete: remove container")
}

// Get method returns structure that contains stable marshaled Container structure,
// signature, public key of the container creator and stable marshaled SessionToken
// structure if it was provided.
func Get(containerID []byte) Container {
	ctx := storage.GetReadOnlyContext()
	return getContainer(ctx, containerID)
}

// Owner method returns 25 byte Owner ID of the container.
func Owner(containerID []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	return getOwnerByID(ctx, containerID)
}

// List method returns list of all container IDs owned by specified owner.
func List(owner []byte) [][]byte {
	ctx := storage.GetReadOnlyContext()

	if len(owner) == 0 {
		return getAllContainers(ctx)
	}

	var list [][]byte

	it := storage.Find(ctx, owner, storage.ValuesOnly)
	for iterator.Next(it) {
		id := iterator.Value(it).([]byte)
		list = append(list, id)
	}

	return list
}

// SetEACL method sets new extended ACL table related to the contract
// if it was invoked by Alphabet nodes of the Inner Ring. Otherwise it produces
// setEACL notification.
//
// EACL should be stable marshaled EACLTable structure from API.
// Signature is a RFC6979 signature of Container.
// PublicKey contains public key of the signer.
// Token is optional and should be stable marshaled SessionToken structure from
// API.
func SetEACL(eACL []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	// get container ID
	offset := int(eACL[1])
	offset = 2 + offset + 4
	containerID := eACL[offset : offset+32]

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("container does not exist")
	}

	if notaryDisabled {
		alphabet := common.AlphabetNodes()
		nodeKey := common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			runtime.Notify("setEACL", eACL, signature, publicKey, token)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{eACL}, []byte("setEACL"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("setEACL: alphabet witness check failed")
		}
	}

	rule := ExtendedACL{
		value: eACL,
		sig:   signature,
		pub:   publicKey,
		token: token,
	}

	key := append(eACLPrefix, containerID...)

	common.SetSerialized(ctx, key, rule)

	runtime.Log("setEACL: success")
}

// EACL method returns structure that contains stable marshaled EACLTable structure,
// signature, public key of the extended ACL setter and stable marshaled SessionToken
// structure if it was provided.
func EACL(containerID []byte) ExtendedACL {
	ctx := storage.GetReadOnlyContext()

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("container does not exist")
	}

	return getEACL(ctx, containerID)
}

// PutContainerSize method saves container size estimation in contract
// memory. Can be invoked only by Storage nodes from the network map. Method
// checks witness based on the provided public key of the Storage node.
func PutContainerSize(epoch int, cid []byte, usedSize int, pubKey interop.PublicKey) {
	ctx := storage.GetContext()

	if !runtime.CheckWitness(pubKey) {
		panic("invalid witness of container size estimation")
	}

	if !isStorageNode(ctx, pubKey) {
		panic("putContainerSize method must be invoked by storage node from network map")
	}

	key := estimationKey(epoch, cid, pubKey)

	s := estimation{
		from: pubKey,
		size: usedSize,
	}

	storage.Put(ctx, key, std.Serialize(s))

	runtime.Log("container: saved container size estimation")
}

// GetContainerSize method returns container ID and slice of container
// estimations. Container estimation includes public key of the Storage Node
// that registered  estimation and value of estimation.
//
// Use ID obtained from ListContainerSizes method. Estimations are removed
// from contract storage every epoch, see NewEpoch method, therefore method
// can return different results in different epochs.
func GetContainerSize(id []byte) containerSizes {
	ctx := storage.GetReadOnlyContext()

	// this `id` expected to be from `ListContainerSizes`
	// therefore it is not contains postfix, we ignore it in the cut.
	ln := len(id)
	cid := id[ln-containerIDSize : ln]

	return getContainerSizeEstimation(ctx, id, cid)
}

// ListContainerSizes method returns IDs of container size estimations
// that has been registered for specified epoch.
func ListContainerSizes(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var buf interface{} = epoch

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

// NewEpoch method removes all container size estimations from epoch older than
// epochNum + 3. Can be invoked only by NewEpoch method of the Netmap contract.
func NewEpoch(epochNum int) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	if notaryDisabled {
		indirectCall := common.FromKnownContract(
			ctx,
			runtime.GetCallingScriptHash(),
			netmapContractKey,
		)
		if !indirectCall {
			panic("newEpoch method must be invoked by inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("newEpoch method must be invoked by inner ring")
		}
	}

	candidates := keysToDelete(ctx, epochNum)
	for _, candidate := range candidates {
		storage.Delete(ctx, candidate)
	}
}

// StartContainerEstimation method produces StartEstimation notification.
// Can be invoked only by Alphabet nodes of the Inner Ring.
func StartContainerEstimation(epoch int) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("startContainerEstimation method must be invoked by inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("startContainerEstimation method must be invoked by inner ring")
		}
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{epoch}, []byte("startEstimation"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	runtime.Notify("StartEstimation", epoch)
	runtime.Log("startEstimation: notification has been produced")
}

// StopContainerEstimation method produces StopEstimation notification.
// Can be invoked only by Alphabet nodes of the Inner Ring.
func StopContainerEstimation(epoch int) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("stopContainerEstimation method must be invoked by inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("stopContainerEstimation method must be invoked by inner ring")
		}
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{epoch}, []byte("stopEstimation"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	runtime.Notify("StopEstimation", epoch)
	runtime.Log("stopEstimation: notification has been produced")
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}

func addContainer(ctx storage.Context, id, owner []byte, container Container) {
	containerListKey := append(owner, id...)
	storage.Put(ctx, containerListKey, id)

	common.SetSerialized(ctx, id, container)
}

func removeContainer(ctx storage.Context, id []byte, owner []byte) {
	containerListKey := append(owner, id...)
	storage.Delete(ctx, containerListKey)

	storage.Delete(ctx, id)
}

func getAllContainers(ctx storage.Context) [][]byte {
	var list [][]byte

	it := storage.Find(ctx, []byte{}, storage.KeysOnly)
	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // it MUST BE `storage.KeysOnly`
		if len(key) == containerIDSize {
			list = append(list, key)
		}
	}

	return list
}

func getEACL(ctx storage.Context, cid []byte) ExtendedACL {
	key := append(eACLPrefix, cid...)
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).(ExtendedACL)
	}

	return ExtendedACL{value: []byte{}, sig: interop.Signature{}, pub: interop.PublicKey{}, token: []byte{}}
}

func getContainer(ctx storage.Context, cid []byte) Container {
	data := storage.Get(ctx, cid)
	if data != nil {
		return std.Deserialize(data.([]byte)).(Container)
	}

	return Container{value: []byte{}, sig: interop.Signature{}, pub: interop.PublicKey{}, token: []byte{}}
}

func getOwnerByID(ctx storage.Context, cid []byte) []byte {
	container := getContainer(ctx, cid)
	if len(container.value) == 0 {
		return nil
	}

	return ownerFromBinaryContainer(container.value)
}

func ownerFromBinaryContainer(container []byte) []byte {
	offset := int(container[1])
	offset = 2 + offset + 4              // version prefix + version size + owner prefix
	return container[offset : offset+25] // offset + size of owner
}

func estimationKey(epoch int, cid []byte, key interop.PublicKey) []byte {
	var buf interface{} = epoch

	hash := crypto.Ripemd160(key)

	result := []byte(estimateKeyPrefix)
	result = append(result, buf.([]byte)...)
	result = append(result, cid...)

	return append(result, hash[:estimatePostfixSize]...)
}

func getContainerSizeEstimation(ctx storage.Context, key, cid []byte) containerSizes {
	var estimations []estimation

	it := storage.Find(ctx, key, storage.ValuesOnly)
	for iterator.Next(it) {
		rawEstimation := iterator.Value(it).([]byte)
		est := std.Deserialize(rawEstimation).(estimation)
		estimations = append(estimations, est)
	}

	return containerSizes{
		cid:         cid,
		estimations: estimations,
	}
}

// isStorageNode looks into _previous_ epoch network map, because storage node
// announce container size estimation of previous epoch.
func isStorageNode(ctx storage.Context, key interop.PublicKey) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	snapshot := contract.Call(netmapContractAddr, "snapshot", contract.ReadOnly, 1).([]storageNode)

	for i := range snapshot {
		nodeInfo := snapshot[i].info
		nodeKey := nodeInfo[2:35] // offset:2, len:33

		if common.BytesEqual(key, nodeKey) {
			return true
		}
	}

	return false
}

func keysToDelete(ctx storage.Context, epoch int) [][]byte {
	results := [][]byte{}

	it := storage.Find(ctx, []byte(estimateKeyPrefix), storage.KeysOnly)
	for iterator.Next(it) {
		k := iterator.Value(it).([]byte)
		nbytes := k[len(estimateKeyPrefix) : len(k)-containerIDSize-estimatePostfixSize]

		var n interface{} = nbytes

		if epoch-n.(int) > cleanupDelta {
			results = append(results, k)
		}
	}

	return results
}
