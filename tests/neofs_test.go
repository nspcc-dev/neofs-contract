package tests

import (
	"path"
	"slices"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/native/nativenames"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/stretchr/testify/require"
)

const neofsPath = "../contracts/neofs"

func deployNeoFSContract(t *testing.T, e *neotest.Executor, addrProc util.Uint160,
	pubs keys.PublicKeys, config ...any) util.Uint160 {
	args := make([]any, 5)
	args[0] = false
	args[1] = addrProc

	arr := make([]any, len(pubs))
	for i := range pubs {
		arr[i] = pubs[i].Bytes()
	}
	args[2] = arr
	args[3] = config

	c := neotest.CompileFile(t, e.CommitteeHash, neofsPath, path.Join(neofsPath, "config.yml"))
	e.DeployContract(t, c, args)
	return c.Hash
}

func newNeoFSInvoker(t *testing.T, n int, config ...any) (*neotest.ContractInvoker, neotest.Signer, keys.PublicKeys) {
	e := newExecutor(t)

	accounts := make([]*wallet.Account, n)
	for i := range accounts {
		acc, err := wallet.NewAccount()
		require.NoError(t, err)

		accounts[i] = acc
	}

	slices.SortFunc(accounts, func(a, b *wallet.Account) int {
		return a.PublicKey().Cmp(b.PublicKey())
	})

	pubs := make(keys.PublicKeys, n)
	for i := range accounts {
		pubs[i] = accounts[i].PrivateKey().PublicKey()
	}

	m := smartcontract.GetMajorityHonestNodeCount(len(accounts))
	for i := range accounts {
		require.NoError(t, accounts[i].ConvertMultisig(m, pubs.Copy()))
	}

	alphabet := neotest.NewMultiSigner(accounts...)
	h := deployNeoFSContract(t, e, util.Uint160{}, pubs, config...)

	gasHash, err := e.Chain.GetNativeContractScriptHash(nativenames.Gas)
	require.NoError(t, err)

	vc := e.CommitteeInvoker(gasHash).WithSigners(e.Validator)
	vc.Invoke(t, true, "transfer",
		e.Validator.ScriptHash(), alphabet.ScriptHash(),
		int64(10_0000_0000), nil)

	return e.CommitteeInvoker(h).WithSigners(alphabet), alphabet, pubs
}

func TestNeoFS_AlphabetList(t *testing.T) {
	const alphabetSize = 4

	e, _, pubs := newNeoFSInvoker(t, alphabetSize)
	arr := make([]stackitem.Item, len(pubs))
	for i := range arr {
		arr[i] = stackitem.NewStruct([]stackitem.Item{
			stackitem.NewByteArray(pubs[i].Bytes()),
		})
	}

	e.Invoke(t, stackitem.NewArray(arr), "alphabetList")
}
