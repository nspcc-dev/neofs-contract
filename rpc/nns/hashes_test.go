package nns

import (
	"errors"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/stretchr/testify/require"
)

type stateGetter struct {
	f func(int32) (*state.Contract, error)
}

func (s stateGetter) GetContractStateByID(id int32) (*state.Contract, error) {
	return s.f(id)
}

func TestInferHash(t *testing.T) {
	var sg stateGetter
	sg.f = func(int32) (*state.Contract, error) {
		return nil, errors.New("bad")
	}
	_, err := InferHash(sg)
	require.Error(t, err)
	sg.f = func(int32) (*state.Contract, error) {
		return &state.Contract{
			ContractBase: state.ContractBase{
				Hash: util.Uint160{0x01, 0x02, 0x03},
			},
		}, nil
	}
	h, err := InferHash(sg)
	require.NoError(t, err)
	require.Equal(t, util.Uint160{0x01, 0x02, 0x03}, h)
}
