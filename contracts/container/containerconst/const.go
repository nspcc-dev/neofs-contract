package containerconst

const (
	// RegistrationFeeKey is a key in netmap config which contains fee for container registration.
	RegistrationFeeKey = "ContainerFee"
	// AliasFeeKey is a key in netmap config which contains fee for nice-name registration.
	AliasFeeKey = "ContainerAliasFee"
	// AlphabetManagesQuotasKey is a key in netmap config which defines if
	// Alphabet is allowed to manage space quotas instead of users.
	AlphabetManagesQuotasKey = "AlphabetManagesQuotas"
	// EpochDurationKey is a key in netmap config which contains epoch duration in seconds.
	EpochDurationKey = "EpochDuration"

	// NotFoundError is returned if container is missing.
	NotFoundError = "container does not exist"

	// ErrorDeleted is returned on attempt to create previously deleted container.
	ErrorDeleted = "container was previously deleted"

	// ErrorTooBigNumberOfNodes is returned if it is assumed that REP or number of REPS
	// in container's placement policy is bigger than 255.
	ErrorTooBigNumberOfNodes = "number of container nodes exceeds limits"

	// ErrorInvalidContainerID is returned on an attempt to work with incorrect container ID.
	ErrorInvalidContainerID = "invalid container id"

	// ErrorInvalidPublicKey is returned on an attempt to work with an incorrect public key.
	ErrorInvalidPublicKey = "invalid public key"

	// AlphabetManagesAttributesKey is a key in netmap config which defines if
	// Alphabet is allowed to manage container attributes instead of users.
	AlphabetManagesAttributesKey = "AlphabetManagesAttributes"
)
