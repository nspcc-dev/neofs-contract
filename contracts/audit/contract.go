package audit

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

type (
	AuditHeader struct {
		Epoch int
		CID   []byte
		From  interop.PublicKey
	}
)

// Audit key is a combination of the epoch, the container ID and the public key of the node that
// has executed the audit. Together, it shouldn't be more than 64 bytes. We can't shrink
// epoch and container ID since we iterate over these values. But we can shrink
// public key by using first bytes of the hashed value.

// V2 format.
const maxKeySize = 24 // 24 + 32 (container ID length) + 8 (epoch length) = 64

func (a AuditHeader) ID() []byte {
	var buf any = a.Epoch

	hashedKey := crypto.Sha256(a.From)
	shortedKey := hashedKey[:maxKeySize]

	return append(buf.([]byte), append(a.CID, shortedKey...)...)
}

// nolint:deadcode,unused
func _deploy(data any, isUpdate bool) {
	ctx := storage.GetContext()
	if isUpdate {
		args := data.([]any)
		version := args[len(args)-1].(int)

		common.CheckVersion(version)

		// switch to notary mode if version of the current contract deployment is
		// earlier than v0.17.0 (initial version when non-notary mode was taken out of
		// use)
		// TODO: avoid number magic, add function for version comparison to common package
		if version < 17_000 {
			switchToNotary(ctx)
		}

		return
	}

	runtime.Log("audit contract initialized")
}

// re-initializes contract from non-notary to notary mode. Does nothing if
// action has already been done. The function is called on contract update with
// storage.Context from _deploy.
//
// switchToNotary removes values stored by 'netmapScriptHash' and 'notary' keys.
//
// nolint:unused
func switchToNotary(ctx storage.Context) {
	const notaryDisabledKey = "notary" // non-notary legacy

	notaryVal := storage.Get(ctx, notaryDisabledKey)
	if notaryVal == nil {
		runtime.Log("contract is already notarized")
		return
	}

	storage.Delete(ctx, notaryDisabledKey)
	storage.Delete(ctx, "netmapScriptHash")

	if notaryVal.(bool) {
		runtime.Log("contract successfully notarized")
	}
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data any) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("audit contract updated")
}

// Put method stores a stable marshalled `DataAuditResult` structure. It can be
// invoked only by Inner Ring nodes.
//
// Inner Ring nodes perform audit of containers and produce `DataAuditResult`
// structures. They are stored in audit contract and used for settlements
// in later epochs.
func Put(rawAuditResult []byte) {
	ctx := storage.GetContext()
	innerRing := common.InnerRingNodes()
	hdr := newAuditHeader(rawAuditResult)
	presented := false

	for i := range innerRing {
		if hdr.From.Equals(innerRing[i]) {
			presented = true

			break
		}
	}

	if !runtime.CheckWitness(hdr.From) || !presented {
		panic("put access denied")
	}

	storage.Put(ctx, hdr.ID(), rawAuditResult)

	runtime.Log("audit: result has been saved")
}

// Get method returns a stable marshaled DataAuditResult structure.
//
// The ID of the DataAuditResult can be obtained from listing methods.
func Get(id []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, id).([]byte)
}

// List method returns a list of all available DataAuditResult IDs from
// the contract storage.
func List() [][]byte {
	ctx := storage.GetReadOnlyContext()
	it := storage.Find(ctx, []byte{}, storage.KeysOnly)

	return list(it)
}

// ListByEpoch method returns a list of DataAuditResult IDs generated during
// the specified epoch.
func ListByEpoch(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()
	var buf any = epoch
	it := storage.Find(ctx, buf.([]byte), storage.KeysOnly)

	return list(it)
}

// ListByCID method returns a list of DataAuditResult IDs generated during
// the specified epoch for the specified container.
func ListByCID(epoch int, cid []byte) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var buf any = epoch

	prefix := append(buf.([]byte), cid...)
	it := storage.Find(ctx, prefix, storage.KeysOnly)

	return list(it)
}

// ListByNode method returns a list of DataAuditResult IDs generated in
// the specified epoch for the specified container by the specified Inner Ring node.
func ListByNode(epoch int, cid []byte, key interop.PublicKey) [][]byte {
	ctx := storage.GetReadOnlyContext()
	hdr := AuditHeader{
		Epoch: epoch,
		CID:   cid,
		From:  key,
	}

	it := storage.Find(ctx, hdr.ID(), storage.KeysOnly)

	return list(it)
}

func list(it iterator.Iterator) [][]byte {
	var result [][]byte

	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // iterator MUST BE `storage.KeysOnly`
		result = append(result, key)
	}

	return result
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

// readNext reads the length from the first byte, and then reads data (max 127 bytes).
func readNext(input []byte) ([]byte, int) {
	var buf any = input[0]
	ln := buf.(int)

	return input[1 : 1+ln], 1 + ln
}

func newAuditHeader(input []byte) AuditHeader {
	// V2 format
	offset := int(input[1])
	offset = 2 + offset + 1 // version prefix + version len + epoch prefix

	var buf any = input[offset : offset+8] // [ 8 integer bytes ]
	epoch := buf.(int)

	offset = offset + 8

	// cid is a nested structure with raw bytes
	// [ cid struct prefix (wireType + len = 2 bytes), cid value wireType (1 byte), ... ]
	cid, cidOffset := readNext(input[offset+2+1:])

	// key is a raw byte
	// [ public key wireType (1 byte), ... ]
	key, _ := readNext(input[offset+2+1+cidOffset+1:])

	return AuditHeader{
		epoch,
		cid,
		key,
	}
}
