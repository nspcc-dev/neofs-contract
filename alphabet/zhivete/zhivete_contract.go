package alphabetcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/engine"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

type (
	irNode struct {
		key []byte
	}
)

const (
	// native gas token script hash
	gasHash = "\xbc\xaf\x41\xd6\x84\xc7\xd4\xad\x6e\xe0\xd9\x9d\xa9\x70\x7b\x9d\x1f\x0c\x8e\x66"

	// native neo token script hash
	neoHash = "\x25\x05\x9e\xcb\x48\x78\xd3\xa8\x75\xf9\x1c\x51\xce\xde\xd3\x30\xd4\x57\x5f\xde"

	name  = "Zhivete"
	index = 6

	netmapContractKey = "netmapScriptHash"
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

func checkPermission(ir []irNode) bool {
	if len(ir) <= index {
		return false
	}

	node := ir[index]
	return runtime.CheckWitness(node.key)
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

func Vote(candidate []byte) {
	innerRingKeys := irList()
	if !checkPermission(innerRingKeys) {
		panic("invalid invoker")
	}

	address := runtime.GetExecutingScriptHash()

	ok := engine.AppCall([]byte(neoHash), "vote", address, candidate).(bool)
	if ok {
		runtime.Log("successfully voted for validator")
	} else {
		runtime.Log("vote has been failed")
	}

	return
}

func Name() string {
	return name
}
