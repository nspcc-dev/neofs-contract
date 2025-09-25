package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
)

// SubscribeForNewEpoch registers calling contract as a NewEpoch
// callback requester. Netmap contract's address is taken from the
// NNS contract, therefore, it must be presented and filled with
// netmap information for a correct SubscribeForNewEpoch call; otherwise
// a successive call is not guaranteed.
// Caller must have `NewEpoch` method with a single numeric argument.
func SubscribeForNewEpoch() {
	netmapContract := ResolveFSContract("netmap")
	contract.Call(netmapContract, "subscribeForNewEpoch", contract.All, runtime.GetExecutingScriptHash())
}

// UnsubscribeFromNewEpoch unregisters calling contract from new epoch events.
// Netmap contract's address is taken from the NNS contract, therefore, it must
// be presented and filled with netmap information for a correct
// UnsubscribeFromNewEpoch call; otherwise a successive call is not guaranteed.
// Calling if no subscription has been done is safe.
func UnsubscribeFromNewEpoch() {
	netmapContract := ResolveFSContract("netmap")
	contract.Call(netmapContract, "unsubscribeFromNewEpoch", contract.All, runtime.GetExecutingScriptHash())
}
