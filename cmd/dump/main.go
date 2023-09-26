package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/nspcc-dev/neofs-contract/tests/dump"
)

func main() {
	neoRPCEndpoint := flag.String("rpc", "", "Network address of the Neo RPC server")
	chainLabel := flag.String("label", "", "Label of the blockchain environment (e.g. 'testnet')")

	flag.Parse()

	switch {
	case *neoRPCEndpoint == "":
		log.Fatal("missing Neo RPC endpoint")
	case *chainLabel == "":
		log.Fatal("missing blockchain label")
	}

	const rootDir = "testdata"

	err := os.MkdirAll(rootDir, 0700)
	if err != nil {
		log.Fatal(fmt.Errorf("create root dir: %v", err))
	}

	err = _dump(*neoRPCEndpoint, rootDir, *chainLabel)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("NeoFS contracts are successfully dumped to '%s/'\n", rootDir)
}

func _dump(neoBlockchainRPCEndpoint, rootDir, label string) error {
	b, err := newRemoteBlockChain(neoBlockchainRPCEndpoint)
	if err != nil {
		return fmt.Errorf("init remote blockchain: %w", err)
	}

	defer b.close()

	d, err := dump.NewCreator(rootDir, dump.ID{
		Label: label,
		Block: b.currentBlock,
	})
	if err != nil {
		return fmt.Errorf("init local dumper: %w", err)
	}

	defer d.Close()

	err = overtakeContracts(b, d)
	if err != nil {
		return err
	}

	err = d.Flush()
	if err != nil {
		return fmt.Errorf("flush dump: %w", err)
	}

	return nil
}

func overtakeContracts(from *remoteBlockchain, to *dump.Creator) error {
	for _, name := range []string{
		"alphabet0",
		"audit",
		"balance",
		"container",
		"neofsid",
		"netmap",
		"reputation",
	} {
		log.Printf("Processing contract '%s'...\n", name)

		ctr, err := from.getNeoFSContractByName(name)
		if err != nil {
			return fmt.Errorf("get '%s' contract state: %w", name, err)
		}

		s := to.AddContract(name, ctr)

		err = from.iterateContractStorage(ctr.Hash, s.Write)
		if err != nil {
			return fmt.Errorf("iterate '%s' contract storage: %w", name, err)
		}
	}

	return nil
}
