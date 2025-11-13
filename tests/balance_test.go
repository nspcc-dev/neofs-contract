package tests

import (
	"bytes"
	"crypto/elliptic"
	"math/big"
	"path"
	"slices"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/balance/balanceconst"
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
	"github.com/stretchr/testify/require"
)

const (
	balancePath = "../contracts/balance"

	basicIncomeRate = 1_0000_0000
)

func deployBalanceContract(t *testing.T, e *neotest.Executor, addrNetmap, addrContainer util.Uint160) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))

	args := make([]any, 3)
	args[0] = false
	args[1] = addrNetmap
	args[2] = addrContainer

	e.DeployContract(t, c, args)
	regContractNNS(t, e, "balance", c.Hash)
	return c.Hash
}

func newBalanceInvoker(t *testing.T) (*neotest.ContractInvoker, *neotest.ContractInvoker, *neotest.ContractInvoker) {
	e := newExecutor(t)

	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))

	nnsHash := deployDefaultNNS(t, e)
	deployNetmapContract(t, e, containerconst.RegistrationFeeKey, int64(containerFee),
		containerconst.AliasFeeKey, int64(containerAliasFee), containerconst.EpochDurationKey, int64(epochDuration),
		balanceconst.BasicIncomeRateKey, int64(basicIncomeRate))
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	deployProxyContract(t, e)
	deployContainerContract(t, e, &ctrNetmap.Hash, &ctrBalance.Hash, &nnsHash)

	return e.CommitteeInvoker(ctrBalance.Hash), e.CommitteeInvoker(ctrContainer.Hash), e.CommitteeInvoker(ctrNetmap.Hash)
}

func TestPayments(t *testing.T) {
	var currEpoch int64 = 123

	balI, cntI, netmapI := newBalanceInvoker(t)
	nodes := []testNodeInfo{
		newStorageNode(t, cntI),
		newStorageNode(t, cntI),
		newStorageNode(t, cntI),
	}
	netmapI.Invoke(t, stackitem.Null{}, "newEpoch", currEpoch)

	slices.SortFunc(nodes, func(a, b testNodeInfo) int {
		keyA, err := keys.NewPublicKeyFromBytes(a.pub, elliptic.P256())
		require.NoError(t, err)
		keyB, err := keys.NewPublicKeyFromBytes(b.pub, elliptic.P256())
		require.NoError(t, err)

		return bytes.Compare(keyA.GetScriptHash().BytesBE(), keyB.GetScriptHash().BytesBE())
	})

	nodesKeys := make([]any, 0, len(nodes))
	for _, node := range nodes {
		nodesKeys = append(nodesKeys, node.pub)
	}

	owner := cntI.NewAccount(t)
	neofsOwnerSH := owner.ScriptHash()
	cnt := dummyContainer(owner)
	balanceMint(t, balI, owner, containerFee*1, []byte{})
	cntI.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token)
	cntI.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cnt.id[:], 0, nodesKeys)
	cntI.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cnt.id[:], []uint8{uint8(len(nodesKeys))})

	t.Run("invalid container ID", func(t *testing.T) {
		balI.InvokeFail(t, "invalid container id", "settleContainerPayment", []byte{1, 2, 3, 4})
	})

	t.Run("non-alphabet witness", func(t *testing.T) {
		balI.WithSigners(cntI.NewAccount(t)).InvokeFail(t, common.ErrAlphabetWitnessFailed, "settleContainerPayment", cnt.id[:])
	})

	t.Run("successful payments", func(t *testing.T) {
		putReports := func(size int64) {
			var reportTxs []*transaction.Transaction
			for _, node := range nodes {
				reportTxs = append(reportTxs, cntI.WithSigners(node.signer).PrepareInvoke(t, "putReport", cnt.id[:], size, 455, node.pub))
			}

			b := balI.NewUnsignedBlock(t, reportTxs...)
			balI.SignBlock(b)
			require.NoError(t, balI.Chain.AddBlock(b))
			for _, tx := range reportTxs {
				balI.CheckHalt(t, tx.Hash(), stackitem.Make(nil))
			}
		}
		trueEpochTick := func() {
			newEpochBlockTXs := []*transaction.Transaction{netmapI.PrepareInvoke(t, "newEpoch", currEpoch)}

			b := balI.NewUnsignedBlock(t, newEpochBlockTXs...)
			b.Timestamp = balI.TopBlock(t).Timestamp + epochDuration*1000
			balI.SignBlock(b)
			require.NoError(t, balI.Chain.AddBlock(b))
			for _, tx := range newEpochBlockTXs {
				balI.CheckHalt(t, tx.Hash(), stackitem.Make(nil))
			}
		}
		fillTestValues := func(gbsInEveryNode, startBalance int64) {
			// ensure fresh reports
			for range 3 {
				currEpoch++
				trueEpochTick()
			}

			balanceBurnEverything(t, balI, owner, []byte{})
			balanceMint(t, balI, owner, startBalance, []byte{})
			require.Equal(t, startBalance, balance(t, balI, owner))

			putReports(gbsInEveryNode << 30)
		}

		t.Run("no info for prev epoch", func(t *testing.T) {
			var (
				gbsInEveryNode int64 = 2
				shouldPay            = int64(0)
			)
			fillTestValues(gbsInEveryNode, shouldPay)

			txH := balI.Invoke(t, stackitem.Make(true), "settleContainerPayment", cnt.id[:])
			res := balI.GetTxExecResult(t, txH)
			require.Len(t, res.Events, 1)
			assertNotificationEvent(t, res.Events[0], "Payment", neofsOwnerSH[:], cnt.id[:], big.NewInt(currEpoch), big.NewInt(shouldPay))

			require.Zero(t, balance(t, balI, owner))
		})

		t.Run("payment for prev epoch", func(t *testing.T) {
			var (
				gbsInEveryNode int64 = 10
				shouldPay            = gbsInEveryNode * int64(len(nodes)*basicIncomeRate)
			)
			fillTestValues(gbsInEveryNode, shouldPay)

			currEpoch++
			trueEpochTick()
			putReports(gbsInEveryNode << 30)
			currEpoch++
			trueEpochTick()
			putReports(1)

			txH := balI.Invoke(t, stackitem.Make(true), "settleContainerPayment", cnt.id[:])
			res := balI.GetTxExecResult(t, txH)
			// (Transfer, TransferX) pair for every transfer + Payment itself
			require.Len(t, res.Events, 2*len(nodes)+1)
			assertNotificationEvent(t, res.Events[len(res.Events)-1], "Payment", neofsOwnerSH[:], cnt.id[:], big.NewInt(currEpoch), big.NewInt(shouldPay))

			require.Zero(t, balance(t, balI, owner))
		})

		t.Run("no info for more that 2 epoches", func(t *testing.T) {
			var (
				gbsInEveryNode int64 = 1
				shouldPay            = gbsInEveryNode * int64(len(nodes)*basicIncomeRate)
			)
			fillTestValues(gbsInEveryNode, shouldPay)

			currEpoch++
			currEpoch++
			currEpoch++
			trueEpochTick()

			txH := balI.Invoke(t, stackitem.Make(true), "settleContainerPayment", cnt.id[:])
			res := balI.GetTxExecResult(t, txH)
			// (Transfer, TransferX) pair for every transfer + Payment itself
			require.Len(t, res.Events, 2*len(nodes)+1)
			assertNotificationEvent(t, res.Events[len(res.Events)-1], "Payment", neofsOwnerSH[:], cnt.id[:], big.NewInt(currEpoch), big.NewInt(shouldPay))

			require.Zero(t, balance(t, balI, owner))
		})
	})

	t.Run("mark unpaid", func(t *testing.T) {
		const gbsInEveryNode = 5
		var shouldPay = int64(gbsInEveryNode * len(nodes) * basicIncomeRate)

		balanceBurnEverything(t, balI, owner, []byte{})
		balanceMint(t, balI, owner, shouldPay-1, []byte{})
		require.Equal(t, shouldPay-1, balance(t, balI, owner))

		currEpoch++
		netmapI.Invoke(t, stackitem.Null{}, "newEpoch", currEpoch)

		for _, node := range nodes {
			cntI.WithSigners(node.signer).Invoke(t, stackitem.Null{}, "putReport",
				cnt.id[:], gbsInEveryNode<<30, 455, node.pub,
			)
		}

		currEpoch++
		currEpoch++
		netmapI.Invoke(t, stackitem.Null{}, "newEpoch", currEpoch)

		txH := balI.Invoke(t, stackitem.Make(false), "settleContainerPayment", cnt.id[:])
		res := balI.GetTxExecResult(t, txH)
		// (Transfer, TransferX) pair for every transfer except the last one + marking unpaid
		require.Len(t, res.Events, 2*(len(nodes)-1)+1)
		assertNotificationEvent(t, res.Events[len(res.Events)-1], "ChangePaymentStatus", cnt.id[:], big.NewInt(currEpoch), true)

		stack, err := balI.TestInvoke(t, "getUnpaidContainerEpoch", cnt.id[:])
		require.NoError(t, err)
		require.Equal(t, 1, stack.Len())
		require.Equal(t, currEpoch, stack.Pop().BigInt().Int64())

		stack, err = balI.TestInvoke(t, "iterateUnpaid")
		require.NoError(t, err)
		require.Equal(t, 1, stack.Len())
		unpaidContainers := iteratorToArray(stack.Pop().Value().(*storage.Iterator))
		require.Len(t, unpaidContainers, 1)
		kv := unpaidContainers[0].Value().([]stackitem.Item)
		cID, err := kv[0].TryBytes()
		require.NoError(t, err)
		require.Equal(t, cnt.id[:], cID)
		unpaidEpochBig, err := kv[1].TryInteger()
		require.NoError(t, err)
		require.Equal(t, currEpoch, unpaidEpochBig.Int64())

		// make balance sufficient and pay one more time

		balanceBurnEverything(t, balI, owner, []byte{})
		balanceMint(t, balI, owner, shouldPay, []byte{})
		require.Equal(t, shouldPay, balance(t, balI, owner))

		txH = balI.Invoke(t, stackitem.Make(true), "settleContainerPayment", cnt.id[:])
		res = balI.GetTxExecResult(t, txH)
		// (Transfer, TransferX) pair for every transfer + marking paid and payment itself
		require.Len(t, res.Events, 2*(len(nodes))+2)
		assertNotificationEvent(t, res.Events[len(res.Events)-2], "ChangePaymentStatus", cnt.id[:], big.NewInt(currEpoch), false)
		assertNotificationEvent(t, res.Events[len(res.Events)-1], "Payment", neofsOwnerSH[:], cnt.id[:], big.NewInt(currEpoch), big.NewInt(shouldPay))

		stack, err = balI.TestInvoke(t, "getUnpaidContainerEpoch", cnt.id[:])
		require.NoError(t, err)
		require.Equal(t, 1, stack.Len())
		require.Equal(t, int64(-1), stack.Pop().BigInt().Int64())

		stack, err = balI.TestInvoke(t, "iterateUnpaid")
		require.NoError(t, err)
		require.Equal(t, 1, stack.Len())
		unpaidContainers = iteratorToArray(stack.Pop().Value().(*storage.Iterator))
		require.Empty(t, unpaidContainers)
	})
}

func balance(t *testing.T, c *neotest.ContractInvoker, acc neotest.Signer) int64 {
	stack, err := c.TestInvoke(t, "balanceOf", acc.ScriptHash())
	require.NoError(t, err)
	require.Equal(t, 1, stack.Len())
	return stack.Pop().BigInt().Int64()
}

func balanceBurnEverything(t *testing.T, c *neotest.ContractInvoker, acc neotest.Signer, details []byte) {
	stack, err := c.TestInvoke(t, "balanceOf", acc.ScriptHash())
	require.NoError(t, err)

	balance := stack.Pop().BigInt().Int64()
	if balance > 0 {
		c.Invoke(t, stackitem.Null{}, "burn", acc.ScriptHash(), balance, details)
	}
}

func balanceMint(t *testing.T, c *neotest.ContractInvoker, acc neotest.Signer, amount int64, details []byte) {
	c.Invoke(t, stackitem.Null{}, "mint", acc.ScriptHash(), amount, details)
}

func TestBalanceLifecycle(t *testing.T) {
	e := newExecutor(t)

	deployDefaultNNS(t, e)
	nHash := deployNetmapContract(t, e)
	bHash := deployBalanceContract(t, e, util.Uint160{}, util.Uint160{})

	c := e.CommitteeInvoker(bHash)
	acc1 := c.NewAccount(t)
	acc2 := c.NewAccount(t)

	checkTotalSupply := func(expected int64) {
		stack, err := c.TestInvoke(t, "totalSupply")
		require.NoError(t, err)
		require.Equal(t, 1, stack.Len())
		bal := stack.Pop().BigInt().Int64()
		require.Equal(t, expected, bal)
	}

	checkBalance := func(acc util.Uint160, expected int64) {
		stack, err := c.TestInvoke(t, "balanceOf", acc)
		require.NoError(t, err)
		require.Equal(t, 1, stack.Len())
		bal := stack.Pop().BigInt().Int64()
		require.Equal(t, expected, bal)
	}

	c.InvokeFail(t, "negative amount", "mint", acc1.ScriptHash(), -100500, []byte("extra"))

	c.Invoke(t, stackitem.Null{}, "mint", acc1.ScriptHash(), 100500, []byte("extra"))
	checkBalance(acc1.ScriptHash(), 100500)
	checkTotalSupply(100500)

	// Fails because acc1 is not a signer.
	c.Invoke(t, false, "transfer", acc1.ScriptHash(), acc2.ScriptHash(), 500, nil)

	c1 := c.WithSigners(acc1)

	// Negative amount, an exception instead of return value as per NEP-17.
	c1.InvokeFail(t, "negative amount", "transfer", acc1.ScriptHash(), acc2.ScriptHash(), -500, nil)

	c1.Invoke(t, true, "transfer", acc1.ScriptHash(), acc2.ScriptHash(), 500, nil)
	checkBalance(acc2.ScriptHash(), 500)
	checkBalance(acc1.ScriptHash(), 100000)
	checkTotalSupply(100500)

	// But c1 can't mint.
	c1.InvokeFail(t, "alphabet witness check failed", "mint", acc1.ScriptHash(), 100500, []byte("extra"))

	// c1 can't burn.
	c1.InvokeFail(t, "alphabet witness check failed", "burn", acc1.ScriptHash(), 1, []byte("extra"))

	// Validators can burn.
	c.Invoke(t, stackitem.Null{}, "burn", acc1.ScriptHash(), 1, []byte("extra"))
	checkBalance(acc1.ScriptHash(), 99999)
	checkTotalSupply(100499)

	c.Invoke(t, stackitem.Null{}, "lock", []byte("details"), acc1.ScriptHash(), util.Uint160{1, 2, 3}, 999, 1)
	checkBalance(util.Uint160{1, 2, 3}, 999)
	checkBalance(acc1.ScriptHash(), 99000)
	checkTotalSupply(100499)

	nc := e.CommitteeInvoker(nHash)
	nc.Invoke(t, stackitem.Null{}, "newEpoch", 1)
	// Unlocked.
	checkBalance(acc1.ScriptHash(), 99999)
	checkBalance(util.Uint160{1, 2, 3}, 0)
	checkTotalSupply(100499)
}
