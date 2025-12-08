package container

import (
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
)

const (
	// RegistrationFeeKey is a key in netmap config which contains fee for container registration.
	RegistrationFeeKey = containerconst.RegistrationFeeKey
	// AliasFeeKey is a key in netmap config which contains fee for nice-name registration.
	AliasFeeKey = containerconst.AliasFeeKey

	// NotFoundError is returned if container is missing.
	NotFoundError = containerconst.NotFoundError

	// ErrorLocked is returned on locked container.
	ErrorLocked = containerconst.ErrorLocked
)
