package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/native/nativenames"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/container"
	"github.com/stretchr/testify/require"
)

const alphabetPath = "../contracts/alphabet"

func deployAlphabetContract(t *testing.T, e *neotest.Executor, addrNetmap, addrProxy *util.Uint160, name string, index, total int64) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, alphabetPath, path.Join(alphabetPath, "config.yml"))

	args := make([]any, 6)
	args[0] = false
	args[1] = addrNetmap
	args[2] = addrProxy
	args[3] = name
	args[4] = index
	args[5] = total

	e.DeployContract(t, c, args)
	return c.Hash
}

func newAlphabetInvoker(t *testing.T, autohashes bool) (*neotest.Executor, *neotest.ContractInvoker) {
	e := newExecutor(t)

	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	ctrProxy := neotest.CompileFile(t, e.CommitteeHash, proxyPath, path.Join(proxyPath, "config.yml"))

	nnsHash := deployDefaultNNS(t, e)
	deployNetmapContract(t, e, container.RegistrationFeeKey, int64(containerFee),
		container.AliasFeeKey, int64(containerAliasFee))
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	deployContainerContract(t, e, &ctrNetmap.Hash, &ctrBalance.Hash, &nnsHash)
	deployProxyContract(t, e)

	var addrNetmap, addrProxy *util.Uint160
	if !autohashes {
		addrNetmap, addrProxy = &ctrNetmap.Hash, &ctrProxy.Hash
	}
	hash := deployAlphabetContract(t, e, addrNetmap, addrProxy, "Az", 0, 1)

	alphabet := getAlphabetAcc(t, e)

	setAlphabetRole(t, e, alphabet.PrivateKey().PublicKey().Bytes())

	return e, e.CommitteeInvoker(hash)
}

func TestEmit(t *testing.T) {
	for autohashes, name := range map[bool]string{
		false: "standard deploy",
		true:  "deploy with no hashes",
	} {
		t.Run(name, func(t *testing.T) {
			_, c := newAlphabetInvoker(t, autohashes)

			const method = "emit"

			alphabet := getAlphabetAcc(t, c.Executor)

			cCommittee := c.WithSigners(neotest.NewSingleSigner(alphabet))
			cCommittee.InvokeFail(t, "no gas to emit", method)

			transferNeoToContract(t, c)

			cCommittee.Invoke(t, stackitem.Null{}, method)

			notAlphabet := c.NewAccount(t)
			cNotAlphabet := c.WithSigners(notAlphabet)

			cNotAlphabet.InvokeFail(t, "invalid invoker", method)
		})
	}
}

func TestVote(t *testing.T) {
	for autohashes, name := range map[bool]string{
		false: "standard deploy",
		true:  "deploy with no hashes",
	} {
		t.Run(name, func(t *testing.T) {
			e, c := newAlphabetInvoker(t, autohashes)

			const method = "vote"

			newAlphabet := c.NewAccount(t)
			newAlphabetPub, ok := vm.ParseSignatureContract(newAlphabet.Script())
			require.True(t, ok)
			cNewAlphabet := c.WithSigners(newAlphabet)

			cNewAlphabet.InvokeFail(t, common.ErrAlphabetWitnessFailed, method, int64(0), []any{newAlphabetPub})
			c.InvokeFail(t, "invalid epoch", method, int64(1), []any{newAlphabetPub})

			setAlphabetRole(t, e, newAlphabetPub)
			transferNeoToContract(t, c)

			neoSH := e.NativeHash(t, nativenames.Neo)
			neoInvoker := c.CommitteeInvoker(neoSH)

			gasSH := e.NativeHash(t, nativenames.Gas)
			gasInvoker := e.CommitteeInvoker(gasSH)

			res, err := gasInvoker.TestInvoke(t, "balanceOf", gasInvoker.Committee.ScriptHash())
			require.NoError(t, err)

			// transfer some GAS to the new alphabet node
			gasInvoker.Invoke(t, stackitem.NewBool(true), "transfer", gasInvoker.Committee.ScriptHash(), newAlphabet.ScriptHash(), res.Top().BigInt().Int64()/2, nil)

			newInvoker := neoInvoker.WithSigners(newAlphabet)

			newInvoker.Invoke(t, stackitem.NewBool(true), "registerCandidate", newAlphabetPub)
			c.Invoke(t, stackitem.Null{}, method, int64(0), []any{newAlphabetPub})

			// wait one block util
			// a new committee is accepted
			c.AddNewBlock(t)

			cNewAlphabet.Invoke(t, stackitem.Null{}, "emit")
			c.InvokeFail(t, "invalid invoker", "emit")
		})
	}
}

func transferNeoToContract(t *testing.T, invoker *neotest.ContractInvoker) {
	neoSH, err := invoker.Chain.GetNativeContractScriptHash(nativenames.Neo)
	require.NoError(t, err)

	neoInvoker := invoker.CommitteeInvoker(neoSH)

	res, err := neoInvoker.TestInvoke(t, "balanceOf", neoInvoker.Committee.ScriptHash())
	require.NoError(t, err)

	// transfer all NEO to alphabet contract
	neoInvoker.Invoke(t, stackitem.NewBool(true), "transfer", neoInvoker.Committee.ScriptHash(), invoker.Hash, res.Top().BigInt().Int64(), nil)
}

func getAlphabetAcc(t *testing.T, e *neotest.Executor) *wallet.Account {
	multi, ok := e.Committee.(neotest.MultiSigner)
	require.True(t, ok)

	return multi.Single(0).Account()
}
