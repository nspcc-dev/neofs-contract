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

const (
	subnetPath = "../subnet"

	errSeparator = ": "
)

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

	id := make([]byte, 5)
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

func TestSubnet_Delete(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	e.InvokeFail(t, "witness check failed", "delete", id)

	cAcc := e.WithSigners(owner)
	cAcc.InvokeFail(t, subnet.ErrInvalidSubnetID, "delete", []byte{1, 1, 1, 1})
	cAcc.InvokeFail(t, subnet.ErrNotExist, "delete", []byte{1, 1, 1, 1, 1})
	cAcc.Invoke(t, stackitem.Null{}, "delete", id)
	cAcc.InvokeFail(t, subnet.ErrNotExist, "get", id)
	cAcc.InvokeFail(t, subnet.ErrNotExist, "delete", id)
}

func TestSubnet_AddNodeAdmin(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "addNodeAdmin"

	e.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, admPub)
	e.InvokeFail(t, method+errSeparator+subnet.ErrInvalidAdmin, method, id, admPub[1:])
	e.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, admPub)

	cAdm := e.WithSigners(adm)
	cAdm.InvokeFail(t, method+errSeparator+"owner witness check failed", method, id, admPub)

	cOwner := e.WithSigners(owner)
	cOwner.Invoke(t, stackitem.Null{}, method, id, admPub)

	cOwner.InvokeFail(t, method+errSeparator+"node admin has already been added", method, id, admPub)
}

func TestSubnet_RemoveNodeAdmin(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "removeNodeAdmin"

	e.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, admPub)
	e.InvokeFail(t, method+errSeparator+subnet.ErrInvalidAdmin, method, id, admPub[1:])
	e.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, admPub)

	cAdm := e.WithSigners(adm)
	cAdm.InvokeFail(t, method+errSeparator+"owner witness check failed", method, id, admPub)

	cOwner := e.WithSigners(owner)

	cOwner.InvokeFail(t, method+errSeparator+subnet.ErrNodeAdmNotExist, method, id, admPub)
	cOwner.Invoke(t, stackitem.Null{}, "addNodeAdmin", id, admPub)
	cOwner.Invoke(t, stackitem.Null{}, method, id, admPub)
	cOwner.InvokeFail(t, method+errSeparator+subnet.ErrNodeAdmNotExist, method, id, admPub)
}

func TestSubnet_AddNode(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	node := e.NewAccount(t)
	nodePub, ok := vm.ParseSignatureContract(node.Script())
	require.True(t, ok)

	const method = "addNode"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, nodePub)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidNode, method, id, nodePub[1:])
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, nodePub)

	cOwn.Invoke(t, stackitem.Null{}, method, id, nodePub)
	cOwn.InvokeFail(t, method+errSeparator+"node has already been added", method, id, nodePub)
}

func TestSubnet_RemoveNode(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	node := e.NewAccount(t)
	nodePub, ok := vm.ParseSignatureContract(node.Script())
	require.True(t, ok)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "removeNode"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, nodePub)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidNode, method, id, nodePub[1:])
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, nodePub)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNodeNotExist, method, id, nodePub)

	cOwn.Invoke(t, stackitem.Null{}, "addNode", id, nodePub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, nodePub)

	cAdm := cOwn.WithSigners(adm)

	cOwn.Invoke(t, stackitem.Null{}, "addNodeAdmin", id, admPub)
	cAdm.InvokeFail(t, method+errSeparator+subnet.ErrNodeNotExist, method, id, nodePub)
}

func TestSubnet_NodeAllowed(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	node := e.NewAccount(t)
	nodePub, ok := vm.ParseSignatureContract(node.Script())
	require.True(t, ok)

	const method = "nodeAllowed"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, nodePub)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidNode, method, id, nodePub[1:])
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, nodePub)
	cOwn.Invoke(t, stackitem.NewBool(false), method, id, nodePub)

	cOwn.Invoke(t, stackitem.Null{}, "addNode", id, nodePub)
	cOwn.Invoke(t, stackitem.NewBool(true), method, id, nodePub)
}

func TestSubnet_AddClientAdmin(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "addClientAdmin"

	groupId := randomBytes(8)

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, groupId, admPub)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidAdmin, method, id, groupId, admPub[1:])
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, groupId, admPub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, admPub)
	cOwn.InvokeFail(t, method+errSeparator+"client admin has already been added", method, id, groupId, admPub)
}

func TestSubnet_RemoveClientAdmin(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "removeClientAdmin"

	groupId := randomBytes(8)

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, groupId, admPub)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidAdmin, method, id, groupId, admPub[1:])
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, groupId, admPub)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrClientAdmNotExist, method, id, groupId, admPub)
	cOwn.Invoke(t, stackitem.Null{}, "addClientAdmin", id, groupId, admPub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, admPub)
}

func TestSubnet_AddUser(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	user := randomBytes(27)

	groupId := randomBytes(8)

	const method = "addUser"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, groupId, user)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, groupId, user)

	cOwn.Invoke(t, stackitem.Null{}, "addClientAdmin", id, groupId, admPub)

	cAdm := e.WithSigners(adm)
	cAdm.Invoke(t, stackitem.Null{}, method, id, groupId, user)
	cOwn.InvokeFail(t, method+errSeparator+"user has already been added", method, id, groupId, user)
}

func TestSubnet_RemoveUser(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	groupId := randomBytes(8)
	user := randomBytes(27)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "removeUser"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, groupId, user)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, groupId, user)

	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrUserNotExist, method, id, groupId, user)
	cOwn.Invoke(t, stackitem.Null{}, "addUser", id, groupId, user)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, user)

	cAdm := cOwn.WithSigners(adm)

	cOwn.Invoke(t, stackitem.Null{}, "addClientAdmin", id, groupId, admPub)
	cAdm.InvokeFail(t, method+errSeparator+subnet.ErrUserNotExist, method, id, groupId, user)
}

func TestSubnet_UserAllowed(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	groupId := randomBytes(5)

	user := randomBytes(27)

	const method = "userAllowed"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, method+errSeparator+subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, user)

	cOwn.Invoke(t, stackitem.NewBool(false), method, id, user)
	cOwn.Invoke(t, stackitem.Null{}, "addUser", id, groupId, user)
	cOwn.Invoke(t, stackitem.NewBool(true), method, id, user)
}

func createSubnet(t *testing.T, e *neotest.ContractInvoker) (id []byte, owner neotest.Signer) {
	var (
		ok  bool
		pub []byte
	)

	owner = e.NewAccount(t)
	pub, ok = vm.ParseSignatureContract(owner.Script())
	require.True(t, ok)

	id = make([]byte, 5)
	binary.LittleEndian.PutUint32(id, 123)
	info := randomBytes(10)

	cBoth := e.WithSigners(e.Committee, owner)
	cBoth.Invoke(t, stackitem.Null{}, "put", id, pub, info)

	return
}
