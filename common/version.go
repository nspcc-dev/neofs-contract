package common

import "github.com/nspcc-dev/neo-go/pkg/interop/native/std"

const (
	major = 0
	minor = 16
	patch = 0

	// Versions from which an update should be performed.
	// These should be used in a group (so prevMinor can be equal to minor if there are
	// any migration routines.
	prevMajor = 0
	prevMinor = 15
	prevPatch = 4

	Version = major*1_000_000 + minor*1_000 + patch

	PrevVersion = prevMajor*1_000_000 + prevMinor*1_000 + prevPatch

	// ErrVersionMismatch is thrown by CheckVersion in case of error.
	ErrVersionMismatch = "previous version mismatch"

	// ErrAlreadyUpdated is thrown by CheckVersion if current version equals to version contract
	// is being updated from.
	ErrAlreadyUpdated = "contract is already of the latest version"
)

// CheckVersion checks that previous version is more than PrevVersion to ensure migrating contract data
// was done successfully.
func CheckVersion(from int) {
	if from < PrevVersion {
		panic(ErrVersionMismatch + ": expected >=" + std.Itoa(PrevVersion, 10))
	}
	if from == Version {
		panic(ErrAlreadyUpdated + ": " + std.Itoa(Version, 10))
	}
}

// AppendVersion appends current contract version to the list of deploy arguments.
func AppendVersion(data interface{}) []interface{} {
	if data == nil {
		return []interface{}{Version}
	}
	return append(data.([]interface{}), Version)
}
