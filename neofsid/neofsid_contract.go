package neofsid

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

type (
	UserInfo struct {
		Keys [][]byte
	}
)

const (
	ownerSize = 1 + interop.Hash160Len + 4
)

const (
	ownerKeysPrefix = 'o'
)

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

		// netmap is not used for quite some time and deleted in 0.19.0.
		if version < 19_000 {
			storage.Delete(ctx, "netmapScriptHash")
		}
		return
	}

	runtime.Log("neofsid contract initialized")
}

// re-initializes contract from non-notary to notary mode. Does nothing if
// action has already been done. The function is called on contract update with
// storage.Context from _deploy.
//
// If contract stores non-empty value by 'ballots' key, switchToNotary panics.
// Otherwise, existing value is removed.
//
// switchToNotary removes values stored by 'containerScriptHash' and 'notary'
// keys.
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
	runtime.Log("neofsid contract updated")
}

// AddKey binds a list of the provided public keys to the OwnerID. It can be invoked only by
// Alphabet nodes.
//
// This method panics if the OwnerID is not an ownerSize byte or the public key is not 33 byte long.
// If the key is already bound, the method ignores it.
func AddKey(owner []byte, keys []interop.PublicKey) {
	// V2 format
	if len(owner) != ownerSize {
		panic("incorrect owner")
	}

	for i := range keys {
		if len(keys[i]) != interop.PublicKeyCompressedLen {
			panic("incorrect public key")
		}
	}

	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	ownerKey := append([]byte{ownerKeysPrefix}, owner...)
	for i := range keys {
		stKey := append(ownerKey, keys[i]...)
		storage.Put(ctx, stKey, []byte{1})
	}

	runtime.Log("key bound to the owner")
}

// RemoveKey unbinds the provided public keys from the OwnerID. It can be invoked only by
// Alphabet nodes.
//
// This method panics if the OwnerID is not an ownerSize byte or the public key is not 33 byte long.
// If the key is already unbound, the method ignores it.
func RemoveKey(owner []byte, keys []interop.PublicKey) {
	// V2 format
	if len(owner) != ownerSize {
		panic("incorrect owner")
	}

	for i := range keys {
		if len(keys[i]) != interop.PublicKeyCompressedLen {
			panic("incorrect public key")
		}
	}

	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	if !runtime.CheckWitness(multiaddr) {
		panic("invocation from non inner ring node")
	}

	ownerKey := append([]byte{ownerKeysPrefix}, owner...)
	for i := range keys {
		stKey := append(ownerKey, keys[i]...)
		storage.Delete(ctx, stKey)
	}
}

// Key method returns a list of 33-byte public keys bound with the OwnerID.
//
// This method panics if the owner is not ownerSize byte long.
func Key(owner []byte) [][]byte {
	// V2 format
	if len(owner) != ownerSize {
		panic("incorrect owner")
	}

	ctx := storage.GetReadOnlyContext()

	ownerKey := append([]byte{ownerKeysPrefix}, owner...)
	info := getUserInfo(ctx, ownerKey)

	return info.Keys
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

func getUserInfo(ctx storage.Context, key any) UserInfo {
	it := storage.Find(ctx, key, storage.KeysOnly|storage.RemovePrefix)
	pubs := [][]byte{}
	for iterator.Next(it) {
		pub := iterator.Value(it).([]byte)
		pubs = append(pubs, pub)
	}

	return UserInfo{Keys: pubs}
}
