package tests

import (
	"crypto/sha256"
	"path"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/container"
	"github.com/nspcc-dev/neofs-contract/nns"
)

const containerPath = "../container"

const (
	containerFee      = 0_0100_0000
	containerAliasFee = 0_0050_0000
)

func deployContainerContract(t *testing.T, e *neotest.Executor, addrNetmap, addrBalance, addrNNS util.Uint160) util.Uint160 {
	args := make([]interface{}, 6)
	args[0] = int64(0)
	args[1] = addrNetmap
	args[2] = addrBalance
	args[3] = util.Uint160{} // not needed for now
	args[4] = addrNNS
	args[5] = "neofs"

	c := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	e.DeployContract(t, c, args)
	return c.Hash
}

func newContainerInvoker(t *testing.T) (*neotest.ContractInvoker, *neotest.ContractInvoker) {
	e := newExecutor(t)

	ctrNNS := neotest.CompileFile(t, e.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))

	e.DeployContract(t, ctrNNS, nil)
	deployNetmapContract(t, e, ctrBalance.Hash, ctrContainer.Hash,
		container.RegistrationFeeKey, int64(containerFee),
		container.AliasFeeKey, int64(containerAliasFee))
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	deployContainerContract(t, e, ctrNetmap.Hash, ctrBalance.Hash, ctrNNS.Hash)
	return e.CommitteeInvoker(ctrContainer.Hash), e.CommitteeInvoker(ctrBalance.Hash)
}

func setContainerOwner(c []byte, acc neotest.Signer) {
	owner, _ := base58.Decode(address.Uint160ToString(acc.ScriptHash()))
	copy(c[6:], owner)
}

type testContainer struct {
	id                     [32]byte
	value, sig, pub, token []byte
}

func dummyContainer(owner neotest.Signer) testContainer {
	value := randomBytes(100)
	value[1] = 0 // zero offset
	setContainerOwner(value, owner)

	return testContainer{
		id:    sha256.Sum256(value),
		value: value,
		sig:   randomBytes(64),
		pub:   randomBytes(33),
		token: randomBytes(42),
	}
}

func TestContainerPut(t *testing.T) {
	c, cBal := newContainerInvoker(t)

	acc := c.NewAccount(t)
	cnt := dummyContainer(acc)

	putArgs := []interface{}{cnt.value, cnt.sig, cnt.pub, cnt.token}
	c.InvokeFail(t, "insufficient balance to create container", "put", putArgs...)

	balanceMint(t, cBal, acc, containerFee*1, []byte{})

	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "put", putArgs...)

	c.Invoke(t, stackitem.Null{}, "put", putArgs...)

	t.Run("with nice names", func(t *testing.T) {
		ctrNNS := neotest.CompileFile(t, c.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
		nnsHash := ctrNNS.Hash

		balanceMint(t, cBal, acc, containerFee*1, []byte{})

		putArgs := []interface{}{cnt.value, cnt.sig, cnt.pub, cnt.token, "mycnt", ""}
		t.Run("no fee for alias", func(t *testing.T) {
			c.InvokeFail(t, "insufficient balance to create container", "putNamed", putArgs...)
		})

		balanceMint(t, cBal, acc, containerAliasFee*1, []byte{})
		c.Invoke(t, stackitem.Null{}, "putNamed", putArgs...)

		expected := stackitem.NewArray([]stackitem.Item{
			stackitem.NewByteArray([]byte(base58.Encode(cnt.id[:]))),
		})
		cNNS := c.CommitteeInvoker(nnsHash)
		cNNS.Invoke(t, expected, "resolve", "mycnt.neofs", int64(nns.TXT))

		t.Run("name is already taken", func(t *testing.T) {
			c.InvokeFail(t, "name is already taken", "putNamed", putArgs...)
		})

		c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)
		cNNS.Invoke(t, stackitem.Null{}, "resolve", "mycnt.neofs", int64(nns.TXT))

		t.Run("register in advance", func(t *testing.T) {
			cnt.value[len(cnt.value)-1] = 10
			cnt.id = sha256.Sum256(cnt.value)

			cNNS.Invoke(t, true, "register",
				"cdn", c.CommitteeHash,
				"whateveriwant@world.com", int64(0), int64(0), int64(0), int64(0))

			cNNS.Invoke(t, true, "register",
				"domain.cdn", c.CommitteeHash,
				"whateveriwant@world.com", int64(0), int64(0), int64(0), int64(0))

			balanceMint(t, cBal, acc, (containerFee+containerAliasFee)*1, []byte{})

			putArgs := []interface{}{cnt.value, cnt.sig, cnt.pub, cnt.token, "domain", "cdn"}
			c2 := c.WithSigners(c.Committee, acc)
			c2.Invoke(t, stackitem.Null{}, "putNamed", putArgs...)

			expected = stackitem.NewArray([]stackitem.Item{
				stackitem.NewByteArray([]byte(base58.Encode(cnt.id[:])))})
			cNNS.Invoke(t, expected, "resolve", "domain.cdn", int64(nns.TXT))
		})
	})
}

func TestContainerDelete(t *testing.T) {
	c, cBal := newContainerInvoker(t)

	acc := c.NewAccount(t)
	cnt := dummyContainer(acc)

	balanceMint(t, cBal, acc, containerFee*1, []byte{})
	c.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token)

	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "delete",
		cnt.id[:], cnt.sig, cnt.token)

	c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)
	})

	c.InvokeFail(t, container.NotFoundError, "get", cnt.id[:])
}

func TestContainerOwner(t *testing.T) {
	c, cBal := newContainerInvoker(t)

	acc := c.NewAccount(t)
	cnt := dummyContainer(acc)

	balanceMint(t, cBal, acc, containerFee*1, []byte{})
	c.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.InvokeFail(t, container.NotFoundError, "owner", id[:])
	})

	owner, _ := base58.Decode(address.Uint160ToString(acc.ScriptHash()))
	c.Invoke(t, stackitem.NewBuffer(owner), "owner", cnt.id[:])
}

func TestContainerGet(t *testing.T) {
	c, cBal := newContainerInvoker(t)

	acc := c.NewAccount(t)
	cnt := dummyContainer(acc)

	balanceMint(t, cBal, acc, containerFee*1, []byte{})

	c.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.InvokeFail(t, container.NotFoundError, "get", id[:])
	})

	expected := stackitem.NewStruct([]stackitem.Item{
		stackitem.NewByteArray(cnt.value),
		stackitem.NewByteArray(cnt.sig),
		stackitem.NewByteArray(cnt.pub),
		stackitem.NewByteArray(cnt.token),
	})
	c.Invoke(t, expected, "get", cnt.id[:])
}

type eacl struct {
	value []byte
	sig   []byte
	pub   []byte
	token []byte
}

func dummyEACL(containerID [32]byte) eacl {
	e := make([]byte, 50)
	copy(e[6:], containerID[:])
	return eacl{
		value: e,
		sig:   randomBytes(64),
		pub:   randomBytes(33),
		token: randomBytes(42),
	}
}

func TestContainerSetEACL(t *testing.T) {
	c, cBal := newContainerInvoker(t)

	acc := c.NewAccount(t)
	cnt := dummyContainer(acc)
	balanceMint(t, cBal, acc, containerFee*1, []byte{})

	c.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		e := dummyEACL(id)
		c.InvokeFail(t, container.NotFoundError, "setEACL", e.value, e.sig, e.pub, e.token)
	})

	e := dummyEACL(cnt.id)
	setArgs := []interface{}{e.value, e.sig, e.pub, e.token}
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "setEACL", setArgs...)

	c.Invoke(t, stackitem.Null{}, "setEACL", setArgs...)

	expected := stackitem.NewStruct([]stackitem.Item{
		stackitem.NewByteArray(e.value),
		stackitem.NewByteArray(e.sig),
		stackitem.NewByteArray(e.pub),
		stackitem.NewByteArray(e.token),
	})
	c.Invoke(t, expected, "eACL", cnt.id[:])
}
