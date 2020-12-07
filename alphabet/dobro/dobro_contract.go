package alphabetcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/engine"
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
	gasHash = "\xbc\xaf\x41\xd6\x84\xc7\xd4\xad\x6e\xe0\xd9\x9d\xa9\x70\x7b\x9d\x1f\x0c\x8e\x66"

	// native neo token script hash
	neoHash = "\x25\x05\x9e\xcb\x48\x78\xd3\xa8\x75\xf9\x1c\x51\xce\xde\xd3\x30\xd4\x57\x5f\xde"

	name  = "Dobro"
	index = 4

	netmapContractKey = "netmapScriptHash"

	threshold = totalAlphabetContracts * 2 / 3 + 1
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
	balance := engine.AppCall([]byte(hash), "balanceOf", addr)
	return balance.(int)
}

func irList() []irNode {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	return engine.AppCall(netmapContractAddr, "innerRingList").([]irNode)
}

func currentEpoch() int {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	return engine.AppCall(netmapContractAddr, "epoch").(int)
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

	_ = engine.AppCall([]byte(neoHash), "transfer", contractHash, contractHash, neo)

	gas := balance(gasHash, contractHash)
	gasPerNode := gas * 7 / 8 / len(innerRingKeys)

	if gasPerNode == 0 {
		runtime.Log("no gas to emit")
		return false
	}

	for i := range innerRingKeys {
		node := innerRingKeys[i]
		address := contract.CreateStandardAccount(node.key)

		_ = engine.AppCall([]byte(gasHash), "transfer", contractHash, address, gasPerNode)
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

		ok := engine.AppCall([]byte(neoHash), "vote", address, candidate).(bool)
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
		result []byte
		epochBytes = epoch.([]byte)
	)

	result = append(result, epochBytes...)

	for i := range args {
		result = append(result, args[i]...)
	}

	return crypto.SHA256(result)
}
