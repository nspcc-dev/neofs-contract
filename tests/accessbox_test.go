package tests

import (
	"bytes"
	"path"
	"sort"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/accessbox/accessboxconst"
	"github.com/stretchr/testify/require"
)

const accessBoxPath = "../contracts/accessbox"

func deployAccessBoxContract(t *testing.T, e *neotest.Executor, netmap, balance *util.Uint160) util.Uint160 {
	c := neotest.CompileFile(t, e.Validator.ScriptHash(), accessBoxPath,
		path.Join(accessBoxPath, "config.yml"))
	e.DeployContract(t, c, []any{*netmap, *balance})
	return c.Hash
}

func newAccessBoxInvoker(t *testing.T) (*neotest.Executor, *neotest.ContractInvoker) {
	e, accessBox, _ := newAccessBoxEnvironment(t, accessboxconst.FeeConfigKey, int64(0))
	return e, accessBox
}

func newAccessBoxEnvironment(t *testing.T, netmapConfig ...any) (*neotest.Executor, *neotest.ContractInvoker, *neotest.ContractInvoker) {
	bc, acc := chain.NewSingle(t)
	e := neotest.NewExecutor(t, bc, acc, acc)
	_ = deployDefaultNNS(t, e)
	netmap := deployNetmapContract(t, e, netmapConfig...)
	balance := deployBalanceContract(t, e)
	h := deployAccessBoxContract(t, e, &netmap, &balance)
	return e, e.CommitteeInvoker(h), e.CommitteeInvoker(balance)
}

func TestAccessBoxPutAndRead(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	payload := []byte{0, 1, 2, 3, 4}
	metadata := []byte("public metadata")
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	c.InvokeFail(t, common.ErrOwnerWitnessFailed, "put", owner.ScriptHash(), 1,
		"team-a", expiresAt, metadata, payload)
	id := putAccessBox(t, ownerC, owner.ScriptHash(), 1, "team-a", expiresAt, metadata, payload)

	c.Invoke(t, stackitem.NewByteArray(payload), "get", id)
	c.Invoke(t, stackitem.Null{}, "get", accessBoxID(2))

	info := getAccessBoxRecord(t, c, id)
	require.Equal(t, id, itemBytes(t, info[0]))
	require.Equal(t, owner.ScriptHash().BytesBE(), itemBytes(t, info[1]))
	require.Equal(t, "team-a", itemString(t, info[2]))
	require.EqualValues(t, 1, itemInt(t, info[3]))
	require.Positive(t, itemInt(t, info[4]))
	require.LessOrEqual(t, itemInt(t, info[4]), int64(c.TopBlock(t).Timestamp))
	require.EqualValues(t, expiresAt, itemInt(t, info[5]))
	require.Equal(t, metadata, itemBytes(t, info[6]))

	info = getAccessBoxRecord(t, c, accessBoxID(2))
	require.True(t, accessBoxRecordIsEmpty(info))

	id2 := putAccessBox(t, ownerC, owner.ScriptHash(), 1, "team-a", expiresAt, metadata, payload)
	require.NotEqual(t, id, id2)
}

func TestAccessBoxList(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	otherOwner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	otherOwnerC := c.WithSigners(otherOwner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	id1 := putAccessBox(t, ownerC, owner.ScriptHash(), 2, "", expiresAt, nil, []byte{2})
	id2 := putAccessBox(t, ownerC, owner.ScriptHash(), 1, "team-a", expiresAt, nil, []byte{1})
	_ = putAccessBox(t, otherOwnerC, otherOwner.ScriptHash(), 1, "", expiresAt, nil, []byte{3})

	allIDs := [][]byte{id1, id2}
	sort.Slice(allIDs, func(i, j int) bool { return bytes.Compare(allIDs[i], allIDs[j]) < 0 })
	require.Equal(t, allIDs, listAccessBoxIDs(t, c, owner.ScriptHash(), 0, nil, 10))
	require.Equal(t, [][]byte{id2}, listAccessBoxIDs(t, c, owner.ScriptHash(), 1, nil, 10))
	require.Equal(t, [][]byte{id1}, listAccessBoxIDs(t, c, owner.ScriptHash(), 2, nil, 10))
	require.Equal(t, [][]byte{allIDs[1]}, listAccessBoxIDs(t, c, owner.ScriptHash(), 0, allIDs[0], 10))
	require.Equal(t, [][]byte{allIDs[0]}, listAccessBoxIDs(t, c, owner.ScriptHash(), 0, nil, 1))
}

func TestAccessBoxRevoke(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	other := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	id := putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte{1})
	c.WithSigners(other).InvokeFail(t, common.ErrOwnerWitnessFailed, "revoke", id)
	ownerC.Invoke(t, stackitem.Null{}, "revoke", id)
	c.Invoke(t, stackitem.Null{}, "get", id)
}

func TestAccessBoxRevokers(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	revoker1 := c.NewAccount(t)
	revoker2 := c.NewAccount(t)
	other := c.NewAccount(t)

	c.WithSigners(other).InvokeFail(t, "", "addRevoker", revoker1.ScriptHash())
	c.Invoke(t, stackitem.Null{}, "addRevoker", revoker1.ScriptHash())
	c.Invoke(t, stackitem.NewBool(true), "isRevoker", revoker1.ScriptHash())
	c.Invoke(t, stackitem.Null{}, "addRevoker", revoker1.ScriptHash())

	c.Invoke(t, stackitem.Null{}, "addRevoker", revoker2.ScriptHash())
	require.ElementsMatch(t, [][]byte{revoker1.ScriptHash().BytesBE(), revoker2.ScriptHash().BytesBE()},
		listAccessBoxRevokers(t, c))

	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000
	id := putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte{1})
	c.WithSigners(revoker1).Invoke(t, stackitem.Null{}, "revoke", id)
	c.Invoke(t, stackitem.Null{}, "get", id)

	c.WithSigners(other).InvokeFail(t, "", "removeRevoker", revoker1.ScriptHash())
	c.Invoke(t, stackitem.Null{}, "removeRevoker", revoker1.ScriptHash())
	c.Invoke(t, stackitem.NewBool(false), "isRevoker", revoker1.ScriptHash())
	c.Invoke(t, stackitem.Null{}, "removeRevoker", revoker1.ScriptHash())
	require.Equal(t, [][]byte{revoker2.ScriptHash().BytesBE()}, listAccessBoxRevokers(t, c))

	id = putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte{2})
	c.WithSigners(revoker1).InvokeFail(t, common.ErrOwnerWitnessFailed, "revoke", id)
	ownerC.Invoke(t, stackitem.Null{}, "revoke", id)
}

func TestAccessBoxRevokerLimit(t *testing.T) {
	_, c := newAccessBoxInvoker(t)

	for range 100 {
		c.Invoke(t, stackitem.Null{}, "addRevoker", c.NewAccount(t).ScriptHash())
	}
	require.Len(t, listAccessBoxRevokers(t, c), 100)

	c.InvokeFail(t, "too many access box revokers", "addRevoker", c.NewAccount(t).ScriptHash())
	require.Len(t, listAccessBoxRevokers(t, c), 100)
}

func TestAccessBoxRevokeOwnerByKind(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	revoker := c.NewAccount(t)
	other := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	revokerC := c.WithSigners(revoker)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	c.Invoke(t, stackitem.Null{}, "addRevoker", revoker.ScriptHash())
	putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte("s3-1"))
	sessionID := putAccessBox(t, ownerC, owner.ScriptHash(), 2, "", expiresAt, nil, []byte("session"))
	putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte("s3-2"))
	patID := putAccessBox(t, ownerC, owner.ScriptHash(), 3, "", expiresAt, nil, []byte("pat"))

	s3IDs := listAccessBoxIDs(t, c, owner.ScriptHash(), 1, nil, 10)
	require.Len(t, s3IDs, 2)
	c.WithSigners(other).InvokeFail(t, common.ErrOwnerWitnessFailed,
		"revokeOwner", owner.ScriptHash(), 1, nil, 1)
	revokerC.Invoke(t, stackitem.NewBuffer(s3IDs[0]), "revokeOwner", owner.ScriptHash(), 1, nil, 1)
	c.Invoke(t, stackitem.Null{}, "get", s3IDs[0])
	require.False(t, accessBoxRecordIsEmpty(getAccessBoxRecord(t, c, s3IDs[1])))
	c.Invoke(t, stackitem.NewByteArray([]byte("session")), "get", sessionID)
	c.Invoke(t, stackitem.NewByteArray([]byte("pat")), "get", patID)

	revokerC.Invoke(t, stackitem.Null{}, "revokeOwner", owner.ScriptHash(), 1, s3IDs[0], 1)
	c.Invoke(t, stackitem.Null{}, "get", s3IDs[1])

	ownerC.Invoke(t, stackitem.Null{}, "revokeOwner", owner.ScriptHash(), 0, nil, 10)
	c.Invoke(t, stackitem.Null{}, "get", sessionID)
	c.Invoke(t, stackitem.Null{}, "get", patID)
}

func TestAccessBoxExpiryAndPruning(t *testing.T) {
	e, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 1_000

	id := putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte{1})

	b := c.NewUnsignedBlock(t)
	b.Timestamp = uint64(expiresAt + 1)
	require.NoError(t, e.Chain.AddBlock(c.SignBlock(b)))

	c.Invoke(t, stackitem.Null{}, "get", id)
	require.NotEmpty(t, getAccessBoxRecord(t, c, id))
	c.Invoke(t, stackitem.Null{}, "pruneExpired", nil, 10)
	require.True(t, accessBoxRecordIsEmpty(getAccessBoxRecord(t, c, id)))
}

func TestAccessBoxPruneExpiredPaging(t *testing.T) {
	e, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 1_000

	ids := make([][]byte, 3)
	for i := range ids {
		ids[i] = putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte{byte(i)})
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i], ids[j]) < 0 })

	b := c.NewUnsignedBlock(t)
	b.Timestamp = uint64(expiresAt + 1)
	require.NoError(t, e.Chain.AddBlock(c.SignBlock(b)))

	cursor := pruneExpiredAccessBoxes(t, c, nil, 1)
	require.Equal(t, ids[0], cursor)
	require.True(t, accessBoxRecordIsEmpty(getAccessBoxRecord(t, c, ids[0])))
	require.False(t, accessBoxRecordIsEmpty(getAccessBoxRecord(t, c, ids[1])))
	require.False(t, accessBoxRecordIsEmpty(getAccessBoxRecord(t, c, ids[2])))
	require.Equal(t, ids[1:], listAccessBoxIDs(t, c, owner.ScriptHash(), 1, nil, 10))

	cursor = pruneExpiredAccessBoxes(t, c, cursor, 1)
	require.Equal(t, ids[1], cursor)
	require.True(t, accessBoxRecordIsEmpty(getAccessBoxRecord(t, c, ids[1])))
	require.False(t, accessBoxRecordIsEmpty(getAccessBoxRecord(t, c, ids[2])))
	require.Equal(t, ids[2:], listAccessBoxIDs(t, c, owner.ScriptHash(), 1, nil, 10))

	cursor = pruneExpiredAccessBoxes(t, c, cursor, 1)
	require.Nil(t, cursor)
	require.True(t, accessBoxRecordIsEmpty(getAccessBoxRecord(t, c, ids[2])))
	require.Empty(t, listAccessBoxIDs(t, c, owner.ScriptHash(), 1, nil, 10))
}

func TestAccessBoxValidation(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	c.Invoke(t, stackitem.Null{}, "get", []byte{1})
	ownerC.InvokeFail(t, "invalid access box id", "revoke", []byte{1})
	ownerC.InvokeFail(t, "invalid access box kind", "put", owner.ScriptHash(), 0,
		"", expiresAt, nil, []byte{1})
	ownerC.InvokeFail(t, "invalid access box kind", "revokeOwner", owner.ScriptHash(), 4, nil, 1)
	ownerC.InvokeFail(t, "invalid payload size", "put", owner.ScriptHash(), 1,
		"", expiresAt, nil, nil)
	expiresAt = int(c.TopBlock(t).Timestamp) + 365*24*60*60*1000 + 60_000
	ownerC.InvokeFail(t, "access box lifetime exceeds one year", "put", owner.ScriptHash(), 1,
		"", expiresAt, nil, []byte{1})
}

func TestAccessBoxFee(t *testing.T) {
	const fee = int64(17)

	bc, validator, committee := chain.NewMultiWithCustomConfig(t, func(c *config.Blockchain) {
		c.Hardforks = nil
	})
	e := neotest.NewExecutor(t, bc, validator, committee)
	transferSomeGasToAccount(t, e, e.Committee)
	_ = deployDefaultNNS(t, e)
	netmap := deployNetmapContract(t, e, accessboxconst.FeeConfigKey, fee)
	balance := deployBalanceContract(t, e)
	accessBox := deployAccessBoxContract(t, e, &netmap, &balance)
	accessBoxC := e.CommitteeInvoker(accessBox)
	balanceC := e.NewInvoker(balance, alphaSigner(t, e))

	owner := accessBoxC.NewAccount(t)
	ownerC := accessBoxC.WithSigners(owner)
	expiresAt := int(accessBoxC.TopBlock(t).Timestamp) + 60_000
	alphabet := getAlphabetAccs(t, e)
	totalFee := fee * int64(len(alphabet))

	ownerC.InvokeFail(t, "insufficient balance to create access box", "put", owner.ScriptHash(), 1,
		"", expiresAt, nil, []byte{1})

	balanceMint(t, balanceC, owner.ScriptHash(), totalFee-1, nil)
	ownerC.InvokeFail(t, "insufficient balance to create access box", "put", owner.ScriptHash(), 1,
		"", expiresAt, nil, []byte{1})

	balanceMint(t, balanceC, owner.ScriptHash(), 1, nil)
	_ = putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte{1})
	balanceC.Invoke(t, int64(0), "balanceOf", owner.ScriptHash())
	for i := range alphabet {
		balanceC.Invoke(t, fee, "balanceOf", alphabet[i].ScriptHash())
	}
}

func TestAccessBoxMissingFeeIsFree(t *testing.T) {
	_, accessBox, balance := newAccessBoxEnvironment(t)
	owner := accessBox.NewAccount(t)
	ownerC := accessBox.WithSigners(owner)
	expiresAt := int(accessBox.TopBlock(t).Timestamp) + 60_000

	_ = putAccessBox(t, ownerC, owner.ScriptHash(), 1, "", expiresAt, nil, []byte{1})
	balance.Invoke(t, int64(0), "balanceOf", owner.ScriptHash())
}

func putAccessBox(t *testing.T, c *neotest.ContractInvoker, owner util.Uint160, kind int, namespace string, expiresAt int, metadata, payload []byte) []byte {
	t.Helper()

	var id []byte
	c.InvokeAndCheck(t, func(t testing.TB, stack []stackitem.Item) {
		require.Len(t, stack, 1)
		id = itemBytes(t, stack[0])
	}, "put", owner, kind, namespace, expiresAt, metadata, payload)
	require.Len(t, id, 16)

	return id
}

func pruneExpiredAccessBoxes(t *testing.T, c *neotest.ContractInvoker, cursor []byte, limit int) []byte {
	t.Helper()

	var next []byte
	c.InvokeAndCheck(t, func(t testing.TB, stack []stackitem.Item) {
		require.Len(t, stack, 1)
		if _, ok := stack[0].(stackitem.Null); !ok {
			next = itemBytes(t, stack[0])
		}
	}, "pruneExpired", cursor, limit)

	return next
}

func getAccessBoxRecord(t *testing.T, c *neotest.ContractInvoker, id []byte) []stackitem.Item {
	t.Helper()
	res, err := c.TestInvoke(t, "getInfo", id)
	require.NoError(t, err)
	return res.Pop().Array()
}

func listAccessBoxIDs(t *testing.T, c *neotest.ContractInvoker, owner util.Uint160, kind int, cursor []byte, limit int) [][]byte {
	t.Helper()
	res, err := c.TestInvoke(t, "list", owner, kind, cursor, limit)
	require.NoError(t, err)

	items := stackItemToArray(res.Pop().Item())
	ids := make([][]byte, len(items))
	for i := range items {
		ids[i] = itemBytes(t, stackItemToArray(items[i])[0])
	}
	return ids
}

func listAccessBoxRevokers(t *testing.T, c *neotest.ContractInvoker) [][]byte {
	t.Helper()
	res, err := c.TestInvoke(t, "listRevokers")
	require.NoError(t, err)

	items := iteratorToArray(res.Pop().Value().(*storage.Iterator))
	revokers := make([][]byte, len(items))
	for i := range items {
		revokers[i] = itemBytes(t, items[i])
	}
	return revokers
}

func accessBoxID(n byte) []byte {
	id := make([]byte, 16)
	id[len(id)-1] = n
	return id
}

func itemBytes(t testing.TB, item stackitem.Item) []byte {
	t.Helper()
	b, err := item.TryBytes()
	require.NoError(t, err)
	return b
}

func itemString(t testing.TB, item stackitem.Item) string {
	t.Helper()
	s, err := item.TryBytes()
	require.NoError(t, err)
	return string(s)
}

func itemInt(t testing.TB, item stackitem.Item) int64 {
	t.Helper()
	i, err := item.TryInteger()
	require.NoError(t, err)
	return i.Int64()
}

func accessBoxRecordIsEmpty(record []stackitem.Item) bool {
	_, ok := record[0].(stackitem.Null)
	return ok
}
