package netmap_test

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/rpc/netmap"
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

	updPrm := []any{
		false,
		util.Uint160{}, // Balance contract
		util.Uint160{}, // Container contract
		[]any{},        // Key list, unused
		[]any{},        // Config
	}

	migration.SkipUnsupportedVersions(t, c, updPrm...)

	// gather values which can't be fetched via contract API
	vSnapshotCount := c.GetStorageItem([]byte("snapshotCount"))
	require.NotNil(t, vSnapshotCount)
	snapshotCount := io.NewBinReaderFromBuf(vSnapshotCount).ReadVarUint()

	// read previous values using contract API
	readUint64 := func(method string, args ...any) uint64 {
		n, err := c.Call(t, method, args...).TryInteger()
		require.NoError(t, err)
		return n.Uint64()
	}

	parseNetmapNodes := func(version uint64, items []stackitem.Item) []netmap.NetmapNode {
		res := make([]netmap.NetmapNode, len(items))
		var err error
		for i := range items {
			arr := items[i].Value().([]stackitem.Item)
			res[i].BLOB, err = arr[0].TryBytes()
			require.NoError(t, err)

			n, err := arr[1].TryInteger()
			require.NoError(t, err)
			res[i].State = n
		}
		return res
	}

	readDiffToSnapshots := func(version uint64) map[int][]netmap.NetmapNode {
		m := make(map[int][]netmap.NetmapNode)
		for i := 0; uint64(i) < snapshotCount; i++ {
			m[i] = parseNetmapNodes(version, c.Call(t, "snapshot", int64(i)).Value().([]stackitem.Item))
		}
		return m
	}
	readVersion := func() uint64 { return readUint64("version") }
	readCurrentEpoch := func() uint64 { return readUint64("epoch") }
	readCurrentEpochBlock := func() uint64 { return readUint64("lastEpochBlock") }
	readCurrentNetmap := func(version uint64) []netmap.NetmapNode {
		return parseNetmapNodes(version, c.Call(t, "netmap").Value().([]stackitem.Item))
	}
	readNetmapCandidates := func(version uint64) []netmap.NetmapNode {
		items := c.Call(t, "netmapCandidates").Value().([]stackitem.Item)
		res := make([]netmap.NetmapNode, len(items))
		var err error
		for i := range items {
			arr := items[i].Value().([]stackitem.Item)
			res[i].BLOB, err = arr[0].TryBytes()
			require.NoError(t, err)

			n, err := arr[1].TryInteger()
			require.NoError(t, err)
			res[i].State = n
		}
		return res
	}
	readConfigs := func() []stackitem.Item {
		return c.Call(t, "listConfig").Value().([]stackitem.Item)
	}

	prevVersion := readVersion()
	prevDiffToSnapshots := readDiffToSnapshots(prevVersion)
	prevCurrentEpoch := readCurrentEpoch()
	prevCurrentEpochBlock := readCurrentEpochBlock()
	prevCurrentNetmap := readCurrentNetmap(prevVersion)
	prevNetmapCandidates := readNetmapCandidates(prevVersion)
	prevConfigs := readConfigs()

	// pre-set Inner Ring
	ir := make(keys.PublicKeys, 2)
	for i := range ir {
		k, err := keys.NewPrivateKey()
		require.NoError(t, err)
		ir[i] = k.PublicKey()
	}

	c.SetInnerRing(t, ir)

	c.CheckUpdateSuccess(t, updPrm...)

	// check that contract was updates as expected
	newVersion := readVersion()
	newDiffToSnapshots := readDiffToSnapshots(newVersion)
	newCurrentEpoch := readCurrentEpoch()
	newCurrentEpochBlock := readCurrentEpochBlock()
	newCurrentNetmap := readCurrentNetmap(newVersion)
	newNetmapCandidates := readNetmapCandidates(newVersion)
	newConfigs := readConfigs()

	require.Nil(t, c.GetStorageItem([]byte("innerring")), "Inner Ring nodes should be removed")
	require.Equal(t, prevCurrentEpoch, newCurrentEpoch, "current epoch should remain")
	require.Equal(t, prevCurrentEpochBlock, newCurrentEpochBlock, "current epoch block should remain (method)")
	require.Nil(t, c.GetStorageItem([]byte("snapshotBlock")), "current epoch block should be removed (storage)")
	require.EqualValues(t, prevCurrentEpochBlock, readUint64("getEpochBlock", prevCurrentEpoch),
		"current epoch block should be resolvable")
	require.ElementsMatch(t, prevConfigs, newConfigs, "config should remain")
	require.ElementsMatch(t, prevCurrentNetmap, newCurrentNetmap, "current netmap should remain")
	require.ElementsMatch(t, prevNetmapCandidates, newNetmapCandidates, "netmap candidates should remain")
	require.ElementsMatch(t, ir, c.InnerRing(t))

	require.Equal(t, len(prevDiffToSnapshots), len(newDiffToSnapshots))
	for k, vPrev := range prevDiffToSnapshots {
		vNew, ok := newDiffToSnapshots[k]
		require.True(t, ok)
		require.ElementsMatch(t, vPrev, vNew, "%d-th past netmap snapshot should remain", k)
	}

	var cleanupThreshItem = c.GetStorageItem([]byte("t"))
	cleanupThresh := io.NewBinReaderFromBuf(cleanupThreshItem).ReadVarUint()
	require.EqualValues(t, 3, cleanupThresh)
}
