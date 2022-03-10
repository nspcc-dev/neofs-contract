package tests

import (
	"math/big"
	"math/rand"
	"path"
	"strings"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/encoding/bigint"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/container"
	"github.com/nspcc-dev/neofs-contract/netmap"
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
	require.Equal(t, 0, len(aer.Events))

	dummyInfo.raw[0] ^= 0xFF
	h = cAcc.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)
	aer = cAcc.CheckHalt(t, h)
	require.Equal(t, 0, len(aer.Events))

	c.InvokeFail(t, common.ErrWitnessFailed, "addPeer", dummyInfo.raw)
	c.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo.raw)
}

func TestNewEpoch(t *testing.T) {
	rand.Seed(42)

	const epochCount = netmap.SnapshotCount * 2

	cNm := newNetmapInvoker(t)
	nodes := make([][]testNodeInfo, epochCount)
	for i := range nodes {
		size := rand.Int()%5 + 1
		arr := make([]testNodeInfo, size)
		for j := 0; j < size; j++ {
			arr[j] = newStorageNode(t, cNm)
		}
		nodes[i] = arr
	}

	for i := 0; i < epochCount; i++ {
		for _, tn := range nodes[i] {
			cNm.WithSigners(tn.signer).Invoke(t, stackitem.Null{}, "addPeer", tn.raw)
			cNm.Invoke(t, stackitem.Null{}, "addPeerIR", tn.raw)
		}

		if i > 0 {
			// Remove random nodes from the previous netmap.
			current := make([]testNodeInfo, 0, len(nodes[i])+len(nodes[i-1]))
			current = append(current, nodes[i]...)

			for j := range nodes[i-1] {
				if rand.Int()%3 == 0 {
					cNm.Invoke(t, stackitem.Null{}, "updateStateIR",
						int64(netmap.OfflineState), nodes[i-1][j].pub)
				} else {
					current = append(current, nodes[i-1][j])
				}
			}
			nodes[i] = current
		}
		cNm.Invoke(t, stackitem.Null{}, "newEpoch", i+1)

		t.Logf("Epoch: %d, Netmap()", i)
		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[i])

		for j := 0; j <= i && j < netmap.SnapshotCount; j++ {
			t.Logf("Epoch: %d, diff: %d", i, j)
			s, err := cNm.TestInvoke(t, "snapshot", int64(j))
			require.NoError(t, err)
			require.Equal(t, 1, s.Len())
			checkSnapshot(t, s, nodes[i-j])
		}

		_, err = cNm.TestInvoke(t, "snapshot", netmap.SnapshotCount)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "incorrect diff"))

		_, err = cNm.TestInvoke(t, "snapshot", -1)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "incorrect diff"))
	}
}

func checkSnapshot(t *testing.T, s *vm.Stack, nodes []testNodeInfo) {
	arr, ok := s.Pop().Value().([]stackitem.Item)
	require.True(t, ok, "expected array")
	require.Equal(t, len(nodes), len(arr), "expected %d nodes", len(nodes))

	actual := make([][]byte, len(nodes))
	expected := make([][]byte, len(nodes))
	for i := range nodes {
		n, ok := arr[i].Value().([]stackitem.Item)
		require.True(t, ok, "expected node struct")
		require.Equal(t, 1, len(n), "expected single field")

		raw, ok := n[0].Value().([]byte)
		require.True(t, ok, "expected bytes")

		actual[i] = raw
		expected[i] = nodes[i].raw
	}

	require.ElementsMatch(t, expected, actual, "snapshot is different")
}

func TestUpdateState(t *testing.T) {
	cNm := newNetmapInvoker(t)

	acc := cNm.NewAccount(t)
	cAcc := cNm.WithSigners(acc)
	dummyInfo := dummyNodeInfo(acc)

	cAcc.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)
	cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo.raw)

	pub, ok := vm.ParseSignatureContract(acc.Script())
	require.True(t, ok)

	t.Run("missing witness", func(t *testing.T) {
		cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed,
			"updateStateIR", int64(2), pub)
		cNm.InvokeFail(t, common.ErrWitnessFailed,
			"updateState", int64(2), pub)
	})

	h := cAcc.Invoke(t, stackitem.Null{}, "updateState", int64(2), pub)
	aer := cAcc.CheckHalt(t, h)
	require.Equal(t, 0, len(aer.Events))

	// Check that updating happens only after `updateState` is called by the alphabet.
	s, err := cAcc.TestInvoke(t, "netmapCandidates")
	require.NoError(t, err)
	require.Equal(t, 1, s.Len())

	arr, ok := s.Pop().Value().([]stackitem.Item)
	require.True(t, ok)
	require.Equal(t, 1, len(arr))

	cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(2), pub)

	cAcc.Invoke(t, stackitem.NewArray([]stackitem.Item{}), "netmapCandidates")
}
