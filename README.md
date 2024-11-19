<p align="center">
<img src="./.github/logo.svg" width="500px" alt="NeoFS">
</p>
<p align="center">
  <a href="https://fs.neo.org">NeoFS</a> related smart contracts.
</p>

---

# Overview

neofs-contract contains all NeoFS-related contracts written for
[neo-go](https://github.com/nspcc-dev/neo-go) compiler. There are contracts
both for the main and FS chains.

Main chain (mainnet) contracts:

- neofs
- processing

FS chain contracts:

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

# Comparing contracts content of NeoFS chains
`scripts` directory contains CLI utilities to compare some of the NeoFS contract
contents between two RPC nodes:
 * `compare-fscontent` performs comparison of container blobs and NetMap contract
   entries.
 * `compare-deposits` performs comparison of mainnet deposits and FS chain balance
   mints.

# License

Contracts are licensed under the GPLv3 license, bindings and other integration
code is provided under the Apache 2.0 license - see [LICENSE.md](LICENSE.md) for details.
