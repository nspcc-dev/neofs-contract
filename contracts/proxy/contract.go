package proxy

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neofs-contract/common"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
func OnNEP17Payment(from interop.Hash160, amount int, data any) {
	caller := runtime.GetCallingScriptHash()
	if !caller.Equals(gas.Hash) {
		common.AbortWithMessage("proxy contract accepts GAS only")
	}
}

// nolint:deadcode,unused
func _deploy(data any, isUpdate bool) {
	if isUpdate {
		args := data.([]any)
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	runtime.Log("proxy contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("proxy contract updated")
}

// Verify checks whether carrier transaction contains either (2/3N + 1) or
// (N/2 + 1) valid multi-signature of the NeoFS Alphabet.
func Verify() bool {
	return common.ContainsAlphabetWitness()
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}
