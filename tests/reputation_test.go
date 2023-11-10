package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

const reputationPath = "../reputation"

func deployReputationContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, reputationPath,
		path.Join(reputationPath, "config.yml"))

	args := make([]any, 1)
	args[0] = false

	e.DeployContract(t, c, args)
	return c.Hash
}

func newReputationInvoker(t *testing.T) *neotest.ContractInvoker {
	bc, acc := chain.NewSingle(t)
	e := neotest.NewExecutor(t, bc, acc, acc)
	h := deployReputationContract(t, e)
	return e.CommitteeInvoker(h)
}

func TestReputation_Put(t *testing.T) {
	e := newReputationInvoker(t)

	peerID := []byte{1, 2, 3}
	e.Invoke(t, stackitem.Null{}, "put", int64(1), peerID, []byte{4})

	t.Run("concurrent invocations", func(t *testing.T) {
		repValue1 := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		repValue2 := []byte{10, 20, 30, 40, 50, 60, 70, 80}
		tx1 := e.PrepareInvoke(t, "put", int64(1), peerID, repValue1)
		tx2 := e.PrepareInvoke(t, "put", int64(1), peerID, repValue2)

		e.AddNewBlock(t, tx1, tx2)
		e.CheckHalt(t, tx1.Hash(), stackitem.Null{})
		e.CheckHalt(t, tx2.Hash(), stackitem.Null{})

		t.Run("get all", func(t *testing.T) {
			result := stackitem.NewArray([]stackitem.Item{
				stackitem.NewBuffer([]byte{4}),
				stackitem.NewBuffer(repValue1),
				stackitem.NewBuffer(repValue2),
			})
			e.Invoke(t, result, "get", int64(1), peerID)
		})
	})
}

func TestReputation_ListByEpoch(t *testing.T) {
	e := newReputationInvoker(t)

	peerIDs := []string{"peer1", "peer2"}
	e.Invoke(t, stackitem.Null{}, "put", int64(1), peerIDs[0], []byte{1})
	e.Invoke(t, stackitem.Null{}, "put", int64(1), peerIDs[0], []byte{2})
	e.Invoke(t, stackitem.Null{}, "put", int64(2), peerIDs[1], []byte{3})
	e.Invoke(t, stackitem.Null{}, "put", int64(2), peerIDs[0], []byte{4})
	e.Invoke(t, stackitem.Null{}, "put", int64(2), peerIDs[1], []byte{5})

	result := stackitem.NewArray([]stackitem.Item{
		stackitem.NewBuffer(append([]byte{1}, peerIDs[0]...)),
	})
	e.Invoke(t, result, "listByEpoch", int64(1))

	result = stackitem.NewArray([]stackitem.Item{
		stackitem.NewBuffer(append([]byte{2}, peerIDs[0]...)),
		stackitem.NewBuffer(append([]byte{2}, peerIDs[1]...)),
	})
	e.Invoke(t, result, "listByEpoch", int64(2))
}
