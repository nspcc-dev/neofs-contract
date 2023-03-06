package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/actor"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/vmstate"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/nspcc-dev/neofs-contract/nns"
)

// rpcNeo represents Neo blockchain service provided via RPC needed for NeoFS
// test purposes.
type rpcNeo interface {
	// Close closes RPC connection and frees internal resources.
	Close()

	// GetContractStateByID receives state.Contract of the smart contract by its ID.
	GetContractStateByID(int32) (*state.Contract, error)

	// GetContractStateByHash receives state.Contract of the smart contract by its
	// address.
	GetContractStateByHash(util.Uint160) (*state.Contract, error)

	// GetStateRootByHeight returns state.MPTRoot at specified blockchain height.
	GetStateRootByHeight(block uint32) (*state.MPTRoot, error)

	// FindStates returns historical contract storage item states at the given state
	// root. The contract is referenced by specified address. Zero values of rest
	// parameters mean full storage history.
	FindStates(stateRoot util.Uint256, contract util.Uint160, historicalPrefix, start []byte, maxCount *int) (result.FindStates, error)
}

// wrapper over rpcNeo providing NeoFS blockchain services needed for current command.
type remoteBlockchain struct {
	rpc   rpcNeo
	actor *actor.Actor

	currentBlock uint32
}

// newRemoteBlockChain dials Neo RPC server and returns remoteBlockchain based
// on the opened connection. Connection and all requests are done within 15
// timeout.
func newRemoteBlockChain(blockChainRPCEndpoint string) (*remoteBlockchain, error) {
	acc, err := wallet.NewAccount()
	if err != nil {
		return nil, fmt.Errorf("generate new Neo account: %w", err)
	}

	c, err := rpcclient.New(context.Background(), blockChainRPCEndpoint, rpcclient.Options{
		DialTimeout:    15 * time.Second,
		RequestTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("RPC client dial: %w", err)
	}

	act, err := actor.NewSimple(c, acc)
	if err != nil {
		return nil, fmt.Errorf("init actor: %w", err)
	}

	nLatestBlock, err := act.GetBlockCount()
	if err != nil {
		return nil, fmt.Errorf("get number of the latest block: %w", err)
	}

	return &remoteBlockchain{
		rpc:          c,
		actor:        act,
		currentBlock: nLatestBlock,
	}, nil
}

func (x *remoteBlockchain) close() {
	x.rpc.Close()
}

// getNeoFSContractByName requests state.Contract for the NeoFS contract
// referenced by given name using provided NeoFS NNS contract.
//
// See also nns.Resolve.
func (x *remoteBlockchain) getNeoFSContractByName(name string) (res state.Contract, err error) {
	nnsContract, err := x.rpc.GetContractStateByID(1)
	if err != nil {
		return res, fmt.Errorf("get state of the NNS contract by ID=1: %w", err)
	}

	const method = "resolve"
	callRes, err := x.actor.Call(nnsContract.Hash, method, name+".neofs", int64(nns.TXT))
	if err != nil {
		return res, fmt.Errorf("call '%s' method of the NNS contract: %w", method, err)
	}

	records, err := unwrap.Array(callRes, nil)
	if err != nil {
		return res, fmt.Errorf("fetch stack item array from '%s' method's response of the NNS contract: %w", method, err)
	}

	strContractHash, err := unwrap.UTF8String(&result.Invoke{
		State: vmstate.Halt.String(),
		Stack: records,
	}, nil)
	if err != nil {
		return res, fmt.Errorf("fetch single UTF-8 string from '%s' method's response of the NNS contract: %w", method, err)
	}

	h, err := address.StringToUint160(strContractHash)
	if err != nil {
		h, err = util.Uint160DecodeStringLE(strContractHash)
		if err != nil {
			return res, fmt.Errorf("expected NNS record about the contract to be its address in Neo or Hex format '%s'", strContractHash)
		}
	}

	contractState, err := x.rpc.GetContractStateByHash(h)
	if err != nil {
		return res, fmt.Errorf("get state of the requested contract by hash '%s': %w", h.StringLE(), err)
	}

	return *contractState, nil
}

// iterateContractStorage iterates over all storage items of the Neo smart
// contract referenced by given address and passes them into f.
// iterateContractStorage breaks on any f's error and returns it.
func (x *remoteBlockchain) iterateContractStorage(contract util.Uint160, f func(key, value []byte) error) error {
	nLatestBlock, err := x.actor.GetBlockCount()
	if err != nil {
		return fmt.Errorf("get number of the latest block: %w", err)
	}

	stateRoot, err := x.rpc.GetStateRootByHeight(nLatestBlock - 1)
	if err != nil {
		return fmt.Errorf("get state root at penult block #%d: %w", nLatestBlock-1, err)
	}

	var start []byte

	for {
		res, err := x.rpc.FindStates(stateRoot.Root, contract, nil, start, nil)
		if err != nil {
			return fmt.Errorf("get historical storage items of the requested contract at state root '%s': %w", stateRoot.Root, err)
		}

		for i := range res.Results {
			err = f(res.Results[i].Key, res.Results[i].Value)
			if err != nil {
				return err
			}
		}

		if !res.Truncated {
			return nil
		}

		start = res.Results[len(res.Results)-1].Key
	}
}
