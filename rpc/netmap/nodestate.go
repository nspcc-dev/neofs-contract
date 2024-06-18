package netmap

import (
	"math/big"

	"github.com/nspcc-dev/neofs-contract/contracts/netmap/nodestate"
)

// Possible node states in [NetmapNode].
var (
	// NodeStateOnline is used by healthy available nodes.
	NodeStateOnline = big.NewInt(int64(nodestate.Online))

	// NodeStateOffline is used by for currently unavailable nodes.
	NodeStateOffline = big.NewInt(int64(nodestate.Offline))

	// NodeStateMaintenance is used by nodes with known partial availability.
	NodeStateMaintenance = big.NewInt(int64(nodestate.Maintenance))
)
