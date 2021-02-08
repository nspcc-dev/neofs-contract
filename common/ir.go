package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

const irListMethod = "innerRingList"

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

// InnerRingList returns list of inner ring nodes through calling
// "innerRingList" method of smart contract.
//
// Address of smart contract is received from storage by key.
func InnerRingListViaStorage(ctx storage.Context, key interface{}) []IRNode {
	sc := storage.Get(ctx, key).([]byte)
	return InnerRingList(sc)
}

// InnerRingList gets list of inner ring through
// calling "innerRingList" method of smart contract.
func InnerRingList(sc interop.Hash160) []IRNode {
	return contract.Call(sc, irListMethod, contract.ReadOnly).([]IRNode)
}
