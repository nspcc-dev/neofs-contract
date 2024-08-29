package tests

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

func randomBytes(n int) []byte {
	if n < 8 {
		panic("too small request")
	}
	a := make([]byte, n)
	binary.LittleEndian.PutUint64(a, rand.Uint64())
	return a
}

// tests contract's 'verify' method checking whether carrier transaction is
// signed by the NeoFS Alphabet.
func testVerify(t testing.TB, contract *neotest.ContractInvoker) {
	const method = "verify"
	contract.Invoke(t, stackitem.NewBool(true), method)
	contract.WithSigners(contract.NewAccount(t)).Invoke(t, stackitem.NewBool(false), method)
}
