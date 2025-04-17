package reputation_test

import (
	"bytes"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "reputation"

func TestMigration(t *testing.T) {
	err := dump.IterateDumps("../../testdata", func(id dump.ID, r *dump.Reader) {
		t.Run(id.String()+"/"+name, func(t *testing.T) {
			testMigrationFromDump(t, r)
		})
	})
	require.NoError(t, err)
}

var notaryDisabledKey = []byte("notary")

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	// gather values which can't be fetched via contract API
	var epochs []uint64

	c := migration.NewContract(t, d, "reputation", migration.ContractOptions{
		StorageDumpHandler: func(key, value []byte) {
			if bytes.HasPrefix(key, []byte{'c'}) {
				epoch := io.NewBinReaderFromBuf(key[1:]).ReadVarUint()
				for i := range epochs {
					if epochs[i] == epoch {
						return
					}
				}
				epochs = append(epochs, epoch)
			}
		},
	})

	migration.SkipUnsupportedVersions(t, c)

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

	prevPendingVotes := readPendingVotes()

	// read previous values using contract API
	readEpochsToTrustValues := func() map[uint64][]stackitem.Item {
		m := make(map[uint64][]stackitem.Item, len(epochs))
		for i := range epochs {
			r := c.Call(t, "listByEpoch", int64(epochs[i]))
			items, ok := r.Value().([]stackitem.Item)
			if !ok {
				require.Equal(t, stackitem.Null{}, r)
			}

			var results []stackitem.Item
			for j := range items {
				bID, err := items[j].TryBytes()
				require.NoError(t, err)

				r := c.Call(t, "getByID", bID)
				res, ok := r.Value().([]stackitem.Item)
				if !ok {
					require.Equal(t, stackitem.Null{}, r)
				}

				results = append(results, res...)
			}

			m[epochs[i]] = results
		}
		return m
	}

	prevEpochsToTrustValues := readEpochsToTrustValues()

	// try to update the contract
	if notaryDisabled && prevPendingVotes {
		c.CheckUpdateFail(t, "pending vote detected")
		return
	}

	c.CheckUpdateSuccess(t)

	// check that contract was updates as expected
	newEpochsToTrustValues := readEpochsToTrustValues()
	newPendingVotes := readPendingVotes()

	require.False(t, newPendingVotes, "there should be no more pending votes")
	require.Nil(t, c.GetStorageItem(notaryDisabledKey), "notary flag should be removed")

	require.Equal(t, len(prevEpochsToTrustValues), len(newEpochsToTrustValues))
	for k, vPrev := range prevEpochsToTrustValues {
		vNew, ok := newEpochsToTrustValues[k]
		require.True(t, ok)
		require.ElementsMatch(t, vPrev, vNew)
	}
}
