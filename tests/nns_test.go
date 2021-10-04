package tests

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/nspcc-dev/neofs-contract/nns"
	"github.com/stretchr/testify/require"
)

const nnsPath = "../nns"

func TestNNSGeneric(t *testing.T) {
	bc := NewChain(t)
	h := DeployContract(t, bc, nnsPath, nil)

	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "symbol")
	CheckTestInvoke(t, bc, tx, "NNS")

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "decimals")
	CheckTestInvoke(t, bc, tx, 0)

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "totalSupply")
	CheckTestInvoke(t, bc, tx, 0)
}

func TestNNSAddRoot(t *testing.T) {
	bc := NewChain(t)
	h := DeployContract(t, bc, nnsPath, nil)

	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "addRoot", "0com")
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "invalid root format")

	acc := NewAccount(t, bc)
	tx = PrepareInvoke(t, bc, acc, h, "addRoot", "com")
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "not witnessed by committee")

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "addRoot", "com")
	AddBlock(t, bc, tx)
	CheckHalt(t, bc, tx.Hash())

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "addRoot", "com")
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "root already exists")
}

func TestNNSRegister(t *testing.T) {
	bc := NewChain(t)
	h := DeployContract(t, bc, nnsPath, nil)

	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "addRoot", "com")
	AddBlockCheckHalt(t, bc, tx)

	acc := NewAccount(t, bc)
	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	tx = PrepareInvoke(t, bc, []*wallet.Account{CommitteeAcc, acc}, h, "register",
		"testdomain.com", acc.Contract.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)
	b := AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, acc, h, "getRecords", "testdomain.com", int64(nns.SOA))
	CheckTestInvoke(t, bc, tx, stackitem.NewArray([]stackitem.Item{stackitem.NewBuffer(
		[]byte(fmt.Sprintf("testdomain.com myemail@nspcc.ru %d %d %d %d %d",
			b.Timestamp, refresh, retry, expire, ttl)))}))

	tx = PrepareInvoke(t, bc, acc, h, "addRecord",
		"testdomain.com", int64(nns.TXT), "first TXT record")
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, acc, h, "addRecord",
		"testdomain.com", int64(nns.TXT), "second TXT record")
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, acc, h, "getRecords", "testdomain.com", int64(nns.TXT))
	CheckTestInvoke(t, bc, tx, stackitem.NewArray([]stackitem.Item{
		stackitem.NewByteArray([]byte("first TXT record")),
		stackitem.NewByteArray([]byte("second TXT record"))}))

	tx = PrepareInvoke(t, bc, acc, h, "setRecord",
		"testdomain.com", int64(nns.TXT), int64(0), "replaced first")
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, acc, h, "getRecords", "testdomain.com", int64(nns.TXT))
	CheckTestInvoke(t, bc, tx, stackitem.NewArray([]stackitem.Item{
		stackitem.NewByteArray([]byte("replaced first")),
		stackitem.NewByteArray([]byte("second TXT record"))}))
}

func TestNNSUpdateSOA(t *testing.T) {
	bc := NewChain(t)
	h := DeployContract(t, bc, nnsPath, nil)

	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "addRoot", "com")
	AddBlockCheckHalt(t, bc, tx)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "register",
		"testdomain.com", CommitteeAcc.Contract.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)
	AddBlockCheckHalt(t, bc, tx)

	refresh *= 2
	retry *= 2
	expire *= 2
	ttl *= 2
	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "updateSOA",
		"testdomain.com", "newemail@nspcc.ru", refresh, retry, expire, ttl)
	b := AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "getRecords", "testdomain.com", int64(nns.SOA))
	CheckTestInvoke(t, bc, tx, stackitem.NewArray([]stackitem.Item{stackitem.NewBuffer(
		[]byte(fmt.Sprintf("testdomain.com newemail@nspcc.ru %d %d %d %d %d",
			b.Timestamp, refresh, retry, expire, ttl)))}))
}

func TestNNSGetAllRecords(t *testing.T) {
	bc := NewChain(t)
	h := DeployContract(t, bc, nnsPath, nil)

	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "addRoot", "com")
	AddBlockCheckHalt(t, bc, tx)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "register",
		"testdomain.com", CommitteeAcc.Contract.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "addRecord",
		"testdomain.com", int64(nns.TXT), "first TXT record")
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "addRecord",
		"testdomain.com", int64(nns.A), "1.2.3.4")
	b := AddBlockCheckHalt(t, bc, tx)
	expSOA := fmt.Sprintf("testdomain.com myemail@nspcc.ru %d %d %d %d %d",
		b.Timestamp, refresh, retry, expire, ttl)

	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "getAllRecords", "testdomain.com")
	v, err := TestInvoke(bc, tx)
	require.NoError(t, err)

	iter := v.Estack().Pop().Value().(*storage.Iterator)
	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(nns.A)),
		stackitem.Make("1.2.3.4"), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(nns.SOA)),
		stackitem.NewBuffer([]byte(expSOA)), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(nns.TXT)),
		stackitem.Make("first TXT record"), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.False(t, iter.Next())
}

func TestNNSSetAdmin(t *testing.T) {
	bc := NewChain(t)
	h := DeployContract(t, bc, nnsPath, nil)

	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "addRoot", "com")
	AddBlockCheckHalt(t, bc, tx)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	tx = PrepareInvoke(t, bc, CommitteeAcc, h, "register",
		"testdomain.com", CommitteeAcc.Contract.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)
	AddBlockCheckHalt(t, bc, tx)

	acc := NewAccount(t, bc)

	tx = PrepareInvoke(t, bc, acc, h, "addRecord",
		"testdomain.com", int64(nns.TXT), "won't be added")
	AddBlock(t, bc, tx)
	CheckFault(t, bc, tx.Hash(), "not witnessed by admin")

	tx = PrepareInvoke(t, bc, []*wallet.Account{CommitteeAcc, acc}, h, "setAdmin",
		"testdomain.com", acc.Contract.ScriptHash())
	AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, acc, h, "addRecord",
		"testdomain.com", int64(nns.TXT), "will be added")
	AddBlockCheckHalt(t, bc, tx)
}

func TestNNSRenew(t *testing.T) {
	bc := NewChain(t)
	h := DeployContract(t, bc, nnsPath, nil)

	tx := PrepareInvoke(t, bc, CommitteeAcc, h, "addRoot", "com")
	AddBlockCheckHalt(t, bc, tx)

	acc := NewAccount(t, bc)
	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	tx = PrepareInvoke(t, bc, []*wallet.Account{CommitteeAcc, acc}, h, "register",
		"testdomain.com", acc.Contract.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)
	b := AddBlockCheckHalt(t, bc, tx)

	tx = PrepareInvoke(t, bc, acc, h, "renew", "testdomain.com")
	AddBlockCheckHalt(t, bc, tx)

	const msPerYear = 365 * 24 * time.Hour / time.Millisecond

	tx = PrepareInvoke(t, bc, acc, h, "properties", "testdomain.com")
	CheckTestInvoke(t, bc, tx, stackitem.NewMapWithValue([]stackitem.MapElement{
		{stackitem.Make("name"), stackitem.Make("testdomain.com")},
		{stackitem.Make("expiration"), stackitem.Make(b.Timestamp + 2*uint64(msPerYear))},
	}))
}
