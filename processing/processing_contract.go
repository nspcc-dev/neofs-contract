package processing

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/roles"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	neofsContractKey = "neofsScriptHash"

	multiaddrMethod = "alphabetAddress"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, []byte(gas.Hash)) {
		panic("onNEP17Payment: processing contract accepts GAS only")
	}
}

func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		return
	}

	arr := data.([]interop.Hash160)
	addrNeoFS := arr[0]

	ctx := storage.GetContext()

	if len(addrNeoFS) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, neofsContractKey, addrNeoFS)

	runtime.Log("processing contract initialized")
}

// Update method updates contract source code and manifest. Can be invoked
// only by side chain committee.
func Update(script []byte, manifest []byte, data interface{}) {
	blockHeight := ledger.CurrentIndex()
	alphabetKeys := roles.GetDesignatedByRole(roles.NeoFSAlphabet, uint32(blockHeight))
	alphabetCommittee := common.Multiaddress(alphabetKeys, true)

	if !runtime.CheckWitness(alphabetCommittee) {
		panic("only side chain committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("processing contract updated")
}

// Verify method returns true if transaction contains valid multi signature of
// Alphabet nodes of the Inner Ring.
func Verify() bool {
	ctx := storage.GetContext()
	neofsContractAddr := storage.Get(ctx, neofsContractKey).(interop.Hash160)
	multiaddr := contract.Call(neofsContractAddr, multiaddrMethod, contract.ReadOnly).(interop.Hash160)

	return runtime.CheckWitness(multiaddr)
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}
