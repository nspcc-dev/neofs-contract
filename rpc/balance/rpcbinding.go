// Package balance contains RPC wrappers for NeoFS Balance contract.
package balance

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/nep17"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"math/big"
	"unicode/utf8"
)

// BalanceAccount is a contract-specific balance.Account type used by its methods.
type BalanceAccount struct {
	Balance *big.Int
	Until *big.Int
	Parent []byte
}

// BalanceToken is a contract-specific balance.Token type used by its methods.
type BalanceToken struct {
	Symbol string
	Decimals *big.Int
	CirculationKey string
}

// CommonBallot is a contract-specific common.Ballot type used by its methods.
type CommonBallot struct {
	ID []byte
	Voters keys.PublicKeys
	Height *big.Int
}

// LedgerBlock is a contract-specific ledger.Block type used by its methods.
type LedgerBlock struct {
	Hash util.Uint256
	Version *big.Int
	PrevHash util.Uint256
	MerkleRoot util.Uint256
	Timestamp *big.Int
	Nonce *big.Int
	Index *big.Int
	NextConsensus util.Uint160
	TransactionsLength *big.Int
}

// LedgerBlockSR is a contract-specific ledger.BlockSR type used by its methods.
type LedgerBlockSR struct {
	Hash util.Uint256
	Version *big.Int
	PrevHash util.Uint256
	MerkleRoot util.Uint256
	Timestamp *big.Int
	Nonce *big.Int
	Index *big.Int
	NextConsensus util.Uint160
	TransactionsLength *big.Int
	PrevStateRoot util.Uint256
}

// LedgerTransaction is a contract-specific ledger.Transaction type used by its methods.
type LedgerTransaction struct {
	Hash util.Uint256
	Version *big.Int
	Nonce *big.Int
	Sender util.Uint160
	SysFee *big.Int
	NetFee *big.Int
	ValidUntilBlock *big.Int
	Script []byte
}

// LedgerTransactionSigner is a contract-specific ledger.TransactionSigner type used by its methods.
type LedgerTransactionSigner struct {
	Account util.Uint160
	Scopes *big.Int
	AllowedContracts []util.Uint160
	AllowedGroups keys.PublicKeys
	Rules []*LedgerWitnessRule
}

// LedgerWitnessCondition is a contract-specific ledger.WitnessCondition type used by its methods.
type LedgerWitnessCondition struct {
	Type *big.Int
	Value any
}

// LedgerWitnessRule is a contract-specific ledger.WitnessRule type used by its methods.
type LedgerWitnessRule struct {
	Action *big.Int
	Condition *LedgerWitnessCondition
}

// ManagementABI is a contract-specific management.ABI type used by its methods.
type ManagementABI struct {
	Methods []*ManagementMethod
	Events []*ManagementEvent
}

// ManagementContract is a contract-specific management.Contract type used by its methods.
type ManagementContract struct {
	ID *big.Int
	UpdateCounter *big.Int
	Hash util.Uint160
	NEF []byte
	Manifest *ManagementManifest
}

// ManagementEvent is a contract-specific management.Event type used by its methods.
type ManagementEvent struct {
	Name string
	Params []*ManagementParameter
}

// ManagementGroup is a contract-specific management.Group type used by its methods.
type ManagementGroup struct {
	PublicKey *keys.PublicKey
	Signature []byte
}

// ManagementManifest is a contract-specific management.Manifest type used by its methods.
type ManagementManifest struct {
	Name string
	Groups []*ManagementGroup
	Features map[string]string
	SupportedStandards []string
	ABI *ManagementABI
	Permissions []*ManagementPermission
	Trusts []util.Uint160
	Extra any
}

// ManagementMethod is a contract-specific management.Method type used by its methods.
type ManagementMethod struct {
	Name string
	Params []*ManagementParameter
	ReturnType *big.Int
	Offset *big.Int
	Safe bool
}

// ManagementParameter is a contract-specific management.Parameter type used by its methods.
type ManagementParameter struct {
	Name string
	Type *big.Int
}

// ManagementPermission is a contract-specific management.Permission type used by its methods.
type ManagementPermission struct {
	Contract util.Uint160
	Methods []string
}

// LockEvent represents "Lock" event emitted by the contract.
type LockEvent struct {
	TxID []byte
	From util.Uint160
	To util.Uint160
	Amount *big.Int
	Until *big.Int
}

// TransferXEvent represents "TransferX" event emitted by the contract.
type TransferXEvent struct {
	From util.Uint160
	To util.Uint160
	Amount *big.Int
	Details []byte
}

// Invoker is used by ContractReader to call various safe methods.
type Invoker interface {
	nep17.Invoker
}

// Actor is used by Contract to call state-changing methods.
type Actor interface {
	Invoker

	nep17.Actor

	MakeCall(contract util.Uint160, method string, params ...any) (*transaction.Transaction, error)
	MakeRun(script []byte) (*transaction.Transaction, error)
	MakeUnsignedCall(contract util.Uint160, method string, attrs []transaction.Attribute, params ...any) (*transaction.Transaction, error)
	MakeUnsignedRun(script []byte, attrs []transaction.Attribute) (*transaction.Transaction, error)
	SendCall(contract util.Uint160, method string, params ...any) (util.Uint256, uint32, error)
	SendRun(script []byte) (util.Uint256, uint32, error)
}

// ContractReader implements safe contract methods.
type ContractReader struct {
	nep17.TokenReader
	invoker Invoker
	hash util.Uint160
}

// Contract implements all contract methods.
type Contract struct {
	ContractReader
	nep17.TokenWriter
	actor Actor
	hash util.Uint160
}

// NewReader creates an instance of ContractReader using provided contract hash and the given Invoker.
func NewReader(invoker Invoker, hash util.Uint160) *ContractReader {
	return &ContractReader{*nep17.NewReader(invoker, hash), invoker, hash}
}

// New creates an instance of Contract using provided contract hash and the given Actor.
func New(actor Actor, hash util.Uint160) *Contract {
	var nep17t = nep17.New(actor, hash)
	return &Contract{ContractReader{nep17t.TokenReader, actor, hash}, nep17t.TokenWriter, actor, hash}
}

// Version invokes `version` method of contract.
func (c *ContractReader) Version() (*big.Int, error) {
	return unwrap.BigInt(c.invoker.Call(c.hash, "version"))
}

// Burn creates a transaction invoking `burn` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Burn(from util.Uint160, amount *big.Int, txDetails []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "burn", from, amount, txDetails)
}

// BurnTransaction creates a transaction invoking `burn` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) BurnTransaction(from util.Uint160, amount *big.Int, txDetails []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "burn", from, amount, txDetails)
}

// BurnUnsigned creates a transaction invoking `burn` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) BurnUnsigned(from util.Uint160, amount *big.Int, txDetails []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "burn", nil, from, amount, txDetails)
}

// Lock creates a transaction invoking `lock` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Lock(txDetails []byte, from util.Uint160, to util.Uint160, amount *big.Int, until *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "lock", txDetails, from, to, amount, until)
}

// LockTransaction creates a transaction invoking `lock` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) LockTransaction(txDetails []byte, from util.Uint160, to util.Uint160, amount *big.Int, until *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "lock", txDetails, from, to, amount, until)
}

// LockUnsigned creates a transaction invoking `lock` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) LockUnsigned(txDetails []byte, from util.Uint160, to util.Uint160, amount *big.Int, until *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "lock", nil, txDetails, from, to, amount, until)
}

// Mint creates a transaction invoking `mint` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Mint(to util.Uint160, amount *big.Int, txDetails []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "mint", to, amount, txDetails)
}

// MintTransaction creates a transaction invoking `mint` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) MintTransaction(to util.Uint160, amount *big.Int, txDetails []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "mint", to, amount, txDetails)
}

// MintUnsigned creates a transaction invoking `mint` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) MintUnsigned(to util.Uint160, amount *big.Int, txDetails []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "mint", nil, to, amount, txDetails)
}

// NewEpoch creates a transaction invoking `newEpoch` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) NewEpoch(epochNum *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "newEpoch", epochNum)
}

// NewEpochTransaction creates a transaction invoking `newEpoch` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) NewEpochTransaction(epochNum *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "newEpoch", epochNum)
}

// NewEpochUnsigned creates a transaction invoking `newEpoch` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) NewEpochUnsigned(epochNum *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "newEpoch", nil, epochNum)
}

// TransferX creates a transaction invoking `transferX` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) TransferX(from util.Uint160, to util.Uint160, amount *big.Int, details []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "transferX", from, to, amount, details)
}

// TransferXTransaction creates a transaction invoking `transferX` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) TransferXTransaction(from util.Uint160, to util.Uint160, amount *big.Int, details []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "transferX", from, to, amount, details)
}

// TransferXUnsigned creates a transaction invoking `transferX` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) TransferXUnsigned(from util.Uint160, to util.Uint160, amount *big.Int, details []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "transferX", nil, from, to, amount, details)
}

// Update creates a transaction invoking `update` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Update(script []byte, manifest []byte, data any) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "update", script, manifest, data)
}

// UpdateTransaction creates a transaction invoking `update` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) UpdateTransaction(script []byte, manifest []byte, data any) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "update", script, manifest, data)
}

// UpdateUnsigned creates a transaction invoking `update` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) UpdateUnsigned(script []byte, manifest []byte, data any) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "update", nil, script, manifest, data)
}

// itemToBalanceAccount converts stack item into *BalanceAccount.
func itemToBalanceAccount(item stackitem.Item, err error) (*BalanceAccount, error) {
	if err != nil {
		return nil, err
	}
	var res = new(BalanceAccount)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of BalanceAccount from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *BalanceAccount) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 3 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Balance, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Balance: %w", err)
	}

	index++
	res.Until, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Until: %w", err)
	}

	index++
	res.Parent, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field Parent: %w", err)
	}

	return nil
}

// itemToBalanceToken converts stack item into *BalanceToken.
func itemToBalanceToken(item stackitem.Item, err error) (*BalanceToken, error) {
	if err != nil {
		return nil, err
	}
	var res = new(BalanceToken)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of BalanceToken from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *BalanceToken) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 3 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Symbol, err = func (item stackitem.Item) (string, error) {
		b, err := item.TryBytes()
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", errors.New("not a UTF-8 string")
		}
		return string(b), nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Symbol: %w", err)
	}

	index++
	res.Decimals, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Decimals: %w", err)
	}

	index++
	res.CirculationKey, err = func (item stackitem.Item) (string, error) {
		b, err := item.TryBytes()
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", errors.New("not a UTF-8 string")
		}
		return string(b), nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field CirculationKey: %w", err)
	}

	return nil
}

// itemToCommonBallot converts stack item into *CommonBallot.
func itemToCommonBallot(item stackitem.Item, err error) (*CommonBallot, error) {
	if err != nil {
		return nil, err
	}
	var res = new(CommonBallot)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of CommonBallot from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *CommonBallot) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 3 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.ID, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field ID: %w", err)
	}

	index++
	res.Voters, err = func (item stackitem.Item) (keys.PublicKeys, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make(keys.PublicKeys, len(arr))
		for i := range res {
			res[i], err = func (item stackitem.Item) (*keys.PublicKey, error) {
				b, err := item.TryBytes()
				if err != nil {
					return nil, err
				}
				k, err := keys.NewPublicKeyFromBytes(b, elliptic.P256())
				if err != nil {
					return nil, err
				}
				return k, nil
			} (arr[i])
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Voters: %w", err)
	}

	index++
	res.Height, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Height: %w", err)
	}

	return nil
}

// itemToLedgerBlock converts stack item into *LedgerBlock.
func itemToLedgerBlock(item stackitem.Item, err error) (*LedgerBlock, error) {
	if err != nil {
		return nil, err
	}
	var res = new(LedgerBlock)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of LedgerBlock from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *LedgerBlock) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 9 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Hash, err = func (item stackitem.Item) (util.Uint256, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint256{}, err
		}
		u, err := util.Uint256DecodeBytesBE(b)
		if err != nil {
			return util.Uint256{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Hash: %w", err)
	}

	index++
	res.Version, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Version: %w", err)
	}

	index++
	res.PrevHash, err = func (item stackitem.Item) (util.Uint256, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint256{}, err
		}
		u, err := util.Uint256DecodeBytesBE(b)
		if err != nil {
			return util.Uint256{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field PrevHash: %w", err)
	}

	index++
	res.MerkleRoot, err = func (item stackitem.Item) (util.Uint256, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint256{}, err
		}
		u, err := util.Uint256DecodeBytesBE(b)
		if err != nil {
			return util.Uint256{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field MerkleRoot: %w", err)
	}

	index++
	res.Timestamp, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Timestamp: %w", err)
	}

	index++
	res.Nonce, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Nonce: %w", err)
	}

	index++
	res.Index, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Index: %w", err)
	}

	index++
	res.NextConsensus, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field NextConsensus: %w", err)
	}

	index++
	res.TransactionsLength, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field TransactionsLength: %w", err)
	}

	return nil
}

// itemToLedgerBlockSR converts stack item into *LedgerBlockSR.
func itemToLedgerBlockSR(item stackitem.Item, err error) (*LedgerBlockSR, error) {
	if err != nil {
		return nil, err
	}
	var res = new(LedgerBlockSR)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of LedgerBlockSR from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *LedgerBlockSR) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 10 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Hash, err = func (item stackitem.Item) (util.Uint256, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint256{}, err
		}
		u, err := util.Uint256DecodeBytesBE(b)
		if err != nil {
			return util.Uint256{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Hash: %w", err)
	}

	index++
	res.Version, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Version: %w", err)
	}

	index++
	res.PrevHash, err = func (item stackitem.Item) (util.Uint256, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint256{}, err
		}
		u, err := util.Uint256DecodeBytesBE(b)
		if err != nil {
			return util.Uint256{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field PrevHash: %w", err)
	}

	index++
	res.MerkleRoot, err = func (item stackitem.Item) (util.Uint256, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint256{}, err
		}
		u, err := util.Uint256DecodeBytesBE(b)
		if err != nil {
			return util.Uint256{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field MerkleRoot: %w", err)
	}

	index++
	res.Timestamp, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Timestamp: %w", err)
	}

	index++
	res.Nonce, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Nonce: %w", err)
	}

	index++
	res.Index, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Index: %w", err)
	}

	index++
	res.NextConsensus, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field NextConsensus: %w", err)
	}

	index++
	res.TransactionsLength, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field TransactionsLength: %w", err)
	}

	index++
	res.PrevStateRoot, err = func (item stackitem.Item) (util.Uint256, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint256{}, err
		}
		u, err := util.Uint256DecodeBytesBE(b)
		if err != nil {
			return util.Uint256{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field PrevStateRoot: %w", err)
	}

	return nil
}

// itemToLedgerTransaction converts stack item into *LedgerTransaction.
func itemToLedgerTransaction(item stackitem.Item, err error) (*LedgerTransaction, error) {
	if err != nil {
		return nil, err
	}
	var res = new(LedgerTransaction)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of LedgerTransaction from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *LedgerTransaction) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 8 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Hash, err = func (item stackitem.Item) (util.Uint256, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint256{}, err
		}
		u, err := util.Uint256DecodeBytesBE(b)
		if err != nil {
			return util.Uint256{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Hash: %w", err)
	}

	index++
	res.Version, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Version: %w", err)
	}

	index++
	res.Nonce, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Nonce: %w", err)
	}

	index++
	res.Sender, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Sender: %w", err)
	}

	index++
	res.SysFee, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field SysFee: %w", err)
	}

	index++
	res.NetFee, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field NetFee: %w", err)
	}

	index++
	res.ValidUntilBlock, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field ValidUntilBlock: %w", err)
	}

	index++
	res.Script, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field Script: %w", err)
	}

	return nil
}

// itemToLedgerTransactionSigner converts stack item into *LedgerTransactionSigner.
func itemToLedgerTransactionSigner(item stackitem.Item, err error) (*LedgerTransactionSigner, error) {
	if err != nil {
		return nil, err
	}
	var res = new(LedgerTransactionSigner)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of LedgerTransactionSigner from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *LedgerTransactionSigner) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 5 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Account, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Account: %w", err)
	}

	index++
	res.Scopes, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Scopes: %w", err)
	}

	index++
	res.AllowedContracts, err = func (item stackitem.Item) ([]util.Uint160, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]util.Uint160, len(arr))
		for i := range res {
			res[i], err = func (item stackitem.Item) (util.Uint160, error) {
				b, err := item.TryBytes()
				if err != nil {
					return util.Uint160{}, err
				}
				u, err := util.Uint160DecodeBytesBE(b)
				if err != nil {
					return util.Uint160{}, err
				}
				return u, nil
			} (arr[i])
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field AllowedContracts: %w", err)
	}

	index++
	res.AllowedGroups, err = func (item stackitem.Item) (keys.PublicKeys, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make(keys.PublicKeys, len(arr))
		for i := range res {
			res[i], err = func (item stackitem.Item) (*keys.PublicKey, error) {
				b, err := item.TryBytes()
				if err != nil {
					return nil, err
				}
				k, err := keys.NewPublicKeyFromBytes(b, elliptic.P256())
				if err != nil {
					return nil, err
				}
				return k, nil
			} (arr[i])
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field AllowedGroups: %w", err)
	}

	index++
	res.Rules, err = func (item stackitem.Item) ([]*LedgerWitnessRule, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]*LedgerWitnessRule, len(arr))
		for i := range res {
			res[i], err = itemToLedgerWitnessRule(arr[i], nil)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Rules: %w", err)
	}

	return nil
}

// itemToLedgerWitnessCondition converts stack item into *LedgerWitnessCondition.
func itemToLedgerWitnessCondition(item stackitem.Item, err error) (*LedgerWitnessCondition, error) {
	if err != nil {
		return nil, err
	}
	var res = new(LedgerWitnessCondition)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of LedgerWitnessCondition from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *LedgerWitnessCondition) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 2 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Type, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Type: %w", err)
	}

	index++
	res.Value, err = arr[index].Value(), error(nil)
	if err != nil {
		return fmt.Errorf("field Value: %w", err)
	}

	return nil
}

// itemToLedgerWitnessRule converts stack item into *LedgerWitnessRule.
func itemToLedgerWitnessRule(item stackitem.Item, err error) (*LedgerWitnessRule, error) {
	if err != nil {
		return nil, err
	}
	var res = new(LedgerWitnessRule)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of LedgerWitnessRule from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *LedgerWitnessRule) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 2 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Action, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Action: %w", err)
	}

	index++
	res.Condition, err = itemToLedgerWitnessCondition(arr[index], nil)
	if err != nil {
		return fmt.Errorf("field Condition: %w", err)
	}

	return nil
}

// itemToManagementABI converts stack item into *ManagementABI.
func itemToManagementABI(item stackitem.Item, err error) (*ManagementABI, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ManagementABI)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ManagementABI from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ManagementABI) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 2 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Methods, err = func (item stackitem.Item) ([]*ManagementMethod, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]*ManagementMethod, len(arr))
		for i := range res {
			res[i], err = itemToManagementMethod(arr[i], nil)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Methods: %w", err)
	}

	index++
	res.Events, err = func (item stackitem.Item) ([]*ManagementEvent, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]*ManagementEvent, len(arr))
		for i := range res {
			res[i], err = itemToManagementEvent(arr[i], nil)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Events: %w", err)
	}

	return nil
}

// itemToManagementContract converts stack item into *ManagementContract.
func itemToManagementContract(item stackitem.Item, err error) (*ManagementContract, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ManagementContract)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ManagementContract from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ManagementContract) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 5 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.ID, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field ID: %w", err)
	}

	index++
	res.UpdateCounter, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field UpdateCounter: %w", err)
	}

	index++
	res.Hash, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Hash: %w", err)
	}

	index++
	res.NEF, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field NEF: %w", err)
	}

	index++
	res.Manifest, err = itemToManagementManifest(arr[index], nil)
	if err != nil {
		return fmt.Errorf("field Manifest: %w", err)
	}

	return nil
}

// itemToManagementEvent converts stack item into *ManagementEvent.
func itemToManagementEvent(item stackitem.Item, err error) (*ManagementEvent, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ManagementEvent)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ManagementEvent from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ManagementEvent) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 2 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Name, err = func (item stackitem.Item) (string, error) {
		b, err := item.TryBytes()
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", errors.New("not a UTF-8 string")
		}
		return string(b), nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Name: %w", err)
	}

	index++
	res.Params, err = func (item stackitem.Item) ([]*ManagementParameter, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]*ManagementParameter, len(arr))
		for i := range res {
			res[i], err = itemToManagementParameter(arr[i], nil)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Params: %w", err)
	}

	return nil
}

// itemToManagementGroup converts stack item into *ManagementGroup.
func itemToManagementGroup(item stackitem.Item, err error) (*ManagementGroup, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ManagementGroup)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ManagementGroup from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ManagementGroup) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 2 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.PublicKey, err = func (item stackitem.Item) (*keys.PublicKey, error) {
		b, err := item.TryBytes()
		if err != nil {
			return nil, err
		}
		k, err := keys.NewPublicKeyFromBytes(b, elliptic.P256())
		if err != nil {
			return nil, err
		}
		return k, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field PublicKey: %w", err)
	}

	index++
	res.Signature, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field Signature: %w", err)
	}

	return nil
}

// itemToManagementManifest converts stack item into *ManagementManifest.
func itemToManagementManifest(item stackitem.Item, err error) (*ManagementManifest, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ManagementManifest)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ManagementManifest from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ManagementManifest) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 8 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Name, err = func (item stackitem.Item) (string, error) {
		b, err := item.TryBytes()
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", errors.New("not a UTF-8 string")
		}
		return string(b), nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Name: %w", err)
	}

	index++
	res.Groups, err = func (item stackitem.Item) ([]*ManagementGroup, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]*ManagementGroup, len(arr))
		for i := range res {
			res[i], err = itemToManagementGroup(arr[i], nil)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Groups: %w", err)
	}

	index++
	res.Features, err = func (item stackitem.Item) (map[string]string, error) {
		m, ok := item.Value().([]stackitem.MapElement)
		if !ok {
			return nil, fmt.Errorf("%s is not a map", item.Type().String())
		}
		res := make(map[string]string)
		for i := range m {
			k, err := func (item stackitem.Item) (string, error) {
				b, err := item.TryBytes()
				if err != nil {
					return "", err
				}
				if !utf8.Valid(b) {
					return "", errors.New("not a UTF-8 string")
				}
				return string(b), nil
			} (m[i].Key)
			if err != nil {
				return nil, fmt.Errorf("key %d: %w", i, err)
			}
			v, err := func (item stackitem.Item) (string, error) {
				b, err := item.TryBytes()
				if err != nil {
					return "", err
				}
				if !utf8.Valid(b) {
					return "", errors.New("not a UTF-8 string")
				}
				return string(b), nil
			} (m[i].Value)
			if err != nil {
				return nil, fmt.Errorf("value %d: %w", i, err)
			}
			res[k] = v
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Features: %w", err)
	}

	index++
	res.SupportedStandards, err = func (item stackitem.Item) ([]string, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]string, len(arr))
		for i := range res {
			res[i], err = func (item stackitem.Item) (string, error) {
				b, err := item.TryBytes()
				if err != nil {
					return "", err
				}
				if !utf8.Valid(b) {
					return "", errors.New("not a UTF-8 string")
				}
				return string(b), nil
			} (arr[i])
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field SupportedStandards: %w", err)
	}

	index++
	res.ABI, err = itemToManagementABI(arr[index], nil)
	if err != nil {
		return fmt.Errorf("field ABI: %w", err)
	}

	index++
	res.Permissions, err = func (item stackitem.Item) ([]*ManagementPermission, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]*ManagementPermission, len(arr))
		for i := range res {
			res[i], err = itemToManagementPermission(arr[i], nil)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Permissions: %w", err)
	}

	index++
	res.Trusts, err = func (item stackitem.Item) ([]util.Uint160, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]util.Uint160, len(arr))
		for i := range res {
			res[i], err = func (item stackitem.Item) (util.Uint160, error) {
				b, err := item.TryBytes()
				if err != nil {
					return util.Uint160{}, err
				}
				u, err := util.Uint160DecodeBytesBE(b)
				if err != nil {
					return util.Uint160{}, err
				}
				return u, nil
			} (arr[i])
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Trusts: %w", err)
	}

	index++
	res.Extra, err = arr[index].Value(), error(nil)
	if err != nil {
		return fmt.Errorf("field Extra: %w", err)
	}

	return nil
}

// itemToManagementMethod converts stack item into *ManagementMethod.
func itemToManagementMethod(item stackitem.Item, err error) (*ManagementMethod, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ManagementMethod)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ManagementMethod from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ManagementMethod) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 5 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Name, err = func (item stackitem.Item) (string, error) {
		b, err := item.TryBytes()
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", errors.New("not a UTF-8 string")
		}
		return string(b), nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Name: %w", err)
	}

	index++
	res.Params, err = func (item stackitem.Item) ([]*ManagementParameter, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]*ManagementParameter, len(arr))
		for i := range res {
			res[i], err = itemToManagementParameter(arr[i], nil)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Params: %w", err)
	}

	index++
	res.ReturnType, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field ReturnType: %w", err)
	}

	index++
	res.Offset, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Offset: %w", err)
	}

	index++
	res.Safe, err = arr[index].TryBool()
	if err != nil {
		return fmt.Errorf("field Safe: %w", err)
	}

	return nil
}

// itemToManagementParameter converts stack item into *ManagementParameter.
func itemToManagementParameter(item stackitem.Item, err error) (*ManagementParameter, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ManagementParameter)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ManagementParameter from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ManagementParameter) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 2 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Name, err = func (item stackitem.Item) (string, error) {
		b, err := item.TryBytes()
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", errors.New("not a UTF-8 string")
		}
		return string(b), nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Name: %w", err)
	}

	index++
	res.Type, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Type: %w", err)
	}

	return nil
}

// itemToManagementPermission converts stack item into *ManagementPermission.
func itemToManagementPermission(item stackitem.Item, err error) (*ManagementPermission, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ManagementPermission)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ManagementPermission from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ManagementPermission) FromStackItem(item stackitem.Item) error {
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 2 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	res.Contract, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Contract: %w", err)
	}

	index++
	res.Methods, err = func (item stackitem.Item) ([]string, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]string, len(arr))
		for i := range res {
			res[i], err = func (item stackitem.Item) (string, error) {
				b, err := item.TryBytes()
				if err != nil {
					return "", err
				}
				if !utf8.Valid(b) {
					return "", errors.New("not a UTF-8 string")
				}
				return string(b), nil
			} (arr[i])
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field Methods: %w", err)
	}

	return nil
}

// LockEventsFromApplicationLog retrieves a set of all emitted events
// with "Lock" name from the provided [result.ApplicationLog].
func LockEventsFromApplicationLog(log *result.ApplicationLog) ([]*LockEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*LockEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "Lock" {
				continue
			}
			event := new(LockEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize LockEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to LockEvent or
// returns an error if it's not possible to do to so.
func (e *LockEvent) FromStackItem(item *stackitem.Array) error {
	if item == nil {
		return errors.New("nil item")
	}
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 5 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	e.TxID, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field TxID: %w", err)
	}

	index++
	e.From, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field From: %w", err)
	}

	index++
	e.To, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field To: %w", err)
	}

	index++
	e.Amount, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Amount: %w", err)
	}

	index++
	e.Until, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Until: %w", err)
	}

	return nil
}

// TransferXEventsFromApplicationLog retrieves a set of all emitted events
// with "TransferX" name from the provided [result.ApplicationLog].
func TransferXEventsFromApplicationLog(log *result.ApplicationLog) ([]*TransferXEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*TransferXEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "TransferX" {
				continue
			}
			event := new(TransferXEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize TransferXEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to TransferXEvent or
// returns an error if it's not possible to do to so.
func (e *TransferXEvent) FromStackItem(item *stackitem.Array) error {
	if item == nil {
		return errors.New("nil item")
	}
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 4 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	e.From, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field From: %w", err)
	}

	index++
	e.To, err = func (item stackitem.Item) (util.Uint160, error) {
		b, err := item.TryBytes()
		if err != nil {
			return util.Uint160{}, err
		}
		u, err := util.Uint160DecodeBytesBE(b)
		if err != nil {
			return util.Uint160{}, err
		}
		return u, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field To: %w", err)
	}

	index++
	e.Amount, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Amount: %w", err)
	}

	index++
	e.Details, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field Details: %w", err)
	}

	return nil
}
