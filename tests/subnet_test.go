package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neofs-contract/common"
)

const subnetPath = "../subnet"

func deploySubnetContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, subnetPath, path.Join(subnetPath, "config.yml"))
	args := []interface{}{true}
	e.DeployContract(t, c, args)
	return c.Hash
}

func newSubnetInvoker(t *testing.T) *neotest.ContractInvoker {
	e := newExecutor(t)
	h := deploySubnetContract(t, e)
	return e.CommitteeInvoker(h)
}

func TestSubnet_Version(t *testing.T) {
	e := newSubnetInvoker(t)
	e.Invoke(t, common.Version, "version")
}
