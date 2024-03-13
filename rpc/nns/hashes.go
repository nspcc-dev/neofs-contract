package nns

import (
	"errors"
	"strings"

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

// nep18Prefix is the string prefix containing NEP-18 address attribute name.
const nep18Prefix = "address="

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

// NewInferredReader creates an instance of [ContractReader] using hash obtained via
// [InferHash].
func NewInferredReader(sg ContractStateGetter, invoker Invoker) (*ContractReader, error) {
	h, err := InferHash(sg)
	if err != nil {
		return nil, err
	}
	return NewReader(invoker, h), nil
}

// NewInferred creates an instance of [Contract] using hash obtained via [InferHash].
func NewInferred(sg ContractStateGetter, actor Actor) (*Contract, error) {
	h, err := InferHash(sg)
	if err != nil {
		return nil, err
	}
	return New(actor, h), nil
}

// AddressFromRecord extracts [util.Uint160] hash from the string using one of
// the following formats:
//   - hex-encoded LE (reversed) hash (the oldest historically)
//   - plain Neo address string ("Nxxxx")
//   - attribute-based NEP-18 string
//
// NeoFS used both for contract hashes stored in NNS at various stages of its
// development.
//
// See also: [AddressFromRecords].
func AddressFromRecord(s string) (util.Uint160, error) {
	h, err := util.Uint160DecodeStringLE(s)
	if err == nil {
		return h, nil
	}

	h, err = address.StringToUint160(strings.TrimPrefix(s, nep18Prefix))
	if err == nil {
		return h, nil
	}
	return util.Uint160{}, errors.New("no valid address found")
}

// AddressFromRecords extracts [util.Uint160] hash from the set of given
// strings using [AddressFromRecord]. Returns the first result that can be
// interpreted as address.
func AddressFromRecords(strs []string) (util.Uint160, error) {
	for i := range strs {
		h, err := AddressFromRecord(strs[i])
		if err == nil {
			return h, nil
		}
	}
	return util.Uint160{}, errors.New("no valid addresses are found")
}

// ResolveFSContract is a convenience method that doesn't exist in the NNS
// contract itself (it doesn't care which data is stored there). It assumes
// that contracts follow the [ContractTLD] convention, gets simple contract
// names (like "container" or "netmap") and extracts the hash for the
// respective NNS record using [AddressFromRecords].
func (c *ContractReader) ResolveFSContract(name string) (util.Uint160, error) {
	strs, err := c.Resolve(name+"."+ContractTLD, TXT)
	if err != nil {
		return util.Uint160{}, err
	}
	return AddressFromRecords(strs)
}
