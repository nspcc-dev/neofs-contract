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
are deployed both in mainnet and sidechain.

Mainnet contract:

- neofs

Sidechain contracts:

- alphabet contracts
- audit
- balance
- container
- neofsid
- netmap
- reputation

# Getting started 

## Prerequisites

To compile smart contracts you need:

-   [neo-go](https://github.com/nspcc-dev/neo-go) >= 0.91.0

Alphabet contracts built from template. To regenerate contracts from 
template you need:

-   [go](https://golang.org/dl/) >= 1.12


## Compilation

To build and compile smart contract run `make all` command. Compiled contracts
`*_contract.nef` and manifest `config.json` files are placed in the 
corresponding directories. 

```
$ make all
neo-go contract compile -i alphabet/az/az_contract.go -c alphabet/config.yml -m alphabet/az/config.json
neo-go contract compile -i alphabet/buky/buky_contract.go -c alphabet/config.yml -m alphabet/buky/config.json
neo-go contract compile -i alphabet/vedi/vedi_contract.go -c alphabet/config.yml -m alphabet/vedi/config.json
neo-go contract compile -i alphabet/glagoli/glagoli_contract.go -c alphabet/config.yml -m alphabet/glagoli/config.json
neo-go contract compile -i alphabet/dobro/dobro_contract.go -c alphabet/config.yml -m alphabet/dobro/config.json
neo-go contract compile -i alphabet/jest/jest_contract.go -c alphabet/config.yml -m alphabet/jest/config.json
neo-go contract compile -i alphabet/zhivete/zhivete_contract.go -c alphabet/config.yml -m alphabet/zhivete/config.json
neo-go contract compile -i audit/audit_contract.go -c audit/config.yml -m audit/config.json
neo-go contract compile -i balance/balance_contract.go -c balance/config.yml -m balance/config.json
neo-go contract compile -i container/container_contract.go -c container/config.yml -m container/config.json
neo-go contract compile -i neofsid/neofsid_contract.go -c neofsid/config.yml -m neofsid/config.json
neo-go contract compile -i netmap/netmap_contract.go -c netmap/config.yml -m netmap/config.json
neo-go contract compile -i reputation/reputation_contract.go -c reputation/config.yml -m reputation/config.json
neo-go contract compile -i neofs/neofs_contract.go -c neofs/config.yml -m neofs/config.json
```

You can specify path to the `neo-go` binary with `NEOGO` environment variable:

```
$ NEOGO=/home/user/neo-go/bin/neo-go make all
```

Remove compiled files with `make clean` command.

Remove compiled files and alphabet contracts with `make mr_proper` command. Next time
`make all` or `make alphabet` command will generate new alphabet contracts from template.


# License

This project is licensed under the GPLv3 License - see the 
[LICENSE.md](LICENSE.md) file for details
