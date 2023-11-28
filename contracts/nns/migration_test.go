package nns_test

import (
	"bytes"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "nns"

func TestMigration(t *testing.T) {
	err := dump.IterateDumps("testdata", func(id dump.ID, r *dump.Reader) {
		t.Run(id.String()+"/"+name, func(t *testing.T) {
			testMigrationFromDump(t, r)
		})
	})
	require.NoError(t, err)
}

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	// gather values which can't be fetched via contract API
	var (
		tlds     [][]byte
		roots    [][]byte
		owners   [][]byte
		balances []int64
	)

	c := migration.NewContract(t, d, name, migration.ContractOptions{
		StorageDumpHandler: func(key, value []byte) {
			if key[0] == 0x21 { // prefixName
				rec, err := stackitem.Deserialize(value)
				require.NoError(t, err)
				itms := rec.Value().([]stackitem.Item)
				require.Equal(t, 4, len(itms))
				name, err := itms[1].TryBytes()
				require.NoError(t, err)
				if !bytes.Contains(name, []byte(".")) {
					tlds = append(tlds, name)
				}
			}
			if key[0] == 0x20 { // prefixRoot
				roots = append(roots, key[1:])
			}
		},
	})
	require.EqualValues(t, roots, tlds)

	migration.SkipUnsupportedVersions(t, c)

	for _, tld := range tlds {
		owner, err := c.Call(t, "ownerOf", tld).TryBytes()
		require.NoError(t, err)
		owners = append(owners, owner)

		bal, err := c.Call(t, "balanceOf", owner).TryInteger()
		require.NoError(t, err)
		require.NotEqual(t, 0, bal.Int64())
		balances = append(balances, bal.Int64())
	}

	c.CheckUpdateSuccess(t)

	for i, tld := range tlds {
		// There is no owner after the upgrade.
		_, err := c.TestInvoke(t, "ownerOf", tld)
		require.ErrorContains(t, err, "token not found")

		bal, err := c.Call(t, "balanceOf", owners[i]).TryInteger()
		require.NoError(t, err)
		require.Greater(t, balances[i], bal.Int64())

		// We've got alphabet signer, so renew should work even though
		// there is no owner.
		_ = c.InvokeAndCheck(t, func(t testing.TB, stack []stackitem.Item) {
			require.Equal(t, 1, len(stack))
			_, err := stack[0].TryInteger()
			require.NoError(t, err)
		}, "renew", tld)
	}
}
