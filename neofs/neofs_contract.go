package smart_contract

/*
	NeoFS Smart Contract for NEO3.0.

	Utility methods, executed once in deploy stage:
	- Init
	- InitConfig

	User related methods:
	- Withdraw
	- Bind
	- Unbind

	Inner ring list related methods:
	- AlphabetList
	- AlphabetAddress
	- InnerRingCandidates
	- InnerRingCandidateAdd
	- InnerRingCandidateRemove
	- AlphabetUpdate

	Config methods:
	- Config
	- ListConfig
	- SetConfig

	Other utility methods:
	- Migrate
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
	record struct {
		key []byte
		val []byte
	}
)

const (
	candidateFeeConfigKey = "InnerRingCandidateFee"
	withdrawFeeConfigKey  = "WithdrawFee"

	version = 3

	alphabetKey       = "alphabet"
	candidatesKey     = "candidates"
	notaryDisabledKey = "notary"

	processingContractKey = "processingScriptHash"

	publicKeySize = 33

	maxBalanceAmount = 9000 // Max integer of Fixed12 in JSON bound (2**53-1)
	maxBalanceAmountGAS = maxBalanceAmount * 1_0000_0000

	// hardcoded value to ignore deposit notification in onReceive
	ignoreDepositNotification = "\x57\x0b"
)

var (
	configPrefix = []byte("config")
)

// _deploy sets up initial alphabet node keys.
func _deploy(data interface{}, isUpdate bool) {
	args := data.([]interface{})
	notaryDisabled := args[0].(bool)
	owner := args[1].(interop.Hash160)
	addrProc := args[2].(interop.Hash160)
	keys := args[3].([]interop.PublicKey)

	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	var irList []common.IRNode

	if len(keys) == 0 {
		panic("neofs: at least one alphabet key must be provided")
	}

	if len(addrProc) != 20 {
		panic("neofs: incorrect length of contract script hash")
	}

	for i := 0; i < len(keys); i++ {
		pub := keys[i]
		if len(pub) != publicKeySize {
			panic("neofs: incorrect public key length")
		}
		irList = append(irList, common.IRNode{PublicKey: pub})
	}

	// initialize all storage slices
	common.SetSerialized(ctx, alphabetKey, irList)
	common.SetSerialized(ctx, candidatesKey, []common.IRNode{})

	storage.Put(ctx, common.OwnerKey, owner)
	storage.Put(ctx, processingContractKey, addrProc)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, notaryDisabled)
	if notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("neofs contract notary disabled")
	}

	runtime.Log("neofs: contract initialized")
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

// AlphabetAddress returns 2\3n+1 multi signature address of alphabet nodes.
func AlphabetAddress() interop.Hash160 {
	ctx := storage.GetReadOnlyContext()
	return multiaddress(getNodes(ctx, alphabetKey))
}

// InnerRingCandidates returns array of inner ring candidate node keys.
func InnerRingCandidates() []common.IRNode {
	ctx := storage.GetReadOnlyContext()
	return getNodes(ctx, candidatesKey)
}

// InnerRingCandidateRemove removes key from the list of inner ring candidates.
func InnerRingCandidateRemove(key interop.PublicKey) bool {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	keyOwner := runtime.CheckWitness(key)

	if !keyOwner {
		if notaryDisabled {
			alphabet = getNodes(ctx, alphabetKey)
			nodeKey = common.InnerRingInvoker(alphabet)
			if len(nodeKey) == 0 {
				panic("irCandidateRemove: this method must be invoked by candidate or alphabet")
			}
		} else {
			multiaddr := AlphabetAddress()
			if !runtime.CheckWitness(multiaddr) {
				panic("irCandidateRemove: this method must be invoked by candidate or alphabet")
			}
		}
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

	if notaryDisabled && !keyOwner {
		threshold := len(alphabet)*2/3 + 1
		id := append(key, []byte("delete")...)
		hashID := crypto.Sha256(id)

		n := common.Vote(ctx, hashID, nodeKey)
		if n < threshold {
			return true
		}

		common.RemoveVotes(ctx, hashID)
	}

	common.SetSerialized(ctx, candidatesKey, nodes)

	return true
}

// InnerRingCandidateAdd adds key to the list of inner ring candidates.
func InnerRingCandidateAdd(key interop.PublicKey) bool {
	ctx := storage.GetContext()

	if !runtime.CheckWitness(key) {
		panic("irCandidateAdd: this method must be invoked by candidate")
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

	if amount <= 0 {
		panic("onNEP17Payment: amount must be positive")
	} else if maxBalanceAmountGAS < amount {
		panic("onNEP17Payment: out of max amount limit")
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

// Withdraw initialize gas asset withdraw from NeoFS balance.
func Withdraw(user interop.Hash160, amount int) bool {
	if !runtime.CheckWitness(user) {
		panic("withdraw: you should be the owner of the wallet")
	}

	if amount < 0 {
		panic("withdraw: non positive amount number")
	}

	if amount > maxBalanceAmount {
		panic("withdraw: out of max amount limit")
	}

	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	// transfer fee to proxy contract to pay cheque invocation
	fee := getConfig(ctx, withdrawFeeConfigKey).(int)

	if notaryDisabled {
		alphabet := getNodes(ctx, alphabetKey)
		for _, node := range alphabet {
			processingAddr := contract.CreateStandardAccount(node.PublicKey)

			transferred := gas.Transfer(user, processingAddr, fee, []byte{})
			if !transferred {
				panic("withdraw: failed to transfer withdraw fee, aborting")
			}
		}
	} else {
		processingAddr := storage.Get(ctx, processingContractKey).(interop.Hash160)

		transferred := gas.Transfer(user, processingAddr, fee, []byte{})
		if !transferred {
			panic("withdraw: failed to transfer withdraw fee, aborting")
		}
	}

	// notify alphabet nodes
	amount = amount * 100000000
	tx := runtime.GetScriptContainer()

	runtime.Notify("Withdraw", user, amount, tx.Hash)

	return true
}

// Cheque sends gas assets back to the user if they were successfully
// locked in NeoFS balance contract.
func Cheque(id []byte, user interop.Hash160, amount int, lockAcc []byte) bool {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getNodes(ctx, alphabetKey)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("cheque: this method must be invoked by alphabet")
		}
	} else {
		multiaddr := AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("cheque: this method must be invoked by alphabet")
		}
	}

	from := runtime.GetExecutingScriptHash()

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	transferred := gas.Transfer(from, user, amount, nil)
	if !transferred {
		panic("cheque: failed to transfer funds, aborting")
	}

	runtime.Log("cheque: funds have been transferred")
	runtime.Notify("Cheque", id, user, amount, lockAcc)

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
func AlphabetUpdate(id []byte, args []interop.PublicKey) bool {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	if len(args) == 0 {
		panic("alphabetUpdate: bad arguments")
	}

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getNodes(ctx, alphabetKey)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("alphabetUpdate: this method must be invoked by alphabet")
		}
	} else {
		multiaddr := AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("alphabetUpdate: this method must be invoked by alphabet")
		}
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

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, alphabetKey, newAlphabet)

	runtime.Notify("AlphabetUpdate", id, newAlphabet)
	runtime.Log("alphabetUpdate: alphabet list has been updated")

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
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getNodes(ctx, alphabetKey)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(key) == 0 {
			panic("setConfig: this method must be invoked by alphabet")
		}
	} else {
		multiaddr := AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("setConfig: this method must be invoked by alphabet")
		}
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	setConfig(ctx, key, val)

	runtime.Notify("SetConfig", id, key, val)
	runtime.Log("setConfig: configuration has been updated")

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

// multiaddress returns multi signature address from list of IRNode structures
// with m = 2/3n+1.
func multiaddress(n []common.IRNode) []byte {
	threshold := len(n)*2/3 + 1

	keys := []interop.PublicKey{}
	for _, node := range n {
		key := node.PublicKey
		keys = append(keys, key)
	}

	return contract.CreateMultisigAccount(threshold, keys)
}
