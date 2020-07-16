package smart_contract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/blockchain"
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/engine"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

type (
	ballot struct {
		id    []byte   // id of the voting decision
		n     [][]byte // already voted inner ring nodes
		block int      // block with the last vote
	}

	node struct {
		pub []byte
	}

	cheque struct {
		id []byte
	}
)

const (
	tokenHash             = "\x3b\x7d\x37\x11\xc6\xf0\xcc\xf9\xb1\xdc\xa9\x03\xd1\xbf\xa1\xd8\x96\xf1\x23\x8c"
	innerRingCandidateFee = 100 * 1000 * 1000 // 10^8
	version               = 2
	innerRingKey          = "innerring"
	voteKey               = "ballots"
	candidatesKey         = "candidates"
	cashedChequesKey      = "cheques"
	blockDiff             = 20 // change base on performance evaluation
	publicKeySize         = 33
)

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
	case "Init":
		if storage.Get(ctx, innerRingKey) != nil {
			panic("neofs: contract already deployed")
		}

		var irList []node

		for i := 0; i < len(args); i++ {
			pub := args[i].([]byte)
			irList = append(irList, node{pub: pub})
		}

		// initialize all storage slices
		setSerialized(ctx, innerRingKey, irList)
		setSerialized(ctx, voteKey, []ballot{})
		setSerialized(ctx, candidatesKey, []node{})
		setSerialized(ctx, cashedChequesKey, []cheque{})

		runtime.Log("neofs: contract initialized")

		return true
	case "InnerRingList":
		return getInnerRingNodes(ctx)
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
		to := runtime.GetExecutingScriptHash()
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
		if len(args) < 2 || len(args) > 3 {
			panic("deposit: bad arguments")
		}

		from := args[0].([]byte)
		if !runtime.CheckWitness(from) {
			panic("deposit: you should be the owner of the wallet")
		}

		amount := args[1].(int)
		if amount > 0 {
			amount = amount * 100000000
		}

		to := runtime.GetExecutingScriptHash()
		params := []interface{}{from, to, amount}

		transferred := engine.AppCall([]byte(tokenHash), "transfer", params).(bool)
		if !transferred {
			panic("deposit: failed to transfer funds, aborting")
		}

		runtime.Log("deposit: funds have been transferred")

		var rcv = from
		if len(args) == 3 {
			rcv = args[2].([]byte) // todo: check if rcv value is valid
		}

		tx := runtime.GetScriptContainer()
		runtime.Notify("Deposit", from, amount, rcv, tx.Hash)

		return true
	case "Withdraw":
		if len(args) != 2 {
			panic("withdraw: bad arguments")
		}

		user := args[0].([]byte)
		if !runtime.CheckWitness(user) {
			panic("withdraw: you should be the owner of the wallet")
		}

		amount := args[1].(int)
		if amount > 0 {
			amount = amount * 100000000
		}

		tx := runtime.GetScriptContainer()
		runtime.Notify("Withdraw", user, amount, tx.Hash)

		return true
	case "Cheque":
		if len(args) != 4 {
			panic("cheque: bad arguments")
		}

		id := args[0].([]byte)      // unique cheque id
		user := args[1].([]byte)    // GAS receiver
		amount := args[2].(int)     // amount of GAS
		lockAcc := args[3].([]byte) // lock account from internal balance contract

		irList := getInnerRingNodes(ctx)
		threshold := len(irList)/3*2 + 1

		cashedCheques := getCashedCheques(ctx)
		hashID := crypto.SHA256(id)

		irKey := innerRingInvoker(irList)
		if len(irKey) == 0 {
			panic("cheque: invoked by non inner ring node")
		}

		c := cheque{id: id}

		list, ok := addCheque(cashedCheques, c)
		if !ok {
			panic("cheque: non unique id")
		}

		n := vote(ctx, hashID, irKey)
		if n >= threshold {
			removeVotes(ctx, hashID)

			from := runtime.GetExecutingScriptHash()
			params := []interface{}{from, user, amount}

			transferred := engine.AppCall([]byte(tokenHash), "transfer", params).(bool)
			if !transferred {
				panic("cheque: failed to transfer funds, aborting")
			}

			runtime.Log("cheque: funds have been transferred")

			setSerialized(ctx, cashedChequesKey, list)
			runtime.Notify("Cheque", id, user, amount, lockAcc)
		}

		return true
	case "Bind", "Unbind":
		if len(args) < 2 {
			panic("binding: bad arguments")
		}

		user := args[0].([]byte)
		if !runtime.CheckWitness(user) {
			panic("binding: you should be the owner of the wallet")
		}

		var keys [][]byte
		for i := 1; i < len(args); i++ {
			pub := args[i].([]byte)
			if len(pub) != publicKeySize {
				panic("binding: incorrect public key size")
			}

			keys = append(keys, pub)
		}

		runtime.Notify(op, user, keys)

		return true
	case "InnerRingUpdate":
		data := args[0].([]byte)

		id := data[:8]
		var ln interface{} = data[8:10]
		listItemCount := ln.(int)
		listSize := listItemCount * 33

		offset := 8 + 2 + listSize

		irList := getSerialized(ctx, "InnerRingList").([]node)
		usedList := getSerialized(ctx, "UsedVerifCheckList").([]cheque)
		threshold := len(irList)/3*2 + 1

		irKey := innerRingInvoker(irList)
		if len(irKey) == 0 {
			panic("innerRingUpdate: invoked by non inner ring node")
		}

		c := cheque{id: id}
		if containsCheck(usedList, c) {
			panic("innerRingUpdate: cheque has non unique id")
		}

		chequeHash := crypto.SHA256(data)

		n := vote(ctx, chequeHash, irKey)
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

			newIRData := binary.Serialize(newIR)
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

		irList := getInnerRingNodes(ctx)
		for i := range irList {
			node := irList[i]

			if bytesEqual(node.pub, key) {
				return true
			}
		}

		return false
	case "Version":
		return version
	}

	panic("unknown operation")
}

// fixme: use strict type deserialization wrappers
func getSerialized(ctx storage.Context, key string) interface{} {
	data := storage.Get(ctx, key).([]byte)
	if len(data) != 0 {
		return binary.Deserialize(data)
	}
	return nil
}

func delSerialized(ctx storage.Context, key string, value []byte) bool {
	data := storage.Get(ctx, key).([]byte)
	deleted := false

	var newList [][]byte
	if len(data) != 0 {
		lst := binary.Deserialize(data).([][]byte)
		for i := 0; i < len(lst); i++ {
			if util.Equals(value, lst[i]) {
				deleted = true
			} else {
				newList = append(newList, lst[i])
			}
		}
		if deleted {
			if len(newList) != 0 {
				data := binary.Serialize(newList)
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
		lst = binary.Deserialize(data).([]interface{})
	}

	lst = append(lst, value)
	data = binary.Serialize(lst)
	storage.Put(ctx, key, data)

	return true
}

func pubToScriptHash(pkey []byte) []byte {
	// pre := []byte{0x21}
	// buf := append(pre, pkey...)
	// buf = append(buf, 0xac)
	// h := crypto.Hash160(buf)
	//
	// return h

	// fixme: someday ripemd syscall will appear
	//        or simply store script-hashes along with public key
	return []byte{0x0F, 0xED}
}

func containsCheck(lst []cheque, c cheque) bool {
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
		lst := binary.Deserialize(data).([]node)
		for i := 0; i < len(lst); i++ {
			n := lst[i]
			if util.Equals(value, n.pub) {
				deleted = true
			} else {
				newList = append(newList, n)
			}
		}
		if deleted {
			data := binary.Serialize(newList)
			storage.Put(ctx, key, data)
			runtime.Log("target element has been removed")
			return true
		}
	}

	runtime.Log("target element has not been removed")
	return false
}

// innerRingInvoker returns public key of inner ring node that invoked contract.
func innerRingInvoker(ir []node) []byte {
	for i := 0; i < len(ir); i++ {
		node := ir[i]
		if runtime.CheckWitness(node.pub) {
			return node.pub
		}
	}

	return nil
}

func vote(ctx storage.Context, id, from []byte) int {
	var (
		newCandidates []ballot
		candidates    = getBallots(ctx)
		found         = -1
		blockHeight   = blockchain.GetHeight()
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]
		if bytesEqual(cnd.id, id) {
			voters := cnd.n

			for j := range voters {
				if bytesEqual(voters[j], from) {
					return len(voters)
				}
			}

			voters = append(voters, from)
			cnd = ballot{id: id, n: voters, block: blockHeight}
			found = len(voters)
		}

		// do not add old ballots, they are invalid
		if blockHeight-cnd.block <= blockDiff {
			newCandidates = append(newCandidates, cnd)
		}
	}

	if found < 0 {
		found = 1
		voters := [][]byte{from}

		newCandidates = append(newCandidates, ballot{
			id:    id,
			n:     voters,
			block: blockHeight})
	}

	setSerialized(ctx, voteKey, newCandidates)

	return found
}

func removeVotes(ctx storage.Context, id []byte) {
	var (
		newCandidates []ballot
		candidates    = getBallots(ctx)
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]
		if !bytesEqual(cnd.id, id) {
			newCandidates = append(newCandidates, cnd)
		}
	}

	setSerialized(ctx, voteKey, newCandidates)
}

// setSerialized serializes data and puts it into contract storage.
func setSerialized(ctx storage.Context, key interface{}, value interface{}) {
	data := binary.Serialize(value)
	storage.Put(ctx, key, data)
}

// getInnerRingNodes returns deserialized slice of inner ring nodes from storage.
func getInnerRingNodes(ctx storage.Context) []node {
	data := storage.Get(ctx, innerRingKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]node)
	}

	return []node{}
}

// getInnerRingNodes returns deserialized slice of used cheques.
func getCashedCheques(ctx storage.Context) []cheque {
	data := storage.Get(ctx, cashedChequesKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]cheque)
	}

	return []cheque{}
}

// getInnerRingNodes returns deserialized slice of vote ballots.
func getBallots(ctx storage.Context) []ballot {
	data := storage.Get(ctx, voteKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]ballot)
	}

	return []ballot{}
}

// addCheque returns slice of cheques with appended cheque 'c' and bool flag
// that set to false if cheque 'c' is already presented in the slice 'lst'.
func addCheque(lst []cheque, c cheque) ([]cheque, bool) {
	for i := 0; i < len(lst); i++ {
		if bytesEqual(c.id, lst[i].id) {
			return nil, false
		}
	}

	lst = append(lst, c)
	return lst, true
}

// bytesEqual compares two slice of bytes by wrapping them into strings,
// which is necessary with new util.Equal interop behaviour, see neo-go#1176.
func bytesEqual(a []byte, b []byte) bool {
	return util.Equals(string(a), string(b))
}
