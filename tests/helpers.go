package tests

import (
	"math/rand"
)

func randomBytes(n int) []byte {
	a := make([]byte, n)
	rand.Read(a)
	return a
}
