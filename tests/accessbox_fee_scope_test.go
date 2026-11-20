package tests

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neofs-contract/contracts/accessbox/accessboxconst"
	"github.com/stretchr/testify/require"
)

func TestAccessBoxFeeOwnerWitnessScope(t *testing.T) {
	const fee = int64(1)

	bc, validator, committee := chain.NewMultiWithCustomConfig(t, func(c *config.Blockchain) {
		c.Hardforks = nil
	})
	e := neotest.NewExecutor(t, bc, validator, committee)
	transferSomeGasToAccount(t, e, e.Committee)
	_ = deployDefaultNNS(t, e)
	deployNetmapContract(t, e, accessboxconst.FeeConfigKey, fee)
	balance := deployBalanceContract(t, e)
	accessBox := deployAccessBoxContract(t, e)
	accessBoxC := e.CommitteeInvoker(accessBox)
	balanceC := e.NewInvoker(balance, alphaSigner(t, e))

	owner := accessBoxC.NewAccount(t)
	totalFee := fee * int64(len(getAlphabetAccs(t, e)))
	balanceMint(t, balanceC, owner.ScriptHash(), totalFee, nil)
	expiresAt := int(accessBoxC.TopBlock(t).Timestamp) + 60_000

	putTx := func(signer transaction.Signer) *transaction.Transaction {
		tx := accessBoxC.PrepareInvokeNoSign(t, "put", owner.ScriptHash(), "s3",
			expiresAt, accessBoxKeysArgs([][]byte{[]byte("a")}), []byte{1})
		tx.Signers = []transaction.Signer{signer}
		neotest.AddNetworkFee(t, e.Chain, tx, owner)
		e.AddSystemFee(tx, -1)
		require.NoError(t, owner.SignTx(e.Chain.GetConfig().Magic, tx))
		return tx
	}

	t.Run("CalledByEntry only", func(t *testing.T) {
		tx := putTx(transaction.Signer{
			Account: owner.ScriptHash(),
			Scopes:  transaction.CalledByEntry,
		})
		e.AddNewBlock(t, tx)
		e.CheckFault(t, tx.Hash(), "failed to transfer access box fee")
		balanceC.Invoke(t, totalFee, "balanceOf", owner.ScriptHash())
	})

	t.Run("allow Access Box to call Balance", func(t *testing.T) {
		tx := putTx(transaction.Signer{
			Account: owner.ScriptHash(),
			Scopes:  transaction.CalledByEntry | transaction.Rules,
			Rules: []transaction.WitnessRule{{
				Action: transaction.WitnessAllow,
				Condition: &transaction.ConditionAnd{
					(*transaction.ConditionCalledByContract)(&accessBox),
					(*transaction.ConditionScriptHash)(&balance),
				},
			}},
		})
		e.AddNewBlock(t, tx)
		e.CheckHalt(t, tx.Hash())
		balanceC.Invoke(t, int64(0), "balanceOf", owner.ScriptHash())
	})
}
