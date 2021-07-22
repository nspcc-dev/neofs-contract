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
	auditHeader struct {
		epoch int
		cid   []byte
		from  interop.PublicKey
	}
)

// Audit key is a combination of epoch, container ID and public key of node that
// executed audit. Together it should be no more than 64 bytes. We can't shrink
// epoch and container ID since we iterate over these values. But we can shrink
// public key by using first bytes of the hashed value.

const maxKeySize = 24 // 24 + 32 (container ID length) + 8 (epoch length) = 64

func (a auditHeader) ID() []byte {
	var buf interface{} = a.epoch

	hashedKey := crypto.Sha256(a.from)
	shortedKey := hashedKey[:maxKeySize]

	return append(buf.([]byte), append(a.cid, shortedKey...)...)
}

const (
	version = 1

	netmapContractKey = "netmapScriptHash"

	notaryDisabledKey = "notary"
)

func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		return
	}

	args := data.([]interface{})
	notaryDisabled := args[0].(bool)
	owner := args[1].(interop.Hash160)
	addrNetmap := args[2].(interop.Hash160)

	ctx := storage.GetContext()

	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	if len(addrNetmap) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, common.OwnerKey, owner)
	storage.Put(ctx, netmapContractKey, addrNetmap)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, notaryDisabled)
	if notaryDisabled {
		runtime.Log("audit contract notary disabled")
	}

	runtime.Log("audit contract initialized")
}

func Migrate(script []byte, manifest []byte, data interface{}) bool {
	ctx := storage.GetReadOnlyContext()

	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	contract.Call(interop.Hash160(management.Hash), "update", contract.All, script, manifest, data)
	runtime.Log("audit contract updated")

	return true
}

func Put(rawAuditResult []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var innerRing []common.IRNode

	if notaryDisabled {
		netmapContract := storage.Get(ctx, netmapContractKey).(interop.Hash160)
		innerRing = common.InnerRingNodesFromNetmap(netmapContract)
	} else {
		innerRing = common.InnerRingNodes()
	}

	hdr := newAuditHeader(rawAuditResult)
	presented := false

	for i := range innerRing {
		ir := innerRing[i]
		if common.BytesEqual(ir.PublicKey, hdr.from) {
			presented = true

			break
		}
	}

	if !runtime.CheckWitness(hdr.from) || !presented {
		panic("audit: put access denied")
	}

	storage.Put(ctx, hdr.ID(), rawAuditResult)

	runtime.Log("audit: result has been saved")
}

func Get(id []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, id).([]byte)
}

func List() [][]byte {
	ctx := storage.GetReadOnlyContext()
	it := storage.Find(ctx, []byte{}, storage.KeysOnly)

	return list(it)
}

func ListByEpoch(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()
	var buf interface{} = epoch
	it := storage.Find(ctx, buf.([]byte), storage.KeysOnly)

	return list(it)
}

func ListByCID(epoch int, cid []byte) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var buf interface{} = epoch

	prefix := append(buf.([]byte), cid...)
	it := storage.Find(ctx, prefix, storage.KeysOnly)

	return list(it)
}

func ListByNode(epoch int, cid []byte, key interop.PublicKey) [][]byte {
	ctx := storage.GetReadOnlyContext()
	hdr := auditHeader{
		epoch: epoch,
		cid:   cid,
		from:  key,
	}

	it := storage.Find(ctx, hdr.ID(), storage.KeysOnly)

	return list(it)
}

func list(it iterator.Iterator) [][]byte {
	var result [][]byte

	ignore := [][]byte{
		[]byte(netmapContractKey),
		[]byte(common.OwnerKey),
		[]byte(notaryDisabledKey),
	}

loop:
	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // iterator MUST BE `storage.KeysOnly`
		for _, ignoreKey := range ignore {
			if common.BytesEqual(key, ignoreKey) {
				continue loop
			}
		}

		result = append(result, key)
	}

	return result
}

func Version() int {
	return version
}

// readNext reads length from first byte and then reads data (max 127 bytes).
func readNext(input []byte) ([]byte, int) {
	var buf interface{} = input[0]
	ln := buf.(int)

	return input[1 : 1+ln], 1 + ln
}

func newAuditHeader(input []byte) auditHeader {
	offset := int(input[1])
	offset = 2 + offset + 1 // version prefix + version len + epoch prefix

	var buf interface{} = input[offset : offset+8] // [ 8 integer bytes ]
	epoch := buf.(int)

	offset = offset + 8

	// cid is a nested structure with raw bytes
	// [ cid struct prefix (wireType + len = 2 bytes), cid value wireType (1 byte), ... ]
	cid, cidOffset := readNext(input[offset+2+1:])

	// key is a raw byte
	// [ public key wireType (1 byte), ... ]
	key, _ := readNext(input[offset+2+1+cidOffset+1:])

	return auditHeader{
		epoch,
		cid,
		key,
	}
}
