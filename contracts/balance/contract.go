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
	"github.com/nspcc-dev/neofs-contract/contracts/balance/balanceconst"
	"github.com/nspcc-dev/neofs-contract/contracts/netmap/nodestate"
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
	accPrefix   = 'a'

	netmapContractKey    = 'b'
	containerContractKey = 'c'

	unpaidContainersPrefix = 'd'

	gigabyte = 1 << 30
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

// nolint:unused
func storeNetmapAndContainer(ctx storage.Context) {
	nnsAddr := common.InferNNSHash()
	netmapAddr := common.ResolveFSContractWithNNS(nnsAddr, "netmap")
	if len(netmapAddr) != interop.Hash160Len {
		panic("NNS contract does not know Netmap address")
	}
	storage.Put(ctx, netmapContractKey, netmapAddr)

	containerAddr := common.ResolveFSContractWithNNS(nnsAddr, "container")
	if len(containerAddr) != interop.Hash160Len {
		panic("NNS contract does not know Container address")
	}
	storage.Put(ctx, containerContractKey, containerAddr)
}

// nolint:unused
func _deploy(data any, isUpdate bool) {
	ctx := storage.GetContext()
	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)

		common.CheckVersion(version)

		if version < 25_000 {
			storeNetmapAndContainer(ctx)

			common.SubscribeForNewEpoch()
		}

		return
	}

	common.SubscribeForNewEpoch()

	runtime.Log("balance contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(nefFile, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nefFile, manifest, common.AppendVersion(data))
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
func TransferX(from, to interop.Hash160, amount int, details []byte) bool {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	return token.transfer(ctx, from, to, amount, true, details)
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

	common.CheckAlphabetWitness()

	details := common.LockTransferDetails(txDetails)

	lockAccount := Account{
		Balance: 0,
		Until:   until,
		Parent:  from,
	}

	common.SetSerialized(ctx, append([]byte{accPrefix}, to...), lockAccount)

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

	common.CheckAlphabetWitness()

	it := storage.Find(ctx, []byte{accPrefix}, storage.KeysOnly|storage.RemovePrefix)
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
// synchronize precision of main chain GAS contract and Balance contract.
// Mint increases total supply of NEP-17 compatible NeoFS token.
func Mint(to interop.Hash160, amount int, txDetails []byte) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

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
// transferred to the user in main chain, therefore the lock account should be destroyed.
// Before that, Alphabet nodes should synchronize precision of main chain GAS
// contract and Balance contract. Burn decreases total supply of NEP-17
// compatible NeoFS token.
func Burn(from interop.Hash160, amount int, txDetails []byte) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

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

// epochBillingStat is a copy of github.com/nspcc-dev/neofs-contract/contracts/container.EpochBillingStat
// to prevent cross-contract imports that may fail due to internal `_deploy` calls.
type epochBillingStat struct {
	Account interop.Hash160

	LatestContainerSize    int
	LatestEpoch            int
	LastUpdateTime         int
	LatestEpochAverageSize int

	PreviousContainerSize    int
	PreviousEpoch            int
	PreviousEpochAverageSize int
}

// nodeReport is a (sufficient) part of github.com/nspcc-dev/neofs-contract/contracts/container.NodeReport
// to prevent cross-contract imports that may fail due to internal `_deploy` calls.
type nodeReport struct {
	PublicKey interop.PublicKey
	// Other fields are irrelevant and therefore omitted.
}

// SettleContainerPayment distributes storage payments from container's owner
// account to storage nodes that serve container's objects. Transaction must be
// witnessed by the actual Alphabet multi-signature. Produces `ChangePaymentStatus`
// notification on any payment status changes. If payment is successful, `Payment`
// notification is thrown. If payment cannot be fully made, container is registered
// as an unpaid one, see [GetUnpaidContainerEpoch].
func SettleContainerPayment(cid interop.Hash256) bool {
	if len(cid) != interop.Hash256Len {
		panic("invalid container id")
	}
	common.CheckAlphabetWitness()

	var (
		ctx                   = storage.GetContext()
		netmapContractAddr    = getContractHash(ctx, netmapContractKey, "netmap")
		containerContractAddr = getContractHash(ctx, containerContractKey, "container")

		containerUserNeoFS = contract.Call(containerContractAddr, "owner", contract.ReadOnly, cid).([]byte)
		containerOwner     = interop.Hash160(common.WalletToScriptHash(containerUserNeoFS))
		rate               = contract.Call(netmapContractAddr, "config", contract.ReadOnly, balanceconst.BasicIncomeRateKey).(int)
		currEpoch          = contract.Call(netmapContractAddr, "epoch", contract.ReadOnly).(int)

		transferredTotal int
		paymentOK        = true
	)

	if rate == 0 {
		return true
	}

	it := contract.Call(containerContractAddr, "iterateBillingStats", contract.ReadOnly, cid).(iterator.Iterator)
	for iterator.Next(it) {
		var (
			r    = iterator.Value(it).(epochBillingStat)
			size int
		)

		switch {
		case r.LatestEpoch < currEpoch-1:
			// no updates for more than 2 epochs, consider load be the same
			size = r.LatestContainerSize
		case r.LatestEpoch == currEpoch:
			if r.PreviousEpoch == currEpoch-1 {
				size = r.PreviousEpochAverageSize
			} else {
				size = r.PreviousContainerSize
			}
		case r.LatestEpoch == currEpoch-1:
			size = r.LatestEpochAverageSize
		default:
			size = 0
		}

		if size == 0 {
			continue
		}

		var rep = contract.Call(containerContractAddr, "getReportByAccount", contract.ReadOnly, cid, r.Account).(nodeReport)
		if len(rep.PublicKey) != interop.PublicKeyCompressedLen {
			continue
		}

		var isOnline = contract.Call(netmapContractAddr, "isStorageNodeStatus", contract.ReadOnly, rep.PublicKey, currEpoch-1, nodestate.Online).(bool)
		if !isOnline {
			continue
		}

		payment := rate * size / gigabyte
		if payment == 0 {
			payment = 1
		}

		paymentOK = token.transfer(ctx, containerOwner, r.Account, payment, true, nil)
		if !paymentOK {
			break
		}

		transferredTotal += payment
	}

	k := append([]byte{unpaidContainersPrefix}, cid...)
	alreadyMarkedUnpaid := storage.Get(ctx, k) != nil
	if paymentOK && alreadyMarkedUnpaid {
		runtime.Notify("ChangePaymentStatus", cid, currEpoch, false)
		storage.Delete(ctx, k)
	} else if !paymentOK && !alreadyMarkedUnpaid {
		runtime.Notify("ChangePaymentStatus", cid, currEpoch, true)
		storage.Put(ctx, k, currEpoch)
	}

	if paymentOK {
		runtime.Notify("Payment", containerOwner, cid, currEpoch, transferredTotal)
	}

	return paymentOK
}

// GetUnpaidContainerEpoch returns epoch from which container is considered
// unpaid. Returns -1 if container is paid successfully.
func GetUnpaidContainerEpoch(cid interop.Hash256) int {
	if len(cid) != interop.Hash256Len {
		panic("invalid container id")
	}
	raw := storage.Get(storage.GetReadOnlyContext(), append([]byte{unpaidContainersPrefix}, cid...))
	if raw == nil {
		return -1
	}

	return raw.(int)
}

// IterateUnpaid is like [GetUnpaidContainerEpoch] but for every unpaid
// container. Iteration is through key-value pair, where key is container ID,
// value is epoch from which container is considered unpaid.
func IterateUnpaid() iterator.Iterator {
	return storage.Find(storage.GetReadOnlyContext(), []byte{unpaidContainersPrefix}, storage.RemovePrefix)
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
	if amount < 0 {
		panic("negative amount")
	}

	amountFrom, ok := t.canTransfer(ctx, from, to, amount, innerRing)
	if !ok {
		return false
	}

	if len(from) == interop.Hash160Len {
		var fromKey = append([]byte{accPrefix}, from...)

		if amountFrom.Balance == amount {
			storage.Delete(ctx, fromKey)
		} else {
			amountFrom.Balance -= amount
			common.SetSerialized(ctx, fromKey, amountFrom)
		}
	}

	if len(to) == interop.Hash160Len {
		var toKey = append([]byte{accPrefix}, to...)

		amountTo := getAccount(ctx, to)
		amountTo.Balance += amount
		common.SetSerialized(ctx, toKey, amountTo)
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

func getAccount(ctx storage.Context, key interop.Hash160) Account {
	data := storage.Get(ctx, append([]byte{accPrefix}, key...))
	if data != nil {
		return std.Deserialize(data.([]byte)).(Account)
	}

	return Account{}
}

func getContractHash(ctx storage.Context, storageKey byte, nnsKey string) interop.Hash160 {
	contractH := storage.Get(ctx, storageKey)
	if contractH == nil {
		nnsAddr := common.InferNNSHash()
		h := common.ResolveFSContractWithNNS(nnsAddr, nnsKey)
		if len(h) != interop.Hash160Len {
			panic("NNS contract does not know " + nnsKey + " address")
		}
		storage.Put(ctx, storageKey, h)

		contractH = h
	}

	return contractH.(interop.Hash160)
}
