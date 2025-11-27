package nns

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-sdk-go/user"
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

type testInv struct {
	err error
	res *result.Invoke
}

func (t *testInv) Call(contract util.Uint160, operation string, params ...any) (*result.Invoke, error) {
	return t.res, t.err
}

func (t *testInv) CallAndExpandIterator(contract util.Uint160, operation string, i int, params ...any) (*result.Invoke, error) {
	return t.res, t.err
}
func (t *testInv) TraverseIterator(uuid.UUID, *result.Iterator, int) ([]stackitem.Item, error) {
	return nil, nil
}
func (t *testInv) TerminateSession(uuid.UUID) error {
	return nil
}

func TestBaseErrors(t *testing.T) {
	ti := new(testInv)
	r := NewReader(ti, util.Uint160{1, 2, 3})

	ti.err = errors.New("bad")
	_, err := r.ResolveFSContract("blah")
	require.Error(t, err)

	ti.err = nil
	ti.res = &result.Invoke{
		State: "HALT",
		Stack: []stackitem.Item{
			stackitem.Make([]stackitem.Item{}),
		},
	}
	_, err = r.ResolveFSContract("blah")
	require.Error(t, err)

	ti.res = &result.Invoke{
		State: "HALT",
		Stack: []stackitem.Item{
			stackitem.Make([]stackitem.Item{
				stackitem.Make(100500),
			}),
		},
	}
	_, err = r.ResolveFSContract("blah")
	require.Error(t, err)

	h := util.Uint160{1, 2, 3, 4, 5}

	ti.res = &result.Invoke{
		State: "HALT",
		Stack: []stackitem.Item{
			stackitem.Make([]stackitem.Item{
				stackitem.Make("addr=" + address.Uint160ToString(h)), // Wrong prefix
			}),
		},
	}
	_, err = r.ResolveFSContract("blah")
	require.Error(t, err)

	ti.res = &result.Invoke{
		State: "HALT",
		Stack: []stackitem.Item{
			stackitem.Make([]stackitem.Item{
				stackitem.Make(h.StringLE()),
			}),
		},
	}
	res, err := r.ResolveFSContract("blah")
	require.NoError(t, err)
	require.Equal(t, h, res)

	ti.res = &result.Invoke{
		State: "HALT",
		Stack: []stackitem.Item{
			stackitem.Make([]stackitem.Item{
				stackitem.Make(address.Uint160ToString(h)),
			}),
		},
	}
	res, err = r.ResolveFSContract("blah")
	require.NoError(t, err)
	require.Equal(t, h, res)

	ti.res = &result.Invoke{
		State: "HALT",
		Stack: []stackitem.Item{
			stackitem.Make([]stackitem.Item{
				stackitem.Make("address=" + address.Uint160ToString(h)), // NEP-18
			}),
		},
	}
	res, err = r.ResolveFSContract("blah")
	require.NoError(t, err)
	require.Equal(t, h, res)
}

func TestHasAddressRecordExist(t *testing.T) {
	ti := new(testInv)
	r := NewReader(ti, util.Uint160{1, 2, 3})

	addr := "NbrUYaZgyhSkNoRo9ugRyEMdUZxrhkNaWB"

	t.Run("bad call", func(t *testing.T) {
		ti.err = errors.New("bad")
		_, err := r.HasAddressRecord("testdomain.com", addr)
		require.Error(t, err)
	})

	t.Run("user exists", func(t *testing.T) {
		ti.err = nil
		ti.res = &result.Invoke{
			State: "HALT",
			Stack: []stackitem.Item{
				stackitem.Make(true),
			},
		}

		userId, err := user.DecodeString(addr)
		require.NoError(t, err)

		t.Run("plain address", func(t *testing.T) {
			exists, err := r.HasAddressRecord("testdomain.com", userId.EncodeToString())
			require.NoError(t, err)
			require.True(t, exists)
		})

		t.Run("hex-encoded LE", func(t *testing.T) {
			exists, err := r.HasAddressRecord("testdomain.com", userId.ScriptHash().StringLE())
			require.NoError(t, err)
			require.True(t, exists)
		})

		t.Run("hex-encoded BE", func(t *testing.T) {
			exists, err := r.HasAddressRecord("testdomain.com", userId.ScriptHash().StringBE())
			require.NoError(t, err)
			require.True(t, exists)
		})

		t.Run("nep18 address", func(t *testing.T) {
			exists, err := r.HasAddressRecord("testdomain.com", "address="+userId.EncodeToString())
			require.NoError(t, err)
			require.True(t, exists)
		})
	})

	t.Run("user does not exist", func(t *testing.T) {
		ti.err = nil
		ti.res = &result.Invoke{
			State: "HALT",
			Stack: []stackitem.Item{
				stackitem.Make(false),
			},
		}
		exists, err := r.HasAddressRecord("testdomain.com", addr)
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("invalid user", func(t *testing.T) {
		ti.err = nil
		_, err := r.HasAddressRecord("testdomain.com", "invalidAddress")
		require.EqualError(t, err, "record address is invalid")
	})
}
