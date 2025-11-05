package proto

import "github.com/nspcc-dev/neo-go/pkg/interop/native/std"

// AssertFieldType checks whether field with given number has expected type. If
// not, AssertFieldType panics.
func AssertFieldType(num, got, exp int) {
	if e := CheckFieldType(num, got, exp); e != "" {
		panic(e)
	}
}

// CheckFieldType checks whether field with given number has expected type and
// returns non-empty exception if not.
func CheckFieldType(num, got, exp int) string {
	if got != exp {
		return "wrong type of field #" + std.Itoa10(num) + ": expected " + StringifyFieldType(exp) + ", got " + StringifyFieldType(exp)
	}

	return ""
}
