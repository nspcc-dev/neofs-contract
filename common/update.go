package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
)

// HasUpdateAccess returns true if contract can be updated.
func HasUpdateAccess() bool {
	return runtime.CheckWitness(CommitteeAddress())
}
