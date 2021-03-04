package balancecontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

type (
	// Token holds all token info.
	Token struct {
		// Ticker symbol
		Symbol string
		// Amount of decimals
		Decimals int
		// Storage key for circulation value
		CirculationKey string
	}

	Account struct {
		// Active  balance
		Balance int
		// Until valid for lock accounts
		Until int
		// Parent field used in lock accounts, used to return assets back if
		// account wasn't burnt.
		Parent []byte
	}
)

const (
	symbol      = "NEOFS"
	decimals    = 12
	circulation = "MainnetGAS"
	version     = 1

	netmapContractKey    = "netmapScriptHash"
	containerContractKey = "containerScriptHash"
)

var (
	lockTransferMsg   = []byte("lock assets to withdraw")
	unlockTransferMsg = []byte("asset lock expired")

	ctx   storage.Context
	token Token
)

// CreateToken initializes the Token Interface for the Smart Contract to operate with.
func CreateToken() Token {
	return Token{
		Symbol:         symbol,
		Decimals:       decimals,
		CirculationKey: circulation,
	}
}

func init() {
	ctx = storage.GetContext()
	token = CreateToken()
}

func Init(owner, addrNetmap, addrContainer interop.Hash160) {
	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	if len(addrNetmap) != 20 || len(addrContainer) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, common.OwnerKey, owner)
	storage.Put(ctx, netmapContractKey, addrNetmap)
	storage.Put(ctx, containerContractKey, addrContainer)

	runtime.Log("balance contract initialized")
}

func Migrate(script []byte, manifest []byte) bool {
	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("balance contract updated")

	return true
}

func Symbol() string {
	return token.Symbol
}

func Decimals() int {
	return token.Decimals
}

func TotalSupply() int {
	return token.getSupply(ctx)
}

func BalanceOf(account interop.Hash160) int {
	return token.balanceOf(ctx, account)
}

func Transfer(from, to interop.Hash160, amount int, data interface{}) bool {
	return token.transfer(ctx, from, to, amount, false, nil)
}

func TransferX(from, to interop.Hash160, amount int, details []byte) bool {
	multiaddr := common.InnerRingMultiAddressViaStorage(ctx, netmapContractKey)
	if !runtime.CheckWitness(multiaddr) {
		panic("transferX: this method must be invoked from inner ring")
	}

	result := token.transfer(ctx, from, to, amount, true, details)
	if result {
		runtime.Log("transferX: success")
	} else {
		// consider panic there
		runtime.Log("transferX: fail")
	}

	return result
}

func Lock(txID []byte, from, to interop.Hash160, amount, until int) bool {
	multiaddr := common.InnerRingMultiAddressViaStorage(ctx, netmapContractKey)
	if !runtime.CheckWitness(multiaddr) {
		panic("lock: this method must be invoked from inner ring")
	}

	lockAccount := Account{
		Balance: 0,
		Until:   until,
		Parent:  from,
	}
	common.SetSerialized(ctx, to, lockAccount)

	result := token.transfer(ctx, from, to, amount, true, lockTransferMsg)
	if !result {
		// consider using `return false` to remove votes
		panic("lock: can't lock funds")
	}

	runtime.Log("lock: created lock account")
	runtime.Notify("Lock", txID, from, to, amount, until)

	return true
}

func NewEpoch(epochNum int) bool {
	multiaddr := common.InnerRingMultiAddressViaStorage(ctx, netmapContractKey)
	if !runtime.CheckWitness(multiaddr) {
		panic("epochNum: this method must be invoked from inner ring")
	}

	it := storage.Find(ctx, []byte{}, storage.KeysOnly)
	for iterator.Next(it) {
		addr := iterator.Value(it).(interop.Hash160) // it MUST BE `storage.KeysOnly`
		if len(addr) != 20 {
			continue
		}

		acc := getAccount(ctx, addr)
		if acc.Until == 0 {
			continue
		}

		if epochNum >= acc.Until {
			// return assets back to the parent
			token.transfer(ctx, addr, acc.Parent, acc.Balance, true, unlockTransferMsg)
		}
	}

	return true
}

func Mint(to interop.Hash160, amount int, details []byte) bool {
	multiaddr := common.InnerRingMultiAddressViaStorage(ctx, netmapContractKey)
	if !runtime.CheckWitness(multiaddr) {
		panic("mint: this method must be invoked from inner ring")
	}

	ok := token.transfer(ctx, nil, to, amount, true, details)
	if !ok {
		panic("mint: can't transfer assets")
	}

	supply := token.getSupply(ctx)
	supply = supply + amount
	storage.Put(ctx, token.CirculationKey, supply)
	runtime.Log("mint: assets were minted")
	runtime.Notify("Mint", to, amount)

	return true
}

func Burn(from interop.Hash160, amount int, details []byte) bool {
	multiaddr := common.InnerRingMultiAddressViaStorage(ctx, netmapContractKey)
	if !runtime.CheckWitness(multiaddr) {
		panic("burn: this method must be invoked from inner ring")
	}

	ok := token.transfer(ctx, from, nil, amount, true, details)
	if !ok {
		panic("burn: can't transfer assets")
	}

	supply := token.getSupply(ctx)
	if supply < amount {
		panic("panic, negative supply after burn")
	}

	supply = supply - amount
	storage.Put(ctx, token.CirculationKey, supply)
	runtime.Log("burn: assets were burned")
	runtime.Notify("Burn", from, amount)

	return true
}

func Version() int {
	return version
}

// getSupply gets the token totalSupply value from VM storage.
func (t Token) getSupply(ctx storage.Context) int {
	supply := storage.Get(ctx, t.CirculationKey)
	if supply != nil {
		return supply.(int)
	}

	return 0
}

// BalanceOf gets the token balance of a specific address.
func (t Token) balanceOf(ctx storage.Context, holder interop.Hash160) int {
	acc := getAccount(ctx, holder)

	return acc.Balance
}

func (t Token) transfer(ctx storage.Context, from, to interop.Hash160, amount int, innerRing bool, details []byte) bool {
	amountFrom, ok := t.canTransfer(ctx, from, to, amount, innerRing)
	if !ok {
		return false
	}

	if len(from) == 20 {
		if amountFrom.Balance == amount {
			storage.Delete(ctx, from)
		} else {
			amountFrom.Balance = amountFrom.Balance - amount // neo-go#953
			common.SetSerialized(ctx, from, amountFrom)
		}
	}

	if len(to) == 20 {
		amountTo := getAccount(ctx, to)
		amountTo.Balance = amountTo.Balance + amount // neo-go#953
		common.SetSerialized(ctx, to, amountTo)
	}

	runtime.Notify("Transfer", from, to, amount)
	runtime.Notify("TransferX", from, to, amount, details)

	return true
}

// canTransfer returns the amount it can transfer.
func (t Token) canTransfer(ctx storage.Context, from, to interop.Hash160, amount int, innerRing bool) (Account, bool) {
	var (
		emptyAcc = Account{}
	)

	if !innerRing {
		if len(to) != 20 || !isUsableAddress(from) {
			runtime.Log("transfer: bad script hashes")
			return emptyAcc, false
		}
	} else if len(from) == 0 {
		return emptyAcc, true
	}

	amountFrom := getAccount(ctx, from)
	if amountFrom.Balance < amount {
		runtime.Log("transfer: not enough assets")
		return emptyAcc, false
	}

	// return amountFrom value back to transfer, reduces extra Get
	return amountFrom, true
}

// isUsableAddress checks if the sender is either the correct NEO address or SC address.
func isUsableAddress(addr interop.Hash160) bool {
	if len(addr) == 20 {
		if runtime.CheckWitness(addr) {
			return true
		}

		// Check if a smart contract is calling script hash
		callingScriptHash := runtime.GetCallingScriptHash()
		if common.BytesEqual(callingScriptHash, addr) {
			return true
		}
	}

	return false
}

func getAccount(ctx storage.Context, key interface{}) Account {
	data := storage.Get(ctx, key)
	if data != nil {
		return binary.Deserialize(data.([]byte)).(Account)
	}

	return Account{}
}
