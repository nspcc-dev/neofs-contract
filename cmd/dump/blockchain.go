package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/actor"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/nspcc-dev/neofs-contract/rpc/nns"
)

// wrapper over rpcNeo providing NeoFS blockchain services needed for current command.
type remoteBlockchain struct {
	rpc   *rpcclient.Client
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
func (x *remoteBlockchain) getNeoFSContractByName(name string, nnsContract *nns.ContractReader) (res state.Contract, err error) {
	h, err := nnsContract.ResolveFSContract(name)
	if err != nil {
		return res, fmt.Errorf("resolving %s: %w", name, err)
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
