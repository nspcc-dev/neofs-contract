package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

const balancePath = "../balance"

func deployBalanceContract(t *testing.T, e *neotest.Executor, addrNetmap, addrContainer util.Uint160) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))

	args := make([]any, 3)
	args[0] = false
	args[1] = addrNetmap
	args[2] = addrContainer

	e.DeployContract(t, c, args)
	regContractNNS(t, e, "balance", c.Hash)
	return c.Hash
}

func balanceMint(t *testing.T, c *neotest.ContractInvoker, acc neotest.Signer, amount int64, details []byte) {
	c.Invoke(t, stackitem.Null{}, "mint", acc.ScriptHash(), amount, details)
}
