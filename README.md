# NeoFS smart-contract
 
This smart-contract controls list of NeoFS Inner Ring nodes, user assets in 
NeoFS balance contract and stores NeoFS runtime configuration.
 
## Getting Started

This repository contains:

-   NeoFS smart-contract in Go

### Prerequisites

To compile smart-contract you need:

-   [neo-go](https://github.com/nspcc-dev/neo-go) >= 0.90.0

## Compiling

To compile smart contract run `make build` command. Compiled contract
`neofs_contract.nef` and manifest `config.json` will be placed in the same 
directory.

```
$ make build
neo-go contract compile -i neofs_contract.go -c neofs_config.yml -m config.json
$ ls neofs_contract.nef config.json 
config.json  neofs_contract.nef
```

You can specify path to the `neo-go` binary with `NEOGO` environment variable:

```
$ NEOGO=/home/user/neo-go/bin/neo-go make build
```

## License

This project is licensed under the GPLv3 License - see the 
[LICENSE.md](LICENSE.md) file for details
