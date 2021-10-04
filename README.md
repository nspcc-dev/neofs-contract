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
- nns
- proxy
- reputation

# Getting started 

## Prerequisites

To compile smart contracts you need:

-   [neo-go](https://github.com/nspcc-dev/neo-go) >= 0.96.0

## Compilation

To build and compile smart contract run `make all` command. Compiled contracts
`*_contract.nef` and manifest `config.json` files are placed in the 
corresponding directories. 

```
$ make all
neo-go contract compile -i alphabet -c alphabet/config.yml -m alphabet/config.json -o alphabet/alphabet_contract.nef
neo-go contract compile -i audit -c audit/config.yml -m audit/config.json -o audit/audit_contract.nef
neo-go contract compile -i balance -c balance/config.yml -m balance/config.json -o balance/balance_contract.nef
neo-go contract compile -i container -c container/config.yml -m container/config.json -o container/container_contract.nef
neo-go contract compile -i neofsid -c neofsid/config.yml -m neofsid/config.json -o neofsid/neofsid_contract.nef
neo-go contract compile -i netmap -c netmap/config.yml -m netmap/config.json -o netmap/netmap_contract.nef
neo-go contract compile -i proxy -c proxy/config.yml -m proxy/config.json -o proxy/proxy_contract.nef
neo-go contract compile -i reputation -c reputation/config.yml -m reputation/config.json -o reputation/reputation_contract.nef
neo-go contract compile -i nns -c nns/config.yml -m nns/config.json -o nns/nns_contract.nef
neo-go contract compile -i neofs -c neofs/config.yml -m neofs/config.json -o neofs/neofs_contract.nef
neo-go contract compile -i processing -c processing/config.yml -m processing/config.json -o processing/processing_contract.nef
```

You can specify path to the `neo-go` binary with `NEOGO` environment variable:

```
$ NEOGO=/home/user/neo-go/bin/neo-go make all
```

Remove compiled files with `make clean` or `make mr_proper` command.

# Testing
Smartcontract tests reside in `tests/` directory. To execute test suite
after applying changes simply run `make test`.
```
$ make test
ok      github.com/nspcc-dev/neofs-contract/tests       0.462s
```

# NeoFS API compatibility

|neofs-contract version|supported NeoFS API versions|
|:------------------:|:--------------------------:|
|v0.9.x|[v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0)|
|v0.10.x|[v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0)|
|v0.11.x|[v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0), [v2.9.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.9.0)|


# License

This project is licensed under the GPLv3 License - see the 
[LICENSE.md](LICENSE.md) file for details
