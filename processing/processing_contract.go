package processing

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	version = 1

	neofsContractKey = "neofsScriptHash"

	multiaddrMethod = "alphabetAddress"
)

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
	owner := arr[0]
	addrNeoFS := arr[1]

	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	if len(addrNeoFS) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, common.OwnerKey, owner)
	storage.Put(ctx, neofsContractKey, addrNeoFS)

	runtime.Log("processing contract initialized")
}

func Migrate(script []byte, manifest []byte, data interface{}) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	contract.Call(interop.Hash160(management.Hash), "update", contract.All, script, manifest, data)
	runtime.Log("processing contract updated")

	return true
}

func Verify() bool {
	ctx := storage.GetContext()
	neofsContractAddr := storage.Get(ctx, neofsContractKey).(interop.Hash160)
	multiaddr := contract.Call(neofsContractAddr, multiaddrMethod, contract.ReadOnly).(interop.Hash160)

	return runtime.CheckWitness(multiaddr)
}

func Version() int {
	return version
}
