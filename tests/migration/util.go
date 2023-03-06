package migration

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/storage"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/vm/vmstate"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/stretchr/testify/require"
)

// checkSingleBoolInStack tests that given []stackitem.Item consists of single
// boolean true value.
func checkSingleTrueInStack(tb testing.TB, stack []stackitem.Item) {
	// FIXME: replace temp hack
	ok, err := unwrap.Bool(&result.Invoke{
		State: vmstate.Halt.String(),
		Stack: stack,
	}, nil)
	require.NoError(tb, err)
	require.True(tb, ok)
}

// inheritor of storage.Store canceling Close method
type nopCloseStore struct {
	storage.Store
}

func (x nopCloseStore) Close() error {
	return nil
}

// SkipUnsupportedVersions calls get current version of the Contract using
// 'version' method and checks can the Contract be updated similar to
// common.CheckVersion. If not, SkipUnsupportedVersions skips corresponding
// test.
func SkipUnsupportedVersions(tb testing.TB, c *Contract, updPrms ...interface{}) {
	n, err := c.Call(tb, "version").TryInteger()
	require.NoError(tb, err, "version response must be integer-convertible")

	prevVersion := n.Int64()
	if prevVersion < common.PrevVersion {
		c.CheckUpdateFail(tb, common.ErrVersionMismatch, updPrms...)
		tb.Skipf("skip contract update due to unsupported version %v < %v", prevVersion, common.PrevVersion)
	} else if prevVersion >= common.Version {
		c.CheckUpdateFail(tb, common.ErrAlreadyUpdated, updPrms...)
		tb.Skipf("skip already do update %v >= %v", prevVersion, common.Version)
	}
}
