package tests

import (
	"math/rand"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/stretchr/testify/require"
)

const netmapPath = "../netmap"

func deployNetmapContract(t *testing.T, bc *core.Blockchain, addrBalance, addrContainer util.Uint160, config ...interface{}) util.Uint160 {
	args := make([]interface{}, 5)
	args[0] = false
	args[1] = addrBalance
	args[2] = addrContainer
	args[3] = []interface{}{CommitteeAcc.PrivateKey().PublicKey().Bytes()}
	args[4] = append([]interface{}{}, config...)
	return DeployContract(t, bc, netmapPath, args)
}

func prepareNetmapContract(t *testing.T, bc *core.Blockchain) util.Uint160 {
	addrNNS := DeployContract(t, bc, nnsPath, nil)

	ctrNetmap, err := ContractInfo(CommitteeAcc.Contract.ScriptHash(), netmapPath)
	require.NoError(t, err)

	ctrBalance, err := ContractInfo(CommitteeAcc.Contract.ScriptHash(), balancePath)
	require.NoError(t, err)

	ctrContainer, err := ContractInfo(CommitteeAcc.Contract.ScriptHash(), containerPath)
	require.NoError(t, err)

	deployContainerContract(t, bc, ctrNetmap.Hash, ctrBalance.Hash, addrNNS)
	deployBalanceContract(t, bc, ctrNetmap.Hash, ctrContainer.Hash)
	return deployNetmapContract(t, bc, ctrBalance.Hash, ctrContainer.Hash, "ContainerFee", []byte{})
}

func dummyNodeInfo(acc *wallet.Account) []byte {
	ni := make([]byte, 66)
	rand.Read(ni)
	copy(ni[2:], acc.PrivateKey().PublicKey().Bytes())
	return ni
}

func TestAddPeer(t *testing.T) {
	bc := NewChain(t)
	h := prepareNetmapContract(t, bc)

	acc := NewAccount(t, bc)
	dummyInfo := dummyNodeInfo(acc)

	acc1 := NewAccount(t, bc)
	tx := PrepareInvoke(t, bc, acc1, h, "addPeer", dummyInfo)
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "witness check failed")

	tx = PrepareInvoke(t, bc, acc, h, "addPeer", dummyInfo)
	AddBlock(t, bc, tx)

	aer := CheckHalt(t, bc, tx.Hash())
	require.Equal(t, 1, len(aer.Events))
	require.Equal(t, "AddPeer", aer.Events[0].Name)
	require.Equal(t, stackitem.NewArray([]stackitem.Item{stackitem.NewByteArray(dummyInfo)}),
		aer.Events[0].Item)

	dummyInfo[0] ^= 0xFF
	tx = PrepareInvoke(t, bc, acc, h, "addPeer", dummyInfo)
	AddBlock(t, bc, tx)

	aer = CheckHalt(t, bc, tx.Hash())
	require.Equal(t, 1, len(aer.Events))
	require.Equal(t, "AddPeer", aer.Events[0].Name)
	require.Equal(t, stackitem.NewArray([]stackitem.Item{stackitem.NewByteArray(dummyInfo)}),
		aer.Events[0].Item)
}
