package containerconst

const (
	// RegistrationFeeKey is a key in netmap config which contains fee for container registration.
	RegistrationFeeKey = "ContainerFee"
	// AliasFeeKey is a key in netmap config which contains fee for nice-name registration.
	AliasFeeKey = "ContainerAliasFee"

	// CleanupDelta contains the number of the last epochs for which container estimations are present.
	CleanupDelta = 3
	// TotalCleanupDelta contains the number of the epochs after which estimation
	// will be removed by epoch tick cleanup if any of the nodes hasn't updated
	// container size and/or container has been removed. It must be greater than CleanupDelta.
	TotalCleanupDelta = CleanupDelta + 1

	// NotFoundError is returned if container is missing.
	NotFoundError = "container does not exist"

	// ErrorDeleted is returned on attempt to create previously deleted container.
	ErrorDeleted = "container was previously deleted"
)
