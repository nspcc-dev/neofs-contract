// Package netmap contains RPC wrappers for NeoFS Netmap contract.
package netmap

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
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

// CommonIRNode is a contract-specific common.IRNode type used by its methods.
type CommonIRNode struct {
	PublicKey *keys.PublicKey
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

// NetmapNode is a contract-specific netmap.Node type used by its methods.
type NetmapNode struct {
	BLOB []byte
	State *big.Int
}

// Netmaprecord is a contract-specific netmap.record type used by its methods.
type Netmaprecord struct {
	Key []byte
	Val []byte
}

// AddPeerSuccessEvent represents "AddPeerSuccess" event emitted by the contract.
type AddPeerSuccessEvent struct {
	PublicKey *keys.PublicKey
}

// UpdateStateSuccessEvent represents "UpdateStateSuccess" event emitted by the contract.
type UpdateStateSuccessEvent struct {
	PublicKey *keys.PublicKey
	State *big.Int
}

// NewEpochEvent represents "NewEpoch" event emitted by the contract.
type NewEpochEvent struct {
	Epoch *big.Int
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

// Config invokes `config` method of contract.
func (c *ContractReader) Config(key []byte) (any, error) {
	return func (item stackitem.Item, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		return item.Value(), error(nil)
	} (unwrap.Item(c.invoker.Call(c.hash, "config", key)))
}

// Epoch invokes `epoch` method of contract.
func (c *ContractReader) Epoch() (*big.Int, error) {
	return unwrap.BigInt(c.invoker.Call(c.hash, "epoch"))
}

// InnerRingList invokes `innerRingList` method of contract.
func (c *ContractReader) InnerRingList() ([]*CommonIRNode, error) {
	return func (item stackitem.Item, err error) ([]*CommonIRNode, error) {
		if err != nil {
			return nil, err
		}
		return func (item stackitem.Item) ([]*CommonIRNode, error) {
			arr, ok := item.Value().([]stackitem.Item)
			if !ok {
				return nil, errors.New("not an array")
			}
			res := make([]*CommonIRNode, len(arr))
			for i := range res {
				res[i], err = itemToCommonIRNode(arr[i], nil)
				if err != nil {
					return nil, fmt.Errorf("item %d: %w", i, err)
				}
			}
			return res, nil
		} (item)
	} (unwrap.Item(c.invoker.Call(c.hash, "innerRingList")))
}

// ListConfig invokes `listConfig` method of contract.
func (c *ContractReader) ListConfig() ([]*Netmaprecord, error) {
	return func (item stackitem.Item, err error) ([]*Netmaprecord, error) {
		if err != nil {
			return nil, err
		}
		return func (item stackitem.Item) ([]*Netmaprecord, error) {
			arr, ok := item.Value().([]stackitem.Item)
			if !ok {
				return nil, errors.New("not an array")
			}
			res := make([]*Netmaprecord, len(arr))
			for i := range res {
				res[i], err = itemToNetmaprecord(arr[i], nil)
				if err != nil {
					return nil, fmt.Errorf("item %d: %w", i, err)
				}
			}
			return res, nil
		} (item)
	} (unwrap.Item(c.invoker.Call(c.hash, "listConfig")))
}

// Netmap invokes `netmap` method of contract.
func (c *ContractReader) Netmap() ([]*NetmapNode, error) {
	return func (item stackitem.Item, err error) ([]*NetmapNode, error) {
		if err != nil {
			return nil, err
		}
		return func (item stackitem.Item) ([]*NetmapNode, error) {
			arr, ok := item.Value().([]stackitem.Item)
			if !ok {
				return nil, errors.New("not an array")
			}
			res := make([]*NetmapNode, len(arr))
			for i := range res {
				res[i], err = itemToNetmapNode(arr[i], nil)
				if err != nil {
					return nil, fmt.Errorf("item %d: %w", i, err)
				}
			}
			return res, nil
		} (item)
	} (unwrap.Item(c.invoker.Call(c.hash, "netmap")))
}

// NetmapCandidates invokes `netmapCandidates` method of contract.
func (c *ContractReader) NetmapCandidates() ([]*NetmapNode, error) {
	return func (item stackitem.Item, err error) ([]*NetmapNode, error) {
		if err != nil {
			return nil, err
		}
		return func (item stackitem.Item) ([]*NetmapNode, error) {
			arr, ok := item.Value().([]stackitem.Item)
			if !ok {
				return nil, errors.New("not an array")
			}
			res := make([]*NetmapNode, len(arr))
			for i := range res {
				res[i], err = itemToNetmapNode(arr[i], nil)
				if err != nil {
					return nil, fmt.Errorf("item %d: %w", i, err)
				}
			}
			return res, nil
		} (item)
	} (unwrap.Item(c.invoker.Call(c.hash, "netmapCandidates")))
}

// Snapshot invokes `snapshot` method of contract.
func (c *ContractReader) Snapshot(diff *big.Int) ([]*NetmapNode, error) {
	return func (item stackitem.Item, err error) ([]*NetmapNode, error) {
		if err != nil {
			return nil, err
		}
		return func (item stackitem.Item) ([]*NetmapNode, error) {
			arr, ok := item.Value().([]stackitem.Item)
			if !ok {
				return nil, errors.New("not an array")
			}
			res := make([]*NetmapNode, len(arr))
			for i := range res {
				res[i], err = itemToNetmapNode(arr[i], nil)
				if err != nil {
					return nil, fmt.Errorf("item %d: %w", i, err)
				}
			}
			return res, nil
		} (item)
	} (unwrap.Item(c.invoker.Call(c.hash, "snapshot", diff)))
}

// SnapshotByEpoch invokes `snapshotByEpoch` method of contract.
func (c *ContractReader) SnapshotByEpoch(epoch *big.Int) ([]*NetmapNode, error) {
	return func (item stackitem.Item, err error) ([]*NetmapNode, error) {
		if err != nil {
			return nil, err
		}
		return func (item stackitem.Item) ([]*NetmapNode, error) {
			arr, ok := item.Value().([]stackitem.Item)
			if !ok {
				return nil, errors.New("not an array")
			}
			res := make([]*NetmapNode, len(arr))
			for i := range res {
				res[i], err = itemToNetmapNode(arr[i], nil)
				if err != nil {
					return nil, fmt.Errorf("item %d: %w", i, err)
				}
			}
			return res, nil
		} (item)
	} (unwrap.Item(c.invoker.Call(c.hash, "snapshotByEpoch", epoch)))
}

// Version invokes `version` method of contract.
func (c *ContractReader) Version() (*big.Int, error) {
	return unwrap.BigInt(c.invoker.Call(c.hash, "version"))
}

// AddPeer creates a transaction invoking `addPeer` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) AddPeer(nodeInfo []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "addPeer", nodeInfo)
}

// AddPeerTransaction creates a transaction invoking `addPeer` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) AddPeerTransaction(nodeInfo []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "addPeer", nodeInfo)
}

// AddPeerUnsigned creates a transaction invoking `addPeer` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) AddPeerUnsigned(nodeInfo []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "addPeer", nil, nodeInfo)
}

// AddPeerIR creates a transaction invoking `addPeerIR` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) AddPeerIR(nodeInfo []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "addPeerIR", nodeInfo)
}

// AddPeerIRTransaction creates a transaction invoking `addPeerIR` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) AddPeerIRTransaction(nodeInfo []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "addPeerIR", nodeInfo)
}

// AddPeerIRUnsigned creates a transaction invoking `addPeerIR` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) AddPeerIRUnsigned(nodeInfo []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "addPeerIR", nil, nodeInfo)
}

// LastEpochBlock creates a transaction invoking `lastEpochBlock` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) LastEpochBlock() (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "lastEpochBlock")
}

// LastEpochBlockTransaction creates a transaction invoking `lastEpochBlock` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) LastEpochBlockTransaction() (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "lastEpochBlock")
}

// LastEpochBlockUnsigned creates a transaction invoking `lastEpochBlock` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) LastEpochBlockUnsigned() (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "lastEpochBlock", nil)
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

// SetConfig creates a transaction invoking `setConfig` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) SetConfig(id []byte, key []byte, val []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "setConfig", id, key, val)
}

// SetConfigTransaction creates a transaction invoking `setConfig` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) SetConfigTransaction(id []byte, key []byte, val []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "setConfig", id, key, val)
}

// SetConfigUnsigned creates a transaction invoking `setConfig` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) SetConfigUnsigned(id []byte, key []byte, val []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "setConfig", nil, id, key, val)
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

// UpdateSnapshotCount creates a transaction invoking `updateSnapshotCount` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) UpdateSnapshotCount(count *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "updateSnapshotCount", count)
}

// UpdateSnapshotCountTransaction creates a transaction invoking `updateSnapshotCount` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) UpdateSnapshotCountTransaction(count *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "updateSnapshotCount", count)
}

// UpdateSnapshotCountUnsigned creates a transaction invoking `updateSnapshotCount` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) UpdateSnapshotCountUnsigned(count *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "updateSnapshotCount", nil, count)
}

// UpdateState creates a transaction invoking `updateState` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) UpdateState(state *big.Int, publicKey *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "updateState", state, publicKey)
}

// UpdateStateTransaction creates a transaction invoking `updateState` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) UpdateStateTransaction(state *big.Int, publicKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "updateState", state, publicKey)
}

// UpdateStateUnsigned creates a transaction invoking `updateState` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) UpdateStateUnsigned(state *big.Int, publicKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "updateState", nil, state, publicKey)
}

// UpdateStateIR creates a transaction invoking `updateStateIR` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) UpdateStateIR(state *big.Int, publicKey *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "updateStateIR", state, publicKey)
}

// UpdateStateIRTransaction creates a transaction invoking `updateStateIR` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) UpdateStateIRTransaction(state *big.Int, publicKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "updateStateIR", state, publicKey)
}

// UpdateStateIRUnsigned creates a transaction invoking `updateStateIR` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) UpdateStateIRUnsigned(state *big.Int, publicKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "updateStateIR", nil, state, publicKey)
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

// itemToCommonIRNode converts stack item into *CommonIRNode.
func itemToCommonIRNode(item stackitem.Item, err error) (*CommonIRNode, error) {
	if err != nil {
		return nil, err
	}
	var res = new(CommonIRNode)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of CommonIRNode from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *CommonIRNode) FromStackItem(item stackitem.Item) error {
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

// itemToNetmapNode converts stack item into *NetmapNode.
func itemToNetmapNode(item stackitem.Item, err error) (*NetmapNode, error) {
	if err != nil {
		return nil, err
	}
	var res = new(NetmapNode)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of NetmapNode from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *NetmapNode) FromStackItem(item stackitem.Item) error {
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
	res.BLOB, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field BLOB: %w", err)
	}

	index++
	res.State, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field State: %w", err)
	}

	return nil
}

// itemToNetmaprecord converts stack item into *Netmaprecord.
func itemToNetmaprecord(item stackitem.Item, err error) (*Netmaprecord, error) {
	if err != nil {
		return nil, err
	}
	var res = new(Netmaprecord)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of Netmaprecord from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *Netmaprecord) FromStackItem(item stackitem.Item) error {
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
	res.Key, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field Key: %w", err)
	}

	index++
	res.Val, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field Val: %w", err)
	}

	return nil
}

// AddPeerSuccessEventsFromApplicationLog retrieves a set of all emitted events
// with "AddPeerSuccess" name from the provided [result.ApplicationLog].
func AddPeerSuccessEventsFromApplicationLog(log *result.ApplicationLog) ([]*AddPeerSuccessEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*AddPeerSuccessEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "AddPeerSuccess" {
				continue
			}
			event := new(AddPeerSuccessEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize AddPeerSuccessEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to AddPeerSuccessEvent or
// returns an error if it's not possible to do to so.
func (e *AddPeerSuccessEvent) FromStackItem(item *stackitem.Array) error {
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
	e.PublicKey, err = func (item stackitem.Item) (*keys.PublicKey, error) {
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

	return nil
}

// UpdateStateSuccessEventsFromApplicationLog retrieves a set of all emitted events
// with "UpdateStateSuccess" name from the provided [result.ApplicationLog].
func UpdateStateSuccessEventsFromApplicationLog(log *result.ApplicationLog) ([]*UpdateStateSuccessEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*UpdateStateSuccessEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "UpdateStateSuccess" {
				continue
			}
			event := new(UpdateStateSuccessEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize UpdateStateSuccessEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to UpdateStateSuccessEvent or
// returns an error if it's not possible to do to so.
func (e *UpdateStateSuccessEvent) FromStackItem(item *stackitem.Array) error {
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
	e.PublicKey, err = func (item stackitem.Item) (*keys.PublicKey, error) {
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
	e.State, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field State: %w", err)
	}

	return nil
}

// NewEpochEventsFromApplicationLog retrieves a set of all emitted events
// with "NewEpoch" name from the provided [result.ApplicationLog].
func NewEpochEventsFromApplicationLog(log *result.ApplicationLog) ([]*NewEpochEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*NewEpochEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "NewEpoch" {
				continue
			}
			event := new(NewEpochEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize NewEpochEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to NewEpochEvent or
// returns an error if it's not possible to do to so.
func (e *NewEpochEvent) FromStackItem(item *stackitem.Array) error {
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
	e.Epoch, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Epoch: %w", err)
	}

	return nil
}