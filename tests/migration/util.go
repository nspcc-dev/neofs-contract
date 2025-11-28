package migration

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/storage"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/vm/vmstate"
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

// inheritor of storage.Store canceling Close method.
type nopCloseStore struct {
	storage.Store
}

func (x nopCloseStore) Close() error {
	return nil
}
