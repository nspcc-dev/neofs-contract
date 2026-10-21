package container_test

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "container"

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
	var (
		owners         [][]byte
		billingBefore  = make(map[string][]int64)
		reportObjCount = make(map[string]int64)
	)

	c := migration.NewContract(t, d, "container", migration.ContractOptions{
		StorageDumpHandler: func(key, value []byte) {
			switch {
			case len(key) == 1+interop.Hash256Len+interop.Hash160Len && key[0] == 'f':
				item, err := stackitem.Deserialize(value)
				require.NoError(t, err)
				fields := item.Value().([]stackitem.Item)
				var sizes = make([]int64, 4)
				for i, index := range []int{1, 4, 5, 7} {
					size, err := fields[index].TryInteger()
					require.NoError(t, err)
					sizes[i] = size.Int64()
				}
				billingBefore[string(key)] = sizes
			case len(key) == 1+interop.Hash256Len+interop.Hash160Len && key[0] == 'i':
				item, err := stackitem.Deserialize(value)
				require.NoError(t, err)
				fields := item.Value().([]stackitem.Item)
				objects, err := fields[2].TryInteger()
				require.NoError(t, err)
				reportKey := append([]byte{'f'}, key[1:]...)
				reportObjCount[string(reportKey)] = objects.Int64()
			}

			const ownerLen = 25
			if len(key) == ownerLen+32 { // + cid
				for i := range owners {
					if bytes.Equal(owners[i], key[:ownerLen]) {
						return
					}
				}
				owners = append(owners, key[:ownerLen])
			}
		},
	})

	// read previous values using contract API
	readAllContainers := func() []stackitem.Item {
		iter, ok := c.Call(t, "containersOf", nil).Value().(*storage.Iterator)
		require.True(t, ok)

		var containers []stackitem.Item
		for iter.Next() {
			containers = append(containers, iter.Value())
		}

		return containers
	}

	readContainerCount := func() uint64 {
		nContainers, err := c.Call(t, "count").TryInteger()
		require.NoError(t, err)
		return nContainers.Uint64()
	}

	readOwnersToContainers := func() map[string][]stackitem.Item {
		m := make(map[string][]stackitem.Item, len(owners))
		for i := range owners {
			iter := c.Call(t, "containersOf", owners[i]).Value().(*storage.Iterator)

			for iter.Next() {
				m[string(owners[i])] = append(m[string(owners[i])], iter.Value())
			}
		}
		return m
	}

	prevContainers := readAllContainers()
	prevContainerCount := readContainerCount()
	prevOwnersToContainers := readOwnersToContainers()

	prevCnrStorageItems := make(map[string][]byte) // CID -> container binary
	c.SeekStorage([]byte{'x'}, func(k, v []byte) bool {
		prevCnrStorageItems[string(k)] = slices.Clone(v)
		return true
	})
	prevEACLStorageItems := make(map[string][]byte) // CID -> eACL binary
	c.SeekStorage([]byte("eACL"), func(k, v []byte) bool {
		prevEACLStorageItems[string(k)] = slices.Clone(v)
		return true
	})

	c.CheckUpdateSuccess(t)

	// check that contract was updates as expected
	newContainers := readAllContainers()
	newContainerCount := readContainerCount()
	newOwnersToContainers := readOwnersToContainers()

	newCnrStorageItems := make(map[string][]byte) // CID -> container binary
	c.SeekStorage([]byte{'x'}, func(k, v []byte) bool {
		newCnrStorageItems[string(k)] = slices.Clone(v)
		return true
	})
	newEACLStorageItems := make(map[string][]byte) // CID -> eACL binary
	c.SeekStorage([]byte("eACL"), func(k, v []byte) bool {
		newEACLStorageItems[string(k)] = slices.Clone(v)
		return true
	})

	require.Equal(t, prevContainerCount, newContainerCount, "number of containers should remain")
	require.ElementsMatch(t, prevContainers, newContainers, "container list should remain")
	require.Empty(t, newCnrStorageItems, "containers' binary items should be dropped")
	require.True(t, maps.EqualFunc(prevEACLStorageItems, newEACLStorageItems, bytes.Equal), "eACLs' binary items should remain")

	require.Equal(t, len(prevOwnersToContainers), len(newOwnersToContainers))
	for k, vPrev := range prevOwnersToContainers {
		vNew, ok := newOwnersToContainers[k]
		require.True(t, ok)
		require.ElementsMatch(t, vPrev, vNew, "containers of '%s' owner should remain", base58.Encode([]byte(k)))
	}

	// assert billing info's Account field is filled (< 0.26.1 migration)
	assertBillingMigration(t, c)

	// assert billing sizes include per-object overhead (< 0.28.0 migration)
	assertBillingSizeMigration(t, c, billingBefore, reportObjCount)

	// assert containers have default zero versions (< 0.27.0 migration)
	assertContainerVersions(t, c)
}

func assertBillingSizeMigration(t *testing.T, c *migration.Contract, before map[string][]int64, objects map[string]int64) {
	require.NotEmpty(t, before)
	var migrated int
	for key, count := range objects {
		if _, ok := before[key]; ok && count > 0 {
			migrated++
		}
	}
	require.Positive(t, migrated, "no billing statistics with object counts found")

	c.SeekStorage([]byte{'f'}, func(k, v []byte) bool {
		oldSizes, ok := before[string(k)]
		if !ok {
			return true
		}
		item, err := stackitem.Deserialize(v)
		require.NoError(t, err)
		fields := item.Value().([]stackitem.Item)
		overhead := objects[string(k)] * (16 << 10)
		for i, index := range []int{1, 4, 5, 7} {
			size, err := fields[index].TryInteger()
			require.NoError(t, err)
			require.Equal(t, oldSizes[i]+overhead, size.Int64(), string(k))
		}
		return true
	})
}

func assertContainerVersions(t *testing.T, c *migration.Contract) {
	c.SeekStorage([]byte{0}, func(k, v []byte) bool {
		item, err := stackitem.Deserialize(v)
		require.NoError(t, err)

		fields := item.Value().([]stackitem.Item)
		require.Len(t, fields, 2) // {Info, ContainerVersion}

		rev, err := fields[1].TryInteger()
		require.NoError(t, err)

		require.EqualValues(t, 1, rev.Int64())

		return true
	})
}

func assertBillingMigration(t *testing.T, c *migration.Contract) {
	c.SeekStorage([]byte{'f'}, func(k, v []byte) bool {
		item, err := stackitem.Deserialize(v)
		require.NoError(t, err)

		fields := item.Value().([]stackitem.Item)
		accField := fields[0]

		acc, err := accField.TryBytes()
		require.NoError(t, err) // it would be Null before migration there

		require.Len(t, acc, interop.Hash160Len)

		return true
	})
}
