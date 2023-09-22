package nns

import (
	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/util"
)

// ID is the default NNS contract ID in all NeoFS networks. NeoFS networks
// always deploy NNS first and can't work without it, therefore it always gets
// an ID of 1.
const ID = 1

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
