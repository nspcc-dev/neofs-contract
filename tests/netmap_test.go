package tests

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core"
	"github.com/nspcc-dev/neo-go/pkg/util"
)

const netmapPath = "../netmap"

func deployNetmapContract(t *testing.T, bc *core.Blockchain, addrBalance, addrContainer util.Uint160, config ...interface{}) util.Uint160 {
	args := make([]interface{}, 5)
	args[0] = false
	args[1] = addrBalance
	args[2] = addrContainer
	args[3] = []interface{}{CommitteeAcc.PrivateKey().PublicKey().Bytes()}
	args[4] = append([]interface{}{}, config...)
	return DeployContract(t, bc, netmapPath, args)
}
