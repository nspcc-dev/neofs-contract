package migration

import (
	"encoding/binary"
	"encoding/json"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/core"
	"github.com/nspcc-dev/neo-go/pkg/core/dao"
	"github.com/nspcc-dev/neo-go/pkg/core/native"
	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/core/storage"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/nns"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/vm/vmstate"
	"github.com/nspcc-dev/neofs-contract/tests"
	"github.com/nspcc-dev/neofs-contract/tests/dump"
	"github.com/stretchr/testify/require"
)

// Contract provides part of Neo blockchain services primarily related to the
// NeoFS contract being tested. Initial state of the tested contract is
// initialized from the dump of the blockchain in which it has already been
// deployed (mostly with real data from public networks, but custom options are
// also possible). The contract itself is identified by its name registered in
// the NeoFS NNS. After preparing the test shell of the blockchain from the
// input data, the contract can be updated using the appropriate methods.
// Contract also provides data access interfaces that can be used to ensure that
// data is migrated correctly.
//
// Contract instances must be constructed using NewContract.
type Contract struct {
	id int32

	exec *neotest.Executor

	invoker *neotest.ContractInvoker

	bNEF      []byte
	jManifest []byte
}

// ContractOptions groups various options of NewContract.
type ContractOptions struct {
	// Path to the directory containing source code of the tested NeoFS contract.
	// Defaults to '../name'.
	SourceCodeDir string

	// Listener of storage dump of the tested contract. Useful for working with raw
	// values that can not be accessed by the contract API.
	StorageDumpHandler func(key, value []byte)
}

// NewContract constructs Contract from provided dump.Reader for the named NeoFS
// contract.
//
// The Contract is initialized with all contracts (states and data) from the
// dump.Reader. If you need to process storage items of the tested contract
// before the chain is initialized, use ContractOptions.StorageDumpHandler. If
// set, NewContract passes each key-value item into the function.
//
// By default, new version of the contract executable is compiled from '../name'
// directory. The path can be overridden by ContractOptions.SourceCodeDir.
//
// To work with NNS, NewContract compiles NeoFS NNS contract from the source located
// in '../nns' directory and deploys it.
func NewContract(tb testing.TB, d *dump.Reader, name string, opts ContractOptions) *Contract {
	lowLevelStore := storage.NewMemoryStore()
	cachedStore := storage.NewMemCachedStore(lowLevelStore) // mem-cached store has sweeter interface
	_dao := dao.NewSimple(lowLevelStore, false, true)

	var id int32
	found := false

	nativeContracts := native.NewContracts(config.ProtocolConfiguration{})

	err := nativeContracts.Management.InitializeCache(_dao)
	require.NoError(tb, err)

	mNameToID := make(map[string]int32)

	err = d.IterateContractStates(func(_name string, _state state.Contract) {
		_state.UpdateCounter = 0 // contract could be dumped as already updated

		err = native.PutContractState(_dao, &_state)
		require.NoError(tb, err)

		if !found {
			found = _name == name
			if found {
				id = _state.ID
			}
		}

		mNameToID[_name] = _state.ID
	})
	require.NoError(tb, err)
	require.True(tb, found)

	err = d.IterateContractStorages(func(_name string, key, value []byte) {
		if opts.StorageDumpHandler != nil && _name == name {
			opts.StorageDumpHandler(key, value)
		}

		id, ok := mNameToID[_name]
		require.True(tb, ok)

		storageKey := make([]byte, 5+len(key))
		storageKey[0] = byte(_dao.Version.StoragePrefix)
		binary.LittleEndian.PutUint32(storageKey[1:], uint32(id))
		copy(storageKey[5:], key)

		cachedStore.Put(storageKey, value)
	})

	_, err = _dao.PersistSync()
	require.NoError(tb, err)

	_, err = cachedStore.PersistSync()
	require.NoError(tb, err)

	// init test blockchain
	useDefaultConfig := func(*config.Blockchain) {}
	var blockChain *core.Blockchain

	{ // FIXME: hack area, track neo-go#2926
		// contracts embedded in the blockchain the moment before are not visible unless
		// the blockchain is run twice. At the same time, in order not to clear the
		// storage, method Close is overridden.
		var run bool // otherwise on tb.Cleanup will panic which is not critical, but not pleasant either
		blockChain, _ = chain.NewSingleWithCustomConfigAndStore(tb, useDefaultConfig, nopCloseStore{lowLevelStore}, run)
		go blockChain.Run()
		blockChain.Close()
	}

	blockChain, alphabetSigner := chain.NewSingleWithCustomConfigAndStore(tb, useDefaultConfig, lowLevelStore, true)

	exec := neotest.NewExecutor(tb, blockChain, alphabetSigner, alphabetSigner)

	// compile and deploy NNS contract
	const nnsSourceCodeDir = "../nns"
	exec.DeployContract(tb,
		neotest.CompileFile(tb, exec.CommitteeHash, nnsSourceCodeDir, filepath.Join(nnsSourceCodeDir, "config.yml")),
		[]interface{}{},
	)

	// compile new contract version
	if opts.SourceCodeDir == "" {
		opts.SourceCodeDir = filepath.Join("..", name)
	}

	ctr := neotest.CompileFile(tb, exec.CommitteeHash, opts.SourceCodeDir, filepath.Join(opts.SourceCodeDir, "config.yml"))

	bNEF, err := ctr.NEF.Bytes()
	require.NoError(tb, err)

	jManifest, err := json.Marshal(ctr.Manifest)
	require.NoError(tb, err)

	return &Contract{
		id:        id,
		exec:      exec,
		invoker:   exec.NewInvoker(exec.ContractHash(tb, id), alphabetSigner),
		bNEF:      bNEF,
		jManifest: jManifest,
	}
}

func (x *Contract) checkUpdate(tb testing.TB, faultException string, args ...interface{}) {
	const updateMethod = "update"

	if faultException != "" {
		x.invoker.InvokeFail(tb, faultException, updateMethod, x.bNEF, x.jManifest, args)
		return
	}

	var noResult stackitem.Null
	x.invoker.Invoke(tb, noResult, updateMethod, x.bNEF, x.jManifest, args)
}

// CheckUpdateSuccess tests that contract update with given arguments succeeds.
// Contract executable (NEF and manifest) is compiled from source code (see
// NewContract for details).
func (x *Contract) CheckUpdateSuccess(tb testing.TB, args ...interface{}) {
	x.checkUpdate(tb, "", args...)
}

// CheckUpdateFail tests that contract update with given arguments fails with exact fault
// exception.
//
// See also CheckUpdateSuccess.
func (x *Contract) CheckUpdateFail(tb testing.TB, faultException string, args ...interface{}) {
	x.checkUpdate(tb, faultException, args...)
}

func makeTestInvoke(tb testing.TB, inv *neotest.ContractInvoker, method string, args ...interface{}) stackitem.Item {
	vmStack, err := inv.TestInvoke(tb, method, args...)
	require.NoError(tb, err, "method '%s'", method)

	// FIXME: temp hack
	res, err := unwrap.Item(&result.Invoke{
		State: vmstate.Halt.String(),
		Stack: vmStack.ToArray(),
	}, nil)
	require.NoError(tb, err)

	return res
}

// Call tests that calling the contract method with optional arguments succeeds
// and result contains single value. The resulting value is returned as
// stackitem.Item.
//
// Note that Call doesn't change the chain state, so only read (aka safe)
// methods should be used.
func (x *Contract) Call(tb testing.TB, method string, args ...interface{}) stackitem.Item {
	return makeTestInvoke(tb, x.invoker, method, args...)
}

// GetStorageItem returns value stored in the tested contract by key.
func (x *Contract) GetStorageItem(key []byte) []byte {
	return x.exec.Chain.GetStorageItem(x.id, key)
}

// RegisterContractInNNS binds given address to the contract referenced by
// provided name via additional record for the 'name.neofs' domain in the NeoFS
// NNS contract. The method is useful when tested contract uses NNS to access
// other contracts.
//
// Record format can be either Neo address or little-endian HEX string. The
// exact format is selected randomly.
//
// See also nns.Register, nns.AddRecord.
func (x *Contract) RegisterContractInNNS(tb testing.TB, name string, addr util.Uint160) {
	nnsInvoker := x.exec.CommitteeInvoker(x.exec.ContractHash(tb, 1))
	nnsInvoker.InvokeAndCheck(tb, checkSingleTrueInStack, "register",
		"neofs",
		x.exec.CommitteeHash,
		"ops@morphbits.io",
		int64(3600),
		int64(600),
		int64(10*365*24*time.Hour/time.Second),
		int64(3600),
	)

	domain := name + ".neofs"

	nnsInvoker.InvokeAndCheck(tb, checkSingleTrueInStack, "register",
		domain,
		x.exec.CommitteeHash,
		"ops@morphbits.io",
		int64(3600),
		int64(600),
		int64(10*365*24*time.Hour/time.Second),
		int64(3600),
	)

	var rec string
	if rand.Int()%2 == 0 {
		rec = address.Uint160ToString(addr)
	} else {
		rec = addr.StringLE()
	}

	var noResult stackitem.Null
	nnsInvoker.Invoke(tb, noResult,
		"addRecord", domain, int64(nns.TXT), rec,
	)
}

// SetInnerRing sets Inner Ring composition using RoleManagement contract. The
// list can be read via InnerRing.
func (x *Contract) SetInnerRing(tb testing.TB, _keys keys.PublicKeys) {
	tests.SetInnerRing(tb, x.exec, _keys)
}

// InnerRing reads Inner Ring composition using RoleManagement contract. The
// list can be set via SetInnerRing.
func (x *Contract) InnerRing(tb testing.TB) keys.PublicKeys {
	return tests.InnerRing(tb, x.exec)
}
