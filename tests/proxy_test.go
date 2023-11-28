package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

const proxyPath = "../contracts/proxy"

func deployProxyContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, proxyPath, path.Join(proxyPath, "config.yml"))
	e.DeployContract(t, c, nil)
	regContractNNS(t, e, "proxy", c.Hash)
	return c.Hash
}

func newProxyInvoker(t *testing.T) *neotest.ContractInvoker {
	e := newExecutor(t)

	ctrProxy := neotest.CompileFile(t, e.CommitteeHash, proxyPath, path.Join(proxyPath, "config.yml"))

	_ = deployDefaultNNS(t, e)
	deployProxyContract(t, e)

	return e.CommitteeInvoker(ctrProxy.Hash)
}

func TestVerify(t *testing.T) {
	e := newProxyInvoker(t)

	const method = "verify"

	e.Invoke(t, stackitem.NewBool(true), method)

	notAlphabet := e.NewAccount(t)
	cNotAlphabet := e.WithSigners(notAlphabet)

	cNotAlphabet.Invoke(t, stackitem.NewBool(false), method)
}
