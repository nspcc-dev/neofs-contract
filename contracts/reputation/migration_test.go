package reputation_test

import (
	"bytes"
	"slices"
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

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	// gather values which can't be fetched via contract API
	var epochs []uint64

	c := migration.NewContract(t, d, "reputation", migration.ContractOptions{
		StorageDumpHandler: func(key, value []byte) {
			if bytes.HasPrefix(key, []byte{'c'}) {
				epoch := io.NewBinReaderFromBuf(key[1:]).ReadVarUint()
				if slices.Contains(epochs, epoch) {
					return
				}
				epochs = append(epochs, epoch)
			}
		},
	})

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

	c.CheckUpdateSuccess(t)

	// check that contract was updates as expected
	newEpochsToTrustValues := readEpochsToTrustValues()

	require.Equal(t, len(prevEpochsToTrustValues), len(newEpochsToTrustValues))
	for k, vPrev := range prevEpochsToTrustValues {
		vNew, ok := newEpochsToTrustValues[k]
		require.True(t, ok)
		require.ElementsMatch(t, vPrev, vNew)
	}
}
