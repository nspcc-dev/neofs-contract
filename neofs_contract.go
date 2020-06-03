package smart_contract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/engine"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/transaction"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

type (
	ballot struct {
		id []byte
		n  int
	}

	node struct {
		pub []byte
	}

	check struct {
		id []byte
	}

	configParam struct {
		key []byte
		val []byte
	}
)

const (
	// GAS NEP-5 HASH
	tokenHash             = "\x77\xea\x59\x6b\x7a\xdf\x7e\x4d\xd1\x40\x76\x97\x31\xb7\xd2\xf0\xe0\x6b\xcd\x9b"
	innerRingCandidateFee = 100 * 1000 * 1000 // 10^8
	version               = 1
	voteKey               = "ballots"
	initConfigKey         = "initConfigKey"
)

var cfgPrefix = []byte("cfg")

func Main(op string, args []interface{}) interface{} {
	// The trigger determines whether this smart-contract is being
	// run in 'verification' or 'application' mode.
	if runtime.GetTrigger() != runtime.Application() {
		return false
	}

	/*
		Utility operations - they will be changed in production:
		- Deploy(params: address, pubKey, ... )  - setup initial inner ring state

		User operations:
		- InnerRingList()                                - get list of inner ring nodes addresses and public keys
		- InnerRingCandidateRemove(params: pubKey)       - remove node with given public key from the inner ring candidate queue
		- InnerRingCandidateAdd(params: pubKey)          - add node to the inner ring candidate queue
		- Deposit(params: pubKey, amount)                - deposit GAS to the NeoFS account
		- Withdraw(params: withdrawCheque)               - withdraw GAS from the NeoFS account
		- InnerRingUpdate(params: irCheque)              - change list of inner ring nodes
		- IsInnerRing(params: pubKey)                    - returns true if pubKey presented in inner ring list
		- SetConfig(params: key, value)                  - set global configuration parameter
		- Config(params: key)                            - get global configuration parameter
		- Version()                                      - get version of the NeoFS smart-contract

		Params:
		- address        - string of the valid multiaddress (github.com/multiformats/multiaddr)
		- pubKey         - 33 byte public key
		- withdrawCheque - serialized structure, that confirms GAS transfer;
		                   contains inner ring signatures
		- irCheque       - serialized structure, that confirms new inner ring node list;
		                   contains inner ring signatures
	*/

	ctx := storage.GetContext()
	switch op {
	case "Deploy":
		irList := getSerialized(ctx, "InnerRingList").([]node)
		if len(irList) >= 3 {
			panic("contract already deployed")
		}

		irList = []node{}
		for i := 0; i < len(args); i++ {
			pub := args[i].([]byte)
			irList = append(irList, node{pub: pub})
		}

		data := runtime.Serialize(irList)
		storage.Put(ctx, "InnerRingList", data)

		data = runtime.Serialize([]interface{}{})
		storage.Put(ctx, "UsedVerifCheckList", data)
		storage.Put(ctx, "InnerRingCandidates", data)

		data = runtime.Serialize([]ballot{})
		storage.Put(ctx, voteKey, data)

		return true
	case "InnerRingList":
		irList := getSerialized(ctx, "InnerRingList").([]node)

		return irList
	case "InnerRingCandidateRemove":
		data := args[0].([]byte) // public key
		if !runtime.CheckWitness(data) {
			panic("you should be the owner of the public key")
		}

		delSerializedIR(ctx, "InnerRingCandidates", data)

		return true
	case "InnerRingCandidateAdd":
		key := args[0].([]byte) // public key

		if !runtime.CheckWitness(key) {
			panic("you should be the owner of the public key")
		}

		candidates := getSerialized(ctx, "InnerRingCandidates").([]node)
		if containsPub(candidates, key) {
			panic("is already in list")
		}

		from := pubToScriptHash(key)
		to := engine.GetExecutingScriptHash()
		params := []interface{}{from, to, innerRingCandidateFee}

		transferred := engine.AppCall([]byte(tokenHash), "transfer", params).(bool)
		if !transferred {
			panic("failed to transfer funds, aborting")
		}

		candidate := node{pub: key}
		if !putSerialized(ctx, "InnerRingCandidates", candidate) {
			panic("failed to put candidate into the queue")
		}

		return true
	case "Deposit":
		pk := args[0].([]byte)
		if !runtime.CheckWitness(pk) {
			panic("you should be the owner of the public key")
		}

		amount := args[1].(int)
		if amount > 0 {
			amount = amount * 100000000
		}

		from := pubToScriptHash(pk)
		to := engine.GetExecutingScriptHash()
		params := []interface{}{from, to, amount}
		transferred := engine.AppCall([]byte(tokenHash), "transfer", params).(bool)
		if !transferred {
			panic("failed to transfer funds, aborting")
		}

		runtime.Log("funds have been transferred")

		var rcv = []byte{}
		if len(args) == 3 {
			rcv = args[2].([]byte) // todo: check if rcv value is valid
		}

		tx := engine.GetScriptContainer()
		txHash := transaction.GetHash(tx)

		runtime.Notify("Deposit", pk, amount, rcv, txHash)

		return true
	case "Withdraw":
		if len(args) != 2 {
			panic("withdraw: bad arguments")
		}

		user := args[0].([]byte)
		if !runtime.CheckWitness(user) {
			// todo: consider something different with neoID
			panic("withdraw: you should be the owner of the account")
		}

		amount := args[1].(int)
		if amount > 0 {
			amount = amount * 100000000
		}

		tx := engine.GetScriptContainer()
		txHash := transaction.GetHash(tx)

		runtime.Notify("Withdraw", user, amount, txHash)

		return true
	case "Cheque":
		if len(args) != 4 {
			panic("cheque: bad arguments")
		}

		id := args[0].([]byte)      // unique cheque id
		user := args[1].([]byte)    // GAS receiver
		amount := args[2].(int)     // amount of GAS
		lockAcc := args[3].([]byte) // lock account from internal banking that must be cashed out

		ctx := storage.GetContext()

		hashID := crypto.Hash256(id)
		irList := getSerialized(ctx, "InnerRingList").([]node)
		usedList := getSerialized(ctx, "UsedVerifCheckList").([]check)
		threshold := len(irList)/3*2 + 1

		if !isInnerRingRequest(irList) {
			panic("cheque: invoked by non inner ring node")
		}

		c := check{id: id} // todo: use different cheque id for inner ring update and withdraw
		if containsCheck(usedList, c) {
			panic("cheque: non unique id")
		}

		n := vote(ctx, hashID)
		if n >= threshold {
			removeVotes(ctx, hashID)

			from := engine.GetExecutingScriptHash()
			params := []interface{}{from, user, amount}

			transferred := engine.AppCall([]byte(tokenHash), "transfer", params).(bool)
			if !transferred {
				panic("cheque: failed to transfer funds, aborting")
			}

			putSerialized(ctx, "UsedVerifCheckList", c)
			runtime.Notify("Cheque", id, user, amount, lockAcc)
		}

		return true
	case "InnerRingUpdate":
		data := args[0].([]byte)

		id := data[:8]
		var ln interface{} = data[8:10]
		listItemCount := ln.(int)
		listSize := listItemCount * 33

		offset := 8 + 2 + listSize

		irList := getSerialized(ctx, "InnerRingList").([]node)
		usedList := getSerialized(ctx, "UsedVerifCheckList").([]check)
		threshold := len(irList)/3*2 + 1

		if !isInnerRingRequest(irList) {
			panic("innerRingUpdate: invoked by non inner ring node")
		}

		c := check{id: id}
		if containsCheck(usedList, c) {
			panic("innerRingUpdate: cheque has non unique id")
		}

		chequeHash := crypto.Hash256(data)

		n := vote(ctx, chequeHash)
		if n >= threshold {
			removeVotes(ctx, chequeHash)

			candidates := getSerialized(ctx, "InnerRingCandidates").([]node)
			offset = 10
			newIR := []node{}

		loop:
			for i := 0; i < listItemCount; i, offset = i+1, offset+33 {
				pub := data[offset : offset+33]

				for j := 0; j < len(irList); j++ {
					n := irList[j]
					if util.Equals(n.pub, pub) {
						newIR = append(newIR, n)
						continue loop
					}
				}

				for j := 0; j < len(candidates); j++ {
					n := candidates[j]
					if util.Equals(n.pub, pub) {
						newIR = append(newIR, n)
						continue loop
					}
				}
			}

			if len(newIR) != listItemCount {
				panic("new inner ring wasn't processed correctly")
			}

			for i := 0; i < len(newIR); i++ {
				n := newIR[i]
				delSerializedIR(ctx, "InnerRingCandidates", n.pub)
			}

			newIRData := runtime.Serialize(newIR)
			storage.Put(ctx, "InnerRingList", newIRData)
			putSerialized(ctx, "UsedVerifCheckList", c)

			runtime.Notify("InnerRingUpdate", c.id, newIRData)
		}

		return true
	case "IsInnerRing":
		if len(args) != 1 {
			panic("isInnerRing: wrong arguments")
		}

		key := args[0].([]byte)
		if len(key) != 33 {
			panic("isInnerRing: incorrect public key")
		}

		irList := getSerialized(ctx, "InnerRingList").([]node)
		for i := range irList {
			node := irList[i]

			if util.Equals(node.pub, key) {
				return true
			}
		}

		return false
	case "SetConfig":
		if len(args) != 2 {
			panic("setConfig: bad arguments")
		}

		irList := getSerialized(ctx, "InnerRingList").([]node)
		threshold := len(irList)/3*2 + 1

		if !isInnerRingRequest(irList) {
			panic("setConfig: invoked by non inner ring node")
		}

		key := args[0].([]byte)
		val := args[1].([]byte)

		id := append(key, val...)
		hashID := crypto.SHA256(id)
		n := vote(ctx, hashID)

		if n >= threshold {
			removeVotes(ctx, hashID)
			storage.Put(ctx, append(cfgPrefix, key...), val)
			runtime.Log("setConfig: configParam updated")
		} else {
			runtime.Log("setConfig: processed inner ring invoke")
		}

		return true
	case "Config":
		if len(args) != 1 {
			panic("configParam: bad argument")
		}

		key := args[0].([]byte)

		return storage.Get(ctx, append(cfgPrefix, key...))
	case "InitConfig":
		ln := len(args)
		if ln%2 != 0 {
			panic("initConfig: bad arguments")
		}

		once := storage.Get(ctx, initConfigKey).([]byte)
		if len(once) != 0 {
			panic("initConfig: initial configParam already deployed")
		}

		for i := 0; i < ln/2; i++ {
			configKey := args[i*2].([]byte)
			configVal := args[i*2+1].([]byte)

			storage.Put(ctx, append(cfgPrefix, configKey...), configVal)
		}

		storage.Put(ctx, initConfigKey, []byte("done"))

		return true
	case "ListConfig":
		var config []configParam
		it := storage.Find(ctx, cfgPrefix)
		for iterator.Next(it) {
			key := iterator.Key(it).([]byte)
			// remove cfgPrefix from the name of the parameter
			key = key[len(cfgPrefix):]
			val := iterator.Value(it).([]byte)

			config = append(config, configParam{key: key, val: val})
		}

		return config
	case "Version":
		return version
	}

	panic("unknown operation")
}

func getSerialized(ctx storage.Context, key string) interface{} {
	data := storage.Get(ctx, key).([]byte)
	if len(data) != 0 {
		return runtime.Deserialize(data)
	}
	return nil
}

func delSerialized(ctx storage.Context, key string, value []byte) bool {
	data := storage.Get(ctx, key).([]byte)
	deleted := false

	var newList [][]byte
	if len(data) != 0 {
		lst := runtime.Deserialize(data).([][]byte)
		for i := 0; i < len(lst); i++ {
			if util.Equals(value, lst[i]) {
				deleted = true
			} else {
				newList = append(newList, lst[i])
			}
		}
		if deleted {
			if len(newList) != 0 {
				data := runtime.Serialize(newList)
				storage.Put(ctx, key, data)
			} else {
				storage.Delete(ctx, key)
			}
			runtime.Log("target element has been removed")
			return true
		}

	}

	runtime.Log("target element has not been removed")
	return false
}

func putSerialized(ctx storage.Context, key string, value interface{}) bool {
	data := storage.Get(ctx, key).([]byte)

	var lst []interface{}
	if len(data) != 0 {
		lst = runtime.Deserialize(data).([]interface{})
	}

	lst = append(lst, value)
	data = runtime.Serialize(lst)
	storage.Put(ctx, key, data)

	return true
}

func pubToScriptHash(pkey []byte) []byte {
	pre := []byte{0x21}
	buf := append(pre, pkey...)
	buf = append(buf, 0xac)
	h := crypto.Hash160(buf)
	return h
}

func containsCheck(lst []check, c check) bool {
	for i := 0; i < len(lst); i++ {
		if util.Equals(c, lst[i]) {
			return true
		}
	}
	return false
}
func containsPub(lst []node, elem []byte) bool {
	for i := 0; i < len(lst); i++ {
		e := lst[i]
		if util.Equals(elem, e.pub) {
			return true
		}
	}
	return false
}

func delSerializedIR(ctx storage.Context, key string, value []byte) bool {
	data := storage.Get(ctx, key).([]byte)
	deleted := false

	newList := []node{}
	if len(data) != 0 {
		lst := runtime.Deserialize(data).([]node)
		for i := 0; i < len(lst); i++ {
			n := lst[i]
			if util.Equals(value, n.pub) {
				deleted = true
			} else {
				newList = append(newList, n)
			}
		}
		if deleted {
			data := runtime.Serialize(newList)
			storage.Put(ctx, key, data)
			runtime.Log("target element has been removed")
			return true
		}
	}

	runtime.Log("target element has not been removed")
	return false
}

// isInnerRingRequest returns true if contract was invoked by inner ring node.
func isInnerRingRequest(irList []node) bool {
	for i := 0; i < len(irList); i++ {
		irNode := irList[i]

		if runtime.CheckWitness(irNode.pub) {
			return true
		}
	}

	return false
}

// todo: votes must be from unique inner ring nods
func vote(ctx storage.Context, id []byte) int {
	var (
		newCandidates = []ballot{}
		candidates    = getSerialized(ctx, voteKey).([]ballot)
		found         = -1
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]
		if util.Equals(cnd.id, id) {
			cnd = ballot{id: id, n: cnd.n + 1}
			found = cnd.n
		}
		newCandidates = append(newCandidates, cnd)
	}

	if found < 0 {
		newCandidates = append(newCandidates, ballot{id: id, n: 1})
		found = 1
	}

	data := runtime.Serialize(newCandidates)
	storage.Put(ctx, voteKey, data)

	return found
}

func removeVotes(ctx storage.Context, id []byte) {
	var (
		newCandidates = []ballot{}
		candidates    = getSerialized(ctx, voteKey).([]ballot)
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]
		if !util.Equals(cnd.id, id) {
			newCandidates = append(newCandidates, cnd)
		}
	}

	data := runtime.Serialize(newCandidates)
	storage.Put(ctx, voteKey, data)
}
