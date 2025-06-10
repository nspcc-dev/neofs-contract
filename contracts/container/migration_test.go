package container_test

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/mr-tron/base58"
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
	var owners [][]byte

	c := migration.NewContract(t, d, "container", migration.ContractOptions{
		StorageDumpHandler: func(key, value []byte) {
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

	migration.SkipUnsupportedVersions(t, c)

	// read previous values using contract API
	readAllContainers := func() []stackitem.Item {
		containers, ok := c.Call(t, "list", []byte{}).Value().([]stackitem.Item)
		require.True(t, ok)
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
			m[string(owners[i])] = c.Call(t, "list", owners[i]).Value().([]stackitem.Item)
		}
		return m
	}

	prevContainers := readAllContainers()
	prevContainerCount := readContainerCount()
	prevOwnersToContainers := readOwnersToContainers()

	prevCnrStorageItems := make(map[string][]byte) // CID -> container binary
	c.SeekStorage([]byte{'x'}, func(k, v []byte) bool {
		prevCnrStorageItems[string(k)] = getStructField0(t, v)
		return true
	})
	prevEACLStorageItems := make(map[string][]byte) // CID -> eACL binary
	c.SeekStorage([]byte("eACL"), func(k, v []byte) bool {
		prevEACLStorageItems[string(k)] = getStructField0(t, v)
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
	require.True(t, maps.EqualFunc(prevCnrStorageItems, newCnrStorageItems, bytes.Equal), "containers' binary items should remain")
	require.True(t, maps.EqualFunc(prevEACLStorageItems, newEACLStorageItems, bytes.Equal), "eACLs' binary items should remain")

	require.Equal(t, len(prevOwnersToContainers), len(newOwnersToContainers))
	for k, vPrev := range prevOwnersToContainers {
		vNew, ok := newOwnersToContainers[k]
		require.True(t, ok)
		require.ElementsMatch(t, vPrev, vNew, "containers of '%s' owner should remain", base58.Encode([]byte(k)))
	}
}

func getStructField0(t testing.TB, v []byte) []byte {
	item, err := stackitem.Deserialize(v)
	require.NoError(t, err)
	arr, ok := item.Value().([]stackitem.Item)
	require.True(t, ok)
	require.NotEmpty(t, arr)
	b, err := arr[0].TryBytes()
	require.NoError(t, err)
	return b
}
