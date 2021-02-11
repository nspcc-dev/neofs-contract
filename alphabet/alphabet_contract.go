package alphabetcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	netmapKey = "netmapScriptHash"
	indexKey  = "index"
	totalKey  = "threshold"
	nameKey   = "name"
	voteKey   = "ballots"
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

// OnNEP17Payment is a callback for NEP-17 compatible native GAS and NEO contracts.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, []byte(gas.Hash)) && !common.BytesEqual(caller, []byte(neo.Hash)) {
		panic("onNEP17Payment: alphabet contract accepts GAS and NEO only")
	}
}

func Init(addrNetmap []byte, name string, index, total int) {
	if storage.Get(ctx, netmapKey) != nil {
		panic("contract already deployed")
	}

	if len(addrNetmap) != 20 {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapKey, addrNetmap)
	storage.Put(ctx, nameKey, name)
	storage.Put(ctx, indexKey, index)
	storage.Put(ctx, totalKey, total)

	common.SetSerialized(ctx, voteKey, []common.Ballot{})

	runtime.Log(name + " contract initialized")
}

func Gas() int {
	return gas.BalanceOf(runtime.GetExecutingScriptHash())
}

func Neo() int {
	return neo.BalanceOf(runtime.GetExecutingScriptHash())
}

func irList() []common.IRNode {
	return common.InnerRingListViaStorage(ctx, netmapKey)
}

func currentEpoch() int {
	netmapContractAddr := storage.Get(ctx, netmapKey).([]byte)
	return contract.Call(netmapContractAddr, "epoch", contract.ReadOnly).(int)
}

func name() string {
	return storage.Get(ctx, nameKey).(string)
}

func index() int {
	return storage.Get(ctx, indexKey).(int)
}

func total() int {
	return storage.Get(ctx, totalKey).(int)
}

func checkPermission(ir []common.IRNode) bool {
	index := index() // read from contract memory

	if len(ir) <= index {
		return false
	}

	node := ir[index]
	return runtime.CheckWitness(node.PublicKey)
}

func Emit() bool {
	innerRingKeys := irList()
	if !checkPermission(innerRingKeys) {
		panic("invalid invoker")
	}

	contractHash := runtime.GetExecutingScriptHash()

	_ = neo.Transfer(contractHash, contractHash, neo.BalanceOf(contractHash), nil)

	gasBalance := gas.BalanceOf(contractHash)
	gasPerNode := gasBalance * 7 / 8 / len(innerRingKeys)

	if gasPerNode == 0 {
		runtime.Log("no gas to emit")
		return false
	}

	for i := range innerRingKeys {
		node := innerRingKeys[i]
		address := contract.CreateStandardAccount(node.PublicKey)

		_ = gas.Transfer(contractHash, address, gasPerNode, nil)
	}

	runtime.Log("utility token has been emitted to inner ring nodes")
	return true
}

func Vote(epoch int, candidates [][]byte) {
	innerRingKeys := irList()
	threshold := total()/3*2 + 1
	index := index()
	name := name()

	key := common.InnerRingInvoker(innerRingKeys)
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

		ok := neo.Vote(address, candidate)
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
	return name()
}

func vote(ctx storage.Context, epoch int, id, from []byte) int {
	var (
		newCandidates []common.Ballot
		candidates    = getBallots(ctx)
		found         = -1
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]
		if common.BytesEqual(cnd.ID, id) {
			voters := cnd.Voters

			for j := range voters {
				if common.BytesEqual(voters[j], from) {
					return len(voters)
				}
			}

			voters = append(voters, from)
			cnd = common.Ballot{ID: id, Voters: voters, Height: epoch}
			found = len(voters)
		}

		// add only valid ballots with current epochs
		if cnd.Height == epoch {
			newCandidates = append(newCandidates, cnd)
		}
	}

	if found < 0 {
		voters := [][]byte{from}
		newCandidates = append(newCandidates, common.Ballot{
			ID:     id,
			Voters: voters,
			Height: epoch})
		found = 1
	}

	common.SetSerialized(ctx, voteKey, newCandidates)

	return found
}

func removeVotes(ctx storage.Context, id []byte) {
	var (
		newCandidates []common.Ballot
		candidates    = getBallots(ctx)
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]
		if !common.BytesEqual(cnd.ID, id) {
			newCandidates = append(newCandidates, cnd)
		}
	}

	common.SetSerialized(ctx, voteKey, newCandidates)
}

func getBallots(ctx storage.Context) []common.Ballot {
	data := storage.Get(ctx, voteKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]common.Ballot)
	}

	return []common.Ballot{}
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
