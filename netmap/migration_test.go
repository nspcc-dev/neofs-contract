package netmap_test

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/netmap"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "netmap"

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
	// init test contract shell
	c := migration.NewContract(t, d, "netmap", migration.ContractOptions{})

	var notary bool
	updPrm := []any{
		!notary,
		util.Uint160{},  // Balance contract
		util.Uint160{},  // Container contract
		[]any{}, // Key list, unused
		[]any{}, // Config
	}

	migration.SkipUnsupportedVersions(t, c, updPrm...)

	// gather values which can't be fetched via contract API
	vSnapshotCount := c.GetStorageItem([]byte("snapshotCount"))
	require.NotNil(t, vSnapshotCount)
	snapshotCount := io.NewBinReaderFromBuf(vSnapshotCount).ReadVarUint()

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
	readUint64 := func(method string) uint64 {
		n, err := c.Call(t, method).TryInteger()
		require.NoError(t, err)
		return n.Uint64()
	}

	parseNetmapNodes := func(version uint64, items []stackitem.Item) []netmap.Node {
		res := make([]netmap.Node, len(items))
		var err error
		for i := range items {
			arr := items[i].Value().([]stackitem.Item)
			res[i].BLOB, err = arr[0].TryBytes()
			require.NoError(t, err)

			if version <= 15_004 {
				res[i].State = netmap.NodeStateOnline
			} else {
				n, err := arr[1].TryInteger()
				require.NoError(t, err)
				res[i].State = netmap.NodeState(n.Int64())
			}
		}
		return res
	}

	readDiffToSnapshots := func(version uint64) map[int][]netmap.Node {
		m := make(map[int][]netmap.Node)
		for i := 0; uint64(i) < snapshotCount; i++ {
			m[i] = parseNetmapNodes(version, c.Call(t, "snapshot", int64(i)).Value().([]stackitem.Item))
		}
		return m
	}
	readVersion := func() uint64 { return readUint64("version") }
	readCurrentEpoch := func() uint64 { return readUint64("epoch") }
	readCurrentEpochBlock := func() uint64 { return readUint64("lastEpochBlock") }
	readCurrentNetmap := func(version uint64) []netmap.Node {
		return parseNetmapNodes(version, c.Call(t, "netmap").Value().([]stackitem.Item))
	}
	readNetmapCandidates := func(version uint64) []netmap.Node {
		items := c.Call(t, "netmapCandidates").Value().([]stackitem.Item)
		res := make([]netmap.Node, len(items))
		var err error
		for i := range items {
			arr := items[i].Value().([]stackitem.Item)
			if version <= 15_004 {
				res[i].BLOB, err = arr[0].Value().([]stackitem.Item)[0].TryBytes()
				require.NoError(t, err)
			} else {
				res[i].BLOB, err = arr[0].TryBytes()
				require.NoError(t, err)
			}

			n, err := arr[1].TryInteger()
			require.NoError(t, err)
			res[i].State = netmap.NodeState(n.Int64())
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

	// try to update the contract
	c.CheckUpdateFail(t, "update to non-notary mode is not supported anymore", updPrm...)
	updPrm[0] = notary

	if notaryDisabled && prevPendingVote {
		c.CheckUpdateFail(t, "pending vote detected", updPrm...)
		return
	}

	c.CheckUpdateSuccess(t, updPrm...)

	// check that contract was updates as expected
	newPendingVotes := readPendingVotes()
	newVersion := readVersion()
	newDiffToSnapshots := readDiffToSnapshots(newVersion)
	newCurrentEpoch := readCurrentEpoch()
	newCurrentEpochBlock := readCurrentEpochBlock()
	newCurrentNetmap := readCurrentNetmap(newVersion)
	newNetmapCandidates := readNetmapCandidates(newVersion)
	newConfigs := readConfigs()

	require.False(t, newPendingVotes, "notary flag should be removed")
	require.Nil(t, c.GetStorageItem(notaryDisabledKey), "notary flag should be removed")
	require.Nil(t, c.GetStorageItem([]byte("innerring")), "Inner Ring nodes should be removed")
	require.Equal(t, prevCurrentEpoch, newCurrentEpoch, "current epoch should remain")
	require.Equal(t, prevCurrentEpochBlock, newCurrentEpochBlock, "current epoch block should remain")
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
}
