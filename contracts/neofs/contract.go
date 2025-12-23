package neofs

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

type (
	Record struct {
		Key []byte
		Val []byte
	}
)

const (
	withdrawFeeConfigKey = "WithdrawFee"

	alphabetKey       = "alphabet"
	notaryDisabledKey = "notary"

	processingContractKey = "processingScriptHash"

	maxBalanceAmount    = 9000 // Max integer of Fixed12 in JSON bound (2**53-1)
	maxBalanceAmountGAS = int64(maxBalanceAmount) * 1_0000_0000

	// hardcoded value to ignore deposit notification in onReceive.
	ignoreDepositNotification = "\x57\x0b"
)

var (
	configPrefix = []byte("config")
)

// _deploy sets up initial alphabet node keys.
// nolint:unused
func _deploy(data any, update bool) {
	ctx := storage.GetContext()

	if update {
		args := data.([]any)
		version := args[len(args)-1].(int)
		common.CheckVersion(version)

		if version < 24_000 {
			const candidatesKey = "candidates"

			it := storage.Find(ctx, candidatesKey, storage.KeysOnly)
			for iterator.Next(it) {
				storage.Delete(ctx, iterator.Value(it))
			}

			const candidateFeeConfigKey = "InnerRingCandidateFee"
			storage.Delete(ctx, append(configPrefix, []byte(candidateFeeConfigKey)...))
		}

		return
	}

	args := data.(struct {
		notaryDisabled bool
		addrProc       interop.Hash160
		keys           []interop.PublicKey
		config         [][]byte
	})

	if len(args.keys) == 0 {
		panic("at least one alphabet key must be provided")
	}

	if len(args.addrProc) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	for _, pub := range args.keys {
		if len(pub) != interop.PublicKeyCompressedLen {
			panic("incorrect public key length")
		}
	}

	// initialize all storage slices
	common.SetSerialized(ctx, alphabetKey, args.keys)

	storage.Put(ctx, processingContractKey, args.addrProc)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, args.notaryDisabled)
	if args.notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("neofs contract notary disabled")
	}

	ln := len(args.config)
	if ln%2 != 0 {
		panic("bad configuration")
	}

	for i := range ln / 2 {
		key := args.config[i*2]
		val := args.config[i*2+1]

		setConfig(ctx, key, val)
	}

	runtime.Log("neofs: contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by the FS chain committee.
func Update(nefFile, manifest []byte, data any) {
	alphabetCommittee := common.Multiaddress(common.InnerRingNodes(), true)

	common.CheckOwnerWitness(alphabetCommittee)

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nefFile, manifest, common.AppendVersion(data))
	runtime.Log("neofs contract updated")
}

// AlphabetList returns an array of alphabet node keys. It is used in notary-disabled
// FS chain environment.
func AlphabetList() []common.IRNode {
	ctx := storage.GetReadOnlyContext()
	pubs := getAlphabetNodes(ctx)
	nodes := []common.IRNode{}
	for i := range pubs {
		nodes = append(nodes, common.IRNode{PublicKey: pubs[i]})
	}
	return nodes
}

// AlphabetAddress returns 2\3n+1 multisignature address of alphabet nodes.
// It is used in notary-disabled FS chain environment.
func AlphabetAddress() interop.Hash160 {
	ctx := storage.GetReadOnlyContext()
	return multiaddress(getAlphabetNodes(ctx))
}

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
// It takes no more than 9000.0 GAS. Native GAS has precision 8, and
// NeoFS balance contract has precision 12. Values bigger than 9000.0 can
// break JSON limits for integers when precision is converted.
func OnNEP17Payment(from interop.Hash160, amount int, data any) {
	rcv := data.(interop.Hash160)
	if rcv.Equals(ignoreDepositNotification) {
		return
	}

	if amount <= 0 {
		common.AbortWithMessage("amount must be positive")
	} else if maxBalanceAmountGAS < int64(amount) {
		common.AbortWithMessage("out of max amount limit")
	}

	caller := runtime.GetCallingScriptHash()
	if !caller.Equals(gas.Hash) {
		common.AbortWithMessage("only GAS can be accepted for deposit")
	}

	switch len(rcv) {
	case interop.Hash160Len:
	case 0:
		rcv = from
	default:
		common.AbortWithMessage("invalid data argument, expected Hash160")
	}

	runtime.Log("funds have been transferred")

	tx := runtime.GetScriptContainer()
	runtime.Notify("Deposit", from, amount, rcv, tx.Hash)
}

// Withdraw initializes gas asset withdraw from NeoFS. It can be invoked only
// by the specified user.
//
// This method produces Withdraw notification to lock assets in FS chain and
// transfers withdraw fee from a user account to each Alphabet node. If notary
// is enabled in main chain, fee is transferred to Processing contract.
// Fee value is specified in NeoFS network config with the key WithdrawFee.
func Withdraw(user interop.Hash160, amount int) {
	if !runtime.CheckWitness(user) {
		panic("you should be the owner of the wallet")
	}

	if amount < 0 {
		panic("non positive amount number")
	}

	if amount > maxBalanceAmount {
		panic("out of max amount limit")
	}

	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	// transfer fee to proxy contract to pay cheque invocation
	fee := getConfig(ctx, withdrawFeeConfigKey).(int)

	if notaryDisabled {
		alphabet := getAlphabetNodes(ctx)
		for _, node := range alphabet {
			processingAddr := contract.CreateStandardAccount(node)

			transferred := gas.Transfer(user, processingAddr, fee, []byte{})
			if !transferred {
				panic("failed to transfer withdraw fee, aborting")
			}
		}
	} else {
		processingAddr := storage.Get(ctx, processingContractKey).(interop.Hash160)

		transferred := gas.Transfer(user, processingAddr, fee, []byte{})
		if !transferred {
			panic("failed to transfer withdraw fee, aborting")
		}
	}

	// notify alphabet nodes
	amount = amount * 100000000
	tx := runtime.GetScriptContainer()

	runtime.Notify("Withdraw", user, amount, tx.Hash)
}

// Cheque transfers GAS back to the user from the contract account, if assets were
// successfully locked in NeoFS balance contract. It can be invoked only by
// Alphabet nodes.
//
// This method produces Cheque notification to burn assets in FS chain.
func Cheque(id []byte, user interop.Hash160, amount int, lockAcc []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getAlphabetNodes(ctx)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked by alphabet")
		}
	} else {
		common.CheckAlphabetWitness()
	}

	from := runtime.GetExecutingScriptHash()

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	transferred := gas.Transfer(from, user, amount, nil)
	if !transferred {
		panic("failed to transfer funds, aborting")
	}

	runtime.Log("funds have been transferred")
	runtime.Notify("Cheque", id, user, amount, lockAcc)
}

// AlphabetUpdate updates a list of alphabet nodes with the provided list of
// public keys. It can be invoked only by alphabet nodes.
//
// This method is used in notary-disabled FS chain environment. In this case,
// the actual alphabet list should be stored in the NeoFS contract.
func AlphabetUpdate(id []byte, args []interop.PublicKey) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	if len(args) == 0 {
		panic("bad arguments")
	}

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getAlphabetNodes(ctx)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked by alphabet")
		}
	} else {
		common.CheckAlphabetWitness()
	}

	newAlphabet := []interop.PublicKey{}

	for _, pubKey := range args {
		if len(pubKey) != interop.PublicKeyCompressedLen {
			panic("invalid public key in alphabet list")
		}

		newAlphabet = append(newAlphabet, pubKey)
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, alphabetKey, newAlphabet)

	runtime.Notify("AlphabetUpdate", id, newAlphabet)
	runtime.Log("alphabet list has been updated")
}

// Config returns configuration value of NeoFS configuration. If the key does
// not exist, returns nil.
func Config(key []byte) any {
	ctx := storage.GetReadOnlyContext()
	return getConfig(ctx, key)
}

// SetConfig key-value pair as a NeoFS runtime configuration value. It can be invoked
// only by Alphabet nodes.
func SetConfig(id, key, val []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getAlphabetNodes(ctx)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(key) == 0 {
			panic("this method must be invoked by alphabet")
		}
	} else {
		common.CheckAlphabetWitness()
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	setConfig(ctx, key, val)

	runtime.Notify("SetConfig", id, key, val)
	runtime.Log("configuration has been updated")
}

// ListConfig returns an array of structures that contain a key and a value of all
// NeoFS configuration records. Key and value are both byte arrays.
func ListConfig() []Record {
	ctx := storage.GetReadOnlyContext()

	var config []Record

	it := storage.Find(ctx, configPrefix, storage.None)
	for iterator.Next(it) {
		pair := iterator.Value(it).(storage.KeyValue)

		r := Record{Key: pair.Key[len(configPrefix):], Val: pair.Value}

		config = append(config, r)
	}

	return config
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}

// getAlphabetNodes returns a deserialized slice of nodes from storage.
func getAlphabetNodes(ctx storage.Context) []interop.PublicKey {
	data := storage.Get(ctx, alphabetKey)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]interop.PublicKey)
	}

	return []interop.PublicKey{}
}

// getConfig returns the installed neofs configuration value or nil if it is not set.
func getConfig(ctx storage.Context, key any) any {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	return storage.Get(ctx, storageKey)
}

// setConfig sets a neofs configuration value in the contract storage.
func setConfig(ctx storage.Context, key, val any) {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	storage.Put(ctx, storageKey, val)
}

// multiaddress returns a multisignature address from the list of IRNode structures
// with m = 2/3n+1.
func multiaddress(keys []interop.PublicKey) []byte {
	threshold := len(keys)*2/3 + 1

	return contract.CreateMultisigAccount(threshold, keys)
}
