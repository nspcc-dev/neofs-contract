package alphabet_test

import (
	"path/filepath"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "alphabet"

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
	c := migration.NewContract(t, d, "alphabet0", migration.ContractOptions{
		SourceCodeDir: filepath.Join("..", name),
	})

	// read previous values using contract API
	readName := func() string {
		b, err := c.Call(t, "name").TryBytes()
		require.NoError(t, err)
		return string(b)
	}

	prevName := readName()

	// try to update the contract
	updPrm := []any{
		false,                 // non-notary mode
		util.Uint160{3, 2, 1}, // unused
		[]byte{},              // Proxy contract (custom)
		"",                    // unused
		0,                     // unused
		0,                     // unused
	}

	c.CheckUpdateSuccess(t, updPrm...)

	// check that contract was updates as expected
	newName := readName()

	require.Nil(t, c.GetStorageItem([]byte("innerring")), "Inner Ring nodes should be removed")
	require.Equal(t, prevName, newName, "name should remain")
}
