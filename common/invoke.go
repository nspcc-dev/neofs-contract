package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
)

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
