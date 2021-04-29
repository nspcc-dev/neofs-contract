package neofsidcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
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
	version = 1

	netmapContractKey    = "netmapScriptHash"
	containerContractKey = "containerScriptHash"
	notaryDisabledKey    = "notary"
)

func Init(notaryDisabled bool, owner, addrNetmap, addrContainer interop.Hash160) {
	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	if len(addrNetmap) != 20 || len(addrContainer) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, common.OwnerKey, owner)
	storage.Put(ctx, netmapContractKey, addrNetmap)
	storage.Put(ctx, containerContractKey, addrContainer)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, notaryDisabled)
	if notaryDisabled {
		common.InitVote(ctx)
	}

	runtime.Log("neofsid contract initialized")
}

func Migrate(script []byte, manifest []byte) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("neofsid contract updated")

	return true
}

func AddKey(owner []byte, keys []interop.PublicKey) bool {
	if len(owner) != 25 {
		panic("addKey: incorrect owner")
	}

	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet     []common.IRNode
		nodeKey      []byte
		inderectCall bool
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("addKey: invocation from non inner ring node")
		}

		inderectCall = common.FromKnownContract(
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

	if notaryDisabled && !inderectCall {
		threshold := len(alphabet)*2/3 + 1
		id := invokeIDKeys(owner, keys, []byte("add"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, owner, info)
	runtime.Log("addKey: key bound to the owner")

	return true
}

func RemoveKey(owner []byte, keys []interop.PublicKey) bool {
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
			return true
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, owner, info)

	return true
}

func Key(owner []byte) [][]byte {
	if len(owner) != 25 {
		panic("key: incorrect owner")
	}

	ctx := storage.GetReadOnlyContext()

	info := getUserInfo(ctx, owner)

	return info.Keys
}

func Version() int {
	return version
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
