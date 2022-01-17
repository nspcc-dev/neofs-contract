package tests

import (
	"math/big"
	"math/rand"
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/encoding/bigint"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/container"
	"github.com/stretchr/testify/require"
)

const netmapPath = "../netmap"

func deployNetmapContract(t *testing.T, e *neotest.Executor, addrBalance, addrContainer util.Uint160, config ...interface{}) util.Uint160 {
	_, pubs, ok := vm.ParseMultiSigContract(e.Committee.Script())
	require.True(t, ok)

	args := make([]interface{}, 5)
	args[0] = false
	args[1] = addrBalance
	args[2] = addrContainer
	args[3] = []interface{}{pubs[0]}
	args[4] = append([]interface{}{}, config...)

	c := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	e.DeployContract(t, c, args)
	return c.Hash
}

func newNetmapInvoker(t *testing.T, config ...interface{}) *neotest.ContractInvoker {
	e := newExecutor(t)

	ctrNNS := neotest.CompileFile(t, e.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))

	e.DeployContract(t, ctrNNS, nil)
	deployContainerContract(t, e, ctrNetmap.Hash, ctrBalance.Hash, ctrNNS.Hash)
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	deployNetmapContract(t, e, ctrBalance.Hash, ctrContainer.Hash, config...)
	return e.CommitteeInvoker(ctrNetmap.Hash)
}

func TestDeploySetConfig(t *testing.T) {
	c := newNetmapInvoker(t, "SomeKey", "TheValue", container.AliasFeeKey, int64(123))
	c.Invoke(t, "TheValue", "config", "SomeKey")
	c.Invoke(t, stackitem.NewByteArray(bigint.ToBytes(big.NewInt(123))),
		"config", container.AliasFeeKey)
}

type testNodeInfo struct {
	signer neotest.SingleSigner
	pub    []byte
	raw    []byte
}

func dummyNodeInfo(acc neotest.Signer) testNodeInfo {
	ni := make([]byte, 66)
	rand.Read(ni)

	s := acc.(neotest.SingleSigner)
	pub := s.Account().PrivateKey().PublicKey().Bytes()
	copy(ni[2:], pub)
	return testNodeInfo{
		signer: s,
		pub:    pub,
		raw:    ni,
	}
}

func newStorageNode(t *testing.T, c *neotest.ContractInvoker) testNodeInfo {
	return dummyNodeInfo(c.NewAccount(t))
}

func TestAddPeer(t *testing.T) {
	c := newNetmapInvoker(t)

	acc := c.NewAccount(t)
	cAcc := c.WithSigners(acc)
	dummyInfo := dummyNodeInfo(acc)

	acc1 := c.NewAccount(t)
	cAcc1 := c.WithSigners(acc1)
	cAcc1.InvokeFail(t, common.ErrWitnessFailed, "addPeer", dummyInfo.raw)

	h := cAcc.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)
	aer := cAcc.CheckHalt(t, h)
	require.Equal(t, 1, len(aer.Events))
	require.Equal(t, "AddPeer", aer.Events[0].Name)
	require.Equal(t, stackitem.NewArray([]stackitem.Item{stackitem.NewByteArray(dummyInfo.raw)}),
		aer.Events[0].Item)

	dummyInfo.raw[0] ^= 0xFF
	h = cAcc.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)
	aer = cAcc.CheckHalt(t, h)
	require.Equal(t, 1, len(aer.Events))
	require.Equal(t, "AddPeer", aer.Events[0].Name)
	require.Equal(t, stackitem.NewArray([]stackitem.Item{stackitem.NewByteArray(dummyInfo.raw)}),
		aer.Events[0].Item)

	c.InvokeFail(t, common.ErrWitnessFailed, "addPeer", dummyInfo.raw)
	c.Invoke(t, stackitem.Null{}, "register", dummyInfo.raw)
}

func TestUpdateState(t *testing.T) {
	e := newNetmapInvoker(t)

	acc := e.NewAccount(t)
	cAcc := e.WithSigners(acc)
	cBoth := e.WithSigners(e.Committee, acc)
	dummyInfo := dummyNodeInfo(acc)

	cBoth.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)

	pub, ok := vm.ParseSignatureContract(acc.Script())
	require.True(t, ok)

	t.Run("missing witness", func(t *testing.T) {
		cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed,
			"updateState", int64(2), pub)
		e.InvokeFail(t, common.ErrWitnessFailed,
			"updateState", int64(2), pub)
	})

	cBoth.Invoke(t, stackitem.Null{}, "updateState", int64(2), pub)
	cAcc.Invoke(t, stackitem.NewArray([]stackitem.Item{}), "netmapCandidates")
}
