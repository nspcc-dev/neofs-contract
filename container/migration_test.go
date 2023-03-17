package container_test

import (
	"bytes"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "container"

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

	// try to update the contract
	var notary bool
	c.CheckUpdateFail(t, "update to non-notary mode is not supported anymore", !notary)

	if notaryDisabled && prevPendingVote {
		c.CheckUpdateFail(t, "pending vote detected", notary)
		return
	}

	c.CheckUpdateSuccess(t, notary)

	// check that contract was updates as expected
	newPendingVote := readPendingVotes()
	newContainers := readAllContainers()
	newContainerCount := readContainerCount()
	newOwnersToContainers := readOwnersToContainers()

	require.Nil(t, c.GetStorageItem(notaryDisabledKey), "notary flag should be removed")
	require.Equal(t, prevContainerCount, newContainerCount, "number of containers should remain")
	require.ElementsMatch(t, prevContainers, newContainers, "container list should remain")
	require.False(t, newPendingVote, "there should be no more pending votes")

	require.Equal(t, len(prevOwnersToContainers), len(newOwnersToContainers))
	for k, vPrev := range prevOwnersToContainers {
		vNew, ok := newOwnersToContainers[k]
		require.True(t, ok)
		require.ElementsMatch(t, vPrev, vNew, "containers of '%s' owner should remain", base58.Encode([]byte(k)))
	}
}
