package nns

// A set of standard contract names deployed into NeoFS sidechain.
const (
	// NameAlphabetPrefix differs from other names in this list, because
	// in reality there will be multiple alphabets contract deployed to
	// a network named alphabet0, alphabet1, alphabet2, etc.
	NameAlphabetPrefix = "alphabet"
	NameAudit          = "audit"
	NameBalance        = "balance"
	NameContainer      = "container"
	NameNeoFSID        = "neofsid"
	NameNetmap         = "netmap"
	NameProxy          = "proxy"
	NameReputation     = "reputation"
)
