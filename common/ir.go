package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/roles"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

const (
	irListMethod    = "innerRingList"
	multiaddrMethod = "multiaddress"
	committeeMethod = "committee"
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

// InnerRingList returns list of inner ring nodes through calling
// "innerRingList" method of smart contract.
//
// Address of smart contract is received from storage by key.
func InnerRingListViaStorage(ctx storage.Context, key interface{}) []IRNode {
	sc := storage.Get(ctx, key).(interop.Hash160)
	return InnerRingList(sc)
}

// InnerRingList gets list of inner ring through
// calling "innerRingList" method of smart contract.
func InnerRingList(sc interop.Hash160) []IRNode {
	return contract.Call(sc, irListMethod, contract.ReadOnly).([]IRNode)
}

// InnerRingMultiAddressViaStorage returns multiaddress of inner ring public
// keys by invoking netmap contract, which scripthash stored in the contract
// storage by the key `key`.
func InnerRingMultiAddressViaStorage(ctx storage.Context, key interface{}) []byte {
	sc := storage.Get(ctx, key).(interop.Hash160)
	return InnerRingMultiAddress(sc)
}

// InnerRingMultiAddress returns multiaddress of inner ring public keys by
// invoking netmap contract.
func InnerRingMultiAddress(sc interop.Hash160) []byte {
	return contract.Call(sc, multiaddrMethod, contract.ReadOnly).([]byte)
}

// CommitteeMultiAddressViaStorage returns multiaddress of committee public
// keys by invoking netmap contract, which scripthash stored in the contract
// storage by the key `key`.
func CommitteeMultiAddressViaStorage(ctx storage.Context, key interface{}) []byte {
	sc := storage.Get(ctx, key).(interop.Hash160)
	return CommitteeMultiAddress(sc)
}

// CommitteeMultiAddress returns multiaddress of committee public keys by
// invoking netmap contract.
func CommitteeMultiAddress(sc interop.Hash160) []byte {
	return contract.Call(sc, committeeMethod, contract.ReadOnly).([]byte)
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
	threshold := len(n)/3*2 + 1
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
