package tests

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/config/netmode"
	"github.com/nspcc-dev/neo-go/pkg/core"
	"github.com/nspcc-dev/neo-go/pkg/core/block"
	"github.com/nspcc-dev/neo-go/pkg/core/fee"
	"github.com/nspcc-dev/neo-go/pkg/core/native"
	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/core/storage"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/callflag"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/trigger"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/emit"
	"github.com/nspcc-dev/neo-go/pkg/vm/opcode"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const validatorWIF = "KxyjQ8eUa4FHt3Gvioyt1Wz29cTUrE4eTqX3yFSk1YFCsPL8uNsY"

// CommitteeAcc is an account used to sign tx as a committee.
var CommitteeAcc *wallet.Account

func init() {
	CommitteeAcc, _ = wallet.NewAccountFromWIF(validatorWIF)
	pubs := keys.PublicKeys{CommitteeAcc.PrivateKey().PublicKey()}
	err := CommitteeAcc.ConvertMultisig(1, pubs)
	if err != nil {
		panic(err)
	}
}

var _nonce uint32

func nonce() uint32 {
	_nonce++
	return _nonce
}

// NewChain creates new blockchain instance with a single validator and
// setups cleanup functions.
func NewChain(t *testing.T) *core.Blockchain {
	protoCfg := config.ProtocolConfiguration{
		Magic:              netmode.UnitTestNet,
		P2PSigExtensions:   true,
		SecondsPerBlock:    1,
		StandbyCommittee:   []string{hex.EncodeToString(CommitteeAcc.PrivateKey().PublicKey().Bytes())},
		ValidatorsCount:    1,
		VerifyBlocks:       true,
		VerifyTransactions: true,
	}

	st := storage.NewMemoryStore()
	log := zaptest.NewLogger(t)
	bc, err := core.NewBlockchain(st, protoCfg, log)
	require.NoError(t, err)
	go bc.Run()
	t.Cleanup(bc.Close)
	return bc
}

// PrepareInvoke creates new invocation transaction.
// Signer can be either bool or *wallet.Account.
// In the first case `true` means sign by committee, `false` means sign by validators.
func PrepareInvoke(t *testing.T, bc *core.Blockchain, signer interface{},
	hash util.Uint160, method string, args ...interface{}) *transaction.Transaction {
	w := io.NewBufBinWriter()
	emit.AppCall(w.BinWriter, hash, method, callflag.All, args...)
	require.NoError(t, w.Err)

	script := w.Bytes()
	tx := transaction.New(script, 0)
	tx.Nonce = nonce()
	tx.ValidUntilBlock = bc.BlockHeight() + 1

	switch s := signer.(type) {
	case *wallet.Account:
		tx.Signers = append(tx.Signers, transaction.Signer{
			Account: s.Contract.ScriptHash(),
			Scopes:  transaction.Global,
		})
		require.NoError(t, addNetworkFee(bc, tx, s))
		require.NoError(t, addSystemFee(bc, tx))
		require.NoError(t, s.SignTx(netmode.UnitTestNet, tx))
	case []*wallet.Account:
		for _, acc := range s {
			tx.Signers = append(tx.Signers, transaction.Signer{
				Account: acc.Contract.ScriptHash(),
				Scopes:  transaction.Global,
			})
			require.NoError(t, addNetworkFee(bc, tx, acc))
		}
		require.NoError(t, addSystemFee(bc, tx))
		for _, acc := range s {
			require.NoError(t, acc.SignTx(netmode.UnitTestNet, tx))
		}
	default:
		panic("invalid signer")
	}

	return tx
}

// NewAccount creates new account and transfers 100.0 GAS to it.
func NewAccount(t *testing.T, bc *core.Blockchain) *wallet.Account {
	acc, err := wallet.NewAccount()
	require.NoError(t, err)

	tx := PrepareInvoke(t, bc, CommitteeAcc,
		bc.UtilityTokenHash(), "transfer",
		CommitteeAcc.Contract.ScriptHash(), acc.Contract.ScriptHash(), int64(100_0000_0000), nil)
	AddBlock(t, bc, tx)
	CheckHalt(t, bc, tx.Hash())
	return acc
}

// DeployContract compiles and deploys contract to bc.
// path should contain Go source files.
// data is an optional argument to `_deploy`.
func DeployContract(t *testing.T, bc *core.Blockchain, path string, data interface{}) util.Uint160 {
	tx, h := newDeployTx(t, bc, path, data)
	AddBlock(t, bc, tx)
	CheckHalt(t, bc, tx.Hash())
	return h
}

// CheckHalt checks that transaction persisted with HALT state.
func CheckHalt(t *testing.T, bc *core.Blockchain, h util.Uint256, stack ...stackitem.Item) *state.AppExecResult {
	aer, err := bc.GetAppExecResults(h, trigger.Application)
	require.NoError(t, err)
	require.Equal(t, vm.HaltState, aer[0].VMState, aer[0].FaultException)
	if len(stack) != 0 {
		require.Equal(t, stack, aer[0].Stack)
	}
	return &aer[0]
}

// CheckFault checks that transaction persisted with FAULT state.
// Raised exception is also checked to contain s as a substring.
func CheckFault(t *testing.T, bc *core.Blockchain, h util.Uint256, s string) {
	aer, err := bc.GetAppExecResults(h, trigger.Application)
	require.NoError(t, err)
	require.Equal(t, vm.FaultState, aer[0].VMState)
	require.True(t, strings.Contains(aer[0].FaultException, s),
		"expected: %s, got: %s", s, aer[0].FaultException)
}

// newDeployTx returns new deployment tx for contract.
func newDeployTx(t *testing.T, bc *core.Blockchain, ctrPath string, data interface{}) (*transaction.Transaction, util.Uint160) {
	c, err := ContractInfo(CommitteeAcc.Contract.ScriptHash(), ctrPath)
	require.NoError(t, err)

	rawManifest, err := json.Marshal(c.Manifest)
	require.NoError(t, err)

	neb, err := c.NEF.Bytes()
	require.NoError(t, err)

	buf := io.NewBufBinWriter()
	emit.AppCall(buf.BinWriter, bc.ManagementContractHash(), "deploy", callflag.All, neb, rawManifest, data)
	require.NoError(t, buf.Err)

	tx := transaction.New(buf.Bytes(), 100*native.GASFactor)
	tx.Nonce = nonce()
	tx.ValidUntilBlock = bc.BlockHeight() + 1
	tx.Signers = []transaction.Signer{{
		Account: CommitteeAcc.Contract.ScriptHash(),
		Scopes:  transaction.Global,
	}}
	require.NoError(t, addNetworkFee(bc, tx, CommitteeAcc))
	require.NoError(t, CommitteeAcc.SignTx(netmode.UnitTestNet, tx))
	return tx, c.Hash
}

func addSystemFee(bc *core.Blockchain, tx *transaction.Transaction) error {
	v, _ := TestInvoke(bc, tx) // ignore error to support failing transactions
	tx.SystemFee = v.GasConsumed()
	return nil
}

func addNetworkFee(bc *core.Blockchain, tx *transaction.Transaction, sender *wallet.Account) error {
	size := io.GetVarSize(tx)
	netFee, sizeDelta := fee.Calculate(bc.GetBaseExecFee(), sender.Contract.Script)
	tx.NetworkFee += netFee
	size += sizeDelta
	for _, cosigner := range tx.Signers {
		contract := bc.GetContractState(cosigner.Account)
		if contract != nil {
			netFee, sizeDelta = fee.Calculate(bc.GetBaseExecFee(), contract.NEF.Script)
			tx.NetworkFee += netFee
			size += sizeDelta
		}
	}
	tx.NetworkFee += int64(size) * bc.FeePerByte()
	return nil
}

// AddBlock creates a new block from provided transactions and adds it on bc.
func AddBlock(t *testing.T, bc *core.Blockchain, txs ...*transaction.Transaction) *block.Block {
	lastBlock, err := bc.GetBlock(bc.GetHeaderHash(int(bc.BlockHeight())))
	require.NoError(t, err)
	b := &block.Block{
		Header: block.Header{
			NextConsensus: CommitteeAcc.Contract.ScriptHash(),
			Script: transaction.Witness{
				VerificationScript: CommitteeAcc.Contract.Script,
			},
			Timestamp: lastBlock.Timestamp + 1,
		},
		Transactions: txs,
	}
	b.PrevHash = lastBlock.Hash()
	b.Index = bc.BlockHeight() + 1
	b.RebuildMerkleRoot()

	sign := CommitteeAcc.PrivateKey().SignHashable(uint32(netmode.UnitTestNet), b)
	b.Script.InvocationScript = append([]byte{byte(opcode.PUSHDATA1), 64}, sign...)
	require.NoError(t, bc.AddBlock(b))

	return b
}

// AddBlockCheckHalt is a convenient wrapper over AddBlock and CheckHalt.
func AddBlockCheckHalt(t *testing.T, bc *core.Blockchain, txs ...*transaction.Transaction) *block.Block {
	b := AddBlock(t, bc, txs...)
	for _, tx := range txs {
		CheckHalt(t, bc, tx.Hash())
	}
	return b
}

// CheckTestInvoke executes transaction without persisting it's state and
// compares the result with the expected.
func CheckTestInvoke(t *testing.T, bc *core.Blockchain, tx *transaction.Transaction, expected interface{}) {
	v, err := TestInvoke(bc, tx)
	require.NoError(t, err)
	require.Equal(t, 1, v.Estack().Len())
	require.Equal(t, stackitem.Make(expected), v.Estack().Pop().Item())
}

// TestInvoke creates a test VM with dummy block and executes transaction in it.
func TestInvoke(bc *core.Blockchain, tx *transaction.Transaction) (*vm.VM, error) {
	lastBlock, err := bc.GetBlock(bc.GetHeaderHash(int(bc.BlockHeight())))
	if err != nil {
		return nil, err
	}
	b := &block.Block{Header: block.Header{
		Index:     bc.BlockHeight() + 1,
		Timestamp: lastBlock.Timestamp + 1,
	}}
	v := bc.GetTestVM(trigger.Application, tx, b)
	v.LoadWithFlags(tx.Script, callflag.All)
	err = v.Run()
	return v, err
}
