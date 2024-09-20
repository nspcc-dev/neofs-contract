package tests

import (
	"testing"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neofs-contract/rpc/nns"
	"github.com/stretchr/testify/require"
)

type (
	// RegisterContractInNNSParams is a param struct for RegisterContractInNNS func.
	RegisterContractInNNSParams struct {
		// Name is a contract name. Domain will be registered in the `.neofs` top-level domain.
		// You custom contract names must not collide with system contract names. See rpc/nns/names.go.
		Name         string
		ContractHash util.Uint160
		Email        string
		Refresh      int64
		Retry        int64
		// Expire in milliseconds
		Expire int64
		TTL    int64
	}
)

// NewRegisterContractInNNSParams is a constructor for RegisterContractInNNSParams.
func NewRegisterContractInNNSParams(name string, contractHash util.Uint160) RegisterContractInNNSParams {
	return RegisterContractInNNSParams{
		Name:         name,
		ContractHash: contractHash,
		Email:        "ops@nspcc.ru",
		Refresh:      3600,
		Retry:        600,
		Expire:       int64(365 * 24 * time.Hour / time.Millisecond), // 1 year.
		TTL:          3600,
	}
}

func nnsContractInvoker(t *testing.T, e *neotest.Executor) *neotest.ContractInvoker {
	nnsHash, err := e.Chain.GetContractScriptHash(nns.ID)
	require.NoError(t, err)

	return e.CommitteeInvoker(nnsHash)
}

func getContractHash(t *testing.T, e *neotest.Executor, name string) util.Uint160 {
	nnsInv := nnsContractInvoker(t, e)
	resolveResult, err := nnsInv.TestInvoke(t, "resolve", name, nns.TXT)
	require.NoError(t, err)

	contractHashBytes, err := resolveResult.Pop().Array()[0].TryBytes()
	require.NoError(t, err)

	netMapContractHash, err := nns.AddressFromRecord(string(contractHashBytes))
	require.NoError(t, err)

	return netMapContractHash
}

// RegisterContractInNNS make registration of the contract hash in nns contract.
func RegisterContractInNNS(t *testing.T, e *neotest.Executor, params RegisterContractInNNSParams) {
	nnsHash, err := e.Chain.GetContractScriptHash(1)
	require.NoError(t, err)

	nnsInv := e.CommitteeInvoker(nnsHash)

	nnsInv.Invoke(t, true, "register", params.Name+".neofs", e.CommitteeHash, params.Email, params.Refresh, params.Retry, params.Email, params.TTL)
	nnsInv.Invoke(t, nil, "addRecord", params.Name+".neofs", nns.TXT, params.ContractHash.StringLE())
}

// TickEpoch increments epoch value by one.
func TickEpoch(t *testing.T, e *neotest.Executor, signers ...neotest.Signer) {
	netMapContractHash := getContractHash(t, e, "netmap.neofs")
	netMapInvoker := e.NewInvoker(netMapContractHash, signers...)

	epochResult, err := netMapInvoker.TestInvoke(t, "epoch")
	require.NoError(t, err)

	epoch := epochResult.Pop().BigInt().Int64()
	netMapInvoker.Invoke(t, nil, "newEpoch", epoch+1)
}
