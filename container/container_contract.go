package containercontract

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

	extendedACL struct {
		val []byte
		sig []byte
		pub interop.PublicKey
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
	version   = 1
	ownersKey = "ownersList"

	neofsIDContractKey = "identityScriptHash"
	balanceContractKey = "balanceScriptHash"
	netmapContractKey  = "netmapScriptHash"
	notaryDisabledKey  = "notary"

	containerFeeKey = "ContainerFee"

	containerIDSize = 32 // SHA256 size

	estimateKeyPrefix = "cnr"
	cleanupDelta      = 3
)

var (
	eACLPrefix = []byte("eACL")
)

func Init(notaryDisabled bool, owner, addrNetmap, addrBalance, addrID interop.Hash160) {
	ctx := storage.GetContext()

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
	}

	runtime.Log("container contract initialized")
}

func Migrate(script []byte, manifest []byte) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("container contract updated")

	return true
}

func Put(container []byte, signature interop.Signature, publicKey interop.PublicKey) bool {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	offset := int(container[1])
	offset = 2 + offset + 4                  // version prefix + version size + owner prefix
	ownerID := container[offset : offset+25] // offset + size of owner
	containerID := crypto.Sha256(container)
	neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).(interop.Hash160)

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

	if !alphabetCall {
		if !isSignedByOwnerKey(container, signature, ownerID, publicKey) {
			// check keys from NeoFSID
			keys := contract.Call(neofsIDContractAddr, "key", contract.ReadOnly, ownerID).([]interop.PublicKey)
			if !verifySignature(container, signature, keys) {
				panic("put: invalid owner signature")
			}
		}

		runtime.Notify("containerPut", container, signature, publicKey)

		return true
	}

	from := common.WalletToScriptHash(ownerID)
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	balanceContractAddr := storage.Get(ctx, balanceContractKey).(interop.Hash160)
	containerFee := contract.Call(netmapContractAddr, "config", contract.ReadOnly, containerFeeKey).(int)
	details := common.ContainerFeeTransferDetails(containerID)

	// todo: check if new container with unique container id

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{container, signature, publicKey}, []byte("put"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	for i := 0; i < len(alphabet); i++ {
		node := alphabet[i]
		to := contract.CreateStandardAccount(node.PublicKey)

		tx := contract.Call(balanceContractAddr, "transferX",
			contract.All,
			from,
			to,
			containerFee,
			details,
		)
		if !tx.(bool) {
			panic("put: can't transfer assets for container creation")
		}
	}

	addContainer(ctx, containerID, ownerID, container)
	contract.Call(neofsIDContractAddr, "addKey", contract.All, ownerID, [][]byte{publicKey})

	runtime.Log("put: added new container")

	return true
}

func Delete(containerID, signature []byte) bool {
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
		// check provided key
		neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).(interop.Hash160)
		keys := contract.Call(neofsIDContractAddr, "key", contract.ReadOnly, ownerID).([]interop.PublicKey)

		if !verifySignature(containerID, signature, keys) {
			panic("delete: invalid owner signature")
		}

		runtime.Notify("containerDelete", containerID, signature)
		return true
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{containerID, signature}, []byte("delete"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	removeContainer(ctx, containerID, ownerID)
	runtime.Log("delete: remove container")

	return true
}

func Get(containerID []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, containerID).([]byte)
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

	owners := common.GetList(ctx, ownersKey)
	for i := 0; i < len(owners); i++ {
		ownerID := owners[i]
		if len(owner) != 0 && !common.BytesEqual(owner, ownerID) {
			continue
		}

		containers := common.GetList(ctx, ownerID)

		for j := 0; j < len(containers); j++ {
			container := containers[j]
			list = append(list, container)
		}
	}

	return list
}

func SetEACL(eACL, signature []byte) bool {
	ctx := storage.GetContext()

	// get container ID
	offset := int(eACL[1])
	offset = 2 + offset + 4
	containerID := eACL[offset : offset+32]

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("setEACL: container does not exists")
	}

	neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).(interop.Hash160)
	keys := contract.Call(neofsIDContractAddr, "key", contract.ReadOnly, ownerID).([]interop.PublicKey)

	if !verifySignature(eACL, signature, keys) {
		panic("setEACL: invalid eACL signature")
	}

	rule := extendedACL{
		val: eACL,
		sig: signature,
	}

	key := append(eACLPrefix, containerID...)
	common.SetSerialized(ctx, key, rule)

	runtime.Log("setEACL: success")

	return true
}

func EACL(containerID []byte) extendedACL {
	ctx := storage.GetReadOnlyContext()

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("getEACL: container does not exists")
	}

	eacl := getEACL(ctx, containerID)

	if len(eacl.sig) == 0 {
		return eacl
	}

	// attach corresponding public key if it was not revoked from neofs id

	neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).(interop.Hash160)
	keys := contract.Call(neofsIDContractAddr, "key", contract.ReadOnly, ownerID).([]interop.PublicKey)

	for i := range keys {
		key := keys[i]
		if crypto.VerifyWithECDsa(eacl.val, key, eacl.sig, crypto.Secp256r1) {
			eacl.pub = key

			break
		}
	}

	return eacl
}

func PutContainerSize(epoch int, cid []byte, usedSize int, pubKey interop.PublicKey) bool {
	ctx := storage.GetContext()

	if !runtime.CheckWitness(pubKey) {
		panic("container: invalid witness for size estimation")
	}

	if !isStorageNode(ctx, pubKey) {
		panic("container: only storage nodes can save size estimations")
	}

	key := estimationKey(epoch, cid)
	s := getContainerSizeEstimation(ctx, key, cid)

	// do not add estimation twice
	for i := range s.estimations {
		est := s.estimations[i]
		if common.BytesEqual(est.from, pubKey) {
			return false
		}
	}

	s.estimations = append(s.estimations, estimation{
		from: pubKey,
		size: usedSize,
	})

	storage.Put(ctx, key, std.Serialize(s))

	runtime.Log("container: saved container size estimation")

	return true
}

func GetContainerSize(id []byte) containerSizes {
	ctx := storage.GetReadOnlyContext()
	return getContainerSizeEstimation(ctx, id, nil)
}

func ListContainerSizes(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var buf interface{} = epoch

	key := []byte(estimateKeyPrefix)
	key = append(key, buf.([]byte)...)

	it := storage.Find(ctx, key, storage.KeysOnly)

	var result [][]byte

	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // it MUST BE `storage.KeysOnly`
		result = append(result, key)
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

func StartContainerEstimation(epoch int) bool {
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
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	runtime.Notify("StartEstimation", epoch)
	runtime.Log("startEstimation: notification has been produced")

	return true
}

func StopContainerEstimation(epoch int) bool {
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
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	runtime.Notify("StopEstimation", epoch)
	runtime.Log("stopEstimation: notification has been produced")

	return true
}

func Version() int {
	return version
}

func addContainer(ctx storage.Context, id []byte, owner []byte, container []byte) {
	addOrAppend(ctx, ownersKey, owner)
	addOrAppend(ctx, owner, id)
	storage.Put(ctx, id, container)
}

func removeContainer(ctx storage.Context, id []byte, owner []byte) {
	n := remove(ctx, owner, id)

	// if it was last container, remove owner from the list of owners
	if n == 0 {
		_ = remove(ctx, ownersKey, owner)
	}

	storage.Delete(ctx, id)
}

func addOrAppend(ctx storage.Context, key interface{}, value []byte) {
	list := common.GetList(ctx, key)
	for i := 0; i < len(list); i++ {
		if common.BytesEqual(list[i], value) {
			return
		}
	}

	if len(list) == 0 {
		list = [][]byte{value}
	} else {
		list = append(list, value)
	}

	common.SetSerialized(ctx, key, list)
}

// remove returns amount of left elements in the list
func remove(ctx storage.Context, key interface{}, value []byte) int {
	var (
		list    = common.GetList(ctx, key)
		newList = [][]byte{}
	)

	for i := 0; i < len(list); i++ {
		if !common.BytesEqual(list[i], value) {
			newList = append(newList, list[i])
		}
	}

	ln := len(newList)
	if ln == 0 {
		storage.Delete(ctx, key)
	} else {
		common.SetSerialized(ctx, key, newList)
	}

	return ln
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

func getEACL(ctx storage.Context, cid []byte) extendedACL {
	key := append(eACLPrefix, cid...)
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).(extendedACL)
	}

	return extendedACL{val: []byte{}, sig: interop.Signature{}, pub: interop.PublicKey{}}
}

func verifySignature(msg []byte, sig interop.Signature, keys []interop.PublicKey) bool {
	for i := range keys {
		key := keys[i]
		if crypto.VerifyWithECDsa(msg, key, sig, crypto.Secp256r1) {
			return true
		}
	}

	return false
}

func getOwnerByID(ctx storage.Context, id []byte) []byte {
	owners := common.GetList(ctx, ownersKey)
	for i := 0; i < len(owners); i++ {
		ownerID := owners[i]
		containers := common.GetList(ctx, ownerID)

		for j := 0; j < len(containers); j++ {
			container := containers[j]
			if common.BytesEqual(container, id) {
				return ownerID
			}
		}
	}

	return nil
}

func isSignedByOwnerKey(msg []byte, sig interop.Signature, owner []byte, key interop.PublicKey) bool {
	if !isOwnerFromKey(owner, key) {
		return false
	}

	return crypto.VerifyWithECDsa(msg, key, sig, crypto.Secp256r1)
}

func isOwnerFromKey(owner []byte, key interop.PublicKey) bool {
	ownerSH := common.WalletToScriptHash(owner)
	keySH := contract.CreateStandardAccount(key)

	return common.BytesEqual(ownerSH, keySH)
}

func estimationKey(epoch int, cid []byte) []byte {
	var buf interface{} = epoch

	result := []byte(estimateKeyPrefix)
	result = append(result, buf.([]byte)...)

	return append(result, cid...)
}

func getContainerSizeEstimation(ctx storage.Context, key, cid []byte) containerSizes {
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).(containerSizes)
	}

	return containerSizes{
		cid:         cid,
		estimations: []estimation{},
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
		k := iterator.Value(it).([]byte) // it MUST BE `storage.KeysOnly`
		nbytes := k[len(estimateKeyPrefix) : len(k)-32]

		var n interface{} = nbytes

		if epoch-n.(int) > cleanupDelta {
			results = append(results, k)
		}
	}

	return results
}
