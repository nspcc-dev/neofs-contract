package proxy

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	containerContractKey = "cnr"
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

// Verify checks whether carrier transaction contains either (2/3N + 1) or
// (N/2 + 1) valid multi-signature of the NeoFS Alphabet.
// Container contract's `SubmitObjectPut` is an exception, Alphabet signature
// is not required, contract's `VerifyPlacementSignatures` is called instead.
func Verify() bool {
	cnrH := storage.Get(storage.GetReadOnlyContext(), containerContractKey).(interop.Hash160)
	tx := runtime.GetScriptContainer()
	if string(tx.Script[len(tx.Script)-5:]) != string([]byte{65, 98, 125, 91, 82}) { // SystemContractCall
		return common.ContainsAlphabetWitness()
	}
	scriptNoSyscall := tx.Script[:len(tx.Script)-5]
	scriptLen := len(scriptNoSyscall)
	if !cnrH.Equals(scriptNoSyscall[scriptLen-20:]) {
		return common.ContainsAlphabetWitness()
	}

	scriptNoContract := scriptNoSyscall[:scriptLen-20-1-1]
	scriptLen = len(scriptNoContract)
	expMethod := "submitObjectPut"
	if scriptLen < len(expMethod) {
		return common.ContainsAlphabetWitness()
	}
	if string(scriptNoContract[scriptLen-len(expMethod):]) != expMethod {
		return common.ContainsAlphabetWitness()
	}

	scriptArgsOnly := scriptNoContract[:scriptLen-len(expMethod)-1-1]
	scriptLen = len(scriptArgsOnly)
	var sigs []interop.Signature
	var metaData []byte
	for i := 0; i < scriptLen; {
		if scriptArgsOnly[i] == 12 { // PUSHDATA1
			newI := i + 2 + interop.SignatureLen
			sigs = append(sigs, scriptArgsOnly[i+2:newI])
			i = newI

			continue
		}

		if scriptArgsOnly[i] != 192 { // PACK
			i++
			continue
		}
		i++

		var metaLen int
		metaLenInBytes := int(scriptArgsOnly[i]) - 11 // PUSHDATA1 to PUSHDATA4
		i++
		//nolint:intrange
		for j := 0; j < metaLenInBytes; j++ {
			metaLen += int(scriptArgsOnly[i]) << (8 * j)
			i++
		}

		metaData = scriptArgsOnly[i : i+metaLen]

		break
	}

	metaMap := std.Deserialize(metaData).(map[string]any)
	cID := metaMap["cid"].(interop.Hash256)

	return contract.Call(cnrH, "verifyPlacementSignatures", contract.ReadOnly, cID, metaData, sigs).(bool)
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}
