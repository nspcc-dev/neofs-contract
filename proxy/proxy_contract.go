package proxycontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	version = 1

	netmapContractKey = "netmapScriptHash"
)

func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, []byte(gas.Hash)) {
		panic("onNEP17Payment: alphabet contract accepts GAS only")
	}
}

func Init(owner, addrNetmap interop.Hash160) {
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

func Migrate(script []byte, manifest []byte) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("proxy contract updated")

	return true
}

func Verify() bool {
	ctx := storage.GetReadOnlyContext()
	sig := common.InnerRingMultiAddressViaStorage(ctx, netmapContractKey)

	if !runtime.CheckWitness(sig) {
		sig = common.CommitteeMultiAddressViaStorage(ctx, netmapContractKey)
		return runtime.CheckWitness(sig)
	}

	return true
}

func Version() int {
	return version
}
