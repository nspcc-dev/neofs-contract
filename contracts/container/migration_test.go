package container_test

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/nspcc-dev/neofs-sdk-go/container"
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

	// assert structuring of containers
	assertStructuring(t, c, prevCnrStorageItems)
}

func assertStructuring(t *testing.T, c *migration.Contract, binCnrs map[string][]byte) {
	structsPrefix := []byte{0x00}

	for i := 1; ; i++ {
		// inlined stuff of c.Invoke()
		tx := c.PrepareInvoke(t, "addStructs")
		c.AddNewBlock(t, tx)

		txRes := c.CheckHalt(t, tx.Hash())
		require.Len(t, txRes.Stack, 1)

		rem, err := txRes.Stack[0].TryBool()
		require.NoError(t, err)

		if !rem {
			break
		}

		count := 0
		c.SeekStorage(structsPrefix, func(_, _ []byte) bool { count++; return true })
		require.EqualValues(t, count, 10*i)
	}

	mStructs := make(map[string][]stackitem.Item)
	c.SeekStorage(structsPrefix, func(k, v []byte) bool {
		item, err := stackitem.Deserialize(v)
		require.NoError(t, err)

		arr, ok := item.Value().([]stackitem.Item)
		require.True(t, ok)

		mStructs[string(k)] = arr

		return true
	})

	require.Equal(t, len(mStructs), len(binCnrs))
	for id, cnrBin := range binCnrs {
		exp := containerBinaryToStruct(t, cnrBin)
		assertEqualItemArray(t, exp, mStructs[id])
	}

	for id, expFields := range mStructs {
		item := c.Call(t, "getInfo", id)

		arr, ok := item.Value().([]stackitem.Item)
		require.True(t, ok)

		assertEqualItemArray(t, expFields, arr)
	}
}

func containerBinaryToStruct(t *testing.T, b []byte) []stackitem.Item {
	// we decode into protocontainer.Container, not to container.Container for the following reasons:
	// 1. container.Container loses already deprecated subnet ID in decode-encode cycle
	// 2. nonce is not exposed from container.Container
	var msg protocontainer.Container
	require.NoError(t, proto.Unmarshal(b, &msg))

	var cnr container.Container
	require.NoError(t, cnr.FromProtoMessage(&msg))

	ownerAddr := msg.OwnerId.GetValue()
	require.Len(t, ownerAddr, common.NeoFSUserAccountLength)

	var attrs []stackitem.Item
	for _, a := range msg.Attributes {
		attrs = append(attrs, stackitem.NewStruct([]stackitem.Item{
			stackitem.Make(a.GetKey()), stackitem.Make(a.GetValue()),
		}))
	}

	policyBin := make([]byte, msg.PlacementPolicy.MarshaledSize())
	msg.PlacementPolicy.MarshalStable(policyBin)

	return []stackitem.Item{
		stackitem.NewStruct([]stackitem.Item{
			stackitem.Make(msg.Version.GetMajor()),
			stackitem.Make(msg.Version.GetMinor()),
		}),
		stackitem.Make(ownerAddr[1:][:20]),
		stackitem.Make(msg.Nonce),
		stackitem.Make(msg.BasicAcl),
		stackitem.NewArray(attrs),
		stackitem.NewByteArray(policyBin),
	}
}

func assertEqualItemArray(t *testing.T, exp, got []stackitem.Item) {
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
