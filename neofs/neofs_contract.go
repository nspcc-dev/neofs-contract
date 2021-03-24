package smart_contract

/*
	NeoFS Smart Contract for NEO3.0.

	Utility methods, executed once in deploy stage:
	- Init
	- InitConfig

	User related methods:
	- Deposit
	- Withdraw
	- Bind
	- Unbind

	Inner ring list related methods:
	- AlphabetList
	- InnerRingCandidates
	- InnerRingCandidateAdd
	- InnerRingCandidateRemove
	- AlphabetUpdate

	Config methods:
	- Config
	- ListConfig
	- SetConfig

	Other utility methods:
	- Version
	- Cheque
*/

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

type (
	cheque struct {
		id []byte
	}

	record struct {
		key []byte
		val []byte
	}
)

const (
	defaultCandidateFee   = 100 * 1_0000_0000 // 100 Fixed8 Gas
	candidateFeeConfigKey = "InnerRingCandidateFee"

	version = 3

	alphabetKey      = "alphabet"
	candidatesKey    = "candidates"
	cashedChequesKey = "cheques"

	publicKeySize = 33

	maxBalanceAmount = 9000 // Max integer of Fixed12 in JSON bound (2**53-1)

	// hardcoded value to ignore deposit notification in onReceive
	ignoreDepositNotification = "\x57\x0b"
)

var (
	configPrefix = []byte("config")
)

// Init set up initial alphabet node keys.
func Init(owner interop.PublicKey, args []interop.PublicKey) bool {
	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	var irList []common.IRNode

	if len(args) == 0 {
		panic("neofs: at least one alphabet key must be provided")
	}

	for i := 0; i < len(args); i++ {
		pub := args[i]
		if len(pub) != publicKeySize {
			panic("neofs: incorrect public key length")
		}
		irList = append(irList, common.IRNode{PublicKey: pub})
	}

	// initialize all storage slices
	common.SetSerialized(ctx, alphabetKey, irList)
	common.InitVote(ctx)
	common.SetSerialized(ctx, candidatesKey, []common.IRNode{})
	common.SetSerialized(ctx, cashedChequesKey, []cheque{})

	storage.Put(ctx, common.OwnerKey, owner)

	runtime.Log("neofs: contract initialized")

	return true
}

// Migrate updates smart contract execution script and manifest.
func Migrate(script []byte, manifest []byte) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("neofs contract updated")

	return true
}

// AlphabetList returns array of alphabet node keys.
func AlphabetList() []common.IRNode {
	ctx := storage.GetReadOnlyContext()
	return getNodes(ctx, alphabetKey)
}

// InnerRingCandidates returns array of inner ring candidate node keys.
func InnerRingCandidates() []common.IRNode {
	ctx := storage.GetReadOnlyContext()
	return getNodes(ctx, candidatesKey)
}

// InnerRingCandidateRemove removes key from the list of inner ring candidates.
func InnerRingCandidateRemove(key interop.PublicKey) bool {
	ctx := storage.GetContext()

	if !runtime.CheckWitness(key) {
		panic("irCandidateRemove: you should be the owner of the public key")
	}

	nodes := []common.IRNode{} // it is explicit declaration of empty slice, not nil
	candidates := getNodes(ctx, candidatesKey)

	for i := range candidates {
		c := candidates[i]
		if !common.BytesEqual(c.PublicKey, key) {
			nodes = append(nodes, c)
		} else {
			runtime.Log("irCandidateRemove: candidate has been removed")
		}
	}

	common.SetSerialized(ctx, candidatesKey, nodes)

	return true
}

// InnerRingCandidateAdd adds key to the list of inner ring candidates.
func InnerRingCandidateAdd(key interop.PublicKey) bool {
	ctx := storage.GetContext()

	if !runtime.CheckWitness(key) {
		panic("irCandidateAdd: you should be the owner of the public key")
	}

	c := common.IRNode{PublicKey: key}
	candidates := getNodes(ctx, candidatesKey)

	list, ok := addNode(candidates, c)
	if !ok {
		panic("irCandidateAdd: candidate already in the list")
	}

	from := contract.CreateStandardAccount(key)
	to := runtime.GetExecutingScriptHash()
	fee := getConfig(ctx, candidateFeeConfigKey).(int)

	transferred := gas.Transfer(from, to, fee, []byte(ignoreDepositNotification))
	if !transferred {
		panic("irCandidateAdd: failed to transfer funds, aborting")
	}

	runtime.Log("irCandidateAdd: candidate has been added")
	common.SetSerialized(ctx, candidatesKey, list)

	return true
}

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	rcv := data.(interop.Hash160)
	if common.BytesEqual(rcv, []byte(ignoreDepositNotification)) {
		return
	}

	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, interop.Hash160(gas.Hash)) {
		panic("onNEP17Payment: only GAS can be accepted for deposit")
	}

	switch len(rcv) {
	case 20:
	case 0:
		rcv = from
	default:
		panic("onNEP17Payment: invalid data argument, expected Hash160")
	}

	runtime.Log("onNEP17Payment: funds have been transferred")

	tx := runtime.GetScriptContainer()
	runtime.Notify("Deposit", from, amount, rcv, tx.Hash)
}

// Deposit gas assets to this script-hash address in NeoFS balance contract.
func Deposit(from interop.Hash160, amount int, rcv interop.Hash160) bool {
	if !runtime.CheckWitness(from) {
		panic("deposit: you should be the owner of the wallet")
	}

	if amount > maxBalanceAmount {
		panic("deposit: out of max amount limit")
	}

	if amount <= 0 {
		return false
	}
	amount = amount * 100000000

	to := runtime.GetExecutingScriptHash()

	transferred := gas.Transfer(from, to, amount, rcv)
	if !transferred {
		panic("deposit: failed to transfer funds, aborting")
	}

	return true
}

// Withdraw initialize gas asset withdraw from NeoFS balance.
func Withdraw(user []byte, amount int) bool {
	if !runtime.CheckWitness(user) {
		panic("withdraw: you should be the owner of the wallet")
	}

	if amount < 0 {
		panic("withdraw: non positive amount number")
	}

	if amount > maxBalanceAmount {
		panic("withdraw: out of max amount limit")
	}

	amount = amount * 100000000

	tx := runtime.GetScriptContainer()
	runtime.Notify("Withdraw", user, amount, tx.Hash)

	return true
}

// Cheque sends gas assets back to the user if they were successfully
// locked in NeoFS balance contract.
func Cheque(id []byte, user interop.Hash160, amount int, lockAcc []byte) bool {
	ctx := storage.GetContext()
	alphabet := getNodes(ctx, alphabetKey)
	threshold := len(alphabet)/3*2 + 1

	cashedCheques := getCashedCheques(ctx)
	hashID := crypto.Sha256(id)

	key := common.InnerRingInvoker(alphabet)
	if len(key) == 0 {
		panic("cheque: invoked by non alphabet node")
	}

	c := cheque{id: id}

	list, ok := addCheque(cashedCheques, c)
	if !ok {
		panic("cheque: non unique id")
	}

	n := common.Vote(ctx, hashID, key)
	if n >= threshold {
		common.RemoveVotes(ctx, hashID)

		from := runtime.GetExecutingScriptHash()

		transferred := gas.Transfer(from, user, amount, nil)
		if !transferred {
			panic("cheque: failed to transfer funds, aborting")
		}

		runtime.Log("cheque: funds have been transferred")

		common.SetSerialized(ctx, cashedChequesKey, list)
		runtime.Notify("Cheque", id, user, amount, lockAcc)
	}

	return true
}

// Bind public key with user's account to use it in NeoFS requests.
func Bind(user []byte, keys []interop.PublicKey) bool {
	if !runtime.CheckWitness(user) {
		panic("binding: you should be the owner of the wallet")
	}

	for i := 0; i < len(keys); i++ {
		pubKey := keys[i]
		if len(pubKey) != publicKeySize {
			panic("binding: incorrect public key size")
		}
	}

	runtime.Notify("Bind", user, keys)

	return true
}

// Unbind public key from user's account
func Unbind(user []byte, keys []interop.PublicKey) bool {
	if !runtime.CheckWitness(user) {
		panic("unbinding: you should be the owner of the wallet")
	}

	for i := 0; i < len(keys); i++ {
		pubKey := keys[i]
		if len(pubKey) != publicKeySize {
			panic("unbinding: incorrect public key size")
		}
	}

	runtime.Notify("Unbind", user, keys)

	return true
}

// AlphabetUpdate updates list of alphabet nodes with provided list of
// public keys.
func AlphabetUpdate(chequeID []byte, args []interop.PublicKey) bool {
	ctx := storage.GetContext()

	if len(args) == 0 {
		panic("alphabetUpdate: bad arguments")
	}

	alphabet := getNodes(ctx, alphabetKey)
	threshold := len(alphabet)/3*2 + 1

	key := common.InnerRingInvoker(alphabet)
	if len(key) == 0 {
		panic("innerRingUpdate: invoked by non alphabet node")
	}

	c := cheque{id: chequeID}

	cashedCheques := getCashedCheques(ctx)

	chequesList, ok := addCheque(cashedCheques, c)
	if !ok {
		panic("irUpdate: non unique chequeID")
	}

	newAlphabet := []common.IRNode{}

	for i := 0; i < len(args); i++ {
		pubKey := args[i]
		if len(pubKey) != publicKeySize {
			panic("alphabetUpdate: invalid public key in alphabet list")
		}

		newAlphabet = append(newAlphabet, common.IRNode{
			PublicKey: pubKey,
		})
	}

	hashID := crypto.Sha256(chequeID)

	n := common.Vote(ctx, hashID, key)
	if n >= threshold {
		common.RemoveVotes(ctx, hashID)

		common.SetSerialized(ctx, alphabetKey, newAlphabet)
		common.SetSerialized(ctx, cashedChequesKey, chequesList)

		runtime.Notify("AlphabetUpdate", c.id, newAlphabet)
		runtime.Log("alphabetUpdate: alphabet list has been updated")
	}

	return true
}

// Config returns value of NeoFS configuration with provided key.
func Config(key []byte) interface{} {
	ctx := storage.GetReadOnlyContext()
	return getConfig(ctx, key)
}

// SetConfig key-value pair as a NeoFS runtime configuration value.
func SetConfig(id, key, val []byte) bool {
	ctx := storage.GetContext()

	// check if it is alphabet invocation
	alphabet := getNodes(ctx, alphabetKey)
	threshold := len(alphabet)/3*2 + 1

	nodeKey := common.InnerRingInvoker(alphabet)
	if len(nodeKey) == 0 {
		panic("setConfig: invoked by non alphabet node")
	}

	// check unique id of the operation
	c := cheque{id: id}
	cashedCheques := getCashedCheques(ctx)

	chequesList, ok := addCheque(cashedCheques, c)
	if !ok {
		panic("setConfig: non unique id")
	}

	// vote for new configuration value
	hashID := crypto.Sha256(id)

	n := common.Vote(ctx, hashID, nodeKey)
	if n >= threshold {
		common.RemoveVotes(ctx, hashID)

		setConfig(ctx, key, val)
		common.SetSerialized(ctx, cashedChequesKey, chequesList)

		runtime.Notify("SetConfig", id, key, val)
		runtime.Log("setConfig: configuration has been updated")
	}

	return true
}

// ListConfig returns array of all key-value pairs of NeoFS configuration.
func ListConfig() []record {
	ctx := storage.GetReadOnlyContext()

	var config []record

	it := storage.Find(ctx, configPrefix, storage.None)
	for iterator.Next(it) {
		pair := iterator.Value(it).([]interface{})
		key := pair[0].([]byte)
		val := pair[1].([]byte)
		r := record{key: key[len(configPrefix):], val: val}

		config = append(config, r)
	}

	return config
}

// InitConfig set up initial NeoFS key-value configuration.
func InitConfig(args [][]byte) bool {
	ctx := storage.GetContext()

	if getConfig(ctx, candidateFeeConfigKey) != nil {
		panic("neofs: configuration already installed")
	}

	ln := len(args)
	if ln%2 != 0 {
		panic("initConfig: bad arguments")
	}

	setConfig(ctx, candidateFeeConfigKey, defaultCandidateFee)

	for i := 0; i < ln/2; i++ {
		key := args[i*2]
		val := args[i*2+1]

		setConfig(ctx, key, val)
	}

	runtime.Log("neofs: config has been installed")

	return true
}

// Version of contract.
func Version() int {
	return version
}

// getNodes returns deserialized slice of nodes from storage.
func getNodes(ctx storage.Context, key string) []common.IRNode {
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]common.IRNode)
	}

	return []common.IRNode{}
}

// getCashedCheques returns deserialized slice of used cheques.
func getCashedCheques(ctx storage.Context) []cheque {
	data := storage.Get(ctx, cashedChequesKey)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]cheque)
	}

	return []cheque{}
}

// getConfig returns installed neofs configuration value or nil if it is not set.
func getConfig(ctx storage.Context, key interface{}) interface{} {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	return storage.Get(ctx, storageKey)
}

// setConfig sets neofs configuration value in the contract storage.
func setConfig(ctx storage.Context, key, val interface{}) {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	storage.Put(ctx, storageKey, val)
}

// addCheque returns slice of cheques with appended cheque 'c' and bool flag
// that set to false if cheque 'c' is already presented in the slice 'lst'.
func addCheque(lst []cheque, c cheque) ([]cheque, bool) {
	for i := 0; i < len(lst); i++ {
		if common.BytesEqual(c.id, lst[i].id) {
			return nil, false
		}
	}

	lst = append(lst, c)

	return lst, true
}

// addNode returns slice of nodes with appended node 'n' and bool flag
// that set to false if node 'n' is already presented in the slice 'lst'.
func addNode(lst []common.IRNode, n common.IRNode) ([]common.IRNode, bool) {
	for i := 0; i < len(lst); i++ {
		if common.BytesEqual(n.PublicKey, lst[i].PublicKey) {
			return nil, false
		}
	}

	lst = append(lst, n)

	return lst, true
}

// rmNodeByKey returns slice of nodes without node with key 'k',
// slices of nodes 'add' with node with key 'k' and bool flag,
// that set to false if node with a key 'k' does not exists in the slice 'lst'.
func rmNodeByKey(lst, add []common.IRNode, k []byte) ([]common.IRNode, []common.IRNode, bool) {
	var (
		flag   bool
		newLst = []common.IRNode{} // it is explicit declaration of empty slice, not nil
	)

	for i := 0; i < len(lst); i++ {
		if common.BytesEqual(k, lst[i].PublicKey) {
			add = append(add, lst[i])
			flag = true
		} else {
			newLst = append(newLst, lst[i])
		}
	}

	return newLst, add, flag
}
