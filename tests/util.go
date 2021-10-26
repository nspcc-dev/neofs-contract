package tests

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
)

func newExecutor(t *testing.T) *neotest.Executor {
	bc, acc := chain.NewSingle(t)
	return neotest.NewExecutor(t, bc, acc, acc)
}
