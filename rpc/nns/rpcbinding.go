// Package nns contains RPC wrappers for NameService contract.
package nns

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/nep11"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"math/big"
	"unicode/utf8"
)

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

// NnsNameState is a contract-specific nns.NameState type used by its methods.
type NnsNameState struct {
	Owner util.Uint160
	Name string
	Expiration *big.Int
	Admin util.Uint160
}

// Invoker is used by ContractReader to call various safe methods.
type Invoker interface {
	nep11.Invoker
}

// Actor is used by Contract to call state-changing methods.
type Actor interface {
	Invoker

	nep11.Actor

	MakeCall(contract util.Uint160, method string, params ...any) (*transaction.Transaction, error)
	MakeRun(script []byte) (*transaction.Transaction, error)
	MakeUnsignedCall(contract util.Uint160, method string, attrs []transaction.Attribute, params ...any) (*transaction.Transaction, error)
	MakeUnsignedRun(script []byte, attrs []transaction.Attribute) (*transaction.Transaction, error)
	SendCall(contract util.Uint160, method string, params ...any) (util.Uint256, uint32, error)
	SendRun(script []byte) (util.Uint256, uint32, error)
}

// ContractReader implements safe contract methods.
type ContractReader struct {
	nep11.NonDivisibleReader
	invoker Invoker
	hash util.Uint160
}

// Contract implements all contract methods.
type Contract struct {
	ContractReader
	nep11.BaseWriter
	actor Actor
	hash util.Uint160
}

// NewReader creates an instance of ContractReader using provided contract hash and the given Invoker.
func NewReader(invoker Invoker, hash util.Uint160) *ContractReader {
	return &ContractReader{*nep11.NewNonDivisibleReader(invoker, hash), invoker, hash}
}

// New creates an instance of Contract using provided contract hash and the given Actor.
func New(actor Actor, hash util.Uint160) *Contract {
	var nep11ndt = nep11.NewNonDivisible(actor, hash)
	return &Contract{ContractReader{nep11ndt.NonDivisibleReader, actor, hash}, nep11ndt.BaseWriter, actor, hash}
}

// GetPrice invokes `getPrice` method of contract.
func (c *ContractReader) GetPrice() (*big.Int, error) {
	return unwrap.BigInt(c.invoker.Call(c.hash, "getPrice"))
}

// GetRecords invokes `getRecords` method of contract.
func (c *ContractReader) GetRecords(name string, typ *big.Int) ([]string, error) {
	return unwrap.ArrayOfUTF8Strings(c.invoker.Call(c.hash, "getRecords", name, typ))
}

// IsAvailable invokes `isAvailable` method of contract.
func (c *ContractReader) IsAvailable(name string) (bool, error) {
	return unwrap.Bool(c.invoker.Call(c.hash, "isAvailable", name))
}

// Resolve invokes `resolve` method of contract.
func (c *ContractReader) Resolve(name string, typ *big.Int) ([]string, error) {
	return unwrap.ArrayOfUTF8Strings(c.invoker.Call(c.hash, "resolve", name, typ))
}

// Roots invokes `roots` method of contract.
func (c *ContractReader) Roots() (uuid.UUID, result.Iterator, error) {
	return unwrap.SessionIterator(c.invoker.Call(c.hash, "roots"))
}

// RootsExpanded is similar to Roots (uses the same contract
// method), but can be useful if the server used doesn't support sessions and
// doesn't expand iterators. It creates a script that will get the specified
// number of result items from the iterator right in the VM and return them to
// you. It's only limited by VM stack and GAS available for RPC invocations.
func (c *ContractReader) RootsExpanded(_numOfIteratorItems int) ([]stackitem.Item, error) {
	return unwrap.Array(c.invoker.CallAndExpandIterator(c.hash, "roots", _numOfIteratorItems))
}

// Version invokes `version` method of contract.
func (c *ContractReader) Version() (*big.Int, error) {
	return unwrap.BigInt(c.invoker.Call(c.hash, "version"))
}

// AddRecord creates a transaction invoking `addRecord` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) AddRecord(name string, typ *big.Int, data string) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "addRecord", name, typ, data)
}

// AddRecordTransaction creates a transaction invoking `addRecord` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) AddRecordTransaction(name string, typ *big.Int, data string) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "addRecord", name, typ, data)
}

// AddRecordUnsigned creates a transaction invoking `addRecord` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) AddRecordUnsigned(name string, typ *big.Int, data string) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "addRecord", nil, name, typ, data)
}

// DeleteRecords creates a transaction invoking `deleteRecords` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) DeleteRecords(name string, typ *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "deleteRecords", name, typ)
}

// DeleteRecordsTransaction creates a transaction invoking `deleteRecords` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) DeleteRecordsTransaction(name string, typ *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "deleteRecords", name, typ)
}

// DeleteRecordsUnsigned creates a transaction invoking `deleteRecords` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) DeleteRecordsUnsigned(name string, typ *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "deleteRecords", nil, name, typ)
}

// GetAllRecords creates a transaction invoking `getAllRecords` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) GetAllRecords(name string) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "getAllRecords", name)
}

// GetAllRecordsTransaction creates a transaction invoking `getAllRecords` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) GetAllRecordsTransaction(name string) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "getAllRecords", name)
}

// GetAllRecordsUnsigned creates a transaction invoking `getAllRecords` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) GetAllRecordsUnsigned(name string) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "getAllRecords", nil, name)
}

func (c *Contract) scriptForRegister(name string, owner util.Uint160, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) ([]byte, error) {
	return smartcontract.CreateCallWithAssertScript(c.hash, "register", name, owner, email, refresh, retry, expire, ttl)
}

// Register creates a transaction invoking `register` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Register(name string, owner util.Uint160, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (util.Uint256, uint32, error) {
	script, err := c.scriptForRegister(name, owner, email, refresh, retry, expire, ttl)
	if err != nil {
		return util.Uint256{}, 0, err
	}
	return c.actor.SendRun(script)
}

// RegisterTransaction creates a transaction invoking `register` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) RegisterTransaction(name string, owner util.Uint160, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (*transaction.Transaction, error) {
	script, err := c.scriptForRegister(name, owner, email, refresh, retry, expire, ttl)
	if err != nil {
		return nil, err
	}
	return c.actor.MakeRun(script)
}

// RegisterUnsigned creates a transaction invoking `register` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) RegisterUnsigned(name string, owner util.Uint160, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (*transaction.Transaction, error) {
	script, err := c.scriptForRegister(name, owner, email, refresh, retry, expire, ttl)
	if err != nil {
		return nil, err
	}
	return c.actor.MakeUnsignedRun(script, nil)
}

// RegisterTLD creates a transaction invoking `registerTLD` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) RegisterTLD(name string, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "registerTLD", name, email, refresh, retry, expire, ttl)
}

// RegisterTLDTransaction creates a transaction invoking `registerTLD` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) RegisterTLDTransaction(name string, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "registerTLD", name, email, refresh, retry, expire, ttl)
}

// RegisterTLDUnsigned creates a transaction invoking `registerTLD` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) RegisterTLDUnsigned(name string, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "registerTLD", nil, name, email, refresh, retry, expire, ttl)
}

// Renew creates a transaction invoking `renew` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Renew(name string) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "renew", name)
}

// RenewTransaction creates a transaction invoking `renew` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) RenewTransaction(name string) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "renew", name)
}

// RenewUnsigned creates a transaction invoking `renew` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) RenewUnsigned(name string) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "renew", nil, name)
}

// SetAdmin creates a transaction invoking `setAdmin` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) SetAdmin(name string, admin util.Uint160) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "setAdmin", name, admin)
}

// SetAdminTransaction creates a transaction invoking `setAdmin` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) SetAdminTransaction(name string, admin util.Uint160) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "setAdmin", name, admin)
}

// SetAdminUnsigned creates a transaction invoking `setAdmin` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) SetAdminUnsigned(name string, admin util.Uint160) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "setAdmin", nil, name, admin)
}

// SetPrice creates a transaction invoking `setPrice` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) SetPrice(price *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "setPrice", price)
}

// SetPriceTransaction creates a transaction invoking `setPrice` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) SetPriceTransaction(price *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "setPrice", price)
}

// SetPriceUnsigned creates a transaction invoking `setPrice` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) SetPriceUnsigned(price *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "setPrice", nil, price)
}

// SetRecord creates a transaction invoking `setRecord` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) SetRecord(name string, typ *big.Int, id *big.Int, data string) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "setRecord", name, typ, id, data)
}

// SetRecordTransaction creates a transaction invoking `setRecord` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) SetRecordTransaction(name string, typ *big.Int, id *big.Int, data string) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "setRecord", name, typ, id, data)
}

// SetRecordUnsigned creates a transaction invoking `setRecord` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) SetRecordUnsigned(name string, typ *big.Int, id *big.Int, data string) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "setRecord", nil, name, typ, id, data)
}

// Update creates a transaction invoking `update` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Update(nef []byte, manifest string, data any) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "update", nef, manifest, data)
}

// UpdateTransaction creates a transaction invoking `update` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) UpdateTransaction(nef []byte, manifest string, data any) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "update", nef, manifest, data)
}

// UpdateUnsigned creates a transaction invoking `update` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) UpdateUnsigned(nef []byte, manifest string, data any) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "update", nil, nef, manifest, data)
}

// UpdateSOA creates a transaction invoking `updateSOA` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) UpdateSOA(name string, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "updateSOA", name, email, refresh, retry, expire, ttl)
}

// UpdateSOATransaction creates a transaction invoking `updateSOA` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) UpdateSOATransaction(name string, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "updateSOA", name, email, refresh, retry, expire, ttl)
}

// UpdateSOAUnsigned creates a transaction invoking `updateSOA` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) UpdateSOAUnsigned(name string, email string, refresh *big.Int, retry *big.Int, expire *big.Int, ttl *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "updateSOA", nil, name, email, refresh, retry, expire, ttl)
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

// itemToNnsNameState converts stack item into *NnsNameState.
func itemToNnsNameState(item stackitem.Item, err error) (*NnsNameState, error) {
	if err != nil {
		return nil, err
	}
	var res = new(NnsNameState)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of NnsNameState from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *NnsNameState) FromStackItem(item stackitem.Item) error {
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
	res.Owner, err = func (item stackitem.Item) (util.Uint160, error) {
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
		return fmt.Errorf("field Owner: %w", err)
	}

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
	res.Expiration, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Expiration: %w", err)
	}

	index++
	res.Admin, err = func (item stackitem.Item) (util.Uint160, error) {
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
		return fmt.Errorf("field Admin: %w", err)
	}

	return nil
}
