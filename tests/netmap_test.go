package tests

import (
	"math/big"
	"math/rand/v2"
	"path"
	"strings"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/bigint"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
	"github.com/nspcc-dev/neofs-contract/contracts/netmap"
	"github.com/nspcc-dev/neofs-contract/contracts/netmap/nodestate"
	"github.com/stretchr/testify/require"
)

const netmapPath = "../contracts/netmap"

func deployNetmapContract(t *testing.T, e *neotest.Executor, config ...any) util.Uint160 {
	_, pubs, ok := vm.ParseMultiSigContract(e.Committee.Script())
	require.True(t, ok)

	args := make([]any, 5)
	args[0] = false
	args[1] = util.Uint160{} // legacy contract hashes
	args[2] = util.Uint160{} // legacy contract hashes
	args[3] = []any{pubs[0]}
	args[4] = config

	c := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	e.DeployContract(t, c, args)
	regContractNNS(t, e, "netmap", c.Hash)
	return c.Hash
}

func newNetmapInvoker(t *testing.T, config ...any) *neotest.ContractInvoker {
	e := newExecutor(t)

	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	deployDefaultNNS(t, e)
	deployNetmapContract(t, e, config...)
	return e.CommitteeInvoker(ctrNetmap.Hash)
}

func TestDeploySetConfig(t *testing.T) {
	c := newNetmapInvoker(t, "SomeKey", "TheValue", containerconst.AliasFeeKey, int64(123))
	c.Invoke(t, "TheValue", "config", "SomeKey")
	c.Invoke(t, stackitem.NewByteArray(bigint.ToBytes(big.NewInt(123))),
		"config", containerconst.AliasFeeKey)
}

type testNodeInfo struct {
	signer neotest.SingleSigner
	pub    []byte
	raw    []byte
	state  nodestate.Type
}

func dummyNodeInfo(acc neotest.Signer) testNodeInfo {
	ni := make([]byte, 66)
	ni[0] = byte(rand.Int())

	s := acc.(neotest.SingleSigner)
	pub := s.Account().PrivateKey().PublicKey().Bytes()
	copy(ni[2:], pub)
	return testNodeInfo{
		signer: s,
		pub:    pub,
		raw:    ni,
		state:  nodestate.Online,
	}
}

func newStorageNode(t *testing.T, c *neotest.ContractInvoker) testNodeInfo {
	return dummyNodeInfo(c.NewAccount(t))
}

func TestAddPeer(t *testing.T) {
	c := newNetmapInvoker(t)

	acc := c.NewAccount(t)
	var cAcc = new(neotest.ContractInvoker)
	*cAcc = *c
	cAcc.Signers = append(cAcc.Signers, acc)
	dummyInfo := dummyNodeInfo(acc)

	acc1 := c.NewAccount(t)
	cAcc1 := c.WithSigners(acc1)
	cAcc1.InvokeFail(t, common.ErrWitnessFailed, "addPeer", dummyInfo.raw)

	h := cAcc.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)
	aer := cAcc.CheckHalt(t, h)
	require.Equal(t, 1, len(aer.Events))

	dummyInfo.raw[0] ^= 0xFF
	h = cAcc.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)
	aer = cAcc.CheckHalt(t, h)
	require.Equal(t, 1, len(aer.Events))

	c.InvokeFail(t, common.ErrWitnessFailed, "addPeer", dummyInfo.raw)
	c.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo.raw)
}

func TestNewEpoch(t *testing.T) {
	const epochCount = netmap.DefaultSnapshotCount * 2

	cNm := newNetmapInvoker(t)
	nodes := make([][]testNodeInfo, epochCount)
	for i := range nodes {
		size := rand.IntN(5) + 1
		arr := make([]testNodeInfo, size)
		for j := range arr {
			arr[j] = newStorageNode(t, cNm)
		}
		nodes[i] = arr
	}

	for i := range nodes {
		for _, tn := range nodes[i] {
			var cAcc = new(neotest.ContractInvoker)
			*cAcc = *cNm
			cAcc.Signers = append(cAcc.Signers, tn.signer)
			cAcc.Invoke(t, stackitem.Null{}, "addPeer", tn.raw)
		}

		if i > 0 {
			// Remove random nodes from the previous netmap.
			current := make([]testNodeInfo, 0, len(nodes[i])+len(nodes[i-1]))
			current = append(current, nodes[i]...)

			for j := range nodes[i-1] {
				if rand.IntN(3) == 0 {
					cNm.Invoke(t, stackitem.Null{}, "updateStateIR",
						int64(nodestate.Offline), nodes[i-1][j].pub)
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

		for j := 0; j <= i && j < netmap.DefaultSnapshotCount; j++ {
			t.Logf("Epoch: %d, diff: %d", i, j)
			checkSnapshotAt(t, j, cNm, nodes[i-j])
		}

		_, err = cNm.TestInvoke(t, "snapshot", netmap.DefaultSnapshotCount)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "incorrect diff"))

		_, err = cNm.TestInvoke(t, "snapshot", -1)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "incorrect diff"))
	}
}

func TestSubscribeForNewEpoch(t *testing.T) {
	e := newExecutor(t)

	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	netmapInvoker := e.CommitteeInvoker(ctrNetmap.Hash)

	nnsHash := deployDefaultNNS(t, e)
	deployNetmapContract(t, e)

	// balance and container contracts subscribe to NewEpoch on their deployments
	deployProxyContract(t, e)
	deployContainerContract(t, e, &ctrNetmap.Hash, &ctrBalance.Hash, &nnsHash)
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)

	t.Run("new epoch", func(t *testing.T) {
		netmapInvoker.Invoke(t, stackitem.Null{}, "newEpoch", 1) // no panic so registrations and calls are OK
	})

	t.Run("double subscription", func(t *testing.T) {
		netmapInvoker.Invoke(t, stackitem.Null{}, "subscribeForNewEpoch", ctrBalance.Hash)
		netmapInvoker.Invoke(t, stackitem.Null{}, "subscribeForNewEpoch", ctrBalance.Hash)

		netmapContractID := netmapInvoker.Executor.Chain.GetContractState(netmapInvoker.Hash).ID

		const subscribersPrefix = "e"
		var foundThirdSubscriber bool
		var balanceSubscribers int
		var containerSubscribers int

		netmapInvoker.Chain.SeekStorage(netmapContractID, append([]byte(subscribersPrefix), 0), func(k, v []byte) bool {
			balanceSubscribers++
			return false
		})
		netmapInvoker.Chain.SeekStorage(netmapContractID, append([]byte(subscribersPrefix), 1), func(k, v []byte) bool {
			containerSubscribers++
			return false
		})
		netmapInvoker.Chain.SeekStorage(netmapContractID, append([]byte(subscribersPrefix), 2), func(k, v []byte) bool {
			foundThirdSubscriber = true
			return false
		})

		require.Equal(t, 1, balanceSubscribers)
		require.Equal(t, 1, containerSubscribers)
		require.False(t, foundThirdSubscriber)
	})
}

func TestUpdateSnapshotCount(t *testing.T) {
	require.True(t, netmap.DefaultSnapshotCount > 5) // sanity check, adjust tests if false.

	prepare := func(t *testing.T, cNm *neotest.ContractInvoker, epochCount int) [][]testNodeInfo {
		nodes := make([][]testNodeInfo, epochCount)
		nodes[0] = []testNodeInfo{newStorageNode(t, cNm)}
		cNm.Invoke(t, stackitem.Null{}, "addPeerIR", nodes[0][0].raw)
		cNm.Invoke(t, stackitem.Null{}, "newEpoch", 1)
		for i := 1; i < len(nodes); i++ {
			sn := newStorageNode(t, cNm)
			nodes[i] = append(nodes[i-1], sn)
			cNm.Invoke(t, stackitem.Null{}, "addPeerIR", sn.raw)
			cNm.Invoke(t, stackitem.Null{}, "newEpoch", i+1)
		}
		return nodes
	}

	t.Run("increase size, extend with nil", func(t *testing.T) {
		// Before: S-old .. S
		// After : S-old .. S nil nil ...
		const epochCount = netmap.DefaultSnapshotCount / 2

		cNm := newNetmapInvoker(t)
		nodes := prepare(t, cNm, epochCount)

		const newCount = netmap.DefaultSnapshotCount + 3
		cNm.Invoke(t, stackitem.Null{}, "updateSnapshotCount", newCount)

		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[epochCount-1])
		for i := range epochCount {
			checkSnapshotAt(t, i, cNm, nodes[epochCount-i-1])
		}
		for i := epochCount; i < newCount; i++ {
			checkSnapshotAt(t, i, cNm, nil)
		}
		_, err = cNm.TestInvoke(t, "snapshot", int64(newCount))
		require.Error(t, err)
	})
	t.Run("increase size, copy old snapshots", func(t *testing.T) {
		// Before: S-x .. S S-old ...
		// After : S-x .. S nil nil S-old ...
		const epochCount = netmap.DefaultSnapshotCount + netmap.DefaultSnapshotCount/2

		cNm := newNetmapInvoker(t)
		nodes := prepare(t, cNm, epochCount)

		const newCount = netmap.DefaultSnapshotCount + 3
		cNm.Invoke(t, stackitem.Null{}, "updateSnapshotCount", newCount)

		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[epochCount-1])
		for i := range newCount - 3 {
			checkSnapshotAt(t, i, cNm, nodes[epochCount-i-1])
		}
		for i := newCount - 3; i < newCount; i++ {
			checkSnapshotAt(t, i, cNm, nil)
		}
		_, err = cNm.TestInvoke(t, "snapshot", int64(newCount))
		require.Error(t, err)
	})
	t.Run("decrease size, small decrease", func(t *testing.T) {
		// Before: S-x .. S S-old ... ...
		// After : S-x .. S S-new ...
		const epochCount = netmap.DefaultSnapshotCount + netmap.DefaultSnapshotCount/2

		cNm := newNetmapInvoker(t)
		nodes := prepare(t, cNm, epochCount)

		const newCount = netmap.DefaultSnapshotCount/2 + 2
		cNm.Invoke(t, stackitem.Null{}, "updateSnapshotCount", newCount)

		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[epochCount-1])
		for i := range newCount {
			checkSnapshotAt(t, i, cNm, nodes[epochCount-i-1])
		}
		_, err = cNm.TestInvoke(t, "snapshot", int64(newCount))
		require.Error(t, err)
	})
	t.Run("decrease size, big decrease", func(t *testing.T) {
		// Before: S-x   ... ... S S-old ... ...
		// After : S-new ...  S
		const epochCount = netmap.DefaultSnapshotCount + netmap.DefaultSnapshotCount/2

		cNm := newNetmapInvoker(t)
		nodes := prepare(t, cNm, epochCount)

		const newCount = netmap.DefaultSnapshotCount/2 - 2
		cNm.Invoke(t, stackitem.Null{}, "updateSnapshotCount", newCount)

		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[epochCount-1])
		for i := range newCount {
			checkSnapshotAt(t, i, cNm, nodes[epochCount-i-1])
		}
		_, err = cNm.TestInvoke(t, "snapshot", int64(newCount))
		require.Error(t, err)
	})
}

func checkSnapshotAt(t *testing.T, epoch int, cNm *neotest.ContractInvoker, nodes []testNodeInfo) {
	s, err := cNm.TestInvoke(t, "snapshot", int64(epoch))
	require.NoError(t, err)
	require.Equal(t, 1, s.Len())
	checkSnapshot(t, s, nodes)
}

func checkSnapshot(t *testing.T, s *vm.Stack, nodes []testNodeInfo) {
	arr, ok := s.Pop().Value().([]stackitem.Item)
	require.True(t, ok, "expected array")
	require.Equal(t, len(nodes), len(arr), "expected %d nodes", len(nodes))

	actual := make([]netmap.Node, len(nodes))
	expected := make([]netmap.Node, len(nodes))
	for i := range nodes {
		n, ok := arr[i].Value().([]stackitem.Item)
		require.True(t, ok, "expected node struct")
		require.Equalf(t, 2, len(n), "expected %d field(s)", 2)

		require.IsType(t, []byte{}, n[0].Value())

		state, err := n[1].TryInteger()
		require.NoError(t, err)

		actual[i].BLOB = n[0].Value().([]byte)
		actual[i].State = nodestate.Type(state.Int64())
		expected[i].BLOB = nodes[i].raw
		expected[i].State = nodes[i].state
	}

	require.ElementsMatch(t, expected, actual, "snapshot is different")
}

func TestUpdateStateIR(t *testing.T) {
	cNm := newNetmapInvoker(t)

	acc := cNm.NewAccount(t)
	pub := acc.(neotest.SingleSigner).Account().PrivateKey().PublicKey().Bytes()

	t.Run("can't move online, need addPeerIR", func(t *testing.T) {
		cNm.InvokeFail(t, "peer is missing", "updateStateIR", int64(nodestate.Online), pub)
	})

	dummyInfo := dummyNodeInfo(acc)
	cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo.raw)

	acc1 := cNm.NewAccount(t)
	dummyInfo1 := dummyNodeInfo(acc1)
	cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo1.raw)

	t.Run("must be signed by the alphabet", func(t *testing.T) {
		cAcc := cNm.WithSigners(acc)
		cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "updateStateIR", int64(nodestate.Offline), pub)
	})
	t.Run("invalid state", func(t *testing.T) {
		cNm.InvokeFail(t, "unsupported state", "updateStateIR", int64(42), pub)
	})

	checkNetmapCandidates(t, cNm, 2)

	// Move the first node offline.
	cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(nodestate.Offline), pub)
	checkNetmapCandidates(t, cNm, 1)

	checkState := func(expected nodestate.Type) {
		arr := checkNetmapCandidates(t, cNm, 1)
		nn := arr[0].Value().([]stackitem.Item)
		state, err := nn[1].TryInteger()
		require.NoError(t, err)
		require.Equal(t, int64(expected), state.Int64())
	}

	// Move the second node in the maintenance state.
	pub1 := acc1.(neotest.SingleSigner).Account().PrivateKey().PublicKey().Bytes()
	t.Run("maintenance -> add peer", func(t *testing.T) {
		cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(nodestate.Maintenance), pub1)
		checkState(nodestate.Maintenance)
		cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo1.raw)
		checkState(nodestate.Online)
	})
	t.Run("maintenance -> online", func(t *testing.T) {
		cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(nodestate.Maintenance), pub1)
		checkState(nodestate.Maintenance)
		cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(nodestate.Online), pub1)
		checkState(nodestate.Online)
	})
}

func TestUpdateState(t *testing.T) {
	cNm := newNetmapInvoker(t)

	accs := []neotest.Signer{cNm.NewAccount(t), cNm.NewAccount(t)}
	pubs := make([][]byte, len(accs))
	for i := range accs {
		dummyInfo := dummyNodeInfo(accs[i])
		cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo.raw)
		pubs[i] = accs[i].(neotest.SingleSigner).Account().PrivateKey().PublicKey().Bytes()
	}

	t.Run("missing witness", func(t *testing.T) {
		cAcc := cNm.WithSigners(accs[0])
		cNm.InvokeFail(t, common.ErrWitnessFailed, "updateState", int64(2), pubs[0])
		cAcc.InvokeFail(t, common.ErrWitnessFailed, "updateState", int64(2), pubs[1])
	})

	checkNetmapCandidates(t, cNm, 2)

	cBoth := cNm.WithSigners(accs[0], cNm.Committee)

	cBoth.Invoke(t, stackitem.Null{}, "updateState", int64(2), pubs[0])
	checkNetmapCandidates(t, cNm, 1)

	t.Run("remove already removed node", func(t *testing.T) {
		cBoth.Invoke(t, stackitem.Null{}, "updateState", int64(2), pubs[0])
		checkNetmapCandidates(t, cNm, 1)
	})

	cBoth = cNm.WithSigners(accs[1], cNm.Committee)
	cBoth.Invoke(t, stackitem.Null{}, "updateState", int64(2), pubs[1])
	checkNetmapCandidates(t, cNm, 0)
}

func checkNetmapCandidates(t *testing.T, c *neotest.ContractInvoker, size int) []stackitem.Item {
	s, err := c.TestInvoke(t, "netmapCandidates")
	require.NoError(t, err)
	require.Equal(t, 1, s.Len())

	arr, ok := s.Pop().Value().([]stackitem.Item)
	require.True(t, ok)
	require.Equal(t, size, len(arr))
	return arr
}

func TestInnerRing(t *testing.T) {
	e := newExecutor(t)

	ir := make(keys.PublicKeys, 2)
	for i := range ir {
		k, err := keys.NewPrivateKey()
		require.NoError(t, err)
		ir[i] = k.PublicKey()
	}

	deployDefaultNNS(t, e)
	deployNetmapContract(t, e)

	SetInnerRing(t, e, ir)
	require.ElementsMatch(t, ir, InnerRing(t, e))
}

func TestAddNode(t *testing.T) {
	var (
		c = newNetmapInvoker(t)

		acc  = c.NewAccount(t)
		pKey = (acc.(neotest.SingleSigner)).Account().PrivateKey().PublicKey()

		nodeStruct = stackitem.NewStruct([]stackitem.Item{
			stackitem.NewArray([]stackitem.Item{stackitem.Make("grpcs://192.0.2.100:8090")}),
			stackitem.NewMapWithValue([]stackitem.MapElement{
				{Key: stackitem.Make("key"), Value: stackitem.Make("value")},
				{Key: stackitem.Make("Capacity"), Value: stackitem.Make("100500")},
			}),
			stackitem.NewByteArray(pKey.Bytes()),
			stackitem.Make(nodestate.Online),
		})
	)

	candidateStruct, err := nodeStruct.Clone()
	require.NoError(t, err)
	candidateStruct.Append(stackitem.Make(0))

	acc1 := c.NewAccount(t)
	cAcc1 := c.WithSigners(acc1)
	cAcc1.InvokeFail(t, common.ErrWitnessFailed, "addNode", nodeStruct)

	c.InvokeFail(t, common.ErrWitnessFailed, "addNode", nodeStruct)

	var cAcc = new(neotest.ContractInvoker)
	*cAcc = *c
	cAcc.Signers = append(cAcc.Signers, acc)

	badStruct, err := nodeStruct.Clone()
	require.NoError(t, err)
	badStruct.Remove(3) // state
	badStruct.Append(stackitem.Make(nodestate.Offline))

	c.InvokeFail(t, "can't add non-online node", "addNode", badStruct)

	badStruct.Remove(3) // state
	badStruct.Remove(2) // key
	badStruct.Append(stackitem.Make(pKey.GetScriptHash()))
	badStruct.Append(stackitem.Make(nodestate.Online))

	c.InvokeFail(t, "incorrect public key", "addNode", badStruct)

	h := cAcc.Invoke(t, stackitem.Null{}, "addNode", nodeStruct)
	aer := cAcc.CheckHalt(t, h)
	require.Equal(t, 1, len(aer.Events))
	require.Equal(t, "AddNode", aer.Events[0].Name)
	require.Equal(t, 3, aer.Events[0].Item.Len())

	// Check addNode doesn't affect current node list.
	var checkZeroList = func(method string, params ...any) {
		s, err := c.TestInvoke(t, method, params...)
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())

		iter, ok := s.Top().Value().(*storage.Iterator)
		require.True(t, ok)
		require.False(t, iter.Next()) // Empty list.
	}
	checkZeroList("listNodes")
	// But it's a part of the candidate list.
	var checkNodeList = func(method string, params ...any) {
		s, err := c.TestInvoke(t, method, params...)
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())

		iter, ok := s.Top().Value().(*storage.Iterator)
		require.True(t, ok)
		actual := make([]stackitem.Item, 0, 1)
		for iter.Next() {
			actual = append(actual, iter.Value())
		}
		if method == "listCandidates" {
			require.ElementsMatch(t, []stackitem.Item{candidateStruct}, actual)
		} else {
			require.ElementsMatch(t, []stackitem.Item{nodeStruct}, actual)
		}
	}
	checkNodeList("listCandidates")

	h = cAcc.Invoke(t, stackitem.Null{}, "updateState", int(nodestate.Maintenance), pKey.Bytes())
	aer = cAcc.CheckHalt(t, h)
	require.Equal(t, 1, len(aer.Events))
	require.Equal(t, "UpdateStateSuccess", aer.Events[0].Name)
	require.Equal(t, 2, aer.Events[0].Item.Len())

	h = cAcc.Invoke(t, stackitem.Null{}, "updateState", int(nodestate.Online), pKey.Bytes())
	aer = cAcc.CheckHalt(t, h)
	require.Equal(t, 1, len(aer.Events))
	require.Equal(t, "UpdateStateSuccess", aer.Events[0].Name)
	require.Equal(t, 2, aer.Events[0].Item.Len())

	// Tick epoch.
	_ = c.Invoke(t, stackitem.Null{}, "newEpoch", 1)

	// New node is added to the netmap.
	checkNodeList("listNodes")

	// Check epoch 0 contents, it still doesn't have any nodes.
	checkZeroList("listNodes", 0)

	// Incorrect deleteNode call.
	cAcc.InvokeFail(t, "incorrect public key", "deleteNode", pKey.Bytes()[:2])

	// Drop the node.
	h = cAcc.Invoke(t, stackitem.Null{}, "deleteNode", pKey.Bytes())
	aer = cAcc.CheckHalt(t, h)
	require.Equal(t, 1, len(aer.Events))
	require.Equal(t, "UpdateStateSuccess", aer.Events[0].Name)
	require.Equal(t, 2, aer.Events[0].Item.Len())

	// Still a part of the map.
	checkNodeList("listNodes")
	// But not on the candidate list
	checkZeroList("listCandidates")

	// Tick epoch.
	_ = c.Invoke(t, stackitem.Null{}, "newEpoch", 2)

	// Current map is empty.
	checkZeroList("listNodes")
	// But some historic data available.
	checkNodeList("listNodes", 1)

	for i := range netmap.DefaultSnapshotCount - 1 {
		_ = c.Invoke(t, stackitem.Null{}, "newEpoch", i+3)
	}
	// Current map is still empty.
	checkZeroList("listNodes")
	// And historic data is gone.
	checkZeroList("listNodes", 1)

	// We're at epoch 11, add node again
	_ = cAcc.Invoke(t, stackitem.Null{}, "addNode", nodeStruct)
	_ = c.Invoke(t, stackitem.Null{}, "newEpoch", 12)
	candidateStruct.Remove(4)
	candidateStruct.Append(stackitem.Make(11))
	checkNodeList("listNodes") // Added.

	_ = c.Invoke(t, stackitem.Null{}, "newEpoch", 13)
	_ = c.Invoke(t, stackitem.Null{}, "newEpoch", 14)
	checkNodeList("listNodes") // +2 epochs, still here

	// Update state at epoch 14.
	_ = cAcc.Invoke(t, stackitem.Null{}, "updateState", int(nodestate.Online), pKey.Bytes())

	_ = c.Invoke(t, stackitem.Null{}, "newEpoch", 15)
	candidateStruct.Remove(4)
	candidateStruct.Append(stackitem.Make(14))
	checkNodeList("listNodes") // Not gone

	for i := 16; i < 16+3; i++ {
		_ = c.Invoke(t, stackitem.Null{}, "newEpoch", i)
	}
	// Cleaned up as stale.
	checkZeroList("listCandidates")
	checkZeroList("listNodes")
}

func TestListConfig(t *testing.T) {
	var c = newNetmapInvoker(t, "key", "value", "some", "setting")

	s, err := c.TestInvoke(t, "listConfig")
	require.NoError(t, err)
	require.Equal(t, 1, s.Len())

	arr, ok := s.Pop().Item().(*stackitem.Array)
	require.True(t, ok)
	require.Equal(t, stackitem.NewArray([]stackitem.Item{
		stackitem.NewStruct([]stackitem.Item{stackitem.Make("UseNodeV2"), stackitem.Make([]byte{1})}),
		stackitem.NewStruct([]stackitem.Item{stackitem.Make("key"), stackitem.Make("value")}),
		stackitem.NewStruct([]stackitem.Item{stackitem.Make("some"), stackitem.Make("setting")}),
	}), arr)
}

func TestCleanupThreshold(t *testing.T) {
	var c = newNetmapInvoker(t)

	s, err := c.TestInvoke(t, "cleanupThreshold")
	require.NoError(t, err)
	require.Equal(t, 1, s.Len())
	require.Equal(t, stackitem.Make(3), s.Pop().Item())

	c.InvokeFail(t, "negative value", "setCleanupThreshold", -1)

	_ = c.Invoke(t, stackitem.Null{}, "setCleanupThreshold", 10)

	s, err = c.TestInvoke(t, "cleanupThreshold")
	require.NoError(t, err)
	require.Equal(t, 1, s.Len())
	require.Equal(t, stackitem.Make(10), s.Pop().Item())
}

func TestGetEpochBlock(t *testing.T) {
	netmapContract := newNetmapInvoker(t)

	assert := func(epoch, exp int) {
		stk, err := netmapContract.TestInvoke(t, "getEpochBlock", epoch)
		require.NoError(t, err)

		items := stk.ToArray()
		require.Len(t, items, 1)

		i, err := items[0].TryInteger()
		require.NoError(t, err)
		require.True(t, i.IsUint64())
		require.EqualValues(t, exp, i.Uint64())
	}

	const firstEpoch = 123
	firstEpochHeight := int(netmapContract.Chain.BlockHeight())

	assert(firstEpoch-1, 0)
	assert(firstEpoch, 0)
	assert(firstEpoch+1, 0)
	assert(firstEpoch+2, 0)

	netmapContract.Invoke(t, stackitem.Null{}, "newEpoch", firstEpoch)

	assert(firstEpoch-1, 0)
	assert(firstEpoch, firstEpochHeight)
	assert(firstEpoch+1, 0)
	assert(firstEpoch+2, 0)

	const secondEpochAfter = 13
	for range secondEpochAfter {
		netmapContract.AddNewBlock(t)
	}

	netmapContract.Invoke(t, stackitem.Null{}, "newEpoch", firstEpoch+1)

	assert(firstEpoch-1, 0)
	assert(firstEpoch, firstEpochHeight)
	assert(firstEpoch+1, firstEpochHeight+1+secondEpochAfter)
	assert(firstEpoch+2, 0)
}

func TestLastEpochBlock(t *testing.T) {
	netmapContract := newNetmapInvoker(t)

	assert := func(exp uint32) {
		stk, err := netmapContract.TestInvoke(t, "lastEpochBlock")
		require.NoError(t, err)

		items := stk.ToArray()
		require.Len(t, items, 1)

		i, err := items[0].TryInteger()
		require.NoError(t, err)
		require.True(t, i.IsUint64())
		require.EqualValues(t, exp, i.Uint64())
	}

	assert(0)

	firstEpochBlock := netmapContract.Chain.BlockHeight()
	netmapContract.Invoke(t, stackitem.Null{}, "newEpoch", 123)

	assert(firstEpochBlock)

	for range 10 {
		netmapContract.AddNewBlock(t)
	}

	secondEpochBlock := netmapContract.Chain.BlockHeight()
	netmapContract.Invoke(t, stackitem.Null{}, "newEpoch", 123+1)

	assert(secondEpochBlock)
}

func TestGetEpochTime(t *testing.T) {
	netmapContract := newNetmapInvoker(t)

	assert := func(epoch, exp uint64) {
		stk, err := netmapContract.TestInvoke(t, "getEpochTime", epoch)
		require.NoError(t, err)

		items := stk.ToArray()
		require.Len(t, items, 1)

		i, err := items[0].TryInteger()
		require.NoError(t, err)
		require.True(t, i.IsUint64())
		require.EqualValues(t, exp, i.Uint64())
	}

	const firstEpoch = 123

	assert(firstEpoch-1, 0)
	assert(firstEpoch, 0)
	assert(firstEpoch+1, 0)
	assert(firstEpoch+2, 0)

	netmapContract.Invoke(t, stackitem.Null{}, "newEpoch", firstEpoch)
	firstEpochBlock := netmapContract.GetBlockByIndex(t, netmapContract.Chain.BlockHeight())

	assert(firstEpoch-1, 0)
	assert(firstEpoch, firstEpochBlock.Timestamp)
	assert(firstEpoch+1, 0)
	assert(firstEpoch+2, 0)

	for range 10 {
		netmapContract.AddNewBlock(t)
	}

	netmapContract.Invoke(t, stackitem.Null{}, "newEpoch", firstEpoch+1)
	secondEpochBlock := netmapContract.GetBlockByIndex(t, netmapContract.Chain.BlockHeight())

	assert(firstEpoch-1, 0)
	assert(firstEpoch, firstEpochBlock.Timestamp)
	assert(firstEpoch+1, secondEpochBlock.Timestamp)
	assert(firstEpoch+2, 0)
}

func TestLastEpochTime(t *testing.T) {
	netmapContract := newNetmapInvoker(t)

	assert := func(exp uint64) {
		stk, err := netmapContract.TestInvoke(t, "lastEpochTime")
		require.NoError(t, err)

		items := stk.ToArray()
		require.Len(t, items, 1)

		i, err := items[0].TryInteger()
		require.NoError(t, err)
		require.True(t, i.IsUint64())
		require.EqualValues(t, exp, i.Uint64())
	}

	assert(0)

	netmapContract.Invoke(t, stackitem.Null{}, "newEpoch", 123)
	firstEpochBlock := netmapContract.GetBlockByIndex(t, netmapContract.Chain.BlockHeight())

	assert(firstEpochBlock.Timestamp)

	for range 10 {
		netmapContract.AddNewBlock(t)
	}

	netmapContract.Invoke(t, stackitem.Null{}, "newEpoch", 123+1)
	secondEpochBlock := netmapContract.GetBlockByIndex(t, netmapContract.Chain.BlockHeight())

	assert(secondEpochBlock.Timestamp)
}
