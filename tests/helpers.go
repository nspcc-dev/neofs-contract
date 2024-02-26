package tests

import (
	"math/rand"
)

func randomBytes(n int) []byte {
	a := make([]byte, n)
	rand.Read(a) //nolint:staticcheck // SA1019: rand.Read has been deprecated since Go 1.20
	return a
}
