package subnet_test

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "subnet"

func TestMigration(t *testing.T) {
	err := dump.IterateDumps("../testdata", func(id dump.ID, r *dump.Reader) {
		t.Run(id.String()+"/"+name, func(t *testing.T) {
			testMigrationFromDump(t, r)
		})
	})
	require.NoError(t, err)
}

var notaryDisabledKey = []byte("notary")

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	// init test contract shell
	c := migration.NewContract(t, d, "subnet", migration.ContractOptions{})

	migration.SkipUnsupportedVersions(t, c)

	// gather values which can't be fetched via contract API
	v := c.GetStorageItem(notaryDisabledKey)
	notaryDisabled := len(v) == 1 && v[0] == 1

	readPendingVotes := func() bool {
		if v := c.GetStorageItem([]byte("ballots")); v != nil {
			item, err := stackitem.Deserialize(v)
			require.NoError(t, err)
			arr, ok := item.Value().([]stackitem.Item)
			if ok {
				return len(arr) > 0
			} else {
				require.Equal(t, stackitem.Null{}, item)
			}
		}
		return false
	}

	prevPendingVote := readPendingVotes()

	// try to update the contract
	var notary bool
	c.CheckUpdateFail(t, "update to non-notary mode is not supported anymore", !notary)

	if notaryDisabled && prevPendingVote {
		c.CheckUpdateFail(t, "pending vote detected", notary)
		return
	}

	c.CheckUpdateSuccess(t, notary)

	// check that contract was updates as expected
	newPendingVotes := readPendingVotes()

	require.False(t, newPendingVotes, "notary flag should be removed")
	require.Nil(t, c.GetStorageItem(notaryDisabledKey), "notary flag should be removed")
}
