package nodestate

// Type is an enumeration for node states.
type Type int

// Various node states.
const (
	_ Type = iota

	// Online stands for nodes that are in full network and
	// operational availability.
	Online

	// Offline stands for nodes that are in network unavailability.
	Offline

	// Maintenance stands for nodes under maintenance with partial
	// network availability.
	Maintenance
)
