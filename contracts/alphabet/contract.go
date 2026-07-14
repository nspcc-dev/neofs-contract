package alphabet

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
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
	nameKey  = "name"

	emissionDivisor   = 16
	emissionProxyPart = 14
	emissionIRPart    = 1
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS and NEO
// contracts.
func OnNEP17Payment(from interop.Hash160, amount int, data any) {
	caller := runtime.GetCallingScriptHash()
	if !caller.Equals(gas.Hash) && !caller.Equals(neo.Hash) {
		common.AbortWithMessage("alphabet contract accepts GAS and NEO only")
	}
}

// nolint:unused
func _deploy(data any, isUpdate bool) {
	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)

		common.CheckVersion(version)

		if version < 27_000 {
			const totalKey = "threshold"

			storage.LocalDelete([]byte(totalKey))
		}

		return
	}

	args := data.(struct {
		_          bool // notaryDisabled
		addrNetmap interop.Hash160
		addrProxy  interop.Hash160
		name       string
		index      int
		_          int // total
	})

	if len(args.addrNetmap) != interop.Hash160Len {
		args.addrNetmap = common.ResolveFSContract("netmap")
	}
	if len(args.addrProxy) != interop.Hash160Len {
		args.addrProxy = common.ResolveFSContract("proxy")
	}

	storage.LocalPut([]byte(netmapKey), args.addrNetmap)
	storage.LocalPut([]byte(proxyKey), args.addrProxy)
	storage.LocalPut([]byte(nameKey), []byte(args.name))
	storage.LocalPut([]byte(indexKey), convert.ToBytes(args.index))

	runtime.Log(args.name + " contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(nefFile, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nefFile, manifest, common.AppendVersion(data))
	runtime.Log("alphabet contract updated")
}

// Gas returns the amount of FS chain GAS stored in the contract account.
func Gas() int {
	return gas.BalanceOf(runtime.GetExecutingScriptHash())
}

// Neo returns the amount of FS chain NEO stored in the contract account.
func Neo() int {
	return neo.BalanceOf(runtime.GetExecutingScriptHash())
}

func currentEpoch() int {
	netmapContractAddr := interop.Hash160(storage.LocalGet([]byte(netmapKey)))
	return contract.Call(netmapContractAddr, "epoch", contract.ReadOnly).(int)
}

func index() int {
	return convert.ToInteger(storage.LocalGet([]byte(indexKey)))
}

func checkPermission(ir []interop.PublicKey) bool {
	index := index() // read from contract memory

	if len(ir) <= index {
		return false
	}

	node := ir[index]
	return runtime.CheckWitness(node)
}

// Emit method produces FS chain GAS and distributes it among Inner Ring nodes
// and proxy contract. It can be invoked only by an Alphabet node of the Inner Ring.
//
// To produce GAS, an alphabet contract transfers all available NEO from the
// contract account to itself. 7/8 of the GAS in the contract account are
// transferred to proxy contract. 1/16 of the GAS are equally distributed
// among all Inner Ring nodes. Remaining 1/16 of the GAS stay in the contract.
func Emit() {
	alphabet := common.AlphabetNodes()
	if !checkPermission(alphabet) {
		panic("invalid invoker")
	}

	contractHash := runtime.GetExecutingScriptHash()

	if !neo.Transfer(contractHash, contractHash, neo.BalanceOf(contractHash), nil) {
		panic("failed to transfer funds, aborting")
	}

	var (
		gasBalance = gas.BalanceOf(contractHash)
		gasDivided = gasBalance / emissionDivisor
		proxyGas   = gasDivided * emissionProxyPart
		IRGas      = gasDivided * emissionIRPart
	)

	if gasDivided == 0 {
		panic("no gas to emit")
	}

	if !gas.Transfer(contractHash, interop.Hash160(storage.LocalGet([]byte(proxyKey))), proxyGas, nil) {
		runtime.Log("could not transfer GAS to proxy contract")
	}

	runtime.Log("utility token has been emitted to proxy contract")

	innerRing := common.InnerRingNodes()

	gasPerNode := IRGas / len(innerRing)

	if gasPerNode != 0 {
		for _, node := range innerRing {
			address := contract.CreateStandardAccount(node)
			if !gas.Transfer(contractHash, address, gasPerNode, nil) {
				runtime.Log("could not transfer GAS to one of IR node")
			}
		}

		runtime.Log("utility token has been emitted to inner ring nodes")
	}
}

// Vote method votes for the FS chain committee. It requires multisignature from
// Alphabet nodes of the Inner Ring.
//
// This method is used when governance changes the list of Alphabet nodes of the
// Inner Ring. Alphabet nodes share keys with FS chain validators, therefore
// it is required to change them as well. To do that, NEO holders (which are
// alphabet contracts) should vote for a new committee.
func Vote(epoch int, candidates []interop.PublicKey) {
	index := index()
	name := Name()

	common.CheckAlphabetWitness()

	curEpoch := currentEpoch()
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
}

// Name returns the name of the contract set at deployment stage.
func Name() string {
	return string(storage.LocalGet([]byte(nameKey)))
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

// Verify checks whether carrier transaction contains either (2/3N + 1) or
// (N/2 + 1) valid multi-signature of the NeoFS Alphabet.
func Verify() bool {
	return common.ContainsAlphabetWitness()
}
