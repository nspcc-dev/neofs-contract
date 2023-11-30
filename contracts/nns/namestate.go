package nns

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
)

// NameState represents domain name state.
type NameState struct {
	// Domain name owner. Nil if owned by the committee.
	Owner      interop.Hash160
	Name       string
	Expiration int64
	Admin      interop.Hash160
}

// ensureNotExpired panics if domain name is expired.
func (n NameState) ensureNotExpired() {
	if int64(runtime.GetTime()) >= n.Expiration {
		panic("name has expired")
	}
}

// checkAdmin panics if script container is not signed by the domain name admin.
func (n NameState) checkAdmin() {
	if len(n.Owner) == 0 {
		checkCommittee()
		return
	}
	if runtime.CheckWitness(n.Owner) {
		return
	}
	if n.Admin == nil || !runtime.CheckWitness(n.Admin) {
		panic("not witnessed by admin")
	}
}
