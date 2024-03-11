package tests

import (
	"math/rand"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

func randomBytes(n int) []byte {
	a := make([]byte, n)
	rand.Read(a) //nolint:staticcheck // SA1019: rand.Read has been deprecated since Go 1.20
	return a
}

// tests contract's 'verify' method checking whether carrier transaction is
// signed by the NeoFS Alphabet.
func testVerify(t testing.TB, contract *neotest.ContractInvoker) {
	const method = "verify"
	contract.Invoke(t, stackitem.NewBool(true), method)
	contract.WithSigners(contract.NewAccount(t)).Invoke(t, stackitem.NewBool(false), method)
}
