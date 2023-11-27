package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/lib/address"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
)

// NNSID is the ID of the NNS contract in NeoFS networks. It's always deployed
// first.
const NNSID = 1

// ContractTLD is the default domain used by NeoFS contracts.
const ContractTLD = "neofs"

// InferNNSHash returns NNS contract hash by [NNSID] or panics if
// it can't be resolved.
func InferNNSHash() interop.Hash160 {
	var nns = management.GetContractByID(NNSID)
	if nns == nil {
		panic("no NNS contract")
	}
	return nns.Hash
}

// ResolveFSContract resolves contract hash by its well-known NeoFS name.
// Contract name should be lowercased, should not include [ContractTLD]. Example
// values: "netmap", "container", etc. Relies on NeoFS-specific NNS setup, see
// [NNSID].
func ResolveFSContract(contractName string) interop.Hash160 {
	var nns = InferNNSHash()

	resResolve := contract.Call(nns, "resolve", contract.ReadOnly, contractName+"."+ContractTLD, 16 /*TXT*/)
	records := resResolve.([]string)

	if len(records) == 0 {
		panic("did not find a record of the " + contractName + " contract in the NNS")
	}
	if len(records[0]) == 2*interop.Hash160Len {
		return convert.ToBytes(std.Atoi(records[0], 16))
	}
	return address.ToHash160(records[0])
}
