package alphabet

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/notary"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

const (
	netmapKey = "netmapScriptHash"
	proxyKey  = "proxyScriptHash"

	indexKey = "index"
	totalKey = "threshold"
	nameKey  = "name"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS and NEO
// contracts.
func OnNEP17Payment(from interop.Hash160, amount int, data any) {
	caller := runtime.GetCallingScriptHash()
	if !caller.Equals(gas.Hash) && !caller.Equals(neo.Hash) {
		common.AbortWithMessage("alphabet contract accepts GAS and NEO only")
	}
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
			switchToNotary(ctx, args)
		}

		return
	}

	args := data.(struct {
		_          bool // notaryDisabled
		addrNetmap interop.Hash160
		addrProxy  interop.Hash160
		name       string
		index      int
		total      int
	})

	if len(args.addrNetmap) != interop.Hash160Len {
		args.addrNetmap = common.ResolveFSContract("netmap")
	}
	if len(args.addrProxy) != interop.Hash160Len {
		args.addrProxy = common.ResolveFSContract("proxy")
	}

	storage.Put(ctx, netmapKey, args.addrNetmap)
	storage.Put(ctx, proxyKey, args.addrProxy)
	storage.Put(ctx, nameKey, args.name)
	storage.Put(ctx, indexKey, args.index)
	storage.Put(ctx, totalKey, args.total)

	runtime.Log(args.name + " contract initialized")
}

// re-initializes contract from non-notary to notary mode. Does nothing if
// action has already been done. The function is called on contract update with
// storage.Context and parameters from _deploy.
//
// switchToNotary panics if address of the Proxy contract (3rd parameter) is
// missing or invalid. Otherwise, on success, the address is stored by
// 'proxyScriptHash' key.
//
// If contract stores non-empty value by 'ballots' key, switchToNotary panics.
// Otherwise, existing value is removed.
//
// switchToNotary removes value stored by 'notary' key.
//
// nolint:unused
func switchToNotary(ctx storage.Context, args []any) {
	const notaryDisabledKey = "notary" // non-notary legacy
	contractName := args[3].(string)

	notaryVal := storage.Get(ctx, notaryDisabledKey)
	if notaryVal == nil {
		runtime.Log(contractName + " contract is already notarized")
		return
	} else if notaryVal.(bool) {
		proxyContract := args[2].(interop.Hash160)
		if len(proxyContract) > 0 {
			if len(proxyContract) != interop.Hash160Len {
				panic("address of the Proxy contract is missing or invalid")
			}
		} else {
			proxyContract = common.ResolveFSContract("proxy")
		}

		if !common.TryPurgeVotes(ctx) {
			panic("pending vote detected")
		}

		// distribute 75% of available GAS:
		//  - 50% to Proxy contract
		//  - the rest is evenly distributed between Inner Ring and storage nodes
		netmapContract := args[1].(interop.Hash160)
		if len(netmapContract) > 0 {
			if len(netmapContract) != interop.Hash160Len {
				panic("address of the Netmap contract is invalid")
			}
		} else {
			netmapContract = storage.Get(ctx, netmapKey).(interop.Hash160)
		}

		storageNodes := contract.Call(netmapContract, "netmap", contract.ReadOnly).([]struct {
			blob []byte // see netmap.Node
		})
		innerRingNodes := common.InnerRingNodesFromNetmap(netmapContract)

		currentContract := runtime.GetExecutingScriptHash()
		currentGAS := gas.BalanceOf(currentContract) * 3 / 4

		if currentGAS == 0 {
			panic("no GAS in the contract")
		}

		toTransfer := currentGAS / 2
		if !gas.Transfer(currentContract, proxyContract, toTransfer, nil) {
			panic("failed to transfer half of GAS to Proxy contract")
		}

		toTransfer = currentGAS - toTransfer

		nNodes := len(storageNodes) + len(innerRingNodes)
		perNodeGAS := toTransfer / nNodes
		// half of GAS goes to node contract, the rest to its notary deposit
		perNodeGASNotary := perNodeGAS / 2

		// limit notary deposit
		const notaryDepositLimit = 20_0000_0000 // TODO: always fixed8?
		if perNodeGASNotary > notaryDepositLimit {
			perNodeGASNotary = notaryDepositLimit
		}

		perNodeGASSimple := perNodeGAS - perNodeGASNotary

		// see https://github.com/nspcc-dev/neo-go/blob/v0.101.0/docs/notary.md#1-notary-deposit
		const lockInterval = 6 * 30 * 24 * 60 * 4 // 6 months blocks of 15s
		notaryTransferData := []any{
			nil,                                  // receiver account (set in loop)
			ledger.CurrentIndex() + lockInterval, // till
		}

		for i := 0; i < len(innerRingNodes); i++ {
			addr := contract.CreateStandardAccount(innerRingNodes[i])
			if !gas.Transfer(currentContract, addr, perNodeGASSimple, nil) {
				panic("failed to transfer part of GAS to the Inner Ring node")
			}

			notaryTransferData[0] = addr
			if !gas.Transfer(currentContract, interop.Hash160(notary.Hash), perNodeGASNotary, notaryTransferData) {
				panic("failed to make notary deposit for the Inner Ring node")
			}
		}

		for i := 0; i < len(storageNodes); i++ {
			publicKey := storageNodes[i].blob[2:35] // hardcoded because there was no other way
			addr := contract.CreateStandardAccount(publicKey)
			if !gas.Transfer(currentContract, addr, perNodeGASSimple, nil) {
				panic("failed to transfer part of GAS to the storage node")
			}

			notaryTransferData[0] = addr
			if !gas.Transfer(currentContract, interop.Hash160(notary.Hash), perNodeGASNotary, notaryTransferData) {
				panic("failed to make notary deposit for the storage node")
			}
		}

		storage.Put(ctx, proxyKey, proxyContract)
	}

	storage.Delete(ctx, notaryDisabledKey)

	if notaryVal.(bool) {
		runtime.Log(contractName + " contract successfully notarized")
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
	runtime.Log("alphabet contract updated")
}

// Gas returns the amount of the sidechain GAS stored in the contract account.
func Gas() int {
	return gas.BalanceOf(runtime.GetExecutingScriptHash())
}

// Neo returns the amount of sidechain NEO stored in the contract account.
func Neo() int {
	return neo.BalanceOf(runtime.GetExecutingScriptHash())
}

func currentEpoch(ctx storage.Context) int {
	netmapContractAddr := storage.Get(ctx, netmapKey).(interop.Hash160)
	return contract.Call(netmapContractAddr, "epoch", contract.ReadOnly).(int)
}

func name(ctx storage.Context) string {
	return storage.Get(ctx, nameKey).(string)
}

func index(ctx storage.Context) int {
	return storage.Get(ctx, indexKey).(int)
}

func checkPermission(ir []interop.PublicKey) bool {
	ctx := storage.GetReadOnlyContext()
	index := index(ctx) // read from contract memory

	if len(ir) <= index {
		return false
	}

	node := ir[index]
	return runtime.CheckWitness(node)
}

// Emit method produces sidechain GAS and distributes it among Inner Ring nodes
// and proxy contract. It can be invoked only by an Alphabet node of the Inner Ring.
//
// To produce GAS, an alphabet contract transfers all available NEO from the
// contract account to itself. 50% of the GAS in the contract account are
// transferred to proxy contract. 43.75% of the GAS are equally distributed
// among all Inner Ring nodes. Remaining 6.25% of the GAS stay in the contract.
func Emit() {
	ctx := storage.GetReadOnlyContext()

	alphabet := common.AlphabetNodes()
	if !checkPermission(alphabet) {
		panic("invalid invoker")
	}

	contractHash := runtime.GetExecutingScriptHash()

	if !neo.Transfer(contractHash, contractHash, neo.BalanceOf(contractHash), nil) {
		panic("failed to transfer funds, aborting")
	}

	gasBalance := gas.BalanceOf(contractHash)

	proxyAddr := storage.Get(ctx, proxyKey).(interop.Hash160)

	proxyGas := gasBalance / 2
	if proxyGas == 0 {
		panic("no gas to emit")
	}

	if !gas.Transfer(contractHash, proxyAddr, proxyGas, nil) {
		runtime.Log("could not transfer GAS to proxy contract")
	}

	gasBalance -= proxyGas

	runtime.Log("utility token has been emitted to proxy contract")

	innerRing := common.InnerRingNodes()

	gasPerNode := gasBalance * 7 / 8 / len(innerRing)

	if gasPerNode != 0 {
		for _, node := range innerRing {
			address := contract.CreateStandardAccount(node)
			if !gas.Transfer(contractHash, address, gasPerNode, nil) {
				runtime.Log("could not transfer GAS to one of IR node")
			}
		}

		runtime.Log("utility token has been emitted to inner ring nodes")
	}
}

// Vote method votes for the sidechain committee. It requires multisignature from
// Alphabet nodes of the Inner Ring.
//
// This method is used when governance changes the list of Alphabet nodes of the
// Inner Ring. Alphabet nodes share keys with sidechain validators, therefore
// it is required to change them as well. To do that, NEO holders (which are
// alphabet contracts) should vote for a new committee.
func Vote(epoch int, candidates []interop.PublicKey) {
	ctx := storage.GetContext()
	index := index(ctx)
	name := name(ctx)

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	curEpoch := currentEpoch(ctx)
	if epoch != curEpoch {
		panic("invalid epoch")
	}

	candidate := candidates[index%len(candidates)]
	address := runtime.GetExecutingScriptHash()

	ok := neo.Vote(address, candidate)
	if ok {
		runtime.Log(name + ": successfully voted for validator")
	} else {
		runtime.Log(name + ": vote has been failed")
	}
}

// Name returns the Glagolitic name of the contract.
func Name() string {
	ctx := storage.GetReadOnlyContext()
	return name(ctx)
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}
