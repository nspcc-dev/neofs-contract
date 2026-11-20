package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	contractcli "github.com/nspcc-dev/neo-go/cli/smartcontract"
	"github.com/nspcc-dev/neo-go/pkg/compiler"
	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/manifest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/accessbox/accessboxconst"
	"github.com/stretchr/testify/require"
)

const accessBoxPath = "../contracts/accessbox"

func deployAccessBoxContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	c := neotest.CompileFile(t, e.Validator.ScriptHash(), accessBoxPath,
		path.Join(accessBoxPath, "config.yml"))
	e.DeployContract(t, c, nil)
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
	deployNetmapContract(t, e, netmapConfig...)
	balance := deployBalanceContract(t, e)
	h := deployAccessBoxContract(t, e)
	return e, e.CommitteeInvoker(h), e.CommitteeInvoker(balance)
}

// compilePreviousAccessBoxContract builds a test-only predecessor whose Update
// method passes the immediately preceding contract version to the new code.
func compilePreviousAccessBoxContract(t *testing.T, sender util.Uint160) *neotest.Contract {
	t.Helper()

	sourcePath, err := filepath.Abs(path.Join(accessBoxPath, "contract.go"))
	require.NoError(t, err)
	source, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	oldCall := []byte("common.AppendVersion(data)")
	require.Equal(t, 1, bytes.Count(source, oldCall))
	source = bytes.Replace(source, oldCall, []byte("append(data.([]any), common.Version-1)"), 1)

	config, err := contractcli.ParseContractConfig(path.Join(accessBoxPath, "config.yml"))
	require.NoError(t, err)
	options := &compiler.Options{
		Name:                       config.Name,
		ContractEvents:             config.Events,
		DeclaredNamedTypes:         config.NamedTypes,
		ContractSupportedStandards: config.SupportedStandards,
		SafeMethods:                config.SafeMethods,
		Overloads:                  config.Overloads,
		SourceURL:                  config.SourceURL,
		Permissions:                make([]manifest.Permission, len(config.Permissions)),
	}
	for i := range config.Permissions {
		options.Permissions[i] = manifest.Permission(config.Permissions[i])
	}
	nef, debugInfo, err := compiler.CompileWithOptions(sourcePath, bytes.NewReader(source), options)
	require.NoError(t, err)
	oldManifest, err := compiler.CreateManifest(debugInfo, options)
	require.NoError(t, err)

	return &neotest.Contract{
		Hash:     state.CreateContractHash(sender, nef.Checksum, oldManifest.Name),
		NEF:      nef,
		Manifest: oldManifest,
	}
}

func TestAccessBoxPutAndRead(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	keys := [][]byte{[]byte("gateway-a"), []byte("gateway-b")}
	value := []byte{0, 1, 2}
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	c.InvokeFail(t, common.ErrOwnerWitnessFailed, "put", owner.ScriptHash(), "s3",
		expiresAt, accessBoxKeysArgs(keys), value)
	id := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt, keys, value)

	c.Invoke(t, stackitem.NewByteArray(value), "get", id, keys[0])
	c.Invoke(t, stackitem.NewByteArray(value), "get", id, keys[1])
	c.Invoke(t, stackitem.Null{}, "get", id, []byte("missing"))
	c.Invoke(t, stackitem.Null{}, "get", accessBoxID(2), keys[0])

	info := getAccessBoxRecord(t, c, id)
	require.Equal(t, id, itemBytes(t, info[0]))
	require.Equal(t, owner.ScriptHash().BytesBE(), itemBytes(t, info[1]))
	require.Equal(t, "s3", itemString(t, info[2]))
	require.Positive(t, itemInt(t, info[3]))
	require.LessOrEqual(t, itemInt(t, info[3]), int64(c.TopBlock(t).Timestamp))
	require.EqualValues(t, expiresAt, itemInt(t, info[4]))
	require.EqualValues(t, len(keys), itemInt(t, info[5]))
	require.EqualValues(t, len(keys)*len(value), itemInt(t, info[6]))

	c.Invoke(t, stackitem.Null{}, "getInfo", accessBoxID(2))

	id2 := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt, keys[:1], value)
	require.NotEqual(t, id, id2)
}

func TestAccessBoxMultiplePutsInOneTransaction(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000
	keys := [][]byte{[]byte("gateway-a"), []byte("gateway-b")}
	values := [][]byte{[]byte("first"), []byte("second")}

	scriptBuilder := smartcontract.NewBuilder()
	for i := range keys {
		scriptBuilder.InvokeMethod(c.Hash, "put", owner.ScriptHash(), "s3", expiresAt,
			accessBoxKeysArgs([][]byte{keys[i]}), values[i])
	}
	script, err := scriptBuilder.Script()
	require.NoError(t, err)

	tx := ownerC.InvokeScript(t, script, ownerC.Signers)
	result := c.GetTxExecResult(t, tx)
	require.Len(t, result.Events, len(keys))

	ids := make([][]byte, len(result.Events))
	for i := range result.Events {
		require.Equal(t, c.Hash, result.Events[i].ScriptHash)
		items := result.Events[i].Item.Value().([]stackitem.Item)
		require.Len(t, items, 1)
		ids[i] = itemBytes(t, items[0])
		assertNotificationEvent(t, result.Events[i], "AccessBoxCreated", ids[i])
		require.Len(t, ids[i], 16)
		c.Invoke(t, stackitem.NewByteArray(values[i]), "get", ids[i], keys[i])
		info := getAccessBoxRecord(t, c, ids[i])
		require.Equal(t, ids[i], itemBytes(t, info[0]))
	}
	require.NotEqual(t, ids[0], ids[1])
}

func TestAccessBoxCreatedNotification(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	var id []byte
	tx := ownerC.InvokeAndCheck(t, func(t testing.TB, stack []stackitem.Item) {
		require.Len(t, stack, 1)
		id = itemBytes(t, stack[0])
	}, "put", owner.ScriptHash(), "s3", expiresAt,
		accessBoxKeysArgs([][]byte{[]byte("gateway")}),
		[]byte("value"))

	result := c.GetTxExecResult(t, tx)
	require.Len(t, result.Events, 1)
	require.Equal(t, c.Hash, result.Events[0].ScriptHash)
	assertNotificationEvent(t, result.Events[0], "AccessBoxCreated", id)

	info := getAccessBoxRecord(t, c, id)
	require.Equal(t, id, itemBytes(t, info[0]))
}

func TestAccessBoxAppendAndRemove(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	other := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	otherC := c.WithSigners(other)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000
	key := []byte("gateway-a")
	id := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{key}, []byte("first"))

	otherC.InvokeFail(t, common.ErrOwnerWitnessFailed, "append", id, []byte("gateway-b"), []byte("second"))
	ownerC.Invoke(t, stackitem.Null{}, "append", id, []byte("gateway-b"), []byte("second"))
	c.Invoke(t, stackitem.NewByteArray([]byte("second")), "get", id, []byte("gateway-b"))
	info := getAccessBoxRecord(t, c, id)
	require.EqualValues(t, 2, itemInt(t, info[5]))
	require.EqualValues(t, len("first")+len("second"), itemInt(t, info[6]))
	listed := listAccessBoxRecords(t, c, owner.ScriptHash(), "s3")
	require.Len(t, listed, 1)
	require.EqualValues(t, 2, itemInt(t, listed[0][5]))
	require.EqualValues(t, len("first")+len("second"), itemInt(t, listed[0][6]))
	ownerC.InvokeFail(t, "access box entry already exists", "append", id, key, []byte("replacement"))

	otherC.InvokeFail(t, common.ErrOwnerWitnessFailed, "remove", id, key)
	ownerC.Invoke(t, stackitem.Null{}, "remove", id, key)
	c.Invoke(t, stackitem.Null{}, "get", id, key)
	info = getAccessBoxRecord(t, c, id)
	require.EqualValues(t, 1, itemInt(t, info[5]))
	require.EqualValues(t, len("second"), itemInt(t, info[6]))
	listed = listAccessBoxRecords(t, c, owner.ScriptHash(), "s3")
	require.EqualValues(t, 1, itemInt(t, listed[0][5]))
	require.EqualValues(t, len("second"), itemInt(t, listed[0][6]))
	ownerC.InvokeFail(t, "access box entry not found", "remove", id, key)

	// The box stays valid after one entry is removed, so the key can be added
	// again without changing the box ID.
	ownerC.Invoke(t, stackitem.Null{}, "append", id, key, []byte("replacement"))
	c.Invoke(t, stackitem.NewByteArray([]byte("replacement")), "get", id, key)
	c.Invoke(t, stackitem.Null{}, "remove", id, key)
	c.Invoke(t, stackitem.Null{}, "get", id, key)
}

func TestAccessBoxEntryLimits(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t, 1_000_0000_0000)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	keys := make([][]byte, 100)
	for i := range keys {
		keys[i] = []byte{byte(i), byte(i >> 8)}
	}
	id := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt, keys, []byte("value"))
	ownerC.InvokeFail(t, "too many access box entries", "append", id, []byte("one-too-many"), []byte("value"))

	// A full box can still be revoked in one invocation because the entry count
	// is bounded.
	c.Invoke(t, stackitem.Null{}, "revoke", id)
	c.Invoke(t, stackitem.Null{}, "getInfo", id)

	const maxEntryValueSize = 65_000
	largeValue := make([]byte, maxEntryValueSize)
	for i := range largeValue {
		largeValue[i] = byte(i)
	}
	ownerC.InvokeFail(t, "access box entry is too large", "put", owner.ScriptHash(), "s3", expiresAt,
		accessBoxKeysArgs([][]byte{[]byte("too-large")}), make([]byte, maxEntryValueSize+1))
	id = putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("a")}, largeValue)
	ownerC.InvokeFail(t, "access box entry is too large", "append", id, []byte("too-large"), make([]byte, maxEntryValueSize+1))
	for i := range 3 {
		ownerC.Invoke(t, stackitem.Null{}, "append", id, []byte{byte('b' + i)}, largeValue)
	}
	ownerC.Invoke(t, stackitem.Null{}, "append", id, []byte("f"), make([]byte, 256*1024-4*maxEntryValueSize))
	info := getAccessBoxRecord(t, c, id)
	require.EqualValues(t, 5, itemInt(t, info[5]))
	require.EqualValues(t, 256*1024, itemInt(t, info[6]))
	ownerC.InvokeFail(t, "access box is too large", "append", id, []byte("g"), []byte{1})

	// The aggregate size limit does not prevent a later revoke from removing
	// every stored entry.
	c.Invoke(t, stackitem.Null{}, "revoke", id)
	c.Invoke(t, stackitem.Null{}, "getInfo", id)
}

func TestAccessBoxList(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	otherOwner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	otherOwnerC := c.WithSigners(otherOwner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	idS3a := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("a")}, []byte("a"))
	idSession := putAccessBox(t, ownerC, owner.ScriptHash(), "session", expiresAt,
		[][]byte{[]byte("a")}, []byte("session"))
	idS3b := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("b")}, []byte("b"))
	_ = putAccessBox(t, otherOwnerC, otherOwner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("other")}, []byte("other"))

	require.ElementsMatch(t, [][]byte{idS3a, idSession, idS3b}, listAccessBoxIDs(t, c, owner.ScriptHash(), ""))
	require.ElementsMatch(t, [][]byte{idS3a, idS3b}, listAccessBoxIDs(t, c, owner.ScriptHash(), "s3"))
	require.Equal(t, [][]byte{idSession}, listAccessBoxIDs(t, c, owner.ScriptHash(), "session"))
	require.Empty(t, listAccessBoxIDs(t, c, owner.ScriptHash(), "pat"))
}

func TestAccessBoxRevoke(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	other := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000
	id := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("a"), []byte("b")}, []byte("value"))

	c.WithSigners(other).InvokeFail(t, common.ErrOwnerWitnessFailed, "revoke", id)
	c.Invoke(t, stackitem.Null{}, "revoke", id)
	c.Invoke(t, stackitem.Null{}, "get", id, []byte("a"))
	c.Invoke(t, stackitem.Null{}, "get", id, []byte("b"))
	c.Invoke(t, stackitem.Null{}, "getInfo", id)
	require.Empty(t, listAccessBoxIDs(t, c, owner.ScriptHash(), ""))

	id = putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("a")}, []byte("first"))
	ownerC.Invoke(t, stackitem.Null{}, "revoke", id)
}

func TestAccessBoxRevokeOwnerByCategory(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	other := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	idS3a := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("a")}, []byte("s3-a"))
	idSession := putAccessBox(t, ownerC, owner.ScriptHash(), "session", expiresAt,
		[][]byte{[]byte("a")}, []byte("session"))
	idS3b := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("b")}, []byte("s3-b"))

	c.WithSigners(other).InvokeFail(t, common.ErrOwnerWitnessFailed,
		"revokeOwner", owner.ScriptHash(), "s3", 1)
	c.Invoke(t, int64(1), "revokeOwner", owner.ScriptHash(), "s3", 1)
	remainingS3 := listAccessBoxIDs(t, c, owner.ScriptHash(), "s3")
	require.Len(t, remainingS3, 1)
	require.True(t, string(remainingS3[0]) == string(idS3a) || string(remainingS3[0]) == string(idS3b))
	c.Invoke(t, stackitem.NewByteArray([]byte("session")), "get", idSession, []byte("a"))

	ownerC.Invoke(t, int64(2), "revokeOwner", owner.ScriptHash(), "", 10)
	require.Empty(t, listAccessBoxIDs(t, c, owner.ScriptHash(), ""))
}

func TestAccessBoxRevokeOwnerBoundsEntryDeletion(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t, 1_000_0000_0000)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000
	keys := make([][]byte, 100)
	for i := range keys {
		keys[i] = []byte{byte(i), byte(i >> 8)}
	}
	for range 5 {
		_ = putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt, keys, []byte("value"))
	}

	// Although the requested record limit is five, the contract removes only
	// two 100-entry boxes in one call.
	c.Invoke(t, int64(2), "revokeOwner", owner.ScriptHash(), "s3", 5)
	require.Len(t, listAccessBoxIDs(t, c, owner.ScriptHash(), "s3"), 3)
	c.Invoke(t, int64(2), "revokeOwner", owner.ScriptHash(), "s3", 5)
	require.Len(t, listAccessBoxIDs(t, c, owner.ScriptHash(), "s3"), 1)
	c.Invoke(t, int64(1), "revokeOwner", owner.ScriptHash(), "s3", 5)
	require.Empty(t, listAccessBoxIDs(t, c, owner.ScriptHash(), "s3"))
}

func TestAccessBoxExpiryAndPruning(t *testing.T) {
	e, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 1_000
	id := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("a")}, []byte("value"))

	b := c.NewUnsignedBlock(t)
	b.Timestamp = uint64(expiresAt + 1)
	require.NoError(t, e.Chain.AddBlock(c.SignBlock(b)))

	c.Invoke(t, stackitem.Null{}, "get", id, []byte("a"))
	require.NotEmpty(t, getAccessBoxRecord(t, c, id))
	ownerC.InvokeFail(t, "access box is expired", "append", id, []byte("b"), []byte("value"))
	c.Invoke(t, int64(1), "pruneExpired", 10)
	c.Invoke(t, stackitem.Null{}, "getInfo", id)
}

func TestAccessBoxPruneExpiredWithMaximumEntries(t *testing.T) {
	e, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t, 1_000_0000_0000)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 1_000
	keys := make([][]byte, 100)
	for i := range keys {
		keys[i] = []byte{byte(i), byte(i >> 8)}
	}
	for range 5 {
		_ = putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt, keys, []byte("value"))
	}

	b := c.NewUnsignedBlock(t)
	b.Timestamp = uint64(expiresAt + 1)
	require.NoError(t, e.Chain.AddBlock(c.SignBlock(b)))

	// Pruning uses the same 256-entry deletion budget as RevokeOwner.
	c.Invoke(t, int64(2), "pruneExpired", 5)
	require.Len(t, listAccessBoxIDs(t, c, owner.ScriptHash(), "s3"), 3)
	c.Invoke(t, int64(2), "pruneExpired", 5)
	require.Len(t, listAccessBoxIDs(t, c, owner.ScriptHash(), "s3"), 1)
	c.Invoke(t, int64(1), "pruneExpired", 5)
	require.Empty(t, listAccessBoxIDs(t, c, owner.ScriptHash(), "s3"))
}

func TestAccessBoxPruneExpiredOrdered(t *testing.T) {
	e, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	now := int(c.TopBlock(t).Timestamp)
	firstExpiresAt := now + 10_000
	secondExpiresAt := now + 20_000
	futureExpiresAt := now + 60_000

	// Write in the opposite order to show that cleanup follows expiry time, not
	// access-box ID.
	futureID := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", futureExpiresAt,
		[][]byte{[]byte("future")}, []byte("future"))
	secondID := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", secondExpiresAt,
		[][]byte{[]byte("second")}, []byte("second"))
	firstID := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", firstExpiresAt,
		[][]byte{[]byte("first")}, []byte("first"))

	b := c.NewUnsignedBlock(t)
	b.Timestamp = uint64(secondExpiresAt + 1)
	require.NoError(t, e.Chain.AddBlock(c.SignBlock(b)))

	c.Invoke(t, int64(1), "pruneExpired", 1)
	c.Invoke(t, stackitem.Null{}, "getInfo", firstID)
	require.NotEmpty(t, getAccessBoxRecord(t, c, secondID))
	require.NotEmpty(t, getAccessBoxRecord(t, c, futureID))

	c.Invoke(t, int64(1), "pruneExpired", 1)
	c.Invoke(t, stackitem.Null{}, "getInfo", secondID)
	require.NotEmpty(t, getAccessBoxRecord(t, c, futureID))

	c.Invoke(t, int64(0), "pruneExpired", 1)
	require.NotEmpty(t, getAccessBoxRecord(t, c, futureID))
}

func TestAccessBoxValidation(t *testing.T) {
	_, c := newAccessBoxInvoker(t)
	owner := c.NewAccount(t)
	ownerC := c.WithSigners(owner)
	expiresAt := int(c.TopBlock(t).Timestamp) + 60_000

	c.Invoke(t, stackitem.Null{}, "get", []byte{1}, []byte("a"))
	ownerC.InvokeFail(t, "invalid access box id", "revoke", []byte{1})
	ownerC.InvokeFail(t, "invalid access box category", "put", owner.ScriptHash(), "",
		expiresAt, accessBoxKeysArgs([][]byte{[]byte("a")}), []byte("value"))
	ownerC.InvokeFail(t, "invalid access box category", "revokeOwner", owner.ScriptHash(), strings.Repeat("a", 17), 1)
	_ = putAccessBox(t, ownerC, owner.ScriptHash(), strings.Repeat("a", 16), expiresAt,
		[][]byte{[]byte(strings.Repeat("k", 47))}, []byte("value"))
	ownerC.InvokeFail(t, "invalid access box entries", "put", owner.ScriptHash(), "s3",
		expiresAt, accessBoxKeysArgs([][]byte{}), []byte("value"))
	tooManyKeys := make([][]byte, 101)
	for i := range tooManyKeys {
		tooManyKeys[i] = []byte{byte(i)}
	}
	ownerC.InvokeFail(t, "too many access box entries", "put", owner.ScriptHash(), "s3",
		expiresAt, accessBoxKeysArgs(tooManyKeys), []byte("value"))
	ownerC.InvokeFail(t, "invalid access box entry key", "put", owner.ScriptHash(), "s3",
		expiresAt, accessBoxKeysArgs([][]byte{nil}), []byte("value"))
	ownerC.InvokeFail(t, "invalid access box entry key", "put", owner.ScriptHash(), "s3",
		expiresAt, accessBoxKeysArgs([][]byte{[]byte(strings.Repeat("a", 48))}), []byte("value"))
	ownerC.InvokeFail(t, "invalid access box entry size", "put", owner.ScriptHash(), "s3",
		expiresAt, accessBoxKeysArgs([][]byte{[]byte("a")}), []byte{})
	ownerC.InvokeFail(t, "duplicate access box entry key", "put", owner.ScriptHash(), "s3",
		expiresAt, accessBoxKeysArgs([][]byte{[]byte("a"), []byte("a")}), []byte("value"))
	ownerC.InvokeFail(t, "access box is already expired", "put", owner.ScriptHash(), "s3",
		int(c.TopBlock(t).Timestamp), accessBoxKeysArgs([][]byte{[]byte("a")}), []byte("value"))
	ownerC.InvokeFail(t, "access box lifetime exceeds one year", "put", owner.ScriptHash(), "s3",
		expiresAt+365*24*60*60*1000, accessBoxKeysArgs([][]byte{[]byte("a")}), []byte("value"))
}

func TestAccessBoxFee(t *testing.T) {
	const fee = int64(17)

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
	ownerC := accessBoxC.WithSigners(owner)
	expiresAt := int(accessBoxC.TopBlock(t).Timestamp) + 60_000
	alphabet := getAlphabetAccs(t, e)
	totalFee := fee * int64(len(alphabet))
	keys := [][]byte{[]byte("a")}
	value := []byte("value")

	ownerC.InvokeFail(t, "insufficient balance to create access box", "put", owner.ScriptHash(), "s3", expiresAt, accessBoxKeysArgs(keys), value)

	balanceMint(t, balanceC, owner.ScriptHash(), totalFee-1, nil)
	ownerC.InvokeFail(t, "insufficient balance to create access box", "put", owner.ScriptHash(), "s3", expiresAt, accessBoxKeysArgs(keys), value)

	balanceMint(t, balanceC, owner.ScriptHash(), 1, nil)
	id := putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt, keys, value)
	balanceC.Invoke(t, int64(0), "balanceOf", owner.ScriptHash())
	for i := range alphabet {
		balanceC.Invoke(t, fee, "balanceOf", alphabet[i].ScriptHash())
	}

	ownerC.Invoke(t, stackitem.Null{}, "append", id, []byte("b"), []byte("another value"))
	balanceC.Invoke(t, int64(0), "balanceOf", owner.ScriptHash())
	ownerC.Invoke(t, stackitem.Null{}, "remove", id, []byte("b"))
	balanceC.Invoke(t, int64(0), "balanceOf", owner.ScriptHash())
}

func TestAccessBoxRejectsNegativeFee(t *testing.T) {
	_, accessBox, _ := newAccessBoxEnvironment(t, accessboxconst.FeeConfigKey, int64(-1))
	owner := accessBox.NewAccount(t)
	ownerC := accessBox.WithSigners(owner)
	expiresAt := int(accessBox.TopBlock(t).Timestamp) + 60_000

	ownerC.InvokeFail(t, "invalid access box fee", "put", owner.ScriptHash(), "s3", expiresAt,
		accessBoxKeysArgs([][]byte{[]byte("a")}), []byte("value"))
}

func TestAccessBoxUpdateDeploymentPreservesDependencies(t *testing.T) {
	bc, account := chain.NewSingle(t)
	e := neotest.NewExecutor(t, bc, account, account)
	_ = deployDefaultNNS(t, e)
	const fee = int64(1)
	deployNetmapContract(t, e, accessboxconst.FeeConfigKey, fee)
	balance := deployBalanceContract(t, e)
	current := neotest.CompileFile(t, e.Validator.ScriptHash(), accessBoxPath,
		path.Join(accessBoxPath, "config.yml"))
	previous := compilePreviousAccessBoxContract(t, e.Validator.ScriptHash())
	e.DeployContract(t, previous, nil)
	accessBox := e.CommitteeInvoker(previous.Hash)

	nef, err := current.NEF.Bytes()
	require.NoError(t, err)
	manifest, err := json.Marshal(current.Manifest)
	require.NoError(t, err)

	// The previous version's Update sends the old version number. Management
	// therefore invokes the current _deploy with isUpdate set to true.
	accessBox.Invoke(t, stackitem.Null{}, "update", nef, manifest, []any{})

	// The update path must retain the Netmap and Balance hashes written during
	// initial deployment, so Put can charge the configured fee.
	balanceC := e.NewInvoker(balance, alphaSigner(t, e))
	owner := accessBox.NewAccount(t)
	ownerC := accessBox.WithSigners(owner)
	expiresAt := int(accessBox.TopBlock(t).Timestamp) + 60_000
	balanceMint(t, balanceC, owner.ScriptHash(), fee*int64(len(getAlphabetAccs(t, e))), nil)
	_ = putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("a")}, []byte("value"))
	balanceC.Invoke(t, int64(0), "balanceOf", owner.ScriptHash())
}

func TestAccessBoxMissingFeeIsFree(t *testing.T) {
	_, accessBox, balance := newAccessBoxEnvironment(t)
	owner := accessBox.NewAccount(t)
	ownerC := accessBox.WithSigners(owner)
	expiresAt := int(accessBox.TopBlock(t).Timestamp) + 60_000

	_ = putAccessBox(t, ownerC, owner.ScriptHash(), "s3", expiresAt,
		[][]byte{[]byte("a")}, []byte("value"))
	balance.Invoke(t, int64(0), "balanceOf", owner.ScriptHash())
}

func putAccessBox(t *testing.T, c *neotest.ContractInvoker, owner util.Uint160, category string, expiresAt int, keys [][]byte, value []byte) []byte {
	t.Helper()

	var id []byte
	c.InvokeAndCheck(t, func(t testing.TB, stack []stackitem.Item) {
		require.Len(t, stack, 1)
		id = itemBytes(t, stack[0])
	}, "put", owner, category, expiresAt, accessBoxKeysArgs(keys), value)
	require.Len(t, id, 16)

	return id
}

func accessBoxKeysArgs(entries [][]byte) []any {
	args := make([]any, len(entries))
	for i := range entries {
		args[i] = entries[i]
	}
	return args
}

func getAccessBoxRecord(t *testing.T, c *neotest.ContractInvoker, id []byte) []stackitem.Item {
	t.Helper()
	res, err := c.TestInvoke(t, "getInfo", id)
	require.NoError(t, err)
	return stackItemToArray(res.Pop().Item())
}

func listAccessBoxIDs(t *testing.T, c *neotest.ContractInvoker, owner util.Uint160, category string) [][]byte {
	t.Helper()
	records := listAccessBoxRecords(t, c, owner, category)
	ids := make([][]byte, len(records))
	for i := range records {
		ids[i] = itemBytes(t, records[i][0])
	}
	return ids
}

func listAccessBoxRecords(t *testing.T, c *neotest.ContractInvoker, owner util.Uint160, category string) [][]stackitem.Item {
	t.Helper()
	res, err := c.TestInvoke(t, "list", owner, category)
	require.NoError(t, err)

	items := iteratorToArray(res.Pop().Value().(*storage.Iterator))
	records := make([][]stackitem.Item, len(items))
	for i := range items {
		records[i] = stackItemToArray(items[i])
	}
	return records
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
