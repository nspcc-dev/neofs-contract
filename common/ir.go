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

const irListMethod = "innerRingList"

// InnerRingInvoker returns the public key of the inner ring node that has invoked the contract.
// Work around for environments without notary support.
func InnerRingInvoker(ir []interop.PublicKey) interop.PublicKey {
	for _, node := range ir {
		if runtime.CheckWitness(node) {
			return node
		}
	}

	return nil
}

// InnerRingNodes return a list of inner ring nodes from state validator role
// in the sidechain.
func InnerRingNodes() []interop.PublicKey {
	blockHeight := ledger.CurrentIndex()
	return roles.GetDesignatedByRole(roles.NeoFSAlphabet, uint32(blockHeight+1))
}

// InnerRingNodesFromNetmap gets a list of inner ring nodes through
// calling "innerRingList" method of smart contract.
// Work around for environments without notary support.
func InnerRingNodesFromNetmap(sc interop.Hash160) []interop.PublicKey {
	nodes := contract.Call(sc, irListMethod, contract.ReadOnly).([]IRNode)
	pubs := []interop.PublicKey{}
	for i := range nodes {
		pubs = append(pubs, nodes[i].PublicKey)
	}
	return pubs
}

// AlphabetNodes returns a list of alphabet nodes from committee in the sidechain.
func AlphabetNodes() []interop.PublicKey {
	return neo.GetCommittee()
}

// AlphabetAddress returns multi address of alphabet public keys.
func AlphabetAddress() []byte {
	alphabet := neo.GetCommittee()
	return Multiaddress(alphabet, false)
}

// CommitteeAddress returns multi address of committee.
func CommitteeAddress() []byte {
	committee := neo.GetCommittee()
	return Multiaddress(committee, true)
}

// Multiaddress returns default multisignature account address for N keys.
// If committee set to true, it is `M = N/2+1` committee account.
func Multiaddress(n []interop.PublicKey, committee bool) []byte {
	threshold := len(n)*2/3 + 1
	if committee {
		threshold = len(n)/2 + 1
	}

	return contract.CreateMultisigAccount(threshold, n)
}

// ContainsAlphabetWitness checks whether carrier transaction contains either
// (2/3N + 1) or (N/2 + 1) valid multi-signature of the NeoFS Alphabet.
func ContainsAlphabetWitness() bool {
	alphabet := neo.GetCommittee()
	if runtime.CheckWitness(Multiaddress(alphabet, false)) {
		return true
	}
	return runtime.CheckWitness(Multiaddress(alphabet, true))
}
