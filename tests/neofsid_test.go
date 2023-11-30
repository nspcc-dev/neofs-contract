package tests

import (
	"bytes"
	"path"
	"sort"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

const neofsidPath = "../contracts/neofsid"

func deployNeoFSIDContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	args := make([]any, 5)
	args[0] = false

	c := neotest.CompileFile(t, e.CommitteeHash, neofsidPath, path.Join(neofsidPath, "config.yml"))
	e.DeployContract(t, c, args)
	regContractNNS(t, e, "neofsid", c.Hash)
	return c.Hash
}

func newNeoFSIDInvoker(t *testing.T) *neotest.ContractInvoker {
	e := newExecutor(t)

	_ = deployDefaultNNS(t, e)
	h := deployNeoFSIDContract(t, e)
	return e.CommitteeInvoker(h)
}

func TestNeoFSID_AddKey(t *testing.T) {
	e := newNeoFSIDInvoker(t)

	pubs := make([][]byte, 6)
	for i := range pubs {
		p, err := keys.NewPrivateKey()
		require.NoError(t, err)
		pubs[i] = p.PublicKey().Bytes()
	}
	acc := e.NewAccount(t)
	owner, _ := base58.Decode(address.Uint160ToString(acc.ScriptHash()))
	e.Invoke(t, stackitem.Null{}, "addKey", owner,
		[]any{pubs[0], pubs[1]})

	sort.Slice(pubs[:2], func(i, j int) bool {
		return bytes.Compare(pubs[i], pubs[j]) == -1
	})
	arr := []stackitem.Item{
		stackitem.NewBuffer(pubs[0]),
		stackitem.NewBuffer(pubs[1]),
	}
	e.Invoke(t, stackitem.NewArray(arr), "key", owner)

	t.Run("multiple addKey per block", func(t *testing.T) {
		tx1 := e.PrepareInvoke(t, "addKey", owner, []any{pubs[2]})
		tx2 := e.PrepareInvoke(t, "addKey", owner, []any{pubs[3], pubs[4]})
		e.AddNewBlock(t, tx1, tx2)
		e.CheckHalt(t, tx1.Hash(), stackitem.Null{})
		e.CheckHalt(t, tx2.Hash(), stackitem.Null{})

		sort.Slice(pubs[:5], func(i, j int) bool {
			return bytes.Compare(pubs[i], pubs[j]) == -1
		})
		arr = []stackitem.Item{
			stackitem.NewBuffer(pubs[0]),
			stackitem.NewBuffer(pubs[1]),
			stackitem.NewBuffer(pubs[2]),
			stackitem.NewBuffer(pubs[3]),
			stackitem.NewBuffer(pubs[4]),
		}
		e.Invoke(t, stackitem.NewArray(arr), "key", owner)
	})

	e.Invoke(t, stackitem.Null{}, "removeKey", owner,
		[]any{pubs[1], pubs[5]})
	arr = []stackitem.Item{
		stackitem.NewBuffer(pubs[0]),
		stackitem.NewBuffer(pubs[2]),
		stackitem.NewBuffer(pubs[3]),
		stackitem.NewBuffer(pubs[4]),
	}
	e.Invoke(t, stackitem.NewArray(arr), "key", owner)

	t.Run("multiple removeKey per block", func(t *testing.T) {
		tx1 := e.PrepareInvoke(t, "removeKey", owner, []any{pubs[2]})
		tx2 := e.PrepareInvoke(t, "removeKey", owner, []any{pubs[0], pubs[4]})
		e.AddNewBlock(t, tx1, tx2)
		e.CheckHalt(t, tx1.Hash(), stackitem.Null{})
		e.CheckHalt(t, tx2.Hash(), stackitem.Null{})

		arr = []stackitem.Item{stackitem.NewBuffer(pubs[3])}
		e.Invoke(t, stackitem.NewArray(arr), "key", owner)
	})
}
