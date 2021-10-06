package tests

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core"
	"github.com/nspcc-dev/neo-go/pkg/util"
)

const balancePath = "../balance"

func deployBalanceContract(t *testing.T, bc *core.Blockchain, addrNetmap, addrContainer util.Uint160) util.Uint160 {
	args := make([]interface{}, 3)
	args[0] = false
	args[1] = addrNetmap
	args[2] = addrContainer
	return DeployContract(t, bc, balancePath, args)
}
