package nns_test

import (
	"context"
	"fmt"
	"log"

	"github.com/nspcc-dev/neo-go/pkg/rpcclient"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/invoker"
	"github.com/nspcc-dev/neofs-contract/rpc/nns"
)

// Resolve addresses of NeoFS smart contracts deployed in a particular
// FS chain by their NNS domain names.
func ExampleContractReader_ResolveFSContract() {
	const fschainRPCEndpoint = "https://rpc1.morph.fs.neo.org:40341"

	c, err := rpcclient.New(context.Background(), fschainRPCEndpoint, rpcclient.Options{})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Init()
	if err != nil {
		log.Fatal(err)
	}

	nnsAddress, err := nns.InferHash(c)
	if err != nil {
		log.Fatal(err)
	}

	nnsContract := nns.NewReader(invoker.New(c, nil), nnsAddress)

	for _, name := range []string{
		nns.NameBalance,
		nns.NameContainer,
		nns.NameNetmap,
		nns.NameProxy,
		nns.NameReputation,
	} {
		addr, err := nnsContract.ResolveFSContract(name)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("%s: %s\n", name, addr)
	}
}
