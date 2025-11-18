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

// Check if a specific user record exists in NNS TXT records for a given domain.
func ExampleContractReader_HasAddressRecord() {
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

	// Check if a specific user is mapped to a domain
	userAddress := "NbrUYaZgyhSkNoRo9ugRyEMdUZxrhkNaWB"
	exists, err := nnsContract.HasAddressRecord("mydomain.container", userAddress)
	if err != nil {
		log.Fatal(err)
	}

	if exists {
		fmt.Printf("User %s is authorized for domain mydomain.container\n", userAddress)
	} else {
		fmt.Printf("User %s is not authorized for domain mydomain.container\n", userAddress)
	}
}
