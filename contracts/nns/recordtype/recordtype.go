package recordtype

// Type is domain name service record types.
type Type byte

// Record types are defined in [RFC 1035](https://tools.ietf.org/html/rfc1035)
const (
	// A represents address record type.
	A Type = 1
	// CNAME represents canonical name record type.
	CNAME Type = 5
	// SOA represents start of authority record type.
	SOA Type = 6
	// TXT represents text record type.
	TXT Type = 16
)

// Record types are defined in [RFC 3596](https://tools.ietf.org/html/rfc3596)
const (
	// AAAA represents IPv6 address record type.
	AAAA Type = 28
)
