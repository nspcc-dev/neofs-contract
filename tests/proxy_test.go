package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

const proxyPath = "../contracts/proxy"

func deployProxyContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	c := neotest.CompileFile(t, e.Validator.ScriptHash(), proxyPath, path.Join(proxyPath, "config.yml"))
	e.DeployContract(t, c, nil)
	regContractNNS(t, e, "proxy", c.Hash)
	return c.Hash
}

func newProxyInvoker(t *testing.T) *neotest.ContractInvoker {
	e := newExecutor(t)

	_ = deployDefaultNNS(t, e)
	proxyHash := deployProxyContract(t, e)

	return e.CommitteeInvoker(proxyHash)
}

func TestProxyVerify(t *testing.T) {
	const method = "verify"
	contract := newProxyInvoker(t)
	contract.Invoke(t, stackitem.NewBool(true), method)
	contract.WithSigners(contract.NewAccount(t)).Invoke(t, stackitem.NewBool(false), method)
}

func TestProxyVerifyWithNoneScopeAlphabetSigner(t *testing.T) {
	c, _, _ := newProxySponsorInvoker(t)

	tx := c.PrepareInvokeNoSign(t, "count")
	tx.Signers = []transaction.Signer{
		{Account: c.Signers[0].ScriptHash(), Scopes: transaction.Global},
		{Account: c.Signers[1].ScriptHash(), Scopes: transaction.Global},
		{Account: c.Committee.ScriptHash(), Scopes: transaction.None},
	}

	signers := []neotest.Signer{c.Signers[0], c.Signers[1], c.Committee}
	neotest.AddNetworkFee(t, c.Chain, tx, signers...)
	c.AddSystemFee(tx, -1)
	for i := range signers {
		require.NoError(t, signers[i].SignTx(c.Executor.Chain.GetConfig().Magic, tx))
	}

	c.AddNewBlock(t, tx)
	c.CheckHalt(t, tx.Hash(), stackitem.Make(0))
}
