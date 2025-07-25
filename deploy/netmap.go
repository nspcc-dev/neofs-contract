package deploy

const (
	MaxObjectSizeConfig           = "MaxObjectSize"
	BasicIncomeRateConfig         = "BasicIncomeRate"
	EpochDurationConfig           = "EpochDuration"
	ContainerFeeConfig            = "ContainerFee"
	ContainerAliasFeeConfig       = "ContainerAliasFee"
	EigenTrustIterationsConfig    = "EigenTrustIterations"
	EigenTrustAlphaConfig         = "EigenTrustAlpha"
	WithdrawFeeConfig             = "WithdrawFee"
	HomomorphicHashingDisabledKey = "HomomorphicHashingDisabled"
	MaintenanceModeAllowedConfig  = "MaintenanceModeAllowed"
)

// RawNetworkParameter is a NeoFS network parameter which is transmitted but
// not interpreted by the NeoFS API protocol.
type RawNetworkParameter struct {
	// Name of the parameter.
	Name string

	// Raw parameter value.
	Value []byte
}

// NetworkConfiguration represents NeoFS network configuration stored
// in FS chain.
type NetworkConfiguration struct {
	MaxObjectSize              uint64
	StoragePrice               uint64
	EpochDuration              uint64
	ContainerFee               uint64
	ContainerAliasFee          uint64
	EigenTrustIterations       uint64
	EigenTrustAlpha            float64
	WithdrawalFee              uint64
	HomomorphicHashingDisabled bool
	MaintenanceModeAllowed     bool
	Raw                        []RawNetworkParameter
}
