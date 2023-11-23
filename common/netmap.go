package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/lib/address"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
)

// SubscribeForNewEpoch registers calling contract as a NewEpoch
// callback requester. Netmap contract's address is taken from the
// NNS contract, therefore, it must be presented and filled with
// netmap information for a correct SubscribeForNewEpoch call; otherwise
// a successive call is not guaranteed.
// Caller must have `NewEpoch` method with a single numeric argument.
func SubscribeForNewEpoch() {
	nnsContract := management.GetContractByID(1)
	if nnsContract == nil {
		panic("missing NNS contract")
	}

	resolveRes := contract.Call(nnsContract.Hash, "resolve", contract.ReadOnly, "netmap.neofs", 16)
	records := resolveRes.([]string)
	if len(records) == 0 {
		panic("did not find a record of the Netmap contract in the NNS")
	}

	var netmapContract interop.Hash160
	if len(records[0]) == 2*interop.Hash160Len {
		netmapContract = convert.ToBytes(std.Atoi(records[0], 16))
	} else {
		netmapContract = address.ToHash160(records[0])
	}

	contract.Call(netmapContract, "subscribeForNewEpoch", contract.All, runtime.GetExecutingScriptHash())
}
