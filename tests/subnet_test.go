package tests

import (
	"encoding/binary"
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/subnet"
	"github.com/stretchr/testify/require"
)

const subnetPath = "../subnet"

func deploySubnetContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, subnetPath, path.Join(subnetPath, "config.yml"))
	args := []interface{}{false}
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

func TestSubnet_Put(t *testing.T) {
	e := newSubnetInvoker(t)

	acc := e.NewAccount(t)
	pub, ok := vm.ParseSignatureContract(acc.Script())
	require.True(t, ok)

	id := make([]byte, 4)
	binary.LittleEndian.PutUint32(id, 123)
	info := randomBytes(10)

	e.InvokeFail(t, "witness check failed", "put", id, pub, info)

	cAcc := e.WithSigners(acc)
	cAcc.InvokeFail(t, "alphabet witness check failed", "put", id, pub, info)

	cBoth := e.WithSigners(e.Committee, acc)
	cBoth.InvokeFail(t, subnet.ErrInvalidSubnetID, "put", []byte{1, 2, 3}, pub, info)
	cBoth.InvokeFail(t, subnet.ErrInvalidOwner, "put", id, pub[10:], info)
	cBoth.Invoke(t, stackitem.Null{}, "put", id, pub, info)
	cAcc.Invoke(t, stackitem.NewBuffer(info), "get", id)
	cBoth.InvokeFail(t, subnet.ErrAlreadyExists, "put", id, pub, info)
}
