# NeoFS smart-contract
 
This smart-contract controls list of NeoFS Inner Ring nodes and provides
methods to deposit and withdraw assets. These assets are used as a payment and 
a reward for data storage. 

 
## Getting Started

This repository contains:

-   NeoFS smart-contract written in Go
-   Unit tests for the smart-contract

### Prerequisites

To compile smart-contract you need:

-   [neo-go](https://github.com/nspcc-dev/neo-go) >= 0.74.0


To run tests you need:

-   [go](https://golang.org/dl/) >= 1.12

## Compiling

To compile smart contract run `make build` command. Compiled contract
`neofs_contract.avm` will be placed in the same directory.

```
$ make build                                                                                                                                                
neo-go contract compile -i neofs_contract.go                                                                                                                                                                              
02a600c56b6a007bc46a517bc468164e656f2e52756e74696d652e476574547269676765726165880d9e640700006c756668164e656f2e53746f726167652e476574436f6e74657874616a527bc46a00c376064465706c6f798764c7016a52c30d496e6e657252696e674c6973747c657c0d6a537bc46a5
3c3c000a0642f0019636f6e747261637420616c7265616479206465706c6f796564680f4e656f2e52756e74696d652e4c6f67f06a51c3c06a547bc46a54c35297009e6448003270726f76696465207061697273206f6620696e6e65722072696e67206164647265737320616e64207075626c6963206b65
...
c46a00c36a59c37c6592fd6476006a52c36a5ac36a59c3ad6469006a58c38b6a587bc4006a5b7bc46a5bc36a57c3c09f6444006a57c36a5bc3c36a59c387642b00156475706c6963617465207075626c6963206b657973680f4e656f2e52756e74696d652e4c6f67f06a5bc38b6a5b7bc462b7ff6a57c36
a59c3787cc86a577bc46a53c30161936a537bc46234ff6a58c36a56c3a2640700516c75661e6e6f7420656e6f756768207665726966696564207369676e617475726573680f4e656f2e52756e74696d652e4c6f6761006c7566
$ ls neofs_contract.avm 
neofs_contract.avm
```

You can specify path to the `neo-go` binary with `NEOGO` environment variable:

```
$ NEOGO=/home/user/neo-go/bin/neo-go make build
```

## Running the tests

`neofs_contract_test.go` file contains tests for most of the provided methods.
It compiles smart-contract and uses instance of the NeoVM to run 
code. 

To test smart contract run `make tests` command.

```
$ make tests 
go mod vendor
go test -mod=vendor -v -race github.com/nspcc-dev/neofs-contract
=== RUN   TestContract
    TestContract: neofs_contract_test.go:360: provide pairs of inner ring address and public key
    TestContract: neofs_contract_test.go:360: contract already deployed
=== RUN   TestContract/InnerRingAddress
    TestContract: neofs_contract_test.go:360: target element has been removed
=== RUN   TestContract/Deposit
    TestContract: neofs_contract_test.go:360: funds have been transfered
=== RUN   TestContract/Withdraw
=== RUN   TestContract/Withdraw/Double_Withdraw
    TestContract: neofs_contract_test.go:360: verification check has already been used
=== RUN   TestContract/InnerRingCandidateAdd
=== RUN   TestContract/InnerRingCandidateAdd/Double_InnerRingCandidateAdd
    TestContract: neofs_contract_test.go:360: is already in list
=== RUN   TestContract/InnerRingCandidateRemove
=== RUN   TestContract/InnerRingCandidateRemove/Remove_unknown_candidate
    TestContract: neofs_contract_test.go:360: target element has not been removed
    TestContract: neofs_contract_test.go:360: target element has not been removed
=== RUN   TestContract/InnerRingUpdate
    TestContract/InnerRingUpdate: neofs_contract_test.go:174: implement getIRExcludeCheque without neofs-node dependency
--- PASS: TestContract (0.43s)
    --- PASS: TestContract/InnerRingAddress (0.00s)
    --- PASS: TestContract/Deposit (0.00s)
    --- PASS: TestContract/Withdraw (0.01s)
        --- PASS: TestContract/Withdraw/Double_Withdraw (0.00s)
    --- PASS: TestContract/InnerRingCandidateAdd (0.00s)
        --- PASS: TestContract/InnerRingCandidateAdd/Double_InnerRingCandidateAdd (0.00s)
    --- PASS: TestContract/InnerRingCandidateRemove (0.00s)
        --- PASS: TestContract/InnerRingCandidateRemove/Remove_unknown_candidate (0.00s)
    --- SKIP: TestContract/InnerRingUpdate (0.00s)
PASS
ok      github.com/nspcc-dev/neofs-contract     0.453s
```

## License

This project is licensed under the GPLv3 License - see the 
[LICENSE.md](LICENSE.md) file for details
