package balance

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
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

	// Account structure stores metadata of each NeoFS balance account.
	Account struct {
		// Active balance
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
)

var token Token

func createToken() Token {
	return Token{
		Symbol:         symbol,
		Decimals:       decimals,
		CirculationKey: circulation,
	}
}

func init() {
	token = createToken()
}

// nolint:deadcode,unused
func _deploy(data any, isUpdate bool) {
	ctx := storage.GetContext()
	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)

		common.CheckVersion(version)

		// switch to notary mode if version of the current contract deployment is
		// earlier than v0.17.0 (initial version when non-notary mode was taken out of
		// use)
		// TODO: avoid number magic, add function for version comparison to common package
		if version < 17_000 {
			switchToNotary(ctx)
		}

		return
	}

	common.SubscribeForNewEpoch()

	runtime.Log("balance contract initialized")
}

// re-initializes contract from non-notary to notary mode. Does nothing if
// action has already been done. The function is called on contract update with
// storage.Context from _deploy.
//
// If contract stores non-empty value by 'ballots' key, switchToNotary panics.
// Otherwise, existing value is removed.
//
// switchToNotary removes values stored by 'netmapScriptHash',
// 'containerScriptHash' and 'notary' keys.
//
// nolint:unused
func switchToNotary(ctx storage.Context) {
	const notaryDisabledKey = "notary" // non-notary legacy

	notaryVal := storage.Get(ctx, notaryDisabledKey)
	if notaryVal == nil {
		runtime.Log("contract is already notarized")
		return
	} else if notaryVal.(bool) && !common.TryPurgeVotes(ctx) {
		panic("pending vote detected")
	}

	storage.Delete(ctx, notaryDisabledKey)
	storage.Delete(ctx, "netmapScriptHash")
	storage.Delete(ctx, "containerScriptHash")

	if notaryVal.(bool) {
		runtime.Log("contract successfully notarized")
	}
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("balance contract updated")
}

// Symbol is a NEP-17 standard method that returns NEOFS token symbol.
func Symbol() string {
	return token.Symbol
}

// Decimals is a NEP-17 standard method that returns precision of NeoFS
// balances.
func Decimals() int {
	return token.Decimals
}

// TotalSupply is a NEP-17 standard method that returns total amount of main
// chain GAS in NeoFS network.
func TotalSupply() int {
	ctx := storage.GetReadOnlyContext()
	return token.getSupply(ctx)
}

// BalanceOf is a NEP-17 standard method that returns NeoFS balance of the specified
// account.
func BalanceOf(account interop.Hash160) int {
	ctx := storage.GetReadOnlyContext()
	return token.balanceOf(ctx, account)
}

// Transfer is a NEP-17 standard method that transfers NeoFS balance from one
// account to another. It can be invoked only by the account owner.
//
// It produces Transfer and TransferX notifications. TransferX notification
// will have empty details field.
func Transfer(from, to interop.Hash160, amount int, data any) bool {
	ctx := storage.GetContext()
	return token.transfer(ctx, from, to, amount, false, nil)
}

// TransferX is a method for NeoFS balance to be transferred from one account to
// another. It can be invoked by the account owner or by Alphabet nodes.
//
// It produces Transfer and TransferX notifications.
//
// TransferX method expands Transfer method by having extra details argument.
// TransferX method also allows to transfer assets by Alphabet nodes of the
// Inner Ring with multisignature.
func TransferX(from, to interop.Hash160, amount int, details []byte) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	result := token.transfer(ctx, from, to, amount, true, details)
	if !result {
		panic("can't transfer assets")
	}

	runtime.Log("successfully transferred assets")
}

// Lock is a method that transfers assets from a user account to the lock account
// related to the user. It can be invoked only by Alphabet nodes of the Inner Ring.
//
// It produces Lock, Transfer and TransferX notifications.
//
// Lock method is invoked by Alphabet nodes of the Inner Ring when they process
// Withdraw notification from NeoFS contract. This should transfer assets
// to a new lock account that won't be used for anything beside Unlock and Burn.
func Lock(txDetails []byte, from, to interop.Hash160, amount, until int) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	details := common.LockTransferDetails(txDetails)

	lockAccount := Account{
		Balance: 0,
		Until:   until,
		Parent:  from,
	}

	common.SetSerialized(ctx, to, lockAccount)

	result := token.transfer(ctx, from, to, amount, true, details)
	if !result {
		// consider using `return false` to remove votes
		panic("can't lock funds")
	}

	runtime.Log("created lock account")
	runtime.Notify("Lock", txDetails, from, to, amount, until)
}

// NewEpoch is a method that checks timeout on lock accounts and returns assets
// if lock is not available anymore. It can be invoked only by NewEpoch method
// of Netmap contract.
//
// It produces Transfer and TransferX notifications.
func NewEpoch(epochNum int) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	it := storage.Find(ctx, []byte{}, storage.KeysOnly)
	for iterator.Next(it) {
		addr := iterator.Value(it).(interop.Hash160) // it MUST BE `storage.KeysOnly`
		if len(addr) != interop.Hash160Len {
			continue
		}

		acc := getAccount(ctx, addr)
		if acc.Until == 0 {
			continue
		}

		if epochNum >= acc.Until {
			details := common.UnlockTransferDetails(epochNum)
			// return assets back to the parent
			token.transfer(ctx, addr, acc.Parent, acc.Balance, true, details)
		}
	}
}

// Mint is a method that transfers assets to a user account from an empty account.
// It can be invoked only by Alphabet nodes of the Inner Ring.
//
// It produces Transfer and TransferX notifications.
//
// Mint method is invoked by Alphabet nodes of the Inner Ring when they process
// Deposit notification from NeoFS contract. Before that, Alphabet nodes should
// synchronize precision of mainchain GAS contract and Balance contract.
// Mint increases total supply of NEP-17 compatible NeoFS token.
func Mint(to interop.Hash160, amount int, txDetails []byte) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	details := common.MintTransferDetails(txDetails)

	ok := token.transfer(ctx, nil, to, amount, true, details)
	if !ok {
		panic("can't transfer assets")
	}

	supply := token.getSupply(ctx)
	supply = supply + amount
	storage.Put(ctx, token.CirculationKey, supply)
	runtime.Log("assets were minted")
}

// Burn is a method that transfers assets from a user account to an empty account.
// It can be invoked only by Alphabet nodes of the Inner Ring.
//
// It produces Transfer and TransferX notifications.
//
// Burn method is invoked by Alphabet nodes of the Inner Ring when they process
// Cheque notification from NeoFS contract. It means that locked assets have been
// transferred to the user in the mainchain, therefore the lock account should be destroyed.
// Before that, Alphabet nodes should synchronize precision of mainchain GAS
// contract and Balance contract. Burn decreases total supply of NEP-17
// compatible NeoFS token.
func Burn(from interop.Hash160, amount int, txDetails []byte) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	details := common.BurnTransferDetails(txDetails)

	ok := token.transfer(ctx, from, nil, amount, true, details)
	if !ok {
		panic("can't transfer assets")
	}

	supply := token.getSupply(ctx)
	if supply < amount {
		panic("negative supply after burn")
	}

	supply = supply - amount
	storage.Put(ctx, token.CirculationKey, supply)
	runtime.Log("assets were burned")
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
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

	if len(from) == interop.Hash160Len {
		if amountFrom.Balance == amount {
			storage.Delete(ctx, from)
		} else {
			amountFrom.Balance = amountFrom.Balance - amount // neo-go#953
			common.SetSerialized(ctx, from, amountFrom)
		}
	}

	if len(to) == interop.Hash160Len {
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
		if len(to) != interop.Hash160Len || !isUsableAddress(from) {
			runtime.Log("bad script hashes")
			return emptyAcc, false
		}
	} else if len(from) == 0 {
		return emptyAcc, true
	}

	amountFrom := getAccount(ctx, from)
	if amountFrom.Balance < amount {
		runtime.Log("not enough assets")
		return emptyAcc, false
	}

	// return amountFrom value back to transfer, reduces extra Get
	return amountFrom, true
}

// isUsableAddress checks if the sender is either a correct NEO address or SC address.
func isUsableAddress(addr interop.Hash160) bool {
	if len(addr) == interop.Hash160Len {
		if runtime.CheckWitness(addr) {
			return true
		}

		// Check if a smart contract is calling script hash
		callingScriptHash := runtime.GetCallingScriptHash()
		if callingScriptHash.Equals(addr) {
			return true
		}
	}

	return false
}

func getAccount(ctx storage.Context, key any) Account {
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).(Account)
	}

	return Account{}
}
