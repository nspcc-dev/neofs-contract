package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

// SetSerialized serializes data and puts it into contract storage.
func SetSerialized(key []byte, value any) {
	data := std.Serialize(value)
	storage.LocalPut(key, data)
}
