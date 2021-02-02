package common

import "github.com/nspcc-dev/neo-go/pkg/interop/crypto"

func InvokeID(args []interface{}, prefix []byte) []byte {
	for i := range args {
		arg := args[i].([]byte)
		prefix = append(prefix, arg...)
	}

	return crypto.SHA256(prefix)
}
