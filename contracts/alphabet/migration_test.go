package alphabet_test

import (
	"path/filepath"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/nspcc-dev/neofs-contract/tests/migration"
	"github.com/stretchr/testify/require"
)

const name = "alphabet"

func TestMigration(t *testing.T) {
	err := dump.IterateDumps("../../testdata", func(id dump.ID, r *dump.Reader) {
		t.Run(id.String()+"/"+name, func(t *testing.T) {
			testMigrationFromDump(t, r)
		})
	})
	require.NoError(t, err)
}

func replaceArgI(vs []any, i int, v any) []any {
	res := make([]any, len(vs))
	copy(res, vs)
	res[i] = v
	return res
}

var notaryDisabledKey = []byte("notary")

func testMigrationFromDump(t *testing.T, d *dump.Reader) {
	// init test contract shell
	c := migration.NewContract(t, d, "alphabet0", migration.ContractOptions{
		SourceCodeDir: filepath.Join("..", name),
	})

	migration.SkipUnsupportedVersions(t, c)

	// gather values which can't be fetched via contract API
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
	readName := func() string {
		b, err := c.Call(t, "name").TryBytes()
		require.NoError(t, err)
		return string(b)
	}

	prevName := readName()

	// try to update the contract
	proxyContract := util.Uint160{1, 2, 3}
	updPrm := []any{
		false,                 // non-notary mode
		util.Uint160{3, 2, 1}, // unused
		[]byte{},              // Proxy contract (custom)
		"",                    // unused
		0,                     // unused
		0,                     // unused
	}

	if notaryDisabled {
		c.CheckUpdateFail(t, "address of the Proxy contract is missing or invalid",
			replaceArgI(updPrm, 2, make([]byte, interop.Hash160Len+1))...)
		c.CheckUpdateFail(t, "token not found", updPrm...)

		c.RegisterContractInNNS(t, "proxy", proxyContract)

		if prevPendingVote {
			c.CheckUpdateFail(t, "pending vote detected", updPrm...)
			return
		}
	}

	c.CheckUpdateSuccess(t, updPrm...)

	// check that contract was updates as expected
	newName := readName()
	newPendingVote := readPendingVotes()

	require.Nil(t, c.GetStorageItem(notaryDisabledKey), "notary flag should be removed")
	require.Nil(t, c.GetStorageItem([]byte("innerring")), "Inner Ring nodes should be removed")
	require.Equal(t, prevName, newName, "name should remain")
	require.False(t, newPendingVote, "there should be no more pending votes")

	if notaryDisabled {
		require.Equal(t, proxyContract[:], c.GetStorageItem([]byte("proxyScriptHash")), "name should remain")
	}
}
