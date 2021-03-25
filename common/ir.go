package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/roles"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
)

type IRNode struct {
	PublicKey interop.PublicKey
}

// InnerRingInvoker returns public key of inner ring node that invoked contract.
func InnerRingInvoker(ir []IRNode) interop.PublicKey {
	for i := 0; i < len(ir); i++ {
		node := ir[i]
		if runtime.CheckWitness(node.PublicKey) {
			return node.PublicKey
		}
	}

	return nil
}

// InnerRingNodes return list of inner ring nodes from state validator role
// in side chain.
func InnerRingNodes() []IRNode {
	blockHeight := ledger.CurrentIndex()
	list := roles.GetDesignatedByRole(roles.NeoFSAlphabet, uint32(blockHeight))
	return keysToNodes(list)
}

// AlphabetNodes return list of alphabet nodes from committee in side chain.
func AlphabetNodes() []IRNode {
	list := neo.GetCommittee()
	return keysToNodes(list)
}

// AlphabetAddress returns multi address of alphabet public keys.
func AlphabetAddress() []byte {
	alphabet := neo.GetCommittee()
	return Multiaddress(alphabet, false)
}

// Multiaddress returns default multi signature account address for N keys.
// If committee set to true, then it is `M = N/2+1` committee account.
func Multiaddress(n []interop.PublicKey, committee bool) []byte {
	threshold := len(n)*2/3 + 1
	if committee {
		threshold = len(n)/2 + 1
	}

	keys := []interop.PublicKey{}
	for _, key := range n {
		keys = append(keys, key)
	}

	return contract.CreateMultisigAccount(threshold, keys)
}

func keysToNodes(list []interop.PublicKey) []IRNode {
	result := []IRNode{}

	for i := range list {
		result = append(result, IRNode{
			PublicKey: list[i],
		})
	}

	return result
}
