package audit_test

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "audit"

func TestMigration(t *testing.T) {
	err := dump.IterateDumps("../testdata", func(id dump.ID, r *dump.Reader) {
		t.Run(id.String()+"/"+name, func(t *testing.T) {
			testMigrationFromDump(t, r)
		})
	})
	require.NoError(t, err)
}

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	// init test contract shell
	c := migration.NewContract(t, d, name, migration.ContractOptions{})

	migration.SkipUnsupportedVersions(t, c)

	// read previous values using contract API
	readAllAuditResults := func() []stackitem.Item {
		r := c.Call(t, "list")
		items, ok := r.Value().([]stackitem.Item)
		if !ok {
			require.Equal(t, stackitem.Null{}, r)
		}

		var results []stackitem.Item

		for i := range items {
			bID, err := items[i].TryBytes()
			require.NoError(t, err)

			results = append(results, c.Call(t, "get", bID))
		}

		return results
	}

	prevAuditResults := readAllAuditResults()

	// try to update the contract
	var notary bool
	c.CheckUpdateFail(t, "update to non-notary mode is not supported anymore", !notary)
	c.CheckUpdateSuccess(t, notary)

	// check that contract was updates as expected
	newAuditResults := readAllAuditResults()

	require.Nil(t, c.GetStorageItem([]byte("notary")), "notary flag should be removed")
	require.Nil(t, c.GetStorageItem([]byte("netmapScriptHash")), "Netmap contract address should be removed")
	require.ElementsMatch(t, prevAuditResults, newAuditResults, "audit results should remain")
}
