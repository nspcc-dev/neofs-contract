package balance_test

import (
	"math/big"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "balance"

func TestMigration(t *testing.T) {
	err := dump.IterateDumps("../../testdata", func(id dump.ID, r *dump.Reader) {
		t.Run(id.String()+"/"+name, func(t *testing.T) {
			testMigrationFromDump(t, r)
		})
	})
	require.NoError(t, err)
}

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	// init test contract shell
	c := migration.NewContract(t, d, name, migration.ContractOptions{})

	// read previous values using contract API
	readTotalSupply := func() int64 {
		n, err := c.Call(t, "totalSupply").TryInteger()
		require.NoError(t, err)
		return n.Int64()
	}

	prevTotalSupply := readTotalSupply()

	var accounts []util.Uint160

	c.SeekStorage([]byte{}, func(k, v []byte) bool {
		if len(k) == util.Uint160Size {
			a, err := util.Uint160DecodeBytesBE(k)
			require.NoError(t, err)
			accounts = append(accounts, a)
		}
		return true
	})

	var balances = make([]*big.Int, 0, len(accounts))
	for i := range accounts {
		n, err := c.Call(t, "balanceOf", accounts[i]).TryInteger()
		require.NoError(t, err)
		balances = append(balances, n)
	}

	c.CheckUpdateSuccess(t)

	// check that contract was updates as expected
	newTotalSupply := readTotalSupply()

	require.Nil(t, c.GetStorageItem([]byte("containerScriptHash")), "Container contract address should be removed")
	require.Nil(t, c.GetStorageItem([]byte("netmapScriptHash")), "Netmap contract address should be removed")

	require.Equal(t, prevTotalSupply, newTotalSupply)

	for i := range accounts {
		// Balances are the same.
		n, err := c.Call(t, "balanceOf", accounts[i]).TryInteger()
		require.NoError(t, err)
		require.Equal(t, balances[i], n)
	}

	c.SeekStorage([]byte{}, func(k, v []byte) bool {
		// Every account migrated.
		require.NotEqual(t, len(k), util.Uint160Size)
		return true
	})
}
