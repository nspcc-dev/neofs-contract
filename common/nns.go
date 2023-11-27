package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
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

// ResolveFSContract returns contract hash by name as registered in NNS or
// panics if it can't be resolved. It's similar to [ResolveFSContractWithNNS],
// but retrieves NNS hash automatically (see [InferNNSHash]).
func ResolveFSContract(name string) interop.Hash160 {
	return ResolveFSContractWithNNS(InferNNSHash(), name)
}

// ResolveFSContractWithNNS uses given NNS contract and returns target contract
// hash by name as registered in NNS (assuming NeoFS-specific NNS setup, see
// [NNSID]) or panics if it can't be resolved. Contract name should be
// lowercased and should not include [ContractTLD]. Example values: "netmap",
// "container", etc.
func ResolveFSContractWithNNS(nns interop.Hash160, contractName string) interop.Hash160 {
	resResolve := contract.Call(nns, "resolve", contract.ReadOnly, contractName+"."+ContractTLD, 16 /*TXT*/)
	records := resResolve.([]string)

	if len(records) == 0 {
		panic("did not find a record of the " + contractName + " contract in the NNS")
	}
	if len(records[0]) == 2*interop.Hash160Len {
		var h = make([]byte, interop.Hash160Len)
		for i := 0; i < interop.Hash160Len; i++ {
			ii := (interop.Hash160Len - i - 1) * 2
			h[i] = byte(std.Atoi(records[0][ii:ii+2], 16))
		}
		return h
	}
	return address.ToHash160(records[0])
}
