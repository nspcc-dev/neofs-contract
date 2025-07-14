package container_test

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
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
	require.True(t, maps.EqualFunc(prevCnrStorageItems, newCnrStorageItems, bytes.Equal), "containers' binary items should remain")
	require.True(t, maps.EqualFunc(prevEACLStorageItems, newEACLStorageItems, bytes.Equal), "eACLs' binary items should remain")

	require.Equal(t, len(prevOwnersToContainers), len(newOwnersToContainers))
	for k, vPrev := range prevOwnersToContainers {
		vNew, ok := newOwnersToContainers[k]
		require.True(t, ok)
		require.ElementsMatch(t, vPrev, vNew, "containers of '%s' owner should remain", base58.Encode([]byte(k)))
	}
}
