package neofsidcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
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
)

func Init(owner, addrNetmap, addrContainer interop.Hash160) {
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

	multiaddr := common.InnerRingMultiAddressViaStorage(ctx, netmapContractKey)
	if !runtime.CheckWitness(multiaddr) {
		panic("addKey: invocation from non inner ring node")
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

	common.SetSerialized(ctx, owner, info)
	runtime.Log("addKey: key bound to the owner")

	return true
}

func RemoveKey(owner []byte, keys []interop.PublicKey) bool {
	if len(owner) != 25 {
		panic("removeKey: incorrect owner")
	}

	ctx := storage.GetContext()

	multiaddr := common.InnerRingMultiAddressViaStorage(ctx, netmapContractKey)
	if !runtime.CheckWitness(multiaddr) {
		panic("removeKey: invocation from non inner ring node")
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
