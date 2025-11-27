package netmap_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "netmap"

func TestMigration(t *testing.T) {
	err := dump.IterateDumps("../../testdata", func(id dump.ID, r *dump.Reader) {
		t.Run(id.String()+"/"+name, func(t *testing.T) {
			testMigrationFromDump(t, r)
		})
	})
	require.NoError(t, err)
}

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	// init test contract shell
	c := migration.NewContract(t, d, "netmap", migration.ContractOptions{})

	migration.SkipUnsupportedVersions(t, c)

	// gather values which can't be fetched via contract API
	vSnapshotCount := c.GetStorageItem([]byte("snapshotCount"))
	require.NotNil(t, vSnapshotCount)

	// read previous values using contract API
	readUint64 := func(method string, args ...any) uint64 {
		n, err := c.Call(t, method, args...).TryInteger()
		require.NoError(t, err)
		return n.Uint64()
	}

	readNodes := func() []stackitem.Item {
		var nodes []stackitem.Item

		iter := c.Call(t, "listNodes").Value().(*storage.Iterator)
		for iter.Next() {
			nodes = append(nodes, iter.Value())
		}
		return nodes
	}
	readCandidates := func() []stackitem.Item {
		var nodes []stackitem.Item

		iter := c.Call(t, "listCandidates").Value().(*storage.Iterator)
		for iter.Next() {
			nodes = append(nodes, iter.Value())
		}
		return nodes
	}
	readVersion := func() uint64 { return readUint64("version") }
	readCurrentEpoch := func() uint64 { return readUint64("epoch") }
	readCurrentEpochBlock := func() uint64 { return readUint64("lastEpochBlock") }
	readConfigs := func() []stackitem.Item {
		return c.Call(t, "listConfig").Value().([]stackitem.Item)
	}

	prevCurrentEpoch := readCurrentEpoch()
	prevCurrentEpochBlock := readCurrentEpochBlock()
	prevNodes := readNodes()
	prevCandidates := readCandidates()
	prevConfigs := readConfigs()

	// pre-set Inner Ring
	ir := make(keys.PublicKeys, 2)
	for i := range ir {
		k, err := keys.NewPrivateKey()
		require.NoError(t, err)
		ir[i] = k.PublicKey()
	}

	c.SetInnerRing(t, ir)

	c.CheckUpdateSuccess(t)

	// check that contract was updates as expected
	newVersion := readVersion()
	newCurrentEpoch := readCurrentEpoch()
	newCurrentEpochBlock := readCurrentEpochBlock()
	newNodes := readNodes()
	newCandidates := readCandidates()
	newConfigs := readConfigs()

	require.Equal(t, uint64(25*1000+1), newVersion)
	require.Nil(t, c.GetStorageItem([]byte("innerring")), "Inner Ring nodes should be removed")
	require.Equal(t, prevCurrentEpoch, newCurrentEpoch, "current epoch should remain")
	require.Equal(t, prevCurrentEpochBlock, newCurrentEpochBlock, "current epoch block should remain (method)")
	require.Nil(t, c.GetStorageItem([]byte("snapshotBlock")), "current epoch block should be removed (storage)")
	require.EqualValues(t, prevCurrentEpochBlock, readUint64("getEpochBlock", prevCurrentEpoch),
		"current epoch block should be resolvable")
	// Adjust configs for migration.
	prevConfigs = slices.DeleteFunc(prevConfigs, func(s stackitem.Item) bool {
		sa := s.Value().([]stackitem.Item)

		return len(sa) > 0 &&
			// IR fee is dropped in 0.24.0.
			(bytes.Equal(sa[0].Value().([]byte), []byte("InnerRingCandidateFee")) ||
				// NodeV2/Maintenance/Audit flags are gone with 0.26.0.
				bytes.Equal(sa[0].Value().([]byte), []byte("AuditFee")) ||
				bytes.Equal(sa[0].Value().([]byte), []byte("MaintenanceModeAllowed")) ||
				bytes.Equal(sa[0].Value().([]byte), []byte("UseNodeV2")))
	})
	require.ElementsMatch(t, prevConfigs, newConfigs, "config should remain")
	require.Equal(t, prevNodes, newNodes)
	require.Equal(t, prevCandidates, newCandidates)
	require.ElementsMatch(t, ir, c.InnerRing(t))

	var cleanupThreshItem = c.GetStorageItem([]byte("t"))
	cleanupThresh := io.NewBinReaderFromBuf(cleanupThreshItem).ReadVarUint()
	require.EqualValues(t, 3, cleanupThresh)

	require.Nil(t, c.GetStorageItem([]byte("snapshotCurrent")))
	c.SeekStorage([]byte("candidate"), func(_, _ []byte) bool {
		t.Fail()
		return false
	})
	c.SeekStorage([]byte("snapshot_"), func(_, _ []byte) bool {
		t.Fail()
		return false
	})
}
