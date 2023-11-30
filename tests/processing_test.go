package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

const processingPath = "../contracts/processing"

func deployProcessingContract(t *testing.T, e *neotest.Executor, addrNeoFS util.Uint160) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, processingPath, path.Join(processingPath, "config.yml"))

	args := make([]any, 1)
	args[0] = addrNeoFS

	e.DeployContract(t, c, args)
	return c.Hash
}

func newProcessingInvoker(t *testing.T) (*neotest.ContractInvoker, neotest.Signer) {
	neofsInvoker, irMultiAcc, _ := newNeoFSInvoker(t, 2)
	hash := deployProcessingContract(t, neofsInvoker.Executor, neofsInvoker.Hash)

	return neofsInvoker.CommitteeInvoker(hash), irMultiAcc
}

func TestVerify_Processing(t *testing.T) {
	c, irMultiAcc := newProcessingInvoker(t)

	const method = "verify"

	cIR := c.WithSigners(irMultiAcc)

	cIR.Invoke(t, stackitem.NewBool(true), method)
	c.Invoke(t, stackitem.NewBool(false), method)
}
