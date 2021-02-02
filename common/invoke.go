package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
)

func InvokeID(args []interface{}, prefix []byte) []byte {
	for i := range args {
		arg := args[i].([]byte)
		prefix = append(prefix, arg...)
	}

	return crypto.SHA256(prefix)
}

type IRNode struct {
	PublicKey []byte
}

// InnerRingInvoker returns public key of inner ring node that invoked contract.
func InnerRingInvoker(ir []IRNode) []byte {
	for i := 0; i < len(ir); i++ {
		node := ir[i]
		if runtime.CheckWitness(node.PublicKey) {
			return node.PublicKey
		}
	}

	return nil
}
