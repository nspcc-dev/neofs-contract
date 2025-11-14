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
	protocontainer "github.com/nspcc-dev/neofs-sdk-go/proto/container"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
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

	infoStorageItems := make(map[string][]byte) // CID -> Info
	c.SeekStorage([]byte{0}, func(k, v []byte) bool {
		infoStorageItems[string(k)] = slices.Clone(v)
		return true
	})
	require.Len(t, infoStorageItems, len(prevCnrStorageItems))

	for id, b := range prevCnrStorageItems {
		cnrStructBytes, ok := infoStorageItems[id]
		require.Truef(t, ok, "container %s is not migrated to structured info", base58.Encode([]byte(id)))

		expFields := containerBytesToStructFields(t, b)

		gotStruct := c.Call(t, "getInfo", id)
		require.IsType(t, (*stackitem.Struct)(nil), gotStruct)
		assertEqualItemArray(t, expFields, gotStruct.Value().([]stackitem.Item))

		gotStruct, err := stackitem.Deserialize(cnrStructBytes)
		require.NoError(t, err)
		require.IsType(t, (*stackitem.Struct)(nil), gotStruct)
		assertEqualItemArray(t, expFields, gotStruct.Value().([]stackitem.Item))
	}
}

// copy-paste from tests/container_test.go.
func containerBytesToStructFields(t testing.TB, b []byte) []stackitem.Item {
	var cnr protocontainer.Container
	require.NoError(t, proto.Unmarshal(b, &cnr))

	var attrs []stackitem.Item
	for _, a := range cnr.Attributes {
		attrs = append(attrs, stackitem.NewStruct([]stackitem.Item{
			stackitem.Make(a.Key), stackitem.Make(a.Value),
		}))
	}

	policyBytes, err := proto.Marshal(cnr.PlacementPolicy)
	require.NoError(t, err)

	return []stackitem.Item{
		stackitem.NewStruct([]stackitem.Item{
			stackitem.Make(cnr.GetVersion().GetMajor()),
			stackitem.Make(cnr.GetVersion().GetMinor()),
		}),
		stackitem.NewBuffer(cnr.GetOwnerId().GetValue()),
		stackitem.NewBuffer(cnr.GetNonce()),
		stackitem.Make(cnr.GetBasicAcl()),
		stackitem.NewArray(attrs),
		stackitem.NewBuffer(policyBytes),
	}
}

func assertEqualItemArray(t testing.TB, exp, got []stackitem.Item) {
	for i := range exp {
		if expArr, err := exp[i].Convert(stackitem.ArrayT); err == nil {
			gotArr, err := got[i].Convert(stackitem.ArrayT)
			require.NoError(t, err)

			assertEqualItemArray(t, stackItemToArray(expArr), stackItemToArray(gotArr))

			continue
		}

		require.Equal(t, exp[i].Value(), got[i].Value(), i)
	}
}

func stackItemToArray(item stackitem.Item) []stackitem.Item {
	if _, ok := item.(stackitem.Null); !ok {
		return item.Value().([]stackitem.Item)
	}
	return nil
}
