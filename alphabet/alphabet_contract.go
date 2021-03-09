package alphabetcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	netmapKey = "netmapScriptHash"
	proxyKey  = "proxyScriptHash"

	indexKey = "index"
	totalKey = "threshold"
	nameKey  = "name"

	version = 1
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS and NEO contracts.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, []byte(gas.Hash)) && !common.BytesEqual(caller, []byte(neo.Hash)) {
		panic("onNEP17Payment: alphabet contract accepts GAS and NEO only")
	}
}

func Init(owner interop.Hash160, addrNetmap, addrProxy interop.Hash160, name string, index, total int) {
	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	if len(addrNetmap) != 20 || len(addrProxy) != 20 {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, common.OwnerKey, owner)
	storage.Put(ctx, netmapKey, addrNetmap)
	storage.Put(ctx, proxyKey, addrProxy)
	storage.Put(ctx, nameKey, name)
	storage.Put(ctx, indexKey, index)
	storage.Put(ctx, totalKey, total)

	runtime.Log(name + " contract initialized")
}

func Migrate(script []byte, manifest []byte) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("alphabet contract updated")

	return true
}

func Gas() int {
	return gas.BalanceOf(runtime.GetExecutingScriptHash())
}

func Neo() int {
	return neo.BalanceOf(runtime.GetExecutingScriptHash())
}

func irList(ctx storage.Context) []common.IRNode {
	return common.InnerRingListViaStorage(ctx, netmapKey)
}

func currentEpoch(ctx storage.Context) int {
	netmapContractAddr := storage.Get(ctx, netmapKey).(interop.Hash160)
	return contract.Call(netmapContractAddr, "epoch", contract.ReadOnly).(int)
}

func name(ctx storage.Context) string {
	return storage.Get(ctx, nameKey).(string)
}

func index(ctx storage.Context) int {
	return storage.Get(ctx, indexKey).(int)
}

func total(ctx storage.Context) int {
	return storage.Get(ctx, totalKey).(int)
}

func checkPermission(ir []common.IRNode) bool {
	ctx := storage.GetReadOnlyContext()
	index := index(ctx) // read from contract memory

	if len(ir) <= index {
		return false
	}

	node := ir[index]
	return runtime.CheckWitness(node.PublicKey)
}

func Emit() bool {
	ctx := storage.GetReadOnlyContext()

	innerRingKeys := irList(ctx)
	if !checkPermission(innerRingKeys) {
		panic("invalid invoker")
	}

	contractHash := runtime.GetExecutingScriptHash()

	neo.Transfer(contractHash, contractHash, neo.BalanceOf(contractHash), nil)

	gasBalance := gas.BalanceOf(contractHash)
	proxyAddr := storage.Get(ctx, proxyKey).(interop.Hash160)

	proxyGas := gasBalance / 2
	if proxyGas == 0 {
		runtime.Log("no gas to emit")
		return false
	}

	gas.Transfer(contractHash, proxyAddr, proxyGas, nil)
	runtime.Log("utility token has been emitted to proxy contract")

	gasPerNode := gasBalance / 2 * 7 / 8 / len(innerRingKeys)

	if gasPerNode != 0 {
		for _, node := range innerRingKeys {
			address := contract.CreateStandardAccount(node.PublicKey)
			gas.Transfer(contractHash, address, gasPerNode, nil)
		}

		runtime.Log("utility token has been emitted to inner ring nodes")
	}

	return true
}

func Vote(epoch int, candidates []interop.PublicKey) {
	ctx := storage.GetReadOnlyContext()
	index := index(ctx)
	name := name(ctx)

	multiaddr := common.InnerRingMultiAddressViaStorage(ctx, netmapKey)
	if !runtime.CheckWitness(multiaddr) {
		panic("invalid invoker")
	}

	curEpoch := currentEpoch(ctx)
	if epoch != curEpoch {
		panic("invalid epoch")
	}

	candidate := candidates[index%len(candidates)]
	address := runtime.GetExecutingScriptHash()

	ok := neo.Vote(address, candidate)
	if ok {
		runtime.Log(name + ": successfully voted for validator")
	} else {
		runtime.Log(name + ": vote has been failed")
	}

	return
}

func Name() string {
	ctx := storage.GetReadOnlyContext()
	return name(ctx)
}

func Version() int {
	return version
}
