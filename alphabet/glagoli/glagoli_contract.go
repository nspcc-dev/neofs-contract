package alphabetcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

type (
	irNode struct {
		key []byte
	}

	ballot struct {
		id     []byte   // hash of validators list
		n      [][]byte // already voted inner ring nodes
		height int      // height is an neofs epoch when ballot was registered
	}
)

const (
	// native gas token script hash
	gasHash = "\x20\x2a\x2d\xd3\xf6\xa6\xe8\xf4\xd3\xb5\xaf\x26\xa7\xfe\xde\xba\x4b\x80\xdf\xb5"

	// native neo token script hash
	neoHash = "\xb9\x7b\x8d\x5a\x73\x11\x81\x6f\xf7\xb1\xbf\xb0\x14\x20\xe2\x59\x0d\xa3\xc1\x24"

	name  = "Glagoli"
	index = 3

	netmapContractKey = "netmapScriptHash"

	threshold = totalAlphabetContracts*2/3 + 1
	voteKey   = "ballots"

	totalAlphabetContracts = 7
)

var (
	ctx storage.Context
)

func init() {
	if runtime.GetTrigger() != runtime.Application {
		panic("contract has not been called in application node")
	}

	ctx = storage.GetContext()
}

// OnPayment is a callback for NEP-17 compatible native GAS and NEO contracts.
func OnPayment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !bytesEqual(caller, []byte(gasHash)) && !bytesEqual(caller, []byte(neoHash)) {
		panic("onPayment: alphabet contract accepts GAS and NEO only")
	}
}

func Init(addrNetmap []byte) {
	if storage.Get(ctx, netmapContractKey) != nil {
		panic("contract already deployed")
	}

	if len(addrNetmap) != 20 {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, addrNetmap)

	setSerialized(ctx, voteKey, []ballot{})

	runtime.Log(name + " contract initialized")
}

func Gas() int {
	contractHash := runtime.GetExecutingScriptHash()
	return balance(gasHash, contractHash)
}

func Neo() int {
	contractHash := runtime.GetExecutingScriptHash()
	return balance(neoHash, contractHash)
}

func balance(hash string, addr []byte) int {
	balance := contract.Call([]byte(hash), "balanceOf", addr)
	return balance.(int)
}

func irList() []irNode {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	return contract.Call(netmapContractAddr, "innerRingList").([]irNode)
}

func currentEpoch() int {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	return contract.Call(netmapContractAddr, "epoch").(int)
}

func checkPermission(ir []irNode) bool {
	if len(ir) <= index {
		return false
	}

	node := ir[index]
	return runtime.CheckWitness(node.key)
}

func innerRingInvoker(ir []irNode) []byte {
	for i := 0; i < len(ir); i++ {
		if i >= totalAlphabetContracts {
			return nil
		}

		node := ir[i]
		if runtime.CheckWitness(node.key) {
			return node.key
		}
	}

	return nil
}

func Emit() bool {
	innerRingKeys := irList()
	if !checkPermission(innerRingKeys) {
		panic("invalid invoker")
	}

	contractHash := runtime.GetExecutingScriptHash()
	neo := balance(neoHash, contractHash)

	_ = contract.Call([]byte(neoHash), "transfer", contractHash, contractHash, neo, nil)

	gas := balance(gasHash, contractHash)
	gasPerNode := gas * 7 / 8 / len(innerRingKeys)

	if gasPerNode == 0 {
		runtime.Log("no gas to emit")
		return false
	}

	for i := range innerRingKeys {
		node := innerRingKeys[i]
		address := contract.CreateStandardAccount(node.key)

		_ = contract.Call([]byte(gasHash), "transfer", contractHash, address, gasPerNode, nil)
	}

	runtime.Log("utility token has been emitted to inner ring nodes")
	return true
}

func Vote(epoch int, candidates [][]byte) {
	innerRingKeys := irList()

	key := innerRingInvoker(innerRingKeys)
	if len(key) == 0 {
		panic("invalid invoker")
	}

	curEpoch := currentEpoch()
	if epoch != curEpoch {
		panic("invalid epoch")
	}

	id := voteID(epoch, candidates)
	n := vote(ctx, curEpoch, id, key)

	if n >= threshold {
		candidate := candidates[index%len(candidates)]
		address := runtime.GetExecutingScriptHash()

		ok := contract.Call([]byte(neoHash), "vote", address, candidate).(bool)
		if ok {
			runtime.Log(name + ": successfully voted for validator")
			removeVotes(ctx, id)
		} else {
			runtime.Log(name + ": vote has been failed")
		}
	} else {
		runtime.Log(name + ": saved vote for validator")
	}

	return
}

func Name() string {
	return name
}

func vote(ctx storage.Context, epoch int, id, from []byte) int {
	var (
		newCandidates []ballot
		candidates    = getBallots(ctx)
		found         = -1
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
			cnd = ballot{id: id, n: voters, height: epoch}
			found = len(voters)
		}

		// add only valid ballots with current epochs
		if cnd.height == epoch {
			newCandidates = append(newCandidates, cnd)
		}
	}

	if found < 0 {
		voters := [][]byte{from}
		newCandidates = append(newCandidates, ballot{
			id:     id,
			n:      voters,
			height: epoch})
		found = 1
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

func getBallots(ctx storage.Context) []ballot {
	data := storage.Get(ctx, voteKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]ballot)
	}

	return []ballot{}
}

func setSerialized(ctx storage.Context, key interface{}, value interface{}) {
	data := binary.Serialize(value)
	storage.Put(ctx, key, data)
}

// neo-go#1176
func bytesEqual(a []byte, b []byte) bool {
	return util.Equals(string(a), string(b))
}

func voteID(epoch interface{}, args [][]byte) []byte {
	var (
		result     []byte
		epochBytes = epoch.([]byte)
	)

	result = append(result, epochBytes...)

	for i := range args {
		result = append(result, args[i]...)
	}

	return crypto.SHA256(result)
}
