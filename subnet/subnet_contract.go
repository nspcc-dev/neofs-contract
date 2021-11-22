package subnet

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	// ErrInvalidSubnetID is thrown when subnet id is not a slice of 4 bytes.
	ErrInvalidSubnetID = "invalid subnet ID"
	// ErrInvalidOwner is thrown when owner has invalid format.
	ErrInvalidOwner = "invalid owner"
	// ErrInvalidAdmin is thrown when admin has invalid format.
	ErrInvalidAdmin = "invalid administrator"
	// ErrAlreadyExists is thrown when id already exists.
	ErrAlreadyExists = "subnet id already exists"
	// ErrSubNotExist is thrown when id doesn't exist.
	ErrSubNotExist = "subnet id doesn't exist"
	// ErrNodeAdmNotExist is thrown when node admin is not found.
	ErrNodeAdmNotExist = "node admin not found"

	errCheckWitnessFailed = "owner witness check failed"

	ownerPrefix       = 'o'
	nodeAdminPrefix   = 'a'
	clientAdminPrefix = 'm'
	nodePrefix        = 'n'
	user              = 'u'
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
			runtime.Notify("SubnetPut", id, ownerKey, info)
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
			panic("put: " + errCheckWitnessFailed)
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
	ctx := storage.GetReadOnlyContext()
	key := append([]byte{infoPrefix}, id...)
	raw := storage.Get(ctx, key)
	if raw == nil {
		panic("get: " + ErrSubNotExist)
	}
	return raw.([]byte)
}

// Delete deletes subnet with the specified id.
func Delete(id []byte) {
	ctx := storage.GetContext()
	key := append([]byte{ownerPrefix}, id...)
	raw := storage.Get(ctx, key)
	if raw == nil {
		panic("delete:" + ErrSubNotExist)
	}

	owner := raw.([]byte)
	if !runtime.CheckWitness(owner) {
		panic("delete: " + errCheckWitnessFailed)
	}

	storage.Delete(ctx, key)

	key[0] = infoPrefix
	storage.Delete(ctx, key)

	runtime.Notify("SubnetDelete", id)
}

// AddNodeAdmin adds new node administrator to the specified subnetwork.
func AddNodeAdmin(subnetID []byte, adminKey interop.PublicKey) {
	if len(adminKey) != interop.PublicKeyCompressedLen {
		panic("addNodeAdmin: " + ErrInvalidAdmin)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic("addNodeAdmin: " + ErrSubNotExist)
	}

	owner := rawOwner.([]byte)
	if !runtime.CheckWitness(owner) {
		panic("addNodeAdmin: " + errCheckWitnessFailed)
	}

	stKey[0] = nodeAdminPrefix
	prefixLen := len(stKey)

	iter := storage.Find(ctx, stKey, storage.KeysOnly)
	for iterator.Next(iter) {
		key := iterator.Value(iter).([]byte)
		if util.Equals(string(key[prefixLen:]), string(adminKey)) {
			panic("addNodeAdmin: node admin has already been added")
		}
	}

	storage.Put(ctx, append(stKey, adminKey...), []byte{1})
}

// RemoveNodeAdmin removes node administrator from the specified subnetwork.
// Must be called by subnet owner only.
func RemoveNodeAdmin(subnetID []byte, adminKey interop.PublicKey) {
	if len(adminKey) != interop.PublicKeyCompressedLen {
		panic("removeNodeAdmin: " + ErrInvalidAdmin)
	}

	ctx := storage.GetContext()

	stOwnerKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stOwnerKey)
	if rawOwner == nil {
		panic("removeNodeAdmin: " + ErrSubNotExist)
	}

	owner := rawOwner.([]byte)
	if !runtime.CheckWitness(owner) {
		panic("removeNodeAdmin: " + errCheckWitnessFailed)
	}

	stOwnerKey[0] = nodeAdminPrefix
	stNodeAdmKey := append(stOwnerKey, adminKey...)

	if storage.Get(ctx, stNodeAdmKey) == nil {
		panic("removeNodeAdmin: " + ErrNodeAdmNotExist)
	}

	storage.Delete(ctx, stNodeAdmKey)
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}
