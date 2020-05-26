package smart_contract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/engine"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

type node struct {
	pub []byte
}

type check struct {
	id     []byte
	height []byte
}

// GAS NEP-5 HASH
const tokenHash = "\x77\xea\x59\x6b\x7a\xdf\x7e\x4d\xd1\x40\x76\x97\x31\xb7\xd2\xf0\xe0\x6b\xcd\x9b"

const innerRingCandidateFee = 100 * 1000 * 1000 // 10^8

const version = 1

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

		return true
	case "Withdraw":
		from := engine.GetExecutingScriptHash()
		data := args[0].([]byte)
		message := data[0:66]
		uuid := data[0:25]
		owner := data[25:50]
		value := data[50:58]
		height := data[58:66]
		offset := 68
		usedList := getSerialized(ctx, "UsedVerifCheckList").([]check)

		c := check{
			id:     uuid,
			height: height,
		}
		if containsCheck(usedList, c) {
			panic("verification check has already been used")
		}

		irList := getSerialized(ctx, "InnerRingList").([]node)
		if !verifySignatures(irList, data, message, offset) {
			panic("can't verify signatures")
		}

		h := owner[1:21]
		params := []interface{}{from, h, value}
		transferred := engine.AppCall([]byte(tokenHash), "transfer", params).(bool)
		if !transferred {
			panic("failed to transfer funds, aborting")
		}

		putSerialized(ctx, "UsedVerifCheckList", c)

		return true
	case "InnerRingUpdate":
		data := args[0].([]byte)

		id := data[:8]
		var ln interface{} = data[8:10]
		listItemCount := ln.(int)
		listSize := listItemCount * 33

		offset := 8 + 2 + listSize
		msg := data[:offset]
		message := crypto.SHA256(msg)

		irList := getSerialized(ctx, "InnerRingList").([]node)
		if !verifySignatures(irList, data, message, offset) {
			panic("can't verify signatures")
		}

		usedList := getSerialized(ctx, "UsedVerifCheckList").([]check)
		c := check{
			id:     id,
			height: []byte{1}, // ir update cheques use height as id
		}
		if containsCheck(usedList, c) {
			panic("check has already been used")
		}

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
func verifySignatures(irList []node, data []byte, message []byte, offset int) bool {
	n := len(irList)
	f := (n - 1) / 3
	s := n - f
	if s < 3 {
		runtime.Log("not enough inner ring nodes for consensus")
		return false
	}

	used := [][]byte{}
	count := 0
	for count < s && offset < len(data) {
		pubkey := data[offset : offset+33]
		signature := data[offset+33 : offset+97]
		if containsPub(irList, pubkey) {
			if crypto.VerifySignature(message, signature, pubkey) {
				count++
				for i := 0; i < len(used); i++ {
					if util.Equals(used[i], pubkey) {
						panic("duplicate public keys")
					}
				}
				used = append(used, pubkey)
			}
		}
		offset += 97
	}

	if count >= s {
		return true
	}

	runtime.Log("not enough verified signatures")
	return false
}
