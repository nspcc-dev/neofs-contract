package tests

import (
	"crypto/sha256"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/core"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/nspcc-dev/neofs-contract/container"
	"github.com/nspcc-dev/neofs-contract/nns"
	"github.com/stretchr/testify/require"
)

const containerPath = "../container"

const containerFee = 0_0100_0000

func deployContainerContract(t *testing.T, bc *core.Blockchain, addrNetmap, addrBalance, addrNNS util.Uint160) util.Uint160 {
	args := make([]interface{}, 6)
	args[0] = int64(0)
	args[1] = addrNetmap
	args[2] = addrBalance
	args[3] = util.Uint160{} // not needed for now
	args[4] = addrNNS
	args[5] = "neofs"
	return DeployContract(t, bc, containerPath, args)
}

func prepareContainerContract(t *testing.T, bc *core.Blockchain) (util.Uint160, util.Uint160) {
	addrNNS := DeployContract(t, bc, nnsPath, nil)

	ctrNetmap, err := ContractInfo(CommitteeAcc.Contract.ScriptHash(), netmapPath)
	require.NoError(t, err)

	ctrBalance, err := ContractInfo(CommitteeAcc.Contract.ScriptHash(), balancePath)
	require.NoError(t, err)

	ctrContainer, err := ContractInfo(CommitteeAcc.Contract.ScriptHash(), containerPath)
	require.NoError(t, err)

	deployNetmapContract(t, bc, ctrBalance.Hash, ctrContainer.Hash, container.RegistrationFeeKey, int64(containerFee))
	balHash := deployBalanceContract(t, bc, ctrNetmap.Hash, ctrContainer.Hash)
	return deployContainerContract(t, bc, ctrNetmap.Hash, ctrBalance.Hash, addrNNS), balHash
}

func setContainerOwner(c []byte, owner *wallet.Account) {
	copy(c[7:], owner.Contract.ScriptHash().BytesBE())
}

type testContainer struct {
	id                     [32]byte
	value, sig, pub, token []byte
}

func dummyContainer(owner *wallet.Account) testContainer {
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
	bc := NewChain(t)
	h, balanceHash := prepareContainerContract(t, bc)

	acc := NewAccount(t, bc)
	c := dummyContainer(acc)

	putArgs := []interface{}{c.value, c.sig, c.pub, c.token}
	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "put", putArgs...)
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "insufficient balance to create container")

	balanceMint(t, bc, acc, balanceHash, containerFee*1, []byte{})

	tx = PrepareInvoke(t, bc, acc, h, "put", putArgs...)
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "alphabet witness check failed")

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "put", putArgs...)
	AddBlockCheckHalt(t, bc, tx)

	t.Run("with nice names", func(t *testing.T) {
		nnsHash := contracts[nnsPath].Hash

		balanceMint(t, bc, acc, balanceHash, containerFee*1, []byte{})

		putArgs := []interface{}{c.value, c.sig, c.pub, c.token, "mycnt", ""}
		tx = PrepareInvoke(t, bc, CommitteeAcc, h, "putNamed", putArgs...)
		AddBlockCheckHalt(t, bc, tx)

		tx = PrepareInvoke(t, bc, acc, nnsHash, "resolve", "mycnt.neofs", int64(nns.TXT))
		CheckTestInvoke(t, bc, tx, stackitem.NewArray([]stackitem.Item{
			stackitem.NewByteArray([]byte(base58.Encode(c.id[:]))),
		}))

		tx = PrepareInvoke(t, bc, CommitteeAcc, h, "delete", c.id[:], c.sig, c.token)
		AddBlockCheckHalt(t, bc, tx)

		tx = PrepareInvoke(t, bc, CommitteeAcc, nnsHash, "resolve", "mycnt.neofs", int64(nns.TXT))
		CheckTestInvoke(t, bc, tx, stackitem.Null{})

		t.Run("register in advance", func(t *testing.T) {
			c.value[len(c.value)-1] = 10
			c.id = sha256.Sum256(c.value)

			t.Run("bad domain owner", func(t *testing.T) {
				tx = PrepareInvoke(t, bc, acc, nnsHash, "register",
					"baddomain.neofs", acc.Contract.ScriptHash(),
					"whateveriwant@world.com", int64(0), int64(0), int64(0), int64(0))
				AddBlockCheckHalt(t, bc, tx)

				tx = PrepareInvoke(t, bc, acc, h, "putNamed",
					c.value, c.sig, c.pub, c.token, "baddomain", "neofs")
				AddBlock(t, bc, tx)
				CheckFault(t, bc, tx.Hash(), "committee must own registered domain")
			})

			tx = PrepareInvoke(t, bc, CommitteeAcc, nnsHash, "register",
				"second.neofs", CommitteeAcc.Contract.ScriptHash(),
				"whateveriwant@world.com", int64(0), int64(0), int64(0), int64(0))
			AddBlockCheckHalt(t, bc, tx)

			balanceMint(t, bc, acc, balanceHash, containerFee*1, []byte{})

			putArgs := []interface{}{c.value, c.sig, c.pub, c.token, "second", "neofs"}
			tx = PrepareInvoke(t, bc, []*wallet.Account{CommitteeAcc, acc}, h, "putNamed", putArgs...)
			AddBlockCheckHalt(t, bc, tx)

			tx = PrepareInvoke(t, bc, CommitteeAcc, nnsHash, "resolve", "second.neofs", int64(nns.TXT))
			CheckTestInvoke(t, bc, tx, stackitem.NewArray([]stackitem.Item{
				stackitem.NewByteArray([]byte(base58.Encode(c.id[:]))),
			}))
		})
	})
}

func TestContainerDelete(t *testing.T) {
	bc := NewChain(t)
	h, balanceHash := prepareContainerContract(t, bc)

	acc := NewAccount(t, bc)
	c := dummyContainer(acc)

	balanceMint(t, bc, acc, balanceHash, containerFee*1, []byte{})
	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "put",
		c.value, c.sig, c.pub, c.token)
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, acc, h, "delete", c.id[:], c.sig, c.token)
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "delete: alphabet witness check failed")

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "delete", c.id[:], c.sig, c.token)
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, acc, h, "get", c.id[:])
	CheckTestInvoke(t, bc, tx, stackitem.NewStruct([]stackitem.Item{
		stackitem.NewBuffer([]byte{}),
		stackitem.NewBuffer([]byte{}),
		stackitem.NewBuffer([]byte{}),
		stackitem.NewBuffer([]byte{}),
	}))
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
	bc := NewChain(t)
	h, balanceHash := prepareContainerContract(t, bc)

	acc := NewAccount(t, bc)
	balanceMint(t, bc, acc, balanceHash, containerFee*1, []byte{})

	c := dummyContainer(acc)
	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "put", c.value, c.sig, c.pub, c.token)
	AddBlockCheckHalt(t, bc, tx)

	e := dummyEACL(c.id)
	tx = PrepareInvoke(t, bc, acc, h, "setEACL", e.value, e.sig, e.pub, e.token)
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "setEACL: alphabet witness check failed")

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "setEACL", e.value, e.sig, e.pub, e.token)
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "eACL", c.id[:])
	CheckTestInvoke(t, bc, tx, stackitem.NewStruct([]stackitem.Item{
		stackitem.NewByteArray(e.value),
		stackitem.NewByteArray(e.sig),
		stackitem.NewByteArray(e.pub),
		stackitem.NewByteArray(e.token),
	}))
}
