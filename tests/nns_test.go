package tests

import (
	"fmt"
	"math/big"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/nns/recordtype"
	"github.com/stretchr/testify/require"
)

const nnsPath = "../contracts/nns"

const (
	msPerYear   = 365 * 24 * time.Hour / time.Millisecond
	maxRecordID = 15 // value from the contract.
)

func newNNSInvoker(t *testing.T, addRoot bool, tldSet ...string) *neotest.ContractInvoker {
	e := newExecutor(t)
	ctr := neotest.CompileFile(t, e.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
	if len(tldSet) > 0 {
		_tldSet := make([]any, len(tldSet))
		for i := range tldSet {
			_tldSet[i] = []any{tldSet[i], "user@domain.org"}
		}
		e.DeployContract(t, ctr, []any{_tldSet})
	} else {
		e.DeployContract(t, ctr, nil)
	}

	c := e.CommitteeInvoker(ctr.Hash)
	if addRoot {
		// Set expiration big enough to pass all tests.
		refresh, retry, expire, ttl := int64(101), int64(102), int64(msPerYear/1000*100), int64(104)
		c.Invoke(t, stackitem.Null{}, "registerTLD",
			"com", "myemail@nspcc.ru", refresh, retry, expire, ttl)
	}
	return c
}

func deployDefaultNNS(t *testing.T, e *neotest.Executor) util.Uint160 {
	ctrNNS := neotest.CompileFile(t, e.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
	e.DeployContract(t, ctrNNS, []any{[]any{[]any{"neofs", "ops@nspcc.io"}}})
	return ctrNNS.Hash
}

func regContractNNS(t *testing.T, e *neotest.Executor, name string, h util.Uint160) {
	nnsHash, err := e.Chain.GetContractScriptHash(1)
	require.NoError(t, err)
	nnsInv := e.CommitteeInvoker(nnsHash)
	nnsInv.Invoke(t, true, "register", name+".neofs", e.CommitteeHash, "ops@nspcc.ru", int64(3600), int64(600), int64(10*msPerYear), int64(3600))
	var addr = h.StringLE()
	if h[0] > 127 {
		addr = address.Uint160ToString(h) // There are two valid representations, so alternate between them.
	}
	nnsInv.Invoke(t, nil, "addRecord", name+".neofs", 16, addr)
}

func TestNNSGeneric(t *testing.T) {
	c := newNNSInvoker(t, false)

	c.Invoke(t, "NNS", "symbol")
	c.Invoke(t, 0, "decimals")
	c.Invoke(t, 0, "totalSupply")
}

func TestNNSRegisterTLD(t *testing.T) {
	c := newNNSInvoker(t, false)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)

	c.InvokeFail(t, "invalid domain fragment", "registerTLD",
		"0com", "email@nspcc.ru", refresh, retry, expire, ttl)

	c.InvokeFail(t, "not a TLD", "registerTLD",
		"neo.org", "email@nspcc.ru", refresh, retry, expire, ttl)

	acc := c.NewAccount(t)
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, "not witnessed by committee", "registerTLD",
		"com", "email@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, stackitem.Null{}, "registerTLD",
		"com", "email@nspcc.ru", refresh, retry, expire, ttl)

	c.InvokeFail(t, "TLD already exists", "registerTLD",
		"com", "email@nspcc.ru", refresh, retry, expire, ttl)
}

func TestNNSRegister(t *testing.T) {
	c := newNNSInvoker(t, false)

	accTop := c.NewAccount(t)
	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c1 := c.WithSigners(c.Committee, accTop)
	c1.Invoke(t, stackitem.Null{}, "registerTLD",
		"com", "myemail@nspcc.ru", refresh, retry, expire, ttl)

	acc := c.NewAccount(t)

	c3 := c.WithSigners(accTop, acc)
	t.Run("domain names with hyphen", func(t *testing.T) {
		c3.InvokeFail(t, "invalid domain fragment", "register",
			"-testdomain.com", acc.ScriptHash(),
			"myemail@nspcc.ru", refresh, retry, expire, ttl)
		c3.InvokeFail(t, "invalid domain fragment", "register",
			"testdomain-.com", acc.ScriptHash(),
			"myemail@nspcc.ru", refresh, retry, expire, ttl)
		c3.Invoke(t, true, "register",
			"test-domain.com", acc.ScriptHash(),
			"myemail@nspcc.ru", refresh, retry, expire, ttl)
	})
	c3.Invoke(t, true, "register",
		"testdomain.com", acc.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	b := c.TopBlock(t)
	expected := stackitem.NewArray([]stackitem.Item{stackitem.NewBuffer(
		[]byte(fmt.Sprintf("testdomain.com myemail@nspcc.ru %d %d %d %d %d",
			b.Timestamp, refresh, retry, expire, ttl)))})
	c.Invoke(t, expected, "getRecords", "testdomain.com", int64(recordtype.SOA))

	cAcc := c.WithSigners(acc)
	cAcc.Invoke(t, stackitem.Null{}, "addRecord",
		"testdomain.com", int64(recordtype.TXT), "first TXT record")
	cAcc.InvokeFail(t, "record already exists", "addRecord",
		"testdomain.com", int64(recordtype.TXT), "first TXT record")
	cAcc.Invoke(t, stackitem.Null{}, "addRecord",
		"testdomain.com", int64(recordtype.TXT), "second TXT record")

	expected = stackitem.NewArray([]stackitem.Item{
		stackitem.NewByteArray([]byte("first TXT record")),
		stackitem.NewByteArray([]byte("second TXT record"))})
	c.Invoke(t, expected, "getRecords", "testdomain.com", int64(recordtype.TXT))

	cAcc.Invoke(t, stackitem.Null{}, "setRecord",
		"testdomain.com", int64(recordtype.TXT), int64(0), "replaced first")

	expected = stackitem.NewArray([]stackitem.Item{
		stackitem.NewByteArray([]byte("replaced first")),
		stackitem.NewByteArray([]byte("second TXT record"))})
	c.Invoke(t, expected, "getRecords", "testdomain.com", int64(recordtype.TXT))
}

func TestNNSRegisterMulti(t *testing.T) {
	c := newNNSInvoker(t, true)

	newArgs := func(domain string, account neotest.Signer) []any {
		return []any{
			domain, account.ScriptHash(), "doesnt@matter.com",
			int64(101), int64(102), int64(103), int64(104),
		}
	}
	acc := c.NewAccount(t)
	cBoth := c.WithSigners(c.Committee, acc)
	args := newArgs("neo.com", acc)
	cBoth.Invoke(t, true, "register", args...)

	c1 := c.WithSigners(acc)
	t.Run("parent domain is missing", func(t *testing.T) {
		msg := "one of the parent domains is not registered"
		args[0] = "testnet.fs.neo.com"
		c1.InvokeFail(t, msg, "register", args...)
	})

	args[0] = "fs.neo.com"
	c1.Invoke(t, true, "register", args...)

	args[0] = "testnet.fs.neo.com"
	c1.Invoke(t, true, "register", args...)

	acc2 := c.NewAccount(t)
	c2 := c.WithSigners(c.Committee, acc2)
	args = newArgs("mainnet.fs.neo.com", acc2)
	c2.InvokeFail(t, "not witnessed by admin", "register", args...)

	c1.Invoke(t, stackitem.Null{}, "addRecord",
		"something.mainnet.fs.neo.com", int64(recordtype.A), "1.2.3.4")
	c1.Invoke(t, stackitem.Null{}, "setRecord",
		"something.mainnet.fs.neo.com", int64(recordtype.A), 0, "2.3.4.5")
	c1.InvokeFail(t, "invalid record id", "setRecord",
		"something.mainnet.fs.neo.com", int64(recordtype.A), 1, "2.3.4.5")
	c1.Invoke(t, stackitem.Null{}, "addRecord",
		"another.fs.neo.com", int64(recordtype.A), "4.3.2.1")

	c2 = c.WithSigners(acc, acc2)
	c2.Invoke(t, stackitem.NewBool(false), "isAvailable", "mainnet.fs.neo.com")
	c2.InvokeFail(t, "parent domain has conflicting records: something.mainnet.fs.neo.com",
		"register", args...)

	c1.Invoke(t, stackitem.Null{}, "deleteRecords",
		"something.mainnet.fs.neo.com", int64(recordtype.A))
	c2.Invoke(t, stackitem.NewBool(true), "isAvailable", "mainnet.fs.neo.com")
	c2.Invoke(t, true, "register", args...)

	c2 = c.WithSigners(acc2)
	c2.Invoke(t, stackitem.Null{}, "addRecord",
		"cdn.mainnet.fs.neo.com", int64(recordtype.A), "166.15.14.13")
	result := stackitem.NewArray([]stackitem.Item{
		stackitem.NewByteArray([]byte("166.15.14.13"))})
	c2.Invoke(t, result, "resolve", "cdn.mainnet.fs.neo.com", int64(recordtype.A))
}

func TestNNSUpdateSOA(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	refresh *= 2
	retry *= 2
	expire *= 2
	ttl *= 2
	c.Invoke(t, stackitem.Null{}, "updateSOA",
		"testdomain.com", "newemail@nspcc.ru", refresh, retry, expire, ttl)

	b := c.TopBlock(t)
	expected := stackitem.NewArray([]stackitem.Item{stackitem.NewBuffer(
		[]byte(fmt.Sprintf("testdomain.com newemail@nspcc.ru %d %d %d %d %d",
			b.Timestamp, refresh, retry, expire, ttl)))})
	c.Invoke(t, expected, "getRecords", "testdomain.com", int64(recordtype.SOA))
}

func TestNNSGetAllRecords(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, stackitem.Null{}, "addRecord", "testdomain.com", int64(recordtype.TXT), "first TXT record")
	c.Invoke(t, stackitem.Null{}, "addRecord", "testdomain.com", int64(recordtype.A), "1.2.3.4")

	b := c.TopBlock(t)
	expSOA := fmt.Sprintf("testdomain.com myemail@nspcc.ru %d %d %d %d %d",
		b.Timestamp, refresh, retry, expire, ttl)

	s, err := c.TestInvoke(t, "getAllRecords", "testdomain.com")
	require.NoError(t, err)

	iter := s.Pop().Value().(*storage.Iterator)
	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(recordtype.A)),
		stackitem.Make("1.2.3.4"), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(recordtype.SOA)),
		stackitem.NewBuffer([]byte(expSOA)), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(recordtype.TXT)),
		stackitem.Make("first TXT record"), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.False(t, iter.Next())
}

func TestExpiration(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(msPerYear/1000*10), int64(104)
	c.Invoke(t, stackitem.Null{}, "registerTLD",
		"newtld", "myemail@nspcc.ru", refresh, retry, expire, ttl)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	checkProperties := func(t *testing.T, expiration uint64) {
		expected := stackitem.NewMapWithValue([]stackitem.MapElement{
			{Key: stackitem.Make("name"), Value: stackitem.Make("testdomain.com")},
			{Key: stackitem.Make("expiration"), Value: stackitem.Make(expiration)},
			{Key: stackitem.Make("admin"), Value: stackitem.Null{}}})
		s, err := c.TestInvoke(t, "properties", "testdomain.com")
		require.NoError(t, err)
		require.Equal(t, expected.Value(), s.Top().Item().Value())
	}

	top := c.TopBlock(t)
	expiration := top.Timestamp + uint64(expire*1000)
	checkProperties(t, expiration)

	b := c.NewUnsignedBlock(t)
	b.Timestamp = expiration - 2 // test invoke is done with +1 timestamp
	require.NoError(t, c.Chain.AddBlock(c.SignBlock(b)))
	checkProperties(t, expiration)

	b = c.NewUnsignedBlock(t)
	b.Timestamp = expiration - 1
	require.NoError(t, c.Chain.AddBlock(c.SignBlock(b)))

	_, err := c.TestInvoke(t, "properties", "testdomain.com")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "name has expired"))

	c.InvokeFail(t, "name has expired", "getAllRecords", "testdomain.com")
	c.InvokeFail(t, "name has expired", "ownerOf", "testdomain.com")

	c.InvokeFail(t, "name has expired", "renew", "newtld")
	c.Invoke(t, stackitem.Null{}, "registerTLD",
		"newtld", "myemail@nspcc.ru", refresh, retry, expire, ttl)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)
}

func TestNNSSetAdmin(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)
	top := c.TopBlock(t)

	acc := c.NewAccount(t)
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, "not witnessed by admin", "addRecord",
		"testdomain.com", int64(recordtype.TXT), "won't be added")

	c1 := c.WithSigners(c.Committee, acc)
	c1.Invoke(t, stackitem.Null{}, "setAdmin", "testdomain.com", acc.ScriptHash())

	expiration := top.Timestamp + uint64(expire*1000)
	expectedProps := stackitem.NewMapWithValue([]stackitem.MapElement{
		{Key: stackitem.Make("name"), Value: stackitem.Make("testdomain.com")},
		{Key: stackitem.Make("expiration"), Value: stackitem.Make(expiration)},
		{Key: stackitem.Make("admin"), Value: stackitem.Make(acc.ScriptHash().BytesBE())}})
	cAcc.Invoke(t, expectedProps, "properties", "testdomain.com")

	cAcc.Invoke(t, stackitem.Null{}, "addRecord",
		"testdomain.com", int64(recordtype.TXT), "will be added")
}

func TestNNSIsAvailable(t *testing.T) {
	c := newNNSInvoker(t, false)

	c.Invoke(t, true, "isAvailable", "com")
	c.InvokeFail(t, "TLD not found", "isAvailable", "domain.com")

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, stackitem.Null{}, "registerTLD",
		"com", "myemail@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, false, "isAvailable", "com")
	c.Invoke(t, true, "isAvailable", "domain.com")

	acc := c.NewAccount(t)
	c1 := c.WithSigners(c.Committee, acc)
	c1.Invoke(t, true, "register",
		"domain.com", acc.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, false, "isAvailable", "domain.com")
}

func TestNNSRenew(t *testing.T) {
	c := newNNSInvoker(t, true)

	acc := c.NewAccount(t)
	c1 := c.WithSigners(c.Committee, acc)
	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c1.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	const msPerYear = 365 * 24 * time.Hour / time.Millisecond
	b := c.TopBlock(t)
	ts := b.Timestamp + uint64(expire*1000) + uint64(msPerYear)

	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, "not witnessed by admin", "renew", "testdomain.com")
	c1.Invoke(t, ts, "renew", "testdomain.com")
	expected := stackitem.NewMapWithValue([]stackitem.MapElement{
		{Key: stackitem.Make("name"), Value: stackitem.Make("testdomain.com")},
		{Key: stackitem.Make("expiration"), Value: stackitem.Make(ts)},
		{Key: stackitem.Make("admin"), Value: stackitem.Null{}}})
	cAcc.Invoke(t, expected, "properties", "testdomain.com")
}

func TestNNSResolve(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register", "test.com", c.CommitteeHash, "myemail@nspcc.ru", refresh, retry, expire, ttl)
	c.Invoke(t, stackitem.Null{}, "addRecord", "test.com", int64(recordtype.TXT), "expected result")
	c.Invoke(t, stackitem.Null{}, "addRecord", "test.com", int64(recordtype.CNAME), "alias.com")

	c.Invoke(t, true, "register", "alias.com", c.CommitteeHash, "myemail@nspcc.ru", refresh, retry, expire, ttl)
	c.Invoke(t, stackitem.Null{}, "addRecord", "alias.com", int64(recordtype.A), "1.2.3.4")
	c.Invoke(t, stackitem.Null{}, "addRecord", "alias.com", int64(recordtype.CNAME), "alias2.com")

	c.Invoke(t, true, "register", "alias2.com", c.CommitteeHash, "myemail@nspcc.ru", refresh, retry, expire, ttl)
	c.Invoke(t, stackitem.Null{}, "addRecord", "alias2.com", int64(recordtype.A), "5.6.7.8")

	records := stackitem.NewArray([]stackitem.Item{stackitem.Make("expected result")})
	c.Invoke(t, records, "resolve", "test.com", int64(recordtype.TXT))
	c.Invoke(t, records, "resolve", "test.com.", int64(recordtype.TXT))
	c.InvokeFail(t, "invalid domain fragment", "resolve", "test.com..", int64(recordtype.TXT))

	// Check CNAME is properly resolved and is not included into the result list.
	c.Invoke(t, stackitem.NewArray([]stackitem.Item{stackitem.Make("1.2.3.4"), stackitem.Make("5.6.7.8")}), "resolve", "test.com", int64(recordtype.A))
	// And this time it should be properly included without resolution.
	c.Invoke(t, stackitem.NewArray([]stackitem.Item{stackitem.Make("alias.com")}), "resolve", "test.com", int64(recordtype.CNAME))
}

func TestNNSRegisterAccess(t *testing.T) {
	inv := newNNSInvoker(t, false)
	const email, refresh, retry, expire, ttl = "user@domain.org", 0, 1, 2, 3
	const tld = "com"
	const registerMethod = "register"
	const registerTLDMethod = "registerTLD"
	const tldDeniedFailMsg = "TLD denied"

	// TLD
	l2OwnerAcc := inv.NewAccount(t)
	l2OwnerInv := inv.WithSigners(l2OwnerAcc)

	l2OwnerInv.InvokeFail(t, tldDeniedFailMsg, registerMethod,
		tld, l2OwnerAcc.ScriptHash(), email, refresh, retry, expire, ttl)
	l2OwnerInv.InvokeFail(t, tldDeniedFailMsg, registerMethod,
		tld, nil, email, refresh, retry, expire, ttl)

	inv.WithSigners(inv.Committee).InvokeFail(t, tldDeniedFailMsg, registerMethod,
		tld, nil, email, refresh, retry, expire, ttl)
	inv.WithSigners(inv.Committee).Invoke(t, stackitem.Null{}, registerTLDMethod,
		tld, email, refresh, retry, expire, ttl)

	// L2
	const l2 = "l2." + tld

	anonymousAcc := inv.NewAccount(t)
	anonymousInv := inv.WithSigners(anonymousAcc)

	l2OwnerInv.InvokeFail(t, "invalid owner", registerMethod,
		l2, nil, email, refresh, retry, expire, ttl)
	l2OwnerInv.InvokeFail(t, common.ErrOwnerWitnessFailed, registerMethod,
		l2, anonymousAcc.ScriptHash(), email, refresh, retry, expire, ttl)
	l2OwnerInv.Invoke(t, true, registerMethod,
		l2, l2OwnerAcc.ScriptHash(), email, refresh, retry, expire, ttl)

	// L3 (by L2 owner)
	const l3ByL2Owner = "l3-owner." + l2

	l2OwnerInv.Invoke(t, true, registerMethod,
		l3ByL2Owner, l2OwnerAcc.ScriptHash(), email, refresh, retry, expire, ttl)

	// L3 (by L2 admin)
	const l3ByL2Admin = "l3-admin." + l2

	l2AdminAcc := inv.NewAccount(t)
	l2AdminInv := inv.WithSigners(l2AdminAcc)

	inv.WithSigners(l2OwnerAcc, l2AdminAcc).Invoke(t, stackitem.Null{}, "setAdmin", l2, l2AdminAcc.ScriptHash())

	anonymousInv.InvokeFail(t, "not witnessed by admin", registerMethod,
		l3ByL2Admin, anonymousAcc.ScriptHash(), email, refresh, retry, expire, ttl)
	l2AdminInv.Invoke(t, true, "register",
		l3ByL2Admin, l2AdminAcc.ScriptHash(), email, refresh, retry, expire, ttl)
}

func TestPredefinedTLD(t *testing.T) {
	predefined := []string{"hello", "world"}
	const otherTLD = "goodbye"

	inv := newNNSInvoker(t, false, predefined...)

	inv.Invoke(t, true, "isAvailable", otherTLD)

	for i := range predefined {
		inv.Invoke(t, false, "isAvailable", predefined[i])
	}
}

func TestNNSTLD(t *testing.T) {
	const tld = "any-tld"
	const tldFailMsg = "token not found"
	const recTyp = int64(recordtype.TXT) // InvokeFail doesn't support recordtype.Type

	inv := newNNSInvoker(t, false, tld)

	inv.InvokeFail(t, tldFailMsg, "addRecord", tld, recTyp, "any data")
	inv.InvokeFail(t, tldFailMsg, "deleteRecords", tld, recTyp)
	inv.InvokeFail(t, tldFailMsg, "getAllRecords", tld)
	inv.InvokeFail(t, tldFailMsg, "getRecords", tld, recTyp)
	inv.Invoke(t, false, "isAvailable", tld)
	inv.InvokeFail(t, tldFailMsg, "ownerOf", tld)
	inv.InvokeFail(t, tldFailMsg, "properties", tld)
	inv.InvokeAndCheck(t, func(t testing.TB, stack []stackitem.Item) {}, "renew", tld)
	inv.InvokeFail(t, tldFailMsg, "resolve", tld, recTyp)
	inv.InvokeFail(t, tldFailMsg, "setAdmin", tld, util.Uint160{})
	inv.InvokeFail(t, tldFailMsg, "setRecord", tld, recTyp, 1, "any data")
	inv.InvokeFail(t, tldFailMsg, "transfer", util.Uint160{}, tld, nil)
	inv.Invoke(t, stackitem.Null{}, "updateSOA", tld, "user@domain.org", 0, 1, 2, 3)
}

func TestNNSRoots(t *testing.T) {
	tlds := []string{"hello", "world"}

	inv := newNNSInvoker(t, false, tlds...)

	stack, err := inv.TestInvoke(t, "roots")
	require.NoError(t, err)
	require.NotEmpty(t, stack)

	it, ok := stack.Pop().Value().(*storage.Iterator)
	require.True(t, ok)

	var res []string

	for it.Next() {
		item := it.Value()

		b, err := item.TryBytes()
		require.NoError(t, err)

		res = append(res, string(b))
	}

	require.ElementsMatch(t, tlds, res)
}

func TestNNSAddRecord(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)
	for i := 0; i <= maxRecordID+1; i++ {
		if i == maxRecordID+1 {
			c.InvokeFail(t, "maximum number of records reached", "addRecord", "testdomain.com", int64(recordtype.TXT), strconv.Itoa(i))
		} else {
			c.Invoke(t, stackitem.Null{}, "addRecord", "testdomain.com", int64(recordtype.TXT), strconv.Itoa(i))
		}
	}
}
