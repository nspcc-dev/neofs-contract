package subnet

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	// ErrInvalidSubnetID is thrown when subnet id is not a slice of 4 bytes.
	ErrInvalidSubnetID = "invalid subnet ID"
	// ErrInvalidOwner is thrown when owner has invalid format.
	ErrInvalidOwner = "invalid owner"
	// ErrAlreadyExists is thrown when id already exists.
	ErrAlreadyExists = "subnet id already exists"
	// ErrNotExist is thrown when id doesn't exist.
	ErrNotExist = "subnet id doesn't exist"

	ownerPrefix       = 'o'
	infoPrefix        = 'i'
	notaryDisabledKey = 'z'
)

// _deploy function sets up initial list of inner ring public keys.
func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		return
	}

	args := data.(struct {
		notaryDisabled bool
	})

	ctx := storage.GetContext()
	storage.Put(ctx, []byte{notaryDisabledKey}, args.notaryDisabled)
}

// Update method updates contract source code and manifest. Can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update", contract.All, script, manifest, data)
	runtime.Log("subnet contract updated")
}

// Put creates new subnet with the specified owner and info.
func Put(id []byte, ownerKey interop.PublicKey, info []byte) {
	if len(id) != 4 {
		panic("put: " + ErrInvalidSubnetID)
	}
	if len(ownerKey) != interop.PublicKeyCompressedLen {
		panic("put: " + ErrInvalidOwner)
	}

	ctx := storage.GetContext()
	stKey := append([]byte{ownerPrefix}, id...)
	if storage.Get(ctx, stKey) != nil {
		panic("put: " + ErrAlreadyExists)
	}

	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)
	if notaryDisabled {
		alphabet := common.AlphabetNodes()
		nodeKey := common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			if !runtime.CheckWitness(ownerKey) {
				panic("put: witness check failed")
			}
			runtime.Notify("SubnetPut", ownerKey, info)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{ownerKey, info}, []byte("put"))
		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		if !runtime.CheckWitness(ownerKey) {
			panic("put: owner witness check failed")
		}

		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("put: alphabet witness check failed")
		}
	}

	storage.Put(ctx, stKey, ownerKey)
	stKey[0] = infoPrefix
	storage.Put(ctx, stKey, info)
}

// Get returns info about subnet with the specified id.
func Get(id []byte) []byte {
	ctx := storage.GetContext()
	key := append([]byte{infoPrefix}, id...)
	raw := storage.Get(ctx, key)
	if raw == nil {
		panic("get: " + ErrNotExist)
	}
	return raw.([]byte)
}

// Delete deletes subnet with the specified id.
func Delete(id []byte) {
	ctx := storage.GetContext()
	key := append([]byte{ownerPrefix}, id...)
	raw := storage.Get(ctx, key)
	if raw == nil {
		panic("delete:" + ErrNotExist)
	}

	owner := raw.([]byte)
	if !runtime.CheckWitness(owner) {
		panic("delete: owner witness check failed")
	}

	storage.Delete(ctx, key)

	key[0] = infoPrefix
	storage.Delete(ctx, key)
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}
