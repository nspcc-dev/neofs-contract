package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

const irListMethod = "innerRingList"

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
	return contract.Call(sc, irListMethod).([]IRNode)
}
