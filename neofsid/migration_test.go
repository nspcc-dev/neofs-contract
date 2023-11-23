package neofsid_test

import (
	"bytes"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "neofsid"

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

	c := migration.NewContract(t, d, "neofsid", migration.ContractOptions{
		StorageDumpHandler: func(key, value []byte) {
			const ownerLen = 25
			if bytes.HasPrefix(key, []byte{'o'}) && len(key[1:]) == ownerLen+interop.PublicKeyCompressedLen {
				owners = append(owners, key[1:1+ownerLen])
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
	readOwnersToKeys := func() map[string][]stackitem.Item {
		m := make(map[string][]stackitem.Item, len(owners))
		for i := range owners {
			m[string(owners[i])] = c.Call(t, "key", owners[i]).Value().([]stackitem.Item)
		}
		return m
	}

	prevOwnersToKeys := readOwnersToKeys()

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
	newOwnersToKeys := readOwnersToKeys()

	require.Nil(t, c.GetStorageItem(notaryDisabledKey), "notary flag should be removed")
	require.Nil(t, c.GetStorageItem([]byte("containerScriptHash")), "Container contract address should be removed")
	require.Nil(t, c.GetStorageItem([]byte("netmapScriptHash")), "Netmap contract address should be removed")
	require.False(t, newPendingVotes, "there should be no more pending votes")

	require.Equal(t, len(prevOwnersToKeys), len(newOwnersToKeys))
	for k, vPrev := range prevOwnersToKeys {
		vNew, ok := newOwnersToKeys[k]
		require.True(t, ok)
		require.ElementsMatch(t, vPrev, vNew)
	}
}
