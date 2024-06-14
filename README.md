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

# Getting started 

## Prerequisites

To compile smart contracts you need:

-   [neo-go](https://github.com/nspcc-dev/neo-go) >= 0.104.0

## Compilation

To build and compile smart contract, run `make all` command. Compiled contracts
`contract.nef` and manifest `manifest.json` files are placed in the
corresponding directories. Generated RPC binding files `rpcbinding.go` are
placed in the corresponding `rpc` directories.

```
$ make all
/home/user/go/bin/cli contract compile -i contracts/alphabet -c contracts/alphabet/config.yml -m contracts/alphabet/manifest.json -o contracts/alphabet/contract.nef --bindings contracts/alphabet/bindings_config.yml
mkdir -p rpc/alphabet
/home/user/go/bin/cli contract generate-rpcwrapper -o rpc/alphabet/rpcbinding.go -m contracts/alphabet/manifest.json --config contracts/alphabet/bindings_config.yml
...
```

You can specify path to the `neo-go` binary with `NEOGO` environment variable:

```
$ NEOGO=/home/user/neo-go/bin/neo-go make all
```

Remove compiled files with `make clean` command.

## Building Debian package

To build Debian package containing compiled contracts, run `make debpackage`
command. Package will install compiled contracts `contract.nef` and manifest
`manifest.json` with corresponding directories to `/var/lib/neofs/contract` for
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

# Comparing contracts content of NeoFS chains
`scripts` directory contains CLI utilities to compare some of the NeoFS contract
contents between two RPC nodes:
 * `compare-fscontent` performs comparison of container blobs and NetMap contract
   entries.

# License

Contracts are licensed under the GPLv3 license, bindings and other integration
code is provided under the Apache 2.0 license - see [LICENSE.md](LICENSE.md) for details.
