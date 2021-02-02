package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

func GetList(ctx storage.Context, key interface{}) [][]byte {
	data := storage.Get(ctx, key)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([][]byte)
	}

	return [][]byte{}
}

// SetSerialized serializes data and puts it into contract storage.
func SetSerialized(ctx storage.Context, key interface{}, value interface{}) {
	data := binary.Serialize(value)
	storage.Put(ctx, key, data)
}
