package std

import "github.com/nspcc-dev/neo-go/pkg/interop/native/std"

// Atoi works like [std.Atoi10] but returns true instead of panic.
func Atoi(s string) (v int, e bool) {
	defer func() {
		if recover() != nil {
			e = true
		}
	}()
	v = std.Atoi10(s)
	return
}
