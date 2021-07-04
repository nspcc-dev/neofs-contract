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
	version = 1

	neofsIDContractKey = "identityScriptHash"
	balanceContractKey = "balanceScriptHash"
	netmapContractKey  = "netmapScriptHash"
	notaryDisabledKey  = "notary"

	containerFeeKey = "ContainerFee"

	containerIDSize = 32 // SHA256 size

	estimateKeyPrefix   = "cnr"
	estimatePostfixSize = 10
	cleanupDelta        = 3
)

var (
	eACLPrefix = []byte("eACL")
)

func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	if isUpdate {
		migrateContainerLists(ctx)    // from v0.9.1 to v0.9.2
		migrateEstimationStorage(ctx) // from v0.9.1 to v0.9.2
		return
	}

	args := data.([]interface{})
	notaryDisabled := args[0].(bool)
	owner := args[1].(interop.Hash160)
	addrNetmap := args[2].(interop.Hash160)
	addrBalance := args[3].(interop.Hash160)
	addrID := args[4].(interop.Hash160)

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	if len(addrNetmap) != 20 || len(addrBalance) != 20 || len(addrID) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, common.OwnerKey, owner)
	storage.Put(ctx, netmapContractKey, addrNetmap)
	storage.Put(ctx, balanceContractKey, addrBalance)
	storage.Put(ctx, neofsIDContractKey, addrID)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, notaryDisabled)
	if notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("container contract notary disabled")
	}

	runtime.Log("container contract initialized")
}

func migrateContainerLists(ctx storage.Context) {
	const ownersKey = "ownersList"

	containers := getAllContainers(ctx)
	for i := range containers {
		containerID := containers[i]
		ownerID := getOwnerByID(ctx, containerID)

		containerListKey := append(ownerID, containerID...)
		storage.Put(ctx, containerListKey, containerID)

		storage.Delete(ctx, ownerID)
	}

	storage.Delete(ctx, ownersKey)
}

func migrateEstimationStorage(ctx storage.Context) {
	// In fact, this method does not migrate estimation storage because this data
	// is valid only for one epoch. Migration routine will be quite complex, so
	// it makes sense to clean all remaining estimations and wait for new one.

	it := storage.Find(ctx, []byte(estimateKeyPrefix), storage.KeysOnly)
	for iterator.Next(it) {
		key := iterator.Value(it).([]byte)
		storage.Delete(ctx, key)
	}
}

func Migrate(script []byte, manifest []byte, data interface{}) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	contract.Call(interop.Hash160(management.Hash), "update", contract.All, script, manifest, data)
	runtime.Log("container contract updated")

	return true
}

func Put(container []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte) {
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

	var ( // for invocation collection without notary
		alphabet     = common.AlphabetNodes()
		nodeKey      []byte
		alphabetCall bool
	)

	if notaryDisabled {
		nodeKey = common.InnerRingInvoker(alphabet)
		alphabetCall = len(nodeKey) != 0
	} else {
		multiaddr := common.AlphabetAddress()
		alphabetCall = runtime.CheckWitness(multiaddr)
	}

	from := common.WalletToScriptHash(ownerID)
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	balanceContractAddr := storage.Get(ctx, balanceContractKey).(interop.Hash160)
	containerFee := contract.Call(netmapContractAddr, "config", contract.ReadOnly, containerFeeKey).(int)
	balance := contract.Call(balanceContractAddr, "balanceOf", contract.ReadOnly, from).(int)
	details := common.ContainerFeeTransferDetails(containerID)

	if !alphabetCall {
		if balance < containerFee*len(alphabet) {
			panic("insufficient balance to create container")
		}
		runtime.Notify("containerPut", container, signature, publicKey, token)
		return
	}
	// todo: check if new container with unique container id

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{container, signature, publicKey}, []byte("put"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

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

	if len(token) == 0 { // if container created directly without session
		contract.Call(neofsIDContractAddr, "addKey", contract.All, ownerID, [][]byte{publicKey})
	}

	runtime.Log("put: added new container")
}

func Delete(containerID []byte, signature interop.Signature, token []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("delete: container does not exist")
	}

	var ( // for invocation collection without notary
		alphabet     []common.IRNode
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
		runtime.Notify("containerDelete", containerID, signature, token)
		return
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{containerID, signature}, []byte("delete"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	removeContainer(ctx, containerID, ownerID)
	runtime.Log("delete: remove container")
}

func Get(containerID []byte) Container {
	ctx := storage.GetReadOnlyContext()
	return getContainer(ctx, containerID)
}

func Owner(containerID []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	return getOwnerByID(ctx, containerID)
}

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

func SetEACL(eACL []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	// get container ID
	offset := int(eACL[1])
	offset = 2 + offset + 4
	containerID := eACL[offset : offset+32]

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("setEACL: container does not exists")
	}

	var ( // for invocation collection without notary
		alphabet     []common.IRNode
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
		runtime.Notify("setEACL", eACL, signature, publicKey, token)
		return
	}

	rule := ExtendedACL{
		value: eACL,
		sig:   signature,
		pub:   publicKey,
		token: token,
	}

	key := append(eACLPrefix, containerID...)

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{eACL}, []byte("setEACL"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, key, rule)

	runtime.Log("setEACL: success")
}

func EACL(containerID []byte) ExtendedACL {
	ctx := storage.GetReadOnlyContext()

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("eACL: container does not exists")
	}

	return getEACL(ctx, containerID)
}

func PutContainerSize(epoch int, cid []byte, usedSize int, pubKey interop.PublicKey) {
	ctx := storage.GetContext()

	if !runtime.CheckWitness(pubKey) {
		panic("container: invalid witness for size estimation")
	}

	if !isStorageNode(ctx, pubKey) {
		panic("container: only storage nodes can save size estimations")
	}

	key := estimationKey(epoch, cid, pubKey)

	s := estimation{
		from: pubKey,
		size: usedSize,
	}

	storage.Put(ctx, key, std.Serialize(s))

	runtime.Log("container: saved container size estimation")
}

func GetContainerSize(id []byte) containerSizes {
	ctx := storage.GetReadOnlyContext()

	// this `id` expected to be from `ListContainerSizes`
	// therefore it is not contains postfix, we ignore it in the cut.
	ln := len(id)
	cid := id[ln-containerIDSize : ln]

	return getContainerSizeEstimation(ctx, id, cid)
}

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
			panic("newEpoch: this method must be invoked from inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("newEpoch: this method must be invoked from inner ring")
		}
	}

	candidates := keysToDelete(ctx, epochNum)
	for _, candidate := range candidates {
		storage.Delete(ctx, candidate)
	}
}

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
			panic("startEstimation: only inner ring nodes can invoke this")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("startEstimation: only inner ring nodes can invoke this")
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
			panic("stopEstimation: only inner ring nodes can invoke this")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("stopEstimation: only inner ring nodes can invoke this")
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

func Version() int {
	return version
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
