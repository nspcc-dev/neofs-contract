package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

const OwnerKey = "contractOwner"

// HasUpdateAccess returns true if contract can be initialized, re-initialized
// or migrated.
func HasUpdateAccess(ctx storage.Context) bool {
	data := storage.Get(ctx, OwnerKey)
	if data == nil { // contract has not been initialized yet, return true
		return true
	}

	owner := data.(interop.Hash160)

	return runtime.CheckWitness(owner)
}
