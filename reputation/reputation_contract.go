package reputationcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	version = 1
)

func Init(owner interop.Hash160) {
	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	storage.Put(ctx, common.OwnerKey, owner)

	runtime.Log("reputation contract initialized")
}

func Migrate(script []byte, manifest []byte) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("reputation contract updated")

	return true
}

func Put(epoch int, peerID []byte, value []byte) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	if !runtime.CheckWitness(multiaddr) {
		runtime.Notify("reputationPut", epoch, peerID, value)
		return
	}

	id := storageID(epoch, peerID)

	reputationValues := GetByID(id)
	reputationValues = append(reputationValues, value)

	rawValues := std.Serialize(reputationValues)
	storage.Put(ctx, id, rawValues)
}

func Get(epoch int, peerID []byte) [][]byte {
	id := storageID(epoch, peerID)
	return GetByID(id)
}

func GetByID(id []byte) [][]byte {
	ctx := storage.GetReadOnlyContext()

	data := storage.Get(ctx, id)
	if data == nil {
		return [][]byte{}
	}

	return std.Deserialize(data.([]byte)).([][]byte)
}

// ListByEpoch returns list of IDs that may be used to get reputation data
// via GetByID method.
func ListByEpoch(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()
	it := storage.Find(ctx, epoch, storage.KeysOnly)

	var result [][]byte

	ignore := [][]byte{
		[]byte(common.OwnerKey),
	}

loop:
	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // iterator MUST BE `storage.KeysOnly`
		for _, ignoreKey := range ignore {
			if common.BytesEqual(key, ignoreKey) {
				continue loop
			}
		}

		result = append(result, key)
	}

	return result
}

func Version() int {
	return version
}

func storageID(epoch int, peerID []byte) []byte {
	var buf interface{} = epoch

	return append(buf.([]byte), peerID...)
}
