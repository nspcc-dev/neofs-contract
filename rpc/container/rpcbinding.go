// Package container contains RPC wrappers for NeoFS Container contract.
package container

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"github.com/google/uuid"
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

// ContainerContainer is a contract-specific container.Container type used by its methods.
type ContainerContainer struct {
	value []byte
	sig []byte
	pub *keys.PublicKey
	token []byte
}

// ContainerEstimation is a contract-specific container.Estimation type used by its methods.
type ContainerEstimation struct {
	From *keys.PublicKey
	Size *big.Int
}

// ContainerExtendedACL is a contract-specific container.ExtendedACL type used by its methods.
type ContainerExtendedACL struct {
	value []byte
	sig []byte
	pub *keys.PublicKey
	token []byte
}

// ContainercontainerSizes is a contract-specific container.containerSizes type used by its methods.
type ContainercontainerSizes struct {
	cid []byte
	estimations []*ContainerEstimation
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

// PutSuccessEvent represents "PutSuccess" event emitted by the contract.
type PutSuccessEvent struct {
	ContainerID util.Uint256
	PublicKey *keys.PublicKey
}

// DeleteSuccessEvent represents "DeleteSuccess" event emitted by the contract.
type DeleteSuccessEvent struct {
	ContainerID []byte
}

// SetEACLSuccessEvent represents "SetEACLSuccess" event emitted by the contract.
type SetEACLSuccessEvent struct {
	ContainerID []byte
	PublicKey *keys.PublicKey
}

// StartEstimationEvent represents "StartEstimation" event emitted by the contract.
type StartEstimationEvent struct {
	Epoch *big.Int
}

// StopEstimationEvent represents "StopEstimation" event emitted by the contract.
type StopEstimationEvent struct {
	Epoch *big.Int
}

// Invoker is used by ContractReader to call various safe methods.
type Invoker interface {
	Call(contract util.Uint160, operation string, params ...any) (*result.Invoke, error)
	CallAndExpandIterator(contract util.Uint160, method string, maxItems int, params ...any) (*result.Invoke, error)
	TerminateSession(sessionID uuid.UUID) error
	TraverseIterator(sessionID uuid.UUID, iterator *result.Iterator, num int) ([]stackitem.Item, error)
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

// ContainersOf invokes `containersOf` method of contract.
func (c *ContractReader) ContainersOf(owner []byte) (uuid.UUID, result.Iterator, error) {
	return unwrap.SessionIterator(c.invoker.Call(c.hash, "containersOf", owner))
}

// ContainersOfExpanded is similar to ContainersOf (uses the same contract
// method), but can be useful if the server used doesn't support sessions and
// doesn't expand iterators. It creates a script that will get the specified
// number of result items from the iterator right in the VM and return them to
// you. It's only limited by VM stack and GAS available for RPC invocations.
func (c *ContractReader) ContainersOfExpanded(owner []byte, _numOfIteratorItems int) ([]stackitem.Item, error) {
	return unwrap.Array(c.invoker.CallAndExpandIterator(c.hash, "containersOf", _numOfIteratorItems, owner))
}

// Count invokes `count` method of contract.
func (c *ContractReader) Count() (*big.Int, error) {
	return unwrap.BigInt(c.invoker.Call(c.hash, "count"))
}

// EACL invokes `eACL` method of contract.
func (c *ContractReader) EACL(containerID []byte) (*ContainerExtendedACL, error) {
	return itemToContainerExtendedACL(unwrap.Item(c.invoker.Call(c.hash, "eACL", containerID)))
}

// Get invokes `get` method of contract.
func (c *ContractReader) Get(containerID []byte) (*ContainerContainer, error) {
	return itemToContainerContainer(unwrap.Item(c.invoker.Call(c.hash, "get", containerID)))
}

// GetContainerSize invokes `getContainerSize` method of contract.
func (c *ContractReader) GetContainerSize(id []byte) (*ContainercontainerSizes, error) {
	return itemToContainercontainerSizes(unwrap.Item(c.invoker.Call(c.hash, "getContainerSize", id)))
}

// IterateAllContainerSizes invokes `iterateAllContainerSizes` method of contract.
func (c *ContractReader) IterateAllContainerSizes(epoch *big.Int) (uuid.UUID, result.Iterator, error) {
	return unwrap.SessionIterator(c.invoker.Call(c.hash, "iterateAllContainerSizes", epoch))
}

// IterateAllContainerSizesExpanded is similar to IterateAllContainerSizes (uses the same contract
// method), but can be useful if the server used doesn't support sessions and
// doesn't expand iterators. It creates a script that will get the specified
// number of result items from the iterator right in the VM and return them to
// you. It's only limited by VM stack and GAS available for RPC invocations.
func (c *ContractReader) IterateAllContainerSizesExpanded(epoch *big.Int, _numOfIteratorItems int) ([]stackitem.Item, error) {
	return unwrap.Array(c.invoker.CallAndExpandIterator(c.hash, "iterateAllContainerSizes", _numOfIteratorItems, epoch))
}

// IterateContainerSizes invokes `iterateContainerSizes` method of contract.
func (c *ContractReader) IterateContainerSizes(epoch *big.Int, cid util.Uint256) (uuid.UUID, result.Iterator, error) {
	return unwrap.SessionIterator(c.invoker.Call(c.hash, "iterateContainerSizes", epoch, cid))
}

// IterateContainerSizesExpanded is similar to IterateContainerSizes (uses the same contract
// method), but can be useful if the server used doesn't support sessions and
// doesn't expand iterators. It creates a script that will get the specified
// number of result items from the iterator right in the VM and return them to
// you. It's only limited by VM stack and GAS available for RPC invocations.
func (c *ContractReader) IterateContainerSizesExpanded(epoch *big.Int, cid util.Uint256, _numOfIteratorItems int) ([]stackitem.Item, error) {
	return unwrap.Array(c.invoker.CallAndExpandIterator(c.hash, "iterateContainerSizes", _numOfIteratorItems, epoch, cid))
}

// List invokes `list` method of contract.
func (c *ContractReader) List(owner []byte) ([][]byte, error) {
	return unwrap.ArrayOfBytes(c.invoker.Call(c.hash, "list", owner))
}

// ListContainerSizes invokes `listContainerSizes` method of contract.
func (c *ContractReader) ListContainerSizes(epoch *big.Int) ([][]byte, error) {
	return unwrap.ArrayOfBytes(c.invoker.Call(c.hash, "listContainerSizes", epoch))
}

// Owner invokes `owner` method of contract.
func (c *ContractReader) Owner(containerID []byte) ([]byte, error) {
	return unwrap.Bytes(c.invoker.Call(c.hash, "owner", containerID))
}

// Version invokes `version` method of contract.
func (c *ContractReader) Version() (*big.Int, error) {
	return unwrap.BigInt(c.invoker.Call(c.hash, "version"))
}

// Delete creates a transaction invoking `delete` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Delete(containerID []byte, signature []byte, token []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "delete", containerID, signature, token)
}

// DeleteTransaction creates a transaction invoking `delete` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) DeleteTransaction(containerID []byte, signature []byte, token []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "delete", containerID, signature, token)
}

// DeleteUnsigned creates a transaction invoking `delete` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) DeleteUnsigned(containerID []byte, signature []byte, token []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "delete", nil, containerID, signature, token)
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

// Put creates a transaction invoking `put` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) Put(container []byte, signature []byte, publicKey *keys.PublicKey, token []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "put", container, signature, publicKey, token)
}

// PutTransaction creates a transaction invoking `put` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) PutTransaction(container []byte, signature []byte, publicKey *keys.PublicKey, token []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "put", container, signature, publicKey, token)
}

// PutUnsigned creates a transaction invoking `put` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) PutUnsigned(container []byte, signature []byte, publicKey *keys.PublicKey, token []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "put", nil, container, signature, publicKey, token)
}

// PutContainerSize creates a transaction invoking `putContainerSize` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) PutContainerSize(epoch *big.Int, cid []byte, usedSize *big.Int, pubKey *keys.PublicKey) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "putContainerSize", epoch, cid, usedSize, pubKey)
}

// PutContainerSizeTransaction creates a transaction invoking `putContainerSize` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) PutContainerSizeTransaction(epoch *big.Int, cid []byte, usedSize *big.Int, pubKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "putContainerSize", epoch, cid, usedSize, pubKey)
}

// PutContainerSizeUnsigned creates a transaction invoking `putContainerSize` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) PutContainerSizeUnsigned(epoch *big.Int, cid []byte, usedSize *big.Int, pubKey *keys.PublicKey) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "putContainerSize", nil, epoch, cid, usedSize, pubKey)
}

// PutNamed creates a transaction invoking `putNamed` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) PutNamed(container []byte, signature []byte, publicKey *keys.PublicKey, token []byte, name string, zone string) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "putNamed", container, signature, publicKey, token, name, zone)
}

// PutNamedTransaction creates a transaction invoking `putNamed` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) PutNamedTransaction(container []byte, signature []byte, publicKey *keys.PublicKey, token []byte, name string, zone string) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "putNamed", container, signature, publicKey, token, name, zone)
}

// PutNamedUnsigned creates a transaction invoking `putNamed` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) PutNamedUnsigned(container []byte, signature []byte, publicKey *keys.PublicKey, token []byte, name string, zone string) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "putNamed", nil, container, signature, publicKey, token, name, zone)
}

// SetEACL creates a transaction invoking `setEACL` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) SetEACL(eACL []byte, signature []byte, publicKey *keys.PublicKey, token []byte) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "setEACL", eACL, signature, publicKey, token)
}

// SetEACLTransaction creates a transaction invoking `setEACL` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) SetEACLTransaction(eACL []byte, signature []byte, publicKey *keys.PublicKey, token []byte) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "setEACL", eACL, signature, publicKey, token)
}

// SetEACLUnsigned creates a transaction invoking `setEACL` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) SetEACLUnsigned(eACL []byte, signature []byte, publicKey *keys.PublicKey, token []byte) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "setEACL", nil, eACL, signature, publicKey, token)
}

// StartContainerEstimation creates a transaction invoking `startContainerEstimation` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) StartContainerEstimation(epoch *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "startContainerEstimation", epoch)
}

// StartContainerEstimationTransaction creates a transaction invoking `startContainerEstimation` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) StartContainerEstimationTransaction(epoch *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "startContainerEstimation", epoch)
}

// StartContainerEstimationUnsigned creates a transaction invoking `startContainerEstimation` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) StartContainerEstimationUnsigned(epoch *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "startContainerEstimation", nil, epoch)
}

// StopContainerEstimation creates a transaction invoking `stopContainerEstimation` method of the contract.
// This transaction is signed and immediately sent to the network.
// The values returned are its hash, ValidUntilBlock value and error if any.
func (c *Contract) StopContainerEstimation(epoch *big.Int) (util.Uint256, uint32, error) {
	return c.actor.SendCall(c.hash, "stopContainerEstimation", epoch)
}

// StopContainerEstimationTransaction creates a transaction invoking `stopContainerEstimation` method of the contract.
// This transaction is signed, but not sent to the network, instead it's
// returned to the caller.
func (c *Contract) StopContainerEstimationTransaction(epoch *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeCall(c.hash, "stopContainerEstimation", epoch)
}

// StopContainerEstimationUnsigned creates a transaction invoking `stopContainerEstimation` method of the contract.
// This transaction is not signed, it's simply returned to the caller.
// Any fields of it that do not affect fees can be changed (ValidUntilBlock,
// Nonce), fee values (NetworkFee, SystemFee) can be increased as well.
func (c *Contract) StopContainerEstimationUnsigned(epoch *big.Int) (*transaction.Transaction, error) {
	return c.actor.MakeUnsignedCall(c.hash, "stopContainerEstimation", nil, epoch)
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

// itemToContainerContainer converts stack item into *ContainerContainer.
func itemToContainerContainer(item stackitem.Item, err error) (*ContainerContainer, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ContainerContainer)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ContainerContainer from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ContainerContainer) FromStackItem(item stackitem.Item) error {
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
	res.value, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field value: %w", err)
	}

	index++
	res.sig, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field sig: %w", err)
	}

	index++
	res.pub, err = func (item stackitem.Item) (*keys.PublicKey, error) {
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
		return fmt.Errorf("field pub: %w", err)
	}

	index++
	res.token, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field token: %w", err)
	}

	return nil
}

// itemToContainerEstimation converts stack item into *ContainerEstimation.
func itemToContainerEstimation(item stackitem.Item, err error) (*ContainerEstimation, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ContainerEstimation)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ContainerEstimation from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ContainerEstimation) FromStackItem(item stackitem.Item) error {
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
	res.From, err = func (item stackitem.Item) (*keys.PublicKey, error) {
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
		return fmt.Errorf("field From: %w", err)
	}

	index++
	res.Size, err = arr[index].TryInteger()
	if err != nil {
		return fmt.Errorf("field Size: %w", err)
	}

	return nil
}

// itemToContainerExtendedACL converts stack item into *ContainerExtendedACL.
func itemToContainerExtendedACL(item stackitem.Item, err error) (*ContainerExtendedACL, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ContainerExtendedACL)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ContainerExtendedACL from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ContainerExtendedACL) FromStackItem(item stackitem.Item) error {
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
	res.value, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field value: %w", err)
	}

	index++
	res.sig, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field sig: %w", err)
	}

	index++
	res.pub, err = func (item stackitem.Item) (*keys.PublicKey, error) {
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
		return fmt.Errorf("field pub: %w", err)
	}

	index++
	res.token, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field token: %w", err)
	}

	return nil
}

// itemToContainercontainerSizes converts stack item into *ContainercontainerSizes.
func itemToContainercontainerSizes(item stackitem.Item, err error) (*ContainercontainerSizes, error) {
	if err != nil {
		return nil, err
	}
	var res = new(ContainercontainerSizes)
	err = res.FromStackItem(item)
	return res, err
}

// FromStackItem retrieves fields of ContainercontainerSizes from the given
// [stackitem.Item] or returns an error if it's not possible to do to so.
func (res *ContainercontainerSizes) FromStackItem(item stackitem.Item) error {
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
	res.cid, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field cid: %w", err)
	}

	index++
	res.estimations, err = func (item stackitem.Item) ([]*ContainerEstimation, error) {
		arr, ok := item.Value().([]stackitem.Item)
		if !ok {
			return nil, errors.New("not an array")
		}
		res := make([]*ContainerEstimation, len(arr))
		for i := range res {
			res[i], err = itemToContainerEstimation(arr[i], nil)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		return res, nil
	} (arr[index])
	if err != nil {
		return fmt.Errorf("field estimations: %w", err)
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

// PutSuccessEventsFromApplicationLog retrieves a set of all emitted events
// with "PutSuccess" name from the provided [result.ApplicationLog].
func PutSuccessEventsFromApplicationLog(log *result.ApplicationLog) ([]*PutSuccessEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*PutSuccessEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "PutSuccess" {
				continue
			}
			event := new(PutSuccessEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize PutSuccessEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to PutSuccessEvent or
// returns an error if it's not possible to do to so.
func (e *PutSuccessEvent) FromStackItem(item *stackitem.Array) error {
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
	e.ContainerID, err = func (item stackitem.Item) (util.Uint256, error) {
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
		return fmt.Errorf("field ContainerID: %w", err)
	}

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

// DeleteSuccessEventsFromApplicationLog retrieves a set of all emitted events
// with "DeleteSuccess" name from the provided [result.ApplicationLog].
func DeleteSuccessEventsFromApplicationLog(log *result.ApplicationLog) ([]*DeleteSuccessEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*DeleteSuccessEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "DeleteSuccess" {
				continue
			}
			event := new(DeleteSuccessEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize DeleteSuccessEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to DeleteSuccessEvent or
// returns an error if it's not possible to do to so.
func (e *DeleteSuccessEvent) FromStackItem(item *stackitem.Array) error {
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
	e.ContainerID, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field ContainerID: %w", err)
	}

	return nil
}

// SetEACLSuccessEventsFromApplicationLog retrieves a set of all emitted events
// with "SetEACLSuccess" name from the provided [result.ApplicationLog].
func SetEACLSuccessEventsFromApplicationLog(log *result.ApplicationLog) ([]*SetEACLSuccessEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*SetEACLSuccessEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "SetEACLSuccess" {
				continue
			}
			event := new(SetEACLSuccessEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize SetEACLSuccessEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to SetEACLSuccessEvent or
// returns an error if it's not possible to do to so.
func (e *SetEACLSuccessEvent) FromStackItem(item *stackitem.Array) error {
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
	e.ContainerID, err = arr[index].TryBytes()
	if err != nil {
		return fmt.Errorf("field ContainerID: %w", err)
	}

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

// StartEstimationEventsFromApplicationLog retrieves a set of all emitted events
// with "StartEstimation" name from the provided [result.ApplicationLog].
func StartEstimationEventsFromApplicationLog(log *result.ApplicationLog) ([]*StartEstimationEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*StartEstimationEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "StartEstimation" {
				continue
			}
			event := new(StartEstimationEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize StartEstimationEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to StartEstimationEvent or
// returns an error if it's not possible to do to so.
func (e *StartEstimationEvent) FromStackItem(item *stackitem.Array) error {
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

// StopEstimationEventsFromApplicationLog retrieves a set of all emitted events
// with "StopEstimation" name from the provided [result.ApplicationLog].
func StopEstimationEventsFromApplicationLog(log *result.ApplicationLog) ([]*StopEstimationEvent, error) {
	if log == nil {
		return nil, errors.New("nil application log")
	}

	var res []*StopEstimationEvent
	for i, ex := range log.Executions {
		for j, e := range ex.Events {
			if e.Name != "StopEstimation" {
				continue
			}
			event := new(StopEstimationEvent)
			err := event.FromStackItem(e.Item)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize StopEstimationEvent from stackitem (execution #%d, event #%d): %w", i, j, err)
			}
			res = append(res, event)
		}
	}

	return res, nil
}

// FromStackItem converts provided [stackitem.Array] to StopEstimationEvent or
// returns an error if it's not possible to do to so.
func (e *StopEstimationEvent) FromStackItem(item *stackitem.Array) error {
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
