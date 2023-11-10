package tests

import (
	"crypto/elliptic"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/core/native/nativenames"
	"github.com/nspcc-dev/neo-go/pkg/core/native/noderoles"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/vm/vmstate"
	"github.com/stretchr/testify/require"
)

func iteratorToArray(iter *storage.Iterator) []stackitem.Item {
	stackItems := make([]stackitem.Item, 0)
	for iter.Next() {
		stackItems = append(stackItems, iter.Value())
	}
	return stackItems
}

func newExecutor(t *testing.T) *neotest.Executor {
	bc, acc := chain.NewSingle(t)
	return neotest.NewExecutor(t, bc, acc, acc)
}

// SetInnerRing sets Inner Ring composition in given neotest.Executor using
// RoleManagement contract. The list can be read via InnerRing.
func SetInnerRing(tb testing.TB, e *neotest.Executor, _keys keys.PublicKeys) {
	bKeys := make([][]byte, len(_keys))
	for i := range _keys {
		bKeys[i] = _keys[i].Bytes()
	}

	_setAlphabetRole(tb, e, bKeys)
}

// InnerRing reads Inner Ring composition from given neotest.Executor using
// RoleManagement contract. The list can be set via SetInnerRing.
func InnerRing(tb testing.TB, e *neotest.Executor) keys.PublicKeys {
	stack, err := roleManagementInvoker(tb, e).TestInvoke(tb, "getDesignatedByRole",
		int64(noderoles.NeoFSAlphabet), e.TopBlock(tb).Index+1)
	require.NoError(tb, err)

	item, err := unwrap.Item(&result.Invoke{
		State: vmstate.Halt.String(),
		Stack: stack.ToArray(),
	}, nil)
	require.NoError(tb, err)

	arr, ok := item.Value().([]stackitem.Item)
	require.True(tb, ok)

	res := make(keys.PublicKeys, len(arr))

	for i := range arr {
		b, err := arr[i].TryBytes()
		require.NoError(tb, err)

		res[i], err = keys.NewPublicKeyFromBytes(b, elliptic.P256())
		require.NoError(tb, err)
	}

	return res
}

func setAlphabetRole(tb testing.TB, e *neotest.Executor, key []byte) {
	_setAlphabetRole(tb, e, [][]byte{key})
}

func _setAlphabetRole(tb testing.TB, e *neotest.Executor, _keys [][]byte) {
	keysArg := make([]any, len(_keys))
	for i := range _keys {
		keysArg[i] = _keys[i]
	}

	roleManagementInvoker(tb, e).Invoke(tb, stackitem.Null{}, "designateAsRole",
		int64(noderoles.NeoFSAlphabet), keysArg)
}

func roleManagementInvoker(tb testing.TB, e *neotest.Executor) *neotest.ContractInvoker {
	roleMgmtContract, err := e.Chain.GetNativeContractScriptHash(nativenames.Designation)
	require.NoError(tb, err)
	return e.CommitteeInvoker(roleMgmtContract)
}
