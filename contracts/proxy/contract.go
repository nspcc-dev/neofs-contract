package proxy

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
func OnNEP17Payment(from interop.Hash160, amount int, data any) {
	caller := runtime.GetCallingScriptHash()
	if !caller.Equals(gas.Hash) {
		common.AbortWithMessage("proxy contract accepts GAS only")
	}
}

// nolint:unused
func _deploy(data any, isUpdate bool) {
	args := data.([]any)

	if isUpdate {
		version := args[len(args)-1].(int)
		common.CheckVersion(version)

		if version < 27_000 {
			const containerContractKey = "c"

			storage.Delete(storage.GetContext(), []byte(containerContractKey))
		}

		return
	}

	runtime.Log("proxy contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(nefFile, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nefFile, manifest, common.AppendVersion(data))
	runtime.Log("proxy contract updated")
}

// Verify checks for alphabet or committee signature for a transaction.
func Verify() bool {
	var (
		signers          = runtime.CurrentSigners()
		alphabetAddress  = common.AlphabetAddress()
		committeeAddress = common.CommitteeAddress()
	)

	for _, signer := range signers {
		if signer.Account.Equals(alphabetAddress) || signer.Account.Equals(committeeAddress) {
			return true
		}
	}

	return false
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}
