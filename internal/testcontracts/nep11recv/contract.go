package nep11recv

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

type Call struct {
	From    interop.Hash160
	TokenID []byte
	Data    any
}

func OnNEP11Payment(from interop.Hash160, amount int, tokenID []byte, data any) {
	if amount != 1 {
		panic("wrong amount")
	}
	storage.Put(storage.GetContext(), "key", std.Serialize(Call{
		From:    from,
		TokenID: tokenID,
		Data:    data,
	}))
}

func Get() Call {
	val := storage.Get(storage.GetReadOnlyContext(), "key")
	if val == nil {
		return Call{}
	}
	return std.Deserialize(val.([]byte)).(Call)
}

func Verify() bool {
	return true
}
