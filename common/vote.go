package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

// BytesEqual compares two slice of bytes by wrapping them into strings,
// which is necessary with new util.Equal interop behaviour, see neo-go#1176.
func BytesEqual(a []byte, b []byte) bool {
	return util.Equals(string(a), string(b))
}
