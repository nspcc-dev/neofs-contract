package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/lib/address"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
)

// ResolveContractHash resolves contract hash by its well-known NeoFS name.
// Contract name should be lowercased, should not include `.neofs` TLD. Example
// values: "netmap", "container", etc.
// Relies on some NeoFS specifics:
//  1. NNS contract should be deployed first (and should have `1` contract ID)
//  2. It should be prefilled with contract hashes by their names (no
//     capitalized chars; no `neofs` TLD)
func ResolveContractHash(contractName string) interop.Hash160 {
	// get NNS contract (it always has ID=1 in the NeoFS Sidechain)
	nnsContract := management.GetContractByID(1)
	if nnsContract == nil {
		panic("missing NNS contract")
	}

	resResolve := contract.Call(nnsContract.Hash, "resolve", contract.ReadOnly,
		contractName+".neofs", 16, // TXT
	)

	records := resResolve.([]string)
	if len(records) == 0 {
		panic("did not find a record of the " + contractName + " contract in the NNS")
	}

	var hash interop.Hash160
	if len(records[0]) == 2*interop.Hash160Len {
		hash = convert.ToBytes(std.Atoi(records[0], 16))
	} else {
		hash = address.ToHash160(records[0])
	}

	return hash
}
