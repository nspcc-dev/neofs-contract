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
are deployed both in main chain and side chain.

Main chain contracts:

- neofs
- processing

Side chain contracts:

- alphabet
- audit
- balance
- container
- neofsid
- netmap
- proxy
- reputation

# Getting started 

## Prerequisites

To compile smart contracts you need:

-   [neo-go](https://github.com/nspcc-dev/neo-go) >= 0.95.0

## Compilation

To build and compile smart contract run `make all` command. Compiled contracts
`*_contract.nef` and manifest `config.json` files are placed in the 
corresponding directories. 

```
$ make all
neo-go contract compile -i alphabet/alphabet_contract.go -c alphabet/config.yml -m alphabet/config.json
neo-go contract compile -i audit/audit_contract.go -c audit/config.yml -m audit/config.json
neo-go contract compile -i balance/balance_contract.go -c balance/config.yml -m balance/config.json
neo-go contract compile -i container/container_contract.go -c container/config.yml -m container/config.json
neo-go contract compile -i neofsid/neofsid_contract.go -c neofsid/config.yml -m neofsid/config.json
neo-go contract compile -i netmap/netmap_contract.go -c netmap/config.yml -m netmap/config.json
neo-go contract compile -i proxy/proxy_contract.go -c proxy/config.yml -m proxy/config.json
neo-go contract compile -i reputation/reputation_contract.go -c reputation/config.yml -m reputation/config.json
neo-go contract compile -i neofs/neofs_contract.go -c neofs/config.yml -m neofs/config.json
neo-go contract compile -i processing/processing_contract.go -c processing/config.yml -m processing/config.json
```

You can specify path to the `neo-go` binary with `NEOGO` environment variable:

```
$ NEOGO=/home/user/neo-go/bin/neo-go make all
```

Remove compiled files with `make clean` or `make mr_proper` command.

# NeoFS API compatibility

|neofs-contract version|supported NeoFS API versions|
|:------------------:|:--------------------------:|
|v0.9.x|[v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0)|


# License

This project is licensed under the GPLv3 License - see the 
[LICENSE.md](LICENSE.md) file for details
