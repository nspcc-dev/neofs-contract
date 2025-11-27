package tests

import (
	"encoding/binary"
	"iter"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

func randomBytes(n int) []byte {
	if n < 8 {
		n = 8
	}
	a := make([]byte, n)
	binary.LittleEndian.PutUint64(a, rand.Uint64())
	return a
}

// tests contract's 'verify' method checking whether carrier transaction is
// signed by the NeoFS Alphabet.
func testVerify(t testing.TB, contract *neotest.ContractInvoker) {
	const method = "verify"
	contract.Invoke(t, stackitem.NewBool(true), method)
	contract.WithSigners(contract.NewAccount(t)).Invoke(t, stackitem.NewBool(false), method)
}

func getContractStorageItem(t testing.TB, exec *neotest.Executor, contract util.Uint160, key []byte) []byte {
	st := exec.Chain.GetContractState(contract)
	require.NotNil(t, st)
	return exec.Chain.GetStorageItem(st.ID, key)
}

func contractStorageItems(t testing.TB, exec *neotest.Executor, contract util.Uint160, prefix []byte) iter.Seq2[[]byte, []byte] {
	st := exec.Chain.GetContractState(contract)
	require.NotNil(t, st)
	return func(yield func([]byte, []byte) bool) {
		exec.Chain.SeekStorage(st.ID, prefix, yield)
	}
}

func assertNotificationEvent(t testing.TB, e state.NotificationEvent, name string, items ...any) {
	require.Equal(t, name, e.Name)
	gotItems := e.Item.Value().([]stackitem.Item)
	require.Len(t, gotItems, len(items))
	for i := range items {
		require.Equal(t, items[i], gotItems[i].Value(), i)
	}
}

func assertEqualItemArray(t testing.TB, exp, got []stackitem.Item) {
	for i := range exp {
		if expArr, err := exp[i].Convert(stackitem.ArrayT); err == nil {
			gotArr, err := got[i].Convert(stackitem.ArrayT)
			require.NoError(t, err)

			assertEqualItemArray(t, stackItemToArray(expArr), stackItemToArray(gotArr))

			continue
		}

		require.Equal(t, exp[i].Value(), got[i].Value(), i)
	}
}

func stackItemToArray(item stackitem.Item) []stackitem.Item {
	if _, ok := item.(stackitem.Null); !ok {
		return item.Value().([]stackitem.Item)
	}
	return nil
}

func deployTestNEP11Receiver(t testing.TB, exec *neotest.Executor) util.Uint160 {
	const dir = "../internal/testcontracts/nep11recv"
	contract := neotest.CompileFile(t, exec.CommitteeHash, dir, filepath.Join(dir, "config.yml"))

	exec.DeployContract(t, contract, nil)

	return contract.Hash
}
