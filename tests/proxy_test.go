package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

const proxyPath = "../proxy"

func deployProxyContract(t *testing.T, e *neotest.Executor, addrNetmap util.Uint160) util.Uint160 {
	args := make([]any, 1)
	args[0] = addrNetmap

	c := neotest.CompileFile(t, e.CommitteeHash, proxyPath, path.Join(proxyPath, "config.yml"))
	e.DeployContract(t, c, args)
	return c.Hash
}

func newProxyInvoker(t *testing.T) *neotest.ContractInvoker {
	e := newExecutor(t)

	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	ctrProxy := neotest.CompileFile(t, e.CommitteeHash, proxyPath, path.Join(proxyPath, "config.yml"))

	deployNetmapContract(t, e, ctrBalance.Hash, ctrContainer.Hash)
	deployProxyContract(t, e, ctrNetmap.Hash)

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
