package proxy

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
	netmapContractKey = "netmapScriptHash"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, []byte(gas.Hash)) {
		panic("onNEP17Payment: proxy contract accepts GAS only")
	}
}

func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		return
	}

	args := data.([]interface{})
	owner := args[0].(interop.Hash160)
	addrNetmap := args[1].(interop.Hash160)

	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	if len(addrNetmap) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, common.OwnerKey, owner)
	storage.Put(ctx, netmapContractKey, addrNetmap)

	runtime.Log("proxy contract initialized")
}

// Update method updates contract source code and manifest. Can be invoked
// only by contract owner.
func Update(script []byte, manifest []byte, data interface{}) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	contract.Call(interop.Hash160(management.Hash), "update", contract.All, script, manifest, data)
	runtime.Log("proxy contract updated")

	return true
}

// Verify method returns true if transaction contains valid multi signature of
// Alphabet nodes of the Inner Ring.
func Verify() bool {
	alphabet := neo.GetCommittee()
	sig := common.Multiaddress(alphabet, false)

	if !runtime.CheckWitness(sig) {
		sig = common.Multiaddress(alphabet, true)
		return runtime.CheckWitness(sig)
	}

	return true
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}
