package tests

import (
	"path"
	"strconv"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/core/native/nativenames"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/scparser"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
	"github.com/stretchr/testify/require"
)

const alphabetPath = "../contracts/alphabet"

const neoTotalSupply = 1_0000_0000

func deployAlphabetContract(t *testing.T, e *neotest.Executor, sender neotest.Signer, addrNetmap, addrProxy *util.Uint160, name string, index int) util.Uint160 {
	transferGasToAccount(t, e, sender)

	c := neotest.CompileFile(t, sender.ScriptHash(), alphabetPath, path.Join(alphabetPath, "config.yml"))

	args := make([]any, 6)
	args[0] = false
	args[1] = addrNetmap
	args[2] = addrProxy
	args[3] = name
	args[4] = index

	e.DeployContractBy(t, sender, c, args)
	return c.Hash
}

func newAlphabetInvoker(t *testing.T, autohashes bool, multi bool) (*neotest.Executor, []*neotest.ContractInvoker) {
	var e *neotest.Executor

	if multi {
		bc, vals, comm := chain.NewMultiWithCustomConfig(t, func(c *config.Blockchain) {
			c.Hardforks = nil // Enable all of them, contracts depend on it.
		})
		e = neotest.NewExecutor(t, bc, vals, comm)
	} else {
		e = newExecutor(t)
	}

	nnsHash := deployDefaultNNS(t, e)
	netmapHash := deployNetmapContract(t, e, containerconst.RegistrationFeeKey, int64(containerFee),
		containerconst.AliasFeeKey, int64(containerAliasFee))
	balanceHash := deployBalanceContract(t, e)
	proxyHash := deployProxyContract(t, e)
	deployContainerContract(t, e, &netmapHash, &balanceHash, &nnsHash)

	var addrNetmap, addrProxy *util.Uint160
	if !autohashes {
		addrNetmap, addrProxy = &netmapHash, &proxyHash
	}

	var invokers []*neotest.ContractInvoker

	accs := getAlphabetAccs(t, e)
	for i, acc := range accs {
		ss := neotest.NewSingleSigner(acc)
		h := deployAlphabetContract(t, e, ss, addrNetmap, addrProxy, "alpha"+strconv.Itoa(i), i)
		invokers = append(invokers, e.NewInvoker(h, ss))
	}

	SetInnerRing(t, e, keys.PublicKeys(e.Chain.ComputeNextBlockValidators()))

	return e, invokers
}

func TestEmit(t *testing.T) {
	for autohashes, name := range map[bool]string{
		false: "standard deploy",
		true:  "deploy with no hashes",
	} {
		t.Run(name, func(t *testing.T) {
			_, cs := newAlphabetInvoker(t, autohashes, true)

			const method = "emit"

			for _, c := range cs {
				c.InvokeFail(t, "no gas to emit", method)
			}

			for _, c := range cs {
				transferNeoToContract(t, c, int64(neoTotalSupply/len(cs)))
			}

			for _, c := range cs {
				c.Invoke(t, stackitem.Null{}, method)
			}

			notAlphabet := cs[0].NewAccount(t)
			cNotAlphabet := cs[0].WithSigners(notAlphabet)

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
			e, cs := newAlphabetInvoker(t, autohashes, false)

			c := cs[0]
			cComm := c.CommitteeInvoker(c.Hash)

			const method = "vote"

			newAlphabet := c.NewAccount(t)
			newAlphabetPub, ok := scparser.ParseSignatureContract(newAlphabet.Script())
			require.True(t, ok)
			cNewAlphabet := c.WithSigners(newAlphabet)

			cNewAlphabet.InvokeFail(t, common.ErrAlphabetWitnessFailed, method, int64(0), []any{newAlphabetPub})
			cComm.InvokeFail(t, "invalid epoch", method, int64(1), []any{newAlphabetPub})

			setAlphabetRole(t, e, newAlphabetPub)
			transferNeoToContract(t, c, neoTotalSupply)

			neoSH := e.NativeHash(t, nativenames.Neo)
			neoInvoker := c.CommitteeInvoker(neoSH)

			// set registration price to minimum so the new alphabet node can afford it
			neoInvoker.Invoke(t, stackitem.Null{}, "setRegisterPrice", int64(1))

			transferGasToAccount(t, e, c.Signers[0])

			gasSH := e.NativeHash(t, nativenames.Gas)
			gasInvoker := e.CommitteeInvoker(gasSH)

			// register new alphabet node as candidate via NEP-27: transfer GAS to NEO contract
			gasInvoker.WithSigners(newAlphabet).Invoke(t, stackitem.NewBool(true), "transfer", newAlphabet.ScriptHash(), neoSH, int64(1), newAlphabetPub)
			cComm.Invoke(t, stackitem.Null{}, method, int64(0), []any{newAlphabetPub})

			// wait one block util
			// a new committee is accepted
			c.AddNewBlock(t)

			cNewAlphabet.Invoke(t, stackitem.Null{}, "emit")
			c.InvokeFail(t, "invalid invoker", "emit")
		})
	}
}

func transferNeoToContract(t *testing.T, invoker *neotest.ContractInvoker, amount int64) {
	neoSH, err := invoker.Chain.GetNativeContractScriptHash(nativenames.Neo)
	require.NoError(t, err)

	neoInvoker := invoker.ValidatorInvoker(neoSH)

	// transfer required amount of NEO to alphabet contract
	neoInvoker.Invoke(t, stackitem.NewBool(true), "transfer", neoInvoker.Validator.ScriptHash(), invoker.Hash, amount, nil)
}

func transferGasToAccount(t *testing.T, e *neotest.Executor, sender neotest.Signer) {
	gasSH, err := e.Chain.GetNativeContractScriptHash(nativenames.Gas)
	require.NoError(t, err)

	gasInvoker := e.ValidatorInvoker(gasSH)
	gasInvoker.Invoke(t, stackitem.NewBool(true), "transfer", gasInvoker.Validator.ScriptHash(), sender.ScriptHash(), 1000_0000_0000, nil)
}

func TestAlphabetVerify(t *testing.T) {
	_, contract := newAlphabetInvoker(t, false, false)
	testVerify(t, contract[0].CommitteeInvoker(contract[0].Hash))
}
