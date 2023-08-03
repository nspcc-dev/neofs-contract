// Package subnet contains RPC wrappers for NeoFS Subnet contract.
package subnet

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
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

// DeleteEvent represents "Delete" event emitted by the contract.
type DeleteEvent struct {
	Id []byte
}

// RemoveNodeEvent represents "RemoveNode" event emitted by the contract.
type RemoveNodeEvent struct {
	SubnetID []byte
	Node *keys.PublicKey
}

// Invoker is used by ContractReader to call various safe methods.
type Invoker interface {
	Call(contract util.Uint160, operation string, params ...any) (*result.Invoke, error)
}

// Actor is used by Contract to call state-changing methods.
type Actor interface {
	Invoker

	MakeCall(contract util.Uint160, method string, params ...any) (*transaction.Transaction, error)
	MakeRun(script []byte) (*transaction.Transaction, error)
	MakeUnsignedCall(contract util.Uint160, method string, attrs []transaction.Attribute, params ...any) (*transaction.Transaction, error)
	MakeUnsignedRun(script []byte, attrs []transaction.Attribute) (*transaction.Transaction, error)
	SendCall(contract util.Uint160, method string, params ...any) (util.Uint256, uint32, error)
	SendRun(script []byte) (util.Uint256, uint32, error)
}

// ContractReader implements safe contract methods.
type ContractReader struct {
	invoker Invoker
	hash util.Uint160
}

// Contract implements all contract methods.
type Contract struct {
	ContractReader
	actor Actor
	hash util.Uint160
}

// NewReader creates an instance of ContractReader using provided contract hash and the given Invoker.
func NewReader(invoker Invoker, hash util.Uint160) *ContractReader {
	return &ContractReader{invoker, hash}
}

// New creates an instance of Contract using provided contract hash and the given Actor.
func New(actor Actor, hash util.Uint160) *Contract {
	return &Contract{ContractReader{actor, hash}, actor, hash}
}

// Version invokes `version` method of contract.
func (c *ContractReader) Version() (*big.Int, error) {
	return unwrap.BigInt(c.invoker.Call(c.hash, "version"))
}

// AddClientAdmin creates a transaction invoking `addClientAdmin` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) AddClientAdmin(subnetID []byte, groupID []byte, adminPublicKey *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "addClientAdmin", subnetID, groupID, adminPublicKey)
}

// AddClientAdminTransaction creates a transaction invoking `addClientAdmin` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) AddClientAdminTransaction(subnetID []byte, groupID []byte, adminPublicKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "addClientAdmin", subnetID, groupID, adminPublicKey)
}

// AddClientAdminUnsigned creates a transaction invoking `addClientAdmin` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) AddClientAdminUnsigned(subnetID []byte, groupID []byte, adminPublicKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "addClientAdmin", nil, subnetID, groupID, adminPublicKey)
}

// AddNode creates a transaction invoking `addNode` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) AddNode(subnetID []byte, node *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "addNode", subnetID, node)
}

// AddNodeTransaction creates a transaction invoking `addNode` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) AddNodeTransaction(subnetID []byte, node *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "addNode", subnetID, node)
}

// AddNodeUnsigned creates a transaction invoking `addNode` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) AddNodeUnsigned(subnetID []byte, node *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "addNode", nil, subnetID, node)
}

// AddNodeAdmin creates a transaction invoking `addNodeAdmin` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) AddNodeAdmin(subnetID []byte, adminKey *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "addNodeAdmin", subnetID, adminKey)
}

// AddNodeAdminTransaction creates a transaction invoking `addNodeAdmin` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) AddNodeAdminTransaction(subnetID []byte, adminKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "addNodeAdmin", subnetID, adminKey)
}

// AddNodeAdminUnsigned creates a transaction invoking `addNodeAdmin` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) AddNodeAdminUnsigned(subnetID []byte, adminKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "addNodeAdmin", nil, subnetID, adminKey)
}

// AddUser creates a transaction invoking `addUser` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) AddUser(subnetID []byte, groupID []byte, userID []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "addUser", subnetID, groupID, userID)
}

// AddUserTransaction creates a transaction invoking `addUser` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) AddUserTransaction(subnetID []byte, groupID []byte, userID []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "addUser", subnetID, groupID, userID)
}

// AddUserUnsigned creates a transaction invoking `addUser` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) AddUserUnsigned(subnetID []byte, groupID []byte, userID []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "addUser", nil, subnetID, groupID, userID)
}

// Delete creates a transaction invoking `delete` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Delete(id []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "delete", id)
}

// DeleteTransaction creates a transaction invoking `delete` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) DeleteTransaction(id []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "delete", id)
}

// DeleteUnsigned creates a transaction invoking `delete` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) DeleteUnsigned(id []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "delete", nil, id)
}

// Get creates a transaction invoking `get` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Get(id []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "get", id)
}

// GetTransaction creates a transaction invoking `get` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) GetTransaction(id []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "get", id)
}

// GetUnsigned creates a transaction invoking `get` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) GetUnsigned(id []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "get", nil, id)
}

func (c *Contract) scriptForNodeAllowed(subnetID []byte, node *keys.PublicKey) ([]byte, error) {
	return smartcontract.CreateCallWithAssertScript(c.hash, "nodeAllowed", subnetID, node)
}

// NodeAllowed creates a transaction invoking `nodeAllowed` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) NodeAllowed(subnetID []byte, node *keys.PublicKey) (util.Uint256, uint32, error) {
	script, err := c.scriptForNodeAllowed(subnetID, node)
	if err != nil {
		return util.Uint256{}, 0, err
	}
	return c.actor.SendRun(script)
}

// NodeAllowedTransaction creates a transaction invoking `nodeAllowed` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) NodeAllowedTransaction(subnetID []byte, node *keys.PublicKey) (*transaction.Transaction, error) {
	script, err := c.scriptForNodeAllowed(subnetID, node)
	if err != nil {
		return nil, err
	}
	return c.actor.MakeRun(script)
}

// NodeAllowedUnsigned creates a transaction invoking `nodeAllowed` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) NodeAllowedUnsigned(subnetID []byte, node *keys.PublicKey) (*transaction.Transaction, error) {
	script, err := c.scriptForNodeAllowed(subnetID, node)
	if err != nil {
		return nil, err
	}
	return c.actor.MakeUnsignedRun(script, nil)
}

// Put creates a transaction invoking `put` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Put(id []byte, ownerKey *keys.PublicKey, info []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "put", id, ownerKey, info)
}

// PutTransaction creates a transaction invoking `put` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) PutTransaction(id []byte, ownerKey *keys.PublicKey, info []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "put", id, ownerKey, info)
}

// PutUnsigned creates a transaction invoking `put` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) PutUnsigned(id []byte, ownerKey *keys.PublicKey, info []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "put", nil, id, ownerKey, info)
}

// RemoveClientAdmin creates a transaction invoking `removeClientAdmin` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) RemoveClientAdmin(subnetID []byte, groupID []byte, adminPublicKey *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "removeClientAdmin", subnetID, groupID, adminPublicKey)
}

// RemoveClientAdminTransaction creates a transaction invoking `removeClientAdmin` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) RemoveClientAdminTransaction(subnetID []byte, groupID []byte, adminPublicKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "removeClientAdmin", subnetID, groupID, adminPublicKey)
}

// RemoveClientAdminUnsigned creates a transaction invoking `removeClientAdmin` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) RemoveClientAdminUnsigned(subnetID []byte, groupID []byte, adminPublicKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "removeClientAdmin", nil, subnetID, groupID, adminPublicKey)
}

// RemoveNode creates a transaction invoking `removeNode` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) RemoveNode(subnetID []byte, node *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "removeNode", subnetID, node)
}

// RemoveNodeTransaction creates a transaction invoking `removeNode` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) RemoveNodeTransaction(subnetID []byte, node *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "removeNode", subnetID, node)
}

// RemoveNodeUnsigned creates a transaction invoking `removeNode` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) RemoveNodeUnsigned(subnetID []byte, node *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "removeNode", nil, subnetID, node)
}

// RemoveNodeAdmin creates a transaction invoking `removeNodeAdmin` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) RemoveNodeAdmin(subnetID []byte, adminKey *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "removeNodeAdmin", subnetID, adminKey)
}

// RemoveNodeAdminTransaction creates a transaction invoking `removeNodeAdmin` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) RemoveNodeAdminTransaction(subnetID []byte, adminKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "removeNodeAdmin", subnetID, adminKey)
}

// RemoveNodeAdminUnsigned creates a transaction invoking `removeNodeAdmin` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) RemoveNodeAdminUnsigned(subnetID []byte, adminKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "removeNodeAdmin", nil, subnetID, adminKey)
}

// RemoveUser creates a transaction invoking `removeUser` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) RemoveUser(subnetID []byte, groupID []byte, userID []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "removeUser", subnetID, groupID, userID)
}

// RemoveUserTransaction creates a transaction invoking `removeUser` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) RemoveUserTransaction(subnetID []byte, groupID []byte, userID []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "removeUser", subnetID, groupID, userID)
}

// RemoveUserUnsigned creates a transaction invoking `removeUser` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) RemoveUserUnsigned(subnetID []byte, groupID []byte, userID []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "removeUser", nil, subnetID, groupID, userID)
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

func (c *Contract) scriptForUserAllowed(subnetID []byte, user []byte) ([]byte, error) {
	return smartcontract.CreateCallWithAssertScript(c.hash, "userAllowed", subnetID, user)
}

// UserAllowed creates a transaction invoking `userAllowed` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) UserAllowed(subnetID []byte, user []byte) (util.Uint256, uint32, error) {
	script, err := c.scriptForUserAllowed(subnetID, user)
	if err != nil {
		return util.Uint256{}, 0, err
	}
	return c.actor.SendRun(script)
}

// UserAllowedTransaction creates a transaction invoking `userAllowed` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) UserAllowedTransaction(subnetID []byte, user []byte) (*transaction.Transaction, error) {
	script, err := c.scriptForUserAllowed(subnetID, user)
	if err != nil {
		return nil, err
	}
	return c.actor.MakeRun(script)
}

// UserAllowedUnsigned creates a transaction invoking `userAllowed` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) UserAllowedUnsigned(subnetID []byte, user []byte) (*transaction.Transaction, error) {
	script, err := c.scriptForUserAllowed(subnetID, user)
	if err != nil {
		return nil, err
	}
	return c.actor.MakeUnsignedRun(script, nil)
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

// DeleteEventsFromApplicationLog retrieves a set of all emitted events
// with "Delete" name from the provided [result.ApplicationLog].
func DeleteEventsFromApplicationLog(log *result.ApplicationLog) ([]*DeleteEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*DeleteEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "Delete" {
				continue
			}
			event := new(DeleteEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize DeleteEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to DeleteEvent or
// returns an error if it's not possible to do to so.
func (e *DeleteEvent) FromStackItem(item *stackitem.Array) error {
	if item == nil {
		return errors.New("nil item")
	}
	arr, ok := item.Value().([]stackitem.Item)
	if !ok {
		return errors.New("not an array")
	}
	if len(arr) != 1 {
		return errors.New("wrong number of structure elements")
	}

	var (
		index = -1
		err error
	)
	index++
	e.Id, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field Id: %w", err)
	}

	return nil
}

// RemoveNodeEventsFromApplicationLog retrieves a set of all emitted events
// with "RemoveNode" name from the provided [result.ApplicationLog].
func RemoveNodeEventsFromApplicationLog(log *result.ApplicationLog) ([]*RemoveNodeEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*RemoveNodeEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "RemoveNode" {
				continue
			}
			event := new(RemoveNodeEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize RemoveNodeEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to RemoveNodeEvent or
// returns an error if it's not possible to do to so.
func (e *RemoveNodeEvent) FromStackItem(item *stackitem.Array) error {
	if item == nil {
		return errors.New("nil item")
	}
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
	e.SubnetID, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field SubnetID: %w", err)
	}

	index++
	e.Node, err = func (item stackitem.Item) (*keys.PublicKey, error) {
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
		return fmt.Errorf("field Node: %w", err)
	}

	return nil
}
