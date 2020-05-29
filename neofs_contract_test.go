package smart_contract

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/compiler"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/trigger"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/emit"
	crypto "github.com/nspcc-dev/neofs-crypto"
	"github.com/nspcc-dev/neofs-crypto/test"
	"github.com/stretchr/testify/require"
)

const contractTemplate = "./neofs_contract.go"

var (
	contractHash = util.Uint160{0x1, 0x2, 0x3, 0x4}
	// token hash is not random to run tests of .avm or .go files
	contractStr = string(contractHash[:])
)

type contract struct {
	script   []byte
	privs    []*ecdsa.PrivateKey
	cgasHash string
}

func TestContract(t *testing.T) {
	const nodeCount = 6
	plug := newStoragePlugin(t)
	contract := initGoContract(t, contractTemplate, nodeCount)

	plug.cgas[contractStr] = util.Fixed8FromInt64(1000)

	var args []interface{}
	for i := range contract.privs {
		args = append(args, crypto.MarshalPublicKey(&contract.privs[i].PublicKey))
	}

	v := initVM(contract, plug)
	loadArg(t, v, "Deploy", args)
	require.NoError(t, v.Run())

	// double deploy
	v = initVM(contract, plug)
	loadArg(t, v, "Deploy", args)
	require.Error(t, v.Run())

	t.Run("Deposit", func(t *testing.T) {
		const (
			amount  = 1000
			balance = 4000
		)

		before := plug.cgas[contractStr]
		gas := util.Fixed8FromInt64(amount)

		key, err := keys.NewPublicKeyFromString("031a6c6fbbdf02ca351745fa86b9ba5a9452d785ac4f7fc2b7548ca2a46c4fcf4a")
		require.NoError(t, err)

		plug.setCGASBalance(key.Bytes(), balance)

		v := initVM(contract, plug)
		loadArg(t, v, "Deposit", []interface{}{key.Bytes(), int(gas.IntegralValue())})
		require.NoError(t, v.Run())

		require.Equal(t, before+gas, plug.cgas[contractStr])
		require.Equal(t, util.Fixed8FromInt64(balance-amount), plug.cgas[string(key.GetScriptHash().BytesBE())])
		checkNotification(t, plug.notify, []byte("Deposit"), key.Bytes(), big.NewInt(int64(gas)), []byte{})
	})

	t.Run("Withdraw", func(t *testing.T) {
		const amount = 21

		gas := util.Fixed8FromInt64(amount)

		key, err := keys.NewPublicKeyFromString("031a6c6fbbdf02ca351745fa86b9ba5a9452d785ac4f7fc2b7548ca2a46c4fcf4a")
		require.NoError(t, err)

		v := initVM(contract, plug)
		loadArg(t, v, "Withdraw", []interface{}{key.Bytes(), amount})
		require.NoError(t, v.Run())
		checkNotification(t, plug.notify, []byte("Withdraw"), key.Bytes(), big.NewInt(int64(gas)))
	})

	t.Run("Cheque", func(t *testing.T) {
		const amount = 21

		id := []byte("id")
		gas := util.Fixed8FromInt64(amount)
		user := randScriptHash()
		lockAcc := randScriptHash()
		contractGas := plug.cgas[contractStr]

		// call it threshold amount of times
		for i := 0; i < 2*nodeCount/3+1; i++ {
			v := initVM(contract, plug)

			loadArg(t, v, "Cheque", []interface{}{id, user, int(gas), lockAcc})
			require.NoError(t, v.Run())
		}

		require.Equal(t, contractGas-gas, plug.cgas[contractStr])
		require.Equal(t, gas, plug.cgas[string(user)])
		checkNotification(t, plug.notify, []byte("Cheque"), id, user, big.NewInt(int64(gas)), lockAcc)

		t.Run("Double cheque", func(t *testing.T) {
			v := initVM(contract, plug)

			loadArg(t, v, "Cheque", []interface{}{id, user, int(gas), lockAcc})
			require.Error(t, v.Run())
		})
	})

	t.Run("InnerRingCandidateAdd", func(t *testing.T) {
		v := initVM(contract, plug)
		before := plug.cgas[contractStr]

		key := crypto.MarshalPublicKey(&test.DecodeKey(1).PublicKey)
		plug.setCGASBalance(key, 4000)

		loadArg(t, v, "InnerRingCandidateAdd", []interface{}{key})
		require.NoError(t, v.Run())

		fee := util.Fixed8FromInt64(1)

		require.Equal(t, before+fee, plug.cgas[contractStr])
		require.Equal(t, util.Fixed8FromInt64(4000)-fee,
			plug.cgas[string(mustPKey(key).GetScriptHash().BytesBE())])
		require.True(t, bytes.Contains(plug.mem["InnerRingCandidates"], key))

		t.Run("Double InnerRingCandidateAdd", func(t *testing.T) {
			v := initVM(contract, plug)
			loadArg(t, v, "InnerRingCandidateAdd", []interface{}{key})
			require.Error(t, v.Run())
		})
	})

	t.Run("InnerRingCandidateRemove", func(t *testing.T) {
		key := crypto.MarshalPublicKey(&test.DecodeKey(2).PublicKey)
		plug.setCGASBalance(key, 4000)

		v := initVM(contract, plug)
		loadArg(t, v, "InnerRingCandidateAdd", []interface{}{key})
		require.NoError(t, v.Run())
		require.True(t, bytes.Contains(plug.mem["InnerRingCandidates"], key))

		t.Run("Remove unknown candidate", func(t *testing.T) {
			v := initVM(contract, plug)
			// unknown candidate
			badKey := crypto.MarshalPublicKey(&test.DecodeKey(3).PublicKey)
			loadArg(t, v, "InnerRingCandidateRemove", []interface{}{badKey})
			require.NoError(t, v.Run())
			require.True(t, bytes.Contains(plug.mem["InnerRingCandidates"], key))
		})

		v = initVM(contract, plug)
		key = mustHex("031a6c6fbbdf02ca351745fa86b9ba5a9452d785ac4f7fc2b7548ca2a46c4fcf4a")
		loadArg(t, v, "InnerRingCandidateRemove", []interface{}{key})
		require.NoError(t, v.Run())
		require.False(t, bytes.Contains(plug.mem["InnerRingCandidates"], key))
	})
}

func initGoContract(t *testing.T, path string, n int) *contract {
	f, err := os.Open(path)
	require.NoError(t, err)

	defer f.Close()

	buf, err := compiler.Compile(f)
	require.NoError(t, err)

	return &contract{script: buf, privs: getKeys(t, n)}
}

func getKeys(t *testing.T, n int) []*ecdsa.PrivateKey {
	privs := make([]*ecdsa.PrivateKey, n)
	for i := range privs {
		var err error

		privs[i], err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
	}

	return privs
}

func randScriptHash() []byte {
	var scriptHash = make([]byte, 20)
	rand.Read(scriptHash)
	return scriptHash
}

func mustHex(s string) []byte {
	result, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Errorf("invalid hex: %v", err))
	}

	return result
}

func initVM(c *contract, plug *storagePlugin) *vm.VM {
	v := vm.New()
	v.Load(c.script)
	v.SetScriptGetter(plug.getScript)
	v.RegisterInteropGetter(plug.getInterop)

	return v
}

func loadArg(t *testing.T, v *vm.VM, operation string, params []interface{}) {
	arr := make([]vm.StackItem, len(params))
	for i := range arr {
		arr[i] = toStackItem(params[i])
		require.NotNil(t, arr[i], "invalid stack item")
	}
	v.Estack().PushVal(vm.NewArrayItem(arr))
	v.Estack().PushVal(operation)
}

func toStackItem(v interface{}) vm.StackItem {
	switch val := v.(type) {
	case int:
		return vm.NewBigIntegerItem(int64(val))
	case string:
		return vm.NewByteArrayItem([]byte(val))
	case []byte:
		return vm.NewByteArrayItem(val)
	default:
		return nil
	}
}

const cgasSyscall = "MockCGAS"

type kv struct {
	Operation string
	Key       []byte
	Value     []byte
}

type storagePlugin struct {
	mem        map[string][]byte
	cgas       map[string]util.Fixed8
	interops   map[uint32]vm.InteropFunc
	storageOps []kv
	notify     []interface{}
}

func newStoragePlugin(t *testing.T) *storagePlugin {
	s := &storagePlugin{
		mem:      make(map[string][]byte),
		cgas:     make(map[string]util.Fixed8),
		interops: make(map[uint32]vm.InteropFunc),
	}

	s.interops[getID("Neo.Storage.Delete")] = s.Delete
	s.interops[getID("Neo.Storage.Get")] = s.Get
	s.interops[getID("Neo.Storage.GetContext")] = s.GetContext
	s.interops[getID("Neo.Storage.Put")] = s.Put
	s.interops[getID("Neo.Runtime.GetExecutingScriptHash")] = s.GetExecutingScriptHash
	s.interops[getID("Neo.Runtime.GetTrigger")] = s.GetTrigger
	s.interops[getID("Neo.Runtime.CheckWitness")] = s.CheckWitness
	s.interops[getID("System.ExecutionEngine.GetExecutingScriptHash")] = s.GetExecutingScriptHash
	s.interops[getID(cgasSyscall)] = s.CGASInvoke
	s.interops[getID("Neo.Runtime.Log")] = func(v *vm.VM) error {
		msg := string(v.Estack().Pop().Bytes())
		t.Log(msg)
		return nil
	}
	s.interops[getID("Neo.Runtime.Notify")] = func(v *vm.VM) error {
		val := v.Estack().Pop().Value()
		s.notify = append(s.notify, toInterface(val))
		return nil
	}

	return s
}

func toInterface(val interface{}) interface{} {
	switch v := val.(type) {
	case []vm.StackItem:
		arr := make([]interface{}, len(v))
		for i, item := range v {
			arr[i] = toInterface(item)
		}
		return arr
	case vm.StackItem:
		return toInterface(v.Value())
	default:
		return v
	}
}

func getID(name string) uint32 {
	return vm.InteropNameToID([]byte(name))
}

func (s *storagePlugin) getInterop(id uint32) *vm.InteropFuncPrice {
	f := s.interops[id]
	if f != nil {
		return &vm.InteropFuncPrice{Func: f, Price: 1}
	}

	switch id {
	case getID("Neo.Runtime.Serialize"):
	case getID("Neo.Runtime.Deserialize"):
	default:
		panic("unexpected interop")
	}

	return nil
}

func mustPKey(pub []byte) *keys.PublicKey {
	var pk keys.PublicKey
	if err := pk.DecodeBytes(pub); err != nil {
		panic(err)
	}
	return &pk
}

func (s *storagePlugin) setCGASBalance(pub []byte, amount int64) {
	pk := mustPKey(pub)
	from := string(pk.GetScriptHash().BytesBE())
	s.cgas[from] = util.Fixed8FromInt64(amount)
}

func (s *storagePlugin) CGASInvoke(v *vm.VM) error {
	op := string(v.Estack().Pop().Bytes())
	args := v.Estack().Pop().Array()

	var result bool

	switch op {
	case "transfer":
		from := args[0].Value().([]byte)
		to := args[1].Value().([]byte)
		if len(from) != 20 || len(to) != 20 {
			panic("invalid arguments")
		}

		var amount util.Fixed8
		val := args[2].Value()
		switch v := val.(type) {
		case *big.Int:
			amount = util.Fixed8(v.Int64())
		case []byte:
			amount = util.Fixed8(emit.BytesToInt(v).Int64())
		default:
			panic("invalid amount")
		}

		if s.cgas[string(from)] >= amount {
			s.cgas[string(from)] -= amount
			s.cgas[string(to)] += amount
			result = true
		}

	default:
		panic("invalid operation")
	}

	v.Estack().PushVal(result)

	return nil
}

func (s *storagePlugin) getScript(u util.Uint160) ([]byte, bool) {
	var realHash util.Uint160
	copy(realHash[:], tokenHash[:])
	if u.Equals(realHash) {
		buf := io.NewBufBinWriter()
		emit.Syscall(buf.BinWriter, cgasSyscall)
		return buf.Bytes(), false
	}
	panic("wrong script hash")
}

func (s *storagePlugin) GetTrigger(v *vm.VM) error {
	// todo: remove byte casting when neo-go issue will be resolved
	//       https: //github.com/nspcc-dev/neo-go/issues/776
	v.Estack().PushVal(byte(trigger.Application))
	return nil
}

func (s *storagePlugin) CheckWitness(v *vm.VM) error {
	v.Estack().PushVal(true)
	return nil
}

func (s *storagePlugin) GetExecutingScriptHash(v *vm.VM) error {
	var h util.Uint160
	copy(h[:], contractHash[:])
	v.Estack().PushVal(h.BytesBE())
	return nil
}

func (s *storagePlugin) logStorage(op string, key, value []byte) {
	s.storageOps = append(s.storageOps, kv{
		Operation: op,
		Key:       key,
		Value:     value,
	})
}

func (s *storagePlugin) Delete(v *vm.VM) error {
	v.Estack().Pop()
	key := v.Estack().Pop().Bytes()
	s.logStorage("Delete", key, s.mem[string(key)])
	delete(s.mem, string(key))
	return nil
}

func (s *storagePlugin) Put(v *vm.VM) error {
	v.Estack().Pop()
	key := v.Estack().Pop().Bytes()
	value := v.Estack().Pop().Bytes()
	s.logStorage("Put", key, value)
	s.mem[string(key)] = value
	return nil
}

func (s *storagePlugin) Get(v *vm.VM) error {
	v.Estack().Pop()
	item := v.Estack().Pop().Bytes()
	if val, ok := s.mem[string(item)]; ok {
		v.Estack().PushVal(val)
		s.logStorage("Get", item, val)
		return nil
	}
	v.Estack().PushVal([]byte{})
	s.logStorage("Get", item, nil)
	return nil
}

func (s *storagePlugin) GetContext(v *vm.VM) error {
	// Pushing anything on the stack here will work. This is just to satisfy
	// the compiler, thinking it has pushed the context ^^.
	v.Estack().PushVal(10)
	return nil
}

func checkNotification(t *testing.T, store []interface{}, args ...interface{}) {
	ln := len(store)
	require.True(t, ln > 0)

	notification := store[ln-1].([]interface{})
	require.Equal(t, len(args), len(notification))

	for i := range args {
		require.Equal(t, args[i], notification[i])
	}
}
