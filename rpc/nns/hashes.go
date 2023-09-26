package nns

import (
	"errors"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/util"
)

// ID is the default NNS contract ID in all NeoFS networks. NeoFS networks
// always deploy NNS first and can't work without it, therefore it always gets
// an ID of 1.
const ID = 1

// ContractTLD is the default TLD for NeoFS contracts. It's a convention that
// is not likely to be used by any non-NeoFS networks, but for NeoFS ones it
// allows to find contract hashes more easily.
const ContractTLD = "neofs"

// ContractStateGetter is the interface required for contract state resolution
// using a known contract ID.
type ContractStateGetter interface {
	GetContractStateByID(int32) (*state.Contract, error)
}

// InferHash simplifies resolving NNS contract hash in existing NeoFS networks.
// It assumes that NNS follows [ID] assignment assumptions which likely won't
// be the case for any non-NeoFS network.
func InferHash(sg ContractStateGetter) (util.Uint160, error) {
	c, err := sg.GetContractStateByID(ID)
	if err != nil {
		return util.Uint160{}, err
	}

	return c.Hash, nil
}

// ResolveFSContract is a convenience method that doesn't exist in the NNS
// contract itself (it doesn't care which data is stored there). It assumes
// that contracts follow the [ContractTLD] convention, gets simple contract
// names (like "container" or "netmap") and extracts the hash for the
// respective NNS record in any of the formats (of which historically there's
// been a few).
func (c *ContractReader) ResolveFSContract(name string) (util.Uint160, error) {
	strs, err := c.Resolve(name+"."+ContractTLD, TXT)
	if err != nil {
		return util.Uint160{}, err
	}
	for i := range strs {
		h, err := util.Uint160DecodeStringLE(strs[i])
		if err == nil {
			return h, nil
		}

		h, err = address.StringToUint160(strs[i])
		if err == nil {
			return h, nil
		}
	}
	return util.Uint160{}, errors.New("no valid hashes are found")
}
