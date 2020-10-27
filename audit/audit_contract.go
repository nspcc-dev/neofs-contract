package auditcontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/engine"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

type (
	irNode struct {
		key []byte
	}

	CheckedNode struct {
		Key    []byte // 33 bytes
		Pair   int    // 2 bytes
		Reward int    // ? up to 32 byte
	}

	AuditResult struct {
		InnerRingNode  []byte // 33 bytes
		Epoch          int    // 8 bytes
		ContainerID    []byte // 32 bytes
		StorageGroupID []byte // 16 bytes
		PoR            bool   // 1 byte
		PDP            bool   // 1 byte
		// --- 91 bytes -- //
		// --- 2 more bytes to size of the []CheckedNode //
		Nodes []CheckedNode // <= 67 bytes per node
		// about 1400 nodes may be presented in container
	}
)

const (
	version = 1

	// 1E-8 GAS in precision of balance container.
	// This value may be calculated in runtime based on decimal value of
	// balance contract. We can also provide methods to change fee
	// in runtime.
	auditFee = 1 * 100_000_000

	ownerIDLength = 25

	journalKey           = "auditJournal"
	balanceContractKey   = "balanceScriptHash"
	containerContractKey = "containerScriptHash"
	netmapContractKey    = "netmapScriptHash"
)

var (
	auditFeeTransferMsg    = []byte("audit execution fee")
	auditRewardTransferMsg = []byte("data audit reward")

	ctx storage.Context
)

func init() {
	if runtime.GetTrigger() != runtime.Application {
		panic("contract has not been called in application node")
	}

	ctx = storage.GetContext()
}

func Init(addrNetmap, addrBalance, addrContainer []byte) {
	if storage.Get(ctx, netmapContractKey) != nil &&
		storage.Get(ctx, balanceContractKey) != nil &&
		storage.Get(ctx, containerContractKey) != nil {
		panic("init: contract already deployed")
	}

	if len(addrNetmap) != 20 || len(addrBalance) != 20 || len(addrContainer) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, addrNetmap)
	storage.Put(ctx, balanceContractKey, addrBalance)
	storage.Put(ctx, containerContractKey, addrContainer)

	setSerialized(ctx, journalKey, []AuditResult{})

	runtime.Log("audit contract initialized")
}

func Put(rawAuditResult []byte) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	innerRing := engine.AppCall(netmapContractAddr, "innerRingList").([]irNode)

	auditResult, err := newAuditResult(rawAuditResult)
	if err {
		panic("put: can't parse audit result")
	}

	var presented = false

	for i := range innerRing {
		ir := innerRing[i]
		if bytesEqual(ir.key, auditResult.InnerRingNode) {
			presented = true
			break
		}
	}

	if !runtime.CheckWitness(auditResult.InnerRingNode) || !presented {
		panic("put: access denied")
	}

	// todo: limit size of the audit journal:
	//       history will be stored in chain (args or notifies)
	//       contract storage will be used as a cache if needed
	journal := getAuditResult(ctx)
	journal = append(journal, auditResult)

	setSerialized(ctx, journalKey, journal)

	if auditResult.PDP && auditResult.PoR {
		// find who is the ownerID
		containerContract := storage.Get(ctx, containerContractKey).([]byte)

		// todo: implement easy way to get owner from the container id
		ownerID := engine.AppCall(containerContract, "owner", auditResult.ContainerID).([]byte)
		if len(ownerID) != ownerIDLength {
			runtime.Log("put: can't get owner id of the container")

			return false
		}

		ownerScriptHash := walletToScripHash(ownerID)

		// transfer fee to the inner ring node
		balanceContract := storage.Get(ctx, balanceContractKey).([]byte)
		irScriptHash := contract.CreateStandardAccount(auditResult.InnerRingNode)

		tx := engine.AppCall(balanceContract, "transferX",
			ownerScriptHash,
			irScriptHash,
			auditFee,
			auditFeeTransferMsg, // todo: add epoch, container and storage group info
		)
		if !tx.(bool) {
			panic("put: can't transfer inner ring fee")
		}

		for i := 0; i < len(auditResult.Nodes); i++ {
			node := auditResult.Nodes[i]
			nodeScriptHash := contract.CreateStandardAccount(node.Key)

			tx := engine.AppCall(balanceContract, "transferX",
				ownerScriptHash,
				nodeScriptHash,
				node.Reward,
				auditRewardTransferMsg, // todo: add epoch, container and storage group info
			)
			if !tx.(bool) {
				runtime.Log("put: can't transfer storage payment")

				return false
			}
		}
	}

	return true
}

func Version() int {
	return version
}

func newAuditResult(data []byte) (AuditResult, bool) {
	var (
		tmp    interface{}
		ln     = len(data)
		result = AuditResult{
			InnerRingNode:  nil, // neo-go#949
			ContainerID:    nil,
			StorageGroupID: nil,
			Nodes:          []CheckedNode{},
		}
	)

	if len(data) < 91 { // all required headers
		runtime.Log("newAuditResult: can't parse audit result header")
		return result, true
	}

	result.InnerRingNode = data[0:33]

	epoch := data[33:41]
	tmp = epoch
	result.Epoch = tmp.(int)

	result.ContainerID = data[41:73]
	result.StorageGroupID = data[73:89]
	result.PoR = util.Equals(data[90], 0x01)
	result.PDP = util.Equals(data[91], 0x01)

	// if there are nodes, that were checked
	if len(data) > 93 {
		rawCounter := data[91:93]
		tmp = rawCounter
		counter := tmp.(int)

		ptr := 93

		for i := 0; i < counter; i++ {
			if ptr+33+2+32 > ln {
				runtime.Log("newAuditResult: broken node")
				return result, false
			}

			node := CheckedNode{
				Key: nil, // neo-go#949
			}
			node.Key = data[ptr : ptr+33]

			pair := data[ptr+33 : ptr+35]
			tmp = pair
			node.Pair = tmp.(int)

			reward := data[ptr+35 : ptr+67]
			tmp = reward
			node.Reward = tmp.(int)

			result.Nodes = append(result.Nodes, node)
		}
	}

	return result, false
}

func getAuditResult(ctx storage.Context) []AuditResult {
	data := storage.Get(ctx, journalKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]AuditResult)
	}

	return []AuditResult{}
}

func setSerialized(ctx storage.Context, key interface{}, value interface{}) {
	data := binary.Serialize(value)
	storage.Put(ctx, key, data)
}

func walletToScripHash(wallet []byte) []byte {
	return wallet[1 : len(wallet)-4]
}

// neo-go#1176
func bytesEqual(a []byte, b []byte) bool {
	return util.Equals(string(a), string(b))
}
