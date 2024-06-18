package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

const balancePath = "../contracts/balance"

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

func TestBalanceLifecycle(t *testing.T) {
	e := newExecutor(t)

	deployDefaultNNS(t, e)
	nHash := deployNetmapContract(t, e)
	bHash := deployBalanceContract(t, e, util.Uint160{}, util.Uint160{})

	c := e.CommitteeInvoker(bHash)
	acc1 := c.NewAccount(t)
	acc2 := c.NewAccount(t)

	checkTotalSupply := func(expected int64) {
		stack, err := c.TestInvoke(t, "totalSupply")
		require.NoError(t, err)
		require.Equal(t, 1, stack.Len())
		bal := stack.Pop().BigInt().Int64()
		require.Equal(t, expected, bal)
	}

	checkBalance := func(acc util.Uint160, expected int64) {
		stack, err := c.TestInvoke(t, "balanceOf", acc)
		require.NoError(t, err)
		require.Equal(t, 1, stack.Len())
		bal := stack.Pop().BigInt().Int64()
		require.Equal(t, expected, bal)
	}

	c.Invoke(t, stackitem.Null{}, "mint", acc1.ScriptHash(), 100500, []byte("extra"))
	checkBalance(acc1.ScriptHash(), 100500)
	checkTotalSupply(100500)

	// Fails because acc1 is not a signer.
	c.Invoke(t, false, "transfer", acc1.ScriptHash(), acc2.ScriptHash(), 500, nil)

	c1 := c.WithSigners(acc1)
	c1.Invoke(t, true, "transfer", acc1.ScriptHash(), acc2.ScriptHash(), 500, nil)
	checkBalance(acc2.ScriptHash(), 500)
	checkBalance(acc1.ScriptHash(), 100000)
	checkTotalSupply(100500)

	// But c1 can't mint.
	c1.InvokeFail(t, "alphabet witness check failed", "mint", acc1.ScriptHash(), 100500, []byte("extra"))

	// c1 can't burn.
	c1.InvokeFail(t, "alphabet witness check failed", "burn", acc1.ScriptHash(), 1, []byte("extra"))

	// Validators can burn.
	c.Invoke(t, stackitem.Null{}, "burn", acc1.ScriptHash(), 1, []byte("extra"))
	checkBalance(acc1.ScriptHash(), 99999)
	checkTotalSupply(100499)

	c.Invoke(t, stackitem.Null{}, "lock", []byte("details"), acc1.ScriptHash(), util.Uint160{1, 2, 3}, 999, 1)
	checkBalance(util.Uint160{1, 2, 3}, 999)
	checkBalance(acc1.ScriptHash(), 99000)
	checkTotalSupply(100499)

	nc := e.CommitteeInvoker(nHash)
	nc.Invoke(t, stackitem.Null{}, "newEpoch", 1)
	// Unlocked.
	checkBalance(acc1.ScriptHash(), 99999)
	checkBalance(util.Uint160{1, 2, 3}, 0)
	checkTotalSupply(100499)
}
