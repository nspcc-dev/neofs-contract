package neofsid

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
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
	netmapContractKey    = "netmapScriptHash"
	containerContractKey = "containerScriptHash"
	notaryDisabledKey    = "notary"
)

func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		return
	}

	args := data.([]interface{})
	notaryDisabled := args[0].(bool)
	addrNetmap := args[1].(interop.Hash160)
	addrContainer := args[2].(interop.Hash160)

	ctx := storage.GetContext()

	if len(addrNetmap) != 20 || len(addrContainer) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, addrNetmap)
	storage.Put(ctx, containerContractKey, addrContainer)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, notaryDisabled)
	if notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("neofsid contract notary disabled")
	}

	runtime.Log("neofsid contract initialized")
}

// Update method updates contract source code and manifest. Can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("neofsid contract updated")
}

// AddKey binds list of provided public keys to OwnerID. Can be invoked only by
// Alphabet nodes.
//
// This method panics if OwnerID is not 25 byte or public key is not 33 byte long.
// If key is already bound, ignores it.
func AddKey(owner []byte, keys []interop.PublicKey) {
	if len(owner) != 25 {
		panic("addKey: incorrect owner")
	}

	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet     []common.IRNode
		nodeKey      []byte
		indirectCall bool
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("addKey: invocation from non inner ring node")
		}

		indirectCall = common.FromKnownContract(
			ctx,
			runtime.GetCallingScriptHash(),
			containerContractKey,
		)
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("addKey: invocation from non inner ring node")
		}
	}

	info := getUserInfo(ctx, owner)

addLoop:
	for i := 0; i < len(keys); i++ {
		pubKey := keys[i]
		if len(pubKey) != 33 {
			panic("addKey: incorrect public key")
		}

		for j := range info.Keys {
			key := info.Keys[j]
			if common.BytesEqual(key, pubKey) {
				continue addLoop
			}
		}

		info.Keys = append(info.Keys, pubKey)
	}

	if notaryDisabled && !indirectCall {
		threshold := len(alphabet)*2/3 + 1
		id := invokeIDKeys(owner, keys, []byte("add"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, owner, info)
	runtime.Log("addKey: key bound to the owner")
}

// RemoveKey unbinds provided public keys from OwnerID. Can be invoked only by
// Alphabet nodes.
//
// This method panics if OwnerID is not 25 byte or public key is not 33 byte long.
// If key is already unbound, ignores it.
func RemoveKey(owner []byte, keys []interop.PublicKey) {
	if len(owner) != 25 {
		panic("removeKey: incorrect owner")
	}

	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("removeKey: invocation from non inner ring node")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		if !runtime.CheckWitness(multiaddr) {
			panic("removeKey: invocation from non inner ring node")
		}
	}

	info := getUserInfo(ctx, owner)
	var leftKeys [][]byte

rmLoop:
	for i := range info.Keys {
		key := info.Keys[i]

		for j := 0; j < len(keys); j++ {
			pubKey := keys[j]
			if len(pubKey) != 33 {
				panic("removeKey: incorrect public key")
			}

			if common.BytesEqual(key, pubKey) {
				continue rmLoop
			}
		}

		leftKeys = append(leftKeys, key)
	}

	info.Keys = leftKeys

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := invokeIDKeys(owner, keys, []byte("remove"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, owner, info)
}

// Key method returns list of 33-byte public keys bound with OwnerID.
//
// This method panics if owner is not 25 byte long.
func Key(owner []byte) [][]byte {
	if len(owner) != 25 {
		panic("key: incorrect owner")
	}

	ctx := storage.GetReadOnlyContext()

	info := getUserInfo(ctx, owner)

	return info.Keys
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}

func getUserInfo(ctx storage.Context, key interface{}) UserInfo {
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).(UserInfo)
	}

	return UserInfo{Keys: [][]byte{}}
}

func invokeIDKeys(owner []byte, keys []interop.PublicKey, prefix []byte) []byte {
	prefix = append(prefix, owner...)
	for i := range keys {
		prefix = append(prefix, keys[i]...)
	}

	return crypto.Sha256(prefix)
}
