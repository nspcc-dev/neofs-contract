package netmap_test

import (
	"bytes"
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

var (
	notaryDisabledKey = []byte("notary")

	newEpochSubsNewPrefix = []byte("e")
	containerHashOldKey   = []byte("containerScriptHash")
	balanceHashOldKey     = []byte("balanceScriptHash")
)

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	var containerHash util.Uint160
	var balanceHash util.Uint160
	var err error

	// init test contract shell
	c := migration.NewContract(t, d, "netmap", migration.ContractOptions{
		StorageDumpHandler: func(key, value []byte) {
			if bytes.Equal(containerHashOldKey, key) {
				containerHash, err = util.Uint160DecodeBytesLE(value)
				require.NoError(t, err)

				return
			}

			if bytes.Equal(balanceHashOldKey, key) {
				balanceHash, err = util.Uint160DecodeBytesLE(value)
				require.NoError(t, err)

				return
			}
		},
	})

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

	// try to update the contract
	if notaryDisabled && prevPendingVote {
		c.CheckUpdateFail(t, "pending vote detected", updPrm...)
		return
	}

	c.CheckUpdateSuccess(t, updPrm...)

	if prevVersion < 19_000 {
		require.NotZerof(t, balanceHash, "missing storage item %q with Balance contract address", balanceHashOldKey)
		require.NotZerof(t, containerHash, "missing storage item %q with Container contract address", containerHashOldKey)
		checkNewEpochSubscribers(t, c, balanceHash, containerHash)
	}

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
	require.Equal(t, prevCurrentEpochBlock, newCurrentEpochBlock, "current epoch block should remain (method)")
	require.Nil(t, c.GetStorageItem([]byte("snapshotBlock")), "current epoch block should be removed (storage)")
	require.EqualValues(t, prevCurrentEpochBlock, readUint64("getEpochBlock", prevCurrentEpoch),
		"current epoch block should be resolvable")
	require.ElementsMatch(t, prevConfigs, newConfigs, "config should remain")
	require.ElementsMatch(t, prevCurrentNetmap, newCurrentNetmap, "current netmap should remain")
	require.ElementsMatch(t, prevNetmapCandidates, newNetmapCandidates, "netmap candidates should remain")
	require.ElementsMatch(t, ir, c.InnerRing(t))
	require.Nil(t, c.GetStorageItem(balanceHashOldKey), "balance contract address should be removed")
	require.Nil(t, c.GetStorageItem(containerHashOldKey), "container contract address should be removed")

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

func checkNewEpochSubscribers(t *testing.T, contract *migration.Contract, balanceWant, containerWant util.Uint160) {
	// contracts are migrated in alphabetical order at least for now

	var balanceMigrated bool
	var containerMigrated bool

	contract.SeekStorage(append(newEpochSubsNewPrefix, 0), func(k, v []byte) bool {
		balanceGot, err := util.Uint160DecodeBytesLE(k)
		require.NoError(t, err)
		require.Equal(t, balanceWant, balanceGot)

		balanceMigrated = true

		return true
	})

	contract.SeekStorage(append(newEpochSubsNewPrefix, 1), func(k, v []byte) bool {
		containerGot, err := util.Uint160DecodeBytesLE(k)
		require.NoError(t, err)
		require.Equal(t, containerWant, containerGot)

		containerMigrated = true

		return true
	})

	require.True(t, balanceMigrated, "balance contact hash migration")
	require.True(t, containerMigrated, "container contact hash migration")
}
