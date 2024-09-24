package tests

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neofs-contract/contracts"
	"github.com/stretchr/testify/require"
)

func TestDeploys(t *testing.T) {
	bc, acc := chain.NewSingle(t)
	e := neotest.NewExecutor(t, bc, acc, acc)

	fsContracts, err := contracts.GetFS()
	require.NoError(t, err)

	for _, fsContract := range fsContracts {
		switch fsContract.Manifest.Name {
		case "NameService":
			deployNNS(t, e, fsContract)
		case "NeoFS Netmap":
			deployNetMap(t, e, fsContract, acc)
		}
	}
}

func deployNNS(t *testing.T, e *neotest.Executor, fsContract contracts.Contract) {
	nnsContract := neotest.Contract{
		Hash:     state.CreateContractHash(e.CommitteeHash, fsContract.NEF.Checksum, fsContract.Manifest.Name),
		NEF:      &fsContract.NEF,
		Manifest: &fsContract.Manifest,
	}

	e.DeployContract(t, &nnsContract, []any{[]any{[]any{"neofs", "ops@nspcc.io"}}})
}

func deployNetMap(t *testing.T, e *neotest.Executor, fsContract contracts.Contract, signer neotest.Signer) {
	netMapContract := neotest.Contract{
		Hash:     state.CreateContractHash(e.CommitteeHash, fsContract.NEF.Checksum, fsContract.Manifest.Name),
		NEF:      &fsContract.NEF,
		Manifest: &fsContract.Manifest,
	}

	netMapContractDeployData := []any{
		false, // notaryDisabled is false
		nil,
		nil,
		nil,
		[]any{},
	}

	e.DeployContract(t, &netMapContract, netMapContractDeployData)

	params := NewRegisterContractInNNSParams("netmap", netMapContract.Hash)
	RegisterContractInNNS(t, e, params)

	// check the contract registered in nns.
	netMapContractHash := getContractHash(t, e, "netmap.neofs")
	require.Equal(t, netMapContract.Hash, netMapContractHash)

	netMapInvoker := e.NewInvoker(netMapContract.Hash, signer)

	var epoch int64
	netMapInvoker.Invoke(t, epoch, "epoch")

	TickEpoch(t, e, signer)
	netMapInvoker.Invoke(t, epoch+1, "epoch")
}
