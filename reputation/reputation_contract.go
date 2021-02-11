package reputationcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const version = 1

const (
	peerIDSize   = 46 // NeoFS PeerIDSize
	trustValSize = 8  // float64

	trustStructSize = 0 +
		peerIDSize + // manager ID
		peerIDSize + // trusted ID
		trustValSize // trust value
)

var (
	trustJournalPrefix = []byte("trustJournal")

	ctx storage.Context
)

func init() {
	ctx = storage.GetContext()
}

func Init(owner interop.Hash160) {
	if !common.HasUpdateAccess(ctx) {
		panic("only owner can reinitialize contract")
	}

	storage.Put(ctx, common.OwnerKey, owner)

	runtime.Log("reputation contract initialized")
}

func Migrate(script []byte, manifest []byte) bool {
	if !common.HasUpdateAccess(ctx) {
		runtime.Log("only owner can update contract")
		return false
	}

	management.Update(script, manifest)
	runtime.Log("reputation contract updated")

	return true
}

func Put(manager, epoch, typ []byte, newTrustList [][]byte) bool {
	if !runtime.CheckWitness(manager) {
		panic("put: incorrect manager key")
	}

	for i := 0; i < len(newTrustList); i++ {
		trustData := newTrustList[i]

		if len(trustData) != trustStructSize {
			panic("list: invalid trust value")
		}
	}

	// todo: consider using notification for inner ring node

	// todo: limit size of the trust journal:
	//       history will be stored in chain (args or notifies)
	//       contract storage will be used as a cache if needed
	key := append(trustJournalPrefix, append(epoch, typ...)...)

	trustList := common.GetList(ctx, key)

	// fixme: with neo3.0 it is kinda unnecessary
	if len(trustList) == 0 {
		// if journal slice is not initialized, then `append` will throw
		trustList = newTrustList
	} else {
		for i := 0; i < len(newTrustList); i++ {
			trustList = append(trustList, newTrustList[i])
		}
	}

	common.SetSerialized(ctx, key, trustList)

	runtime.Log("trust list was successfully updated")

	return true
}

func List(epoch, typ []byte) [][]byte {
	key := append(trustJournalPrefix, append(epoch, typ...)...)

	return common.GetList(ctx, key)
}
