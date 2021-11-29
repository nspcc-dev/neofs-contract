package common

var (
	mintPrefix         = []byte{0x01}
	burnPrefix         = []byte{0x02}
	lockPrefix         = []byte{0x03}
	unlockPrefix       = []byte{0x04}
	containerFeePrefix = []byte{0x10}
)

func WalletToScriptHash(wallet []byte) []byte {
	// V2 format
	return wallet[1 : len(wallet)-4]
}

func MintTransferDetails(txDetails []byte) []byte {
	return append(mintPrefix, txDetails...)
}

func BurnTransferDetails(txDetails []byte) []byte {
	return append(burnPrefix, txDetails...)
}

func LockTransferDetails(txDetails []byte) []byte {
	return append(lockPrefix, txDetails...)
}

func UnlockTransferDetails(epoch int) []byte {
	var buf interface{} = epoch
	return append(unlockPrefix, buf.([]byte)...)
}

func ContainerFeeTransferDetails(cid []byte) []byte {
	return append(containerFeePrefix, cid...)
}
