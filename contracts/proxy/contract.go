package proxy

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	containerContractKey = "c"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
func OnNEP17Payment(from interop.Hash160, amount int, data any) {
	caller := runtime.GetCallingScriptHash()
	if !caller.Equals(gas.Hash) {
		common.AbortWithMessage("proxy contract accepts GAS only")
	}
}

// nolint:deadcode,unused
func _deploy(data any, isUpdate bool) {
	ctx := storage.GetContext()
	args := data.([]any)

	if isUpdate {
		version := args[len(args)-1].(int)
		common.CheckVersion(version)

		if version < 23_000 {
			addrNNS := common.InferNNSHash()
			if len(addrNNS) != interop.Hash160Len {
				panic("do not know NNS hash")
			}
			addrContainer := common.ResolveFSContractWithNNS(addrNNS, "container")
			if len(addrContainer) != interop.Hash160Len {
				panic("NNS does not know Container address")
			}
			storage.Put(ctx, containerContractKey, addrContainer)
		}

		return
	}

	addrNNS := common.InferNNSHash()
	if len(addrNNS) != interop.Hash160Len {
		panic("do not know NNS hash")
	}
	addrContainer := common.ResolveFSContractWithNNS(addrNNS, "container")
	if len(addrContainer) != interop.Hash160Len {
		panic("NNS does not know Container address")
	}
	storage.Put(ctx, containerContractKey, addrContainer)

	runtime.Log("proxy contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(nefFile, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, nefFile, manifest, common.AppendVersion(data))
	runtime.Log("proxy contract updated")
}

const submitObjectPutMethod = "submitObjectPut"

// Verify checks whether carrier transaction contains either (2/3N + 1) or
// (N/2 + 1) valid multi-signature of the NeoFS Alphabet.
// Container contract's `SubmitObjectPut` is an exception, Alphabet signature
// is not required, contract's `VerifyPlacementSignatures` is called instead.
func Verify() bool {
	if common.ContainsAlphabetWitness() {
		return true
	}

	// Container's `SubmitObjectPut` case is only expected then

	var (
		script          = runtime.GetScriptContainer().Script
		i               int
		vectors         [][]interop.Signature
		placementVector []interop.Signature
		vectorsLen      int
		done            bool
	)
	for i < len(script) && !done {
		op := script[i]
		switch {
		case op == 0x0C: // PUSHDATA1
			placementVector, i = parseSignaturesVector(i, script)
			vectors = append(vectors, placementVector)
		case op <= 0x05: // PUSHINT8 to PUSHINT256
			lenInBytes := int(op + 1)
			i++
			vectorsLen = convert.ToInteger(script[i : i+lenInBytes])
			i += lenInBytes
			done = true
		case 0x10 < op && op <= 0x20: // PUSH1 to PUSH16
			vectorsLen = int(op - 0x10)
			i++
			done = true
		default:
			panic("invalid op in signatures " + std.Itoa(int(op), 16))
		}
	}
	if vectorsLen != len(vectors) {
		panic("number of vectors and pack opcode do not match")
	}
	if script[i] != 0xC0 { // PACK
		panic("vectors array does not start with PACK")
	}
	i++

	op := int(script[i])
	i++
	var metaLenInBytes int
	switch op {
	case 0x0C: // PUSHDATA1
		metaLenInBytes = 1
	case 0x0D: // PUSHDATA2
		metaLenInBytes = 2
	case 0x0E: // PUSHDATA4
		metaLenInBytes = 4
	default:
		panic("invalid meta push opcode " + std.Itoa(op, 16))
	}
	var metaLen int
	//nolint:intrange
	for j := 0; j < metaLenInBytes; j++ {
		metaLen += int(script[i]) << (8 * j)
		i++
	}

	metaData := script[i : i+metaLen]
	i += metaLen
	metaMap := std.Deserialize(metaData).(map[string]any)
	cID := metaMap["cid"].(interop.Hash256)
	if len(cID) != interop.Hash256Len {
		panic("invalid cID length " + std.Itoa(len(cID), 10))
	}

	if !(script[i] == 0x12 && script[i+1] == 0xC0) { // PUSH2 and PACK for method's args packing
		panic("invalid method arguments packing")
	}
	i += 2

	if !(0x10 <= script[i] && script[i] <= 0x1F) { // PUSH0 to PUSH15
		panic("invalid call flag " + std.Itoa(int(script[i]), 16))
	}
	i++

	expMethodLen := len(submitObjectPutMethod)
	if !(script[i] == 0x0C &&
		script[i+1] == byte(expMethodLen) &&
		string(script[i+2:i+2+expMethodLen]) == submitObjectPutMethod) {
		panic("invalid method name")
	}
	i += 2 + expMethodLen

	if !(script[i] == 0x0C && script[i+1] == byte(interop.Hash160Len)) {
		panic("invalid container address")
	}
	i += 2

	cnrH := storage.Get(storage.GetReadOnlyContext(), containerContractKey).(interop.Hash160)
	if !cnrH.Equals(script[i : i+interop.Hash160Len]) {
		panic("not a Container contract call")
	}
	i += interop.Hash160Len

	if i+5 != len(script) {
		panic("unexpected opcodes found in script")
	}

	if string(script[i:]) != string([]byte{0x41, 0x62, 0x7D, 0x5B, 0x52}) { // SystemContractCall
		panic("not a contract call")
	}

	return contract.Call(cnrH, "verifyPlacementSignatures", contract.ReadOnly, cID, metaData, vectors).(bool)
}

func parseSignaturesVector(i int, script []byte) ([]interop.Signature, int) {
	var (
		vector  []interop.Signature
		sigsLen int
	)
	for i < len(script) {
		op := script[i]
		var done bool

		switch {
		case op == 0x0C: // PUSHDATA1
			i++
			if script[i] != interop.SignatureLen {
				panic("invalid signature length " + std.Itoa(int(script[i]), 10))
			}
			i++
			vector = append(vector, script[i:i+interop.SignatureLen])
			i += interop.SignatureLen
		case op <= 0x05: // PUSHINT8 to PUSHINT256
			lenInBytes := int(op + 1)
			i++
			sigsLen = convert.ToInteger(script[i : i+lenInBytes])
			i += lenInBytes
			done = true
		case 0x10 < op && op <= 0x20: // PUSH1 to PUSH16
			sigsLen = int(op - 0x10)
			i++
			done = true
		default:
			panic("invalid op in signatures " + std.Itoa(int(op), 16))
		}

		if done {
			break
		}
	}
	if sigsLen != len(vector) {
		panic("number of signatures in vector and pack opcode do not match")
	}
	if script[i] != 0xC0 { // PACK
		panic("signatures vector array does not start with PACK")
	}
	i++

	return vector, i
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}
