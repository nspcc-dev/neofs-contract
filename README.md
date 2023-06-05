<p align="center">
<img src="./.github/logo.svg" width="500px" alt="NeoFS">
</p>
<p align="center">
  <a href="https://fs.neo.org">NeoFS</a> related smart contracts.
</p>

---

# Overview

NeoFS-Contract contains all NeoFS related contracts written for
[neo-go](https://github.com/nspcc-dev/neo-go) compiler. These contracts
are deployed both in the mainchain and the sidechain.

Mainchain contracts:

- neofs
- processing

Sidechain contracts:

- alphabet
- audit
- balance
- container
- neofsid
- netmap
- nns
- proxy
- reputation
- subnet

# Getting started 

## Prerequisites

To compile smart contracts you need:

-   [neo-go](https://github.com/nspcc-dev/neo-go) >= 0.102.0

## Compilation

To build and compile smart contract, run `make all` command. Compiled contracts
`*_contract.nef` and manifest `config.json` files are placed in the 
corresponding directories. Generated RPC binding files `rpcbinding.go` are
placed in the corresponding `rpc` directories.

```
$ make all
/home/user/go/bin/cli contract compile -i alphabet -c alphabet/config.yml -m alphabet/config.json -o alphabet/alphabet_contract.nef --bindings alphabet/bindings_config.yml
mkdir -p rpc/alphabet
/home/user/go/bin/cli contract generate-rpcwrapper -o rpc/alphabet/rpcbinding.go -m alphabet/config.json --config alphabet/bindings_config.yml
...
```

You can specify path to the `neo-go` binary with `NEOGO` environment variable:

```
$ NEOGO=/home/user/neo-go/bin/neo-go make all
```

Remove compiled files with `make clean` or `make mr_proper` command.

## Building Debian package

To build Debian package containing compiled contracts, run `make debpackage`
command. Package will install compiled contracts `*_contract.nef` and manifest 
`config.json` with corresponding directories to `/var/lib/neofs/contract` for 
further usage.
It will download and build neo-go, if needed.

To clean package-related files, use `make debclean`.

# Testing
Smartcontract tests reside in `tests/` directory. To execute test suite
after applying changes, simply run `make test`.
```
$ make test
ok      github.com/nspcc-dev/neofs-contract/tests       0.462s
```

# NeoFS API compatibility

| neofs-contract version |                                                                                           supported NeoFS API versions                                                                                           |
|:----------------------:|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|
|         v0.9.x         |                                    [v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0)                                    |
|        v0.10.x         |                                    [v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0)                                    |
|        v0.11.x         | [v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0), [v2.9.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.9.0) |
|        v0.12.x         |                                                                      [v2.10.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.10.0)                                                                      |
|        v0.13.x         |                                                                      [v2.11.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.11.0)                                                                      |
|        v0.14.x         |                                                                      [v2.11.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.11.0)                                                                      |
|        v0.15.x         |                                  [v2.11.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.11.0), [v2.12.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.12.0)                                  |
|        v0.15.x         |                                  [v2.11.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.11.0), [v2.12.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.12.0)                                  |
|        v0.16.x         |                                                                      [v2.14.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.14.0)                       |        v0.17.x         |                                                                      [v2.14.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.14.0)                                                                      |


# License

This project is licensed under the GPLv3 License - see the 
[LICENSE.md](LICENSE.md) file for details
