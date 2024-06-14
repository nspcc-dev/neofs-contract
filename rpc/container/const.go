package container

import (
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
)

const (
	// RegistrationFeeKey is a key in netmap config which contains fee for container registration.
	RegistrationFeeKey = containerconst.RegistrationFeeKey
	// AliasFeeKey is a key in netmap config which contains fee for nice-name registration.
	AliasFeeKey = containerconst.AliasFeeKey

	// CleanupDelta contains the number of the last epochs for which container estimations are present.
	CleanupDelta = containerconst.CleanupDelta
	// TotalCleanupDelta contains the number of the epochs after which estimation
	// will be removed by epoch tick cleanup if any of the nodes hasn't updated
	// container size and/or container has been removed. It must be greater than CleanupDelta.
	TotalCleanupDelta = containerconst.TotalCleanupDelta

	// NotFoundError is returned if container is missing.
	NotFoundError = containerconst.NotFoundError
)
