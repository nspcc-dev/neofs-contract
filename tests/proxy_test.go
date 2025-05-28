package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
)

const proxyPath = "../contracts/proxy"

func deployProxyContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	regContractNNS(t, e, "container", ctrContainer.Hash)

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

func TestProxyVerify(t *testing.T) {
	testVerify(t, newProxyInvoker(t))
}
