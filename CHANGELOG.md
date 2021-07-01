# Changelog
Changelog for NeoFS Contract

## [0.9.2] - 2021-07-01

### Fixed
- Execution of multiple `container.Put`, `container.Delete`, 
  `container.PutContainerSize` and `netmap.AddPeer` invocations now possible
  in the single block (#100, #102).
  
### Added
- Target NeoFS API version in README.md.

### Changed
- Notary enabled images for neofs-dev-env do not contain predefined network
  map anymore.

## [0.9.1] - 2021-06-24 

### Fixed
- Notification parameter types in container, neofs and netmap manifests (#94).
- Method permissions in manifests (#96).

### Added
- Balance check before notification at `container.Put` method.

### Removed
- v0.8.0 to v0.9.0 migration code.

## [0.9.0] - 2021-06-03 - Seongmodo (석모도, 席毛島)

Session token support in container contract.

### Fixed
- `_deploy` methods process `isUpdate` argument now.

### Added
- Changelog file.
- `netmap.NetmapCandidates` method.

### Changed
- Container contract now stores public key, signature and session token of
  new containers and extended ACL tables.
- Most of the contract methods that invoked by inner ring do not return bool
  value anymore. Such methods throw panic instead.
- Migrate methods now accept data.

### Removed
- Container and extended ACL signature checks in container contract.

## [0.8.0] - 2021-05-19 - Dolsando (돌산도, 突山島)

N3 Testnet RC2 compatible contracts.

### Changed

- Contract initialization moved to `_deploy` method.

### Removed

- `Deposit` method from `NeoFS` contract uses direct GAS transfer to contract address for deposit.
- Unused transfer description variables in `Balance` contract and total function in `Alphabet` contract.

### Updated

- NEO Go to N3 RC2 compatible v0.95.0.

## [0.7.0] - 2021-05-04 - Daecheongdo (대청도, 大靑島)

Combine notary and non-notary work flows in smart contracts.

### Fixed

- Integers are not used as search prefixes anymore.

### Added

- Notary and non-notary work flows in all contracts. Notary can be disabled at contract initialization.
- `Processing` contract in main chain to pay for `NeoFS` contract invocations from alphabet when notary enabled.
 - Fee payments at `neofs.Withdraw` invocation.

### Changed

- `Reputation` contract stores new global reputation structures.
- All `balance.transferX` invocations are provided with encoded transfer details.

### Removed

- Cheque storage in `NeoFS` contract to decrease invocation costs.

## [0.6.0] - 2021-03-26 - Yeongheungdo (영흥도, 靈興島)

Governance update.

### Fixed

- Threshold (N) calculation.

### Changed

- Inner ring keys are accessed from `NeoFSAlphabet` role in side chain.
- Alphabet keys are accessed from committee in side chain.
- `NeoFS` contract now manages alphabet keys and do not update candidate list automatically.
- `NeoFS` contract can be initiated with any non zero amount of alphabet keys now.
- `neofs.InnerRingList` renamed to `neofs.AlphabetList`.
- `neofs.InnerRingUpdate` renamed to `neofs.AlphabetUpdate` and it produces `AlphabetUpdate` event.
- `Netmap` contract does not manage inner ring keys now.

### Removed

- `neofs.IsInnerRing`, `netmap.InnerRingLost`, `netmap.Multiaddress`, 
  `netmap.Committee`, `netmap.UpdateInnerRing` methods.

## [0.5.1] - 2021-03-22

### Fixed

- Methods with notifications are no longer considered to be safe.

## [0.5.0] - 2021-03-22 - Jebudo (제부도, 濟扶島)

### Fixed

- Various typos.

### Added

- Proxy contract.
- `Multiaddress` and `Committee` methods in `Netmap` contract.
- List of safe methods in contract configs.

### Changed

- Smart contracts use read-only storage context where it is possible.
- `Netmap` contract triggers clean-up methods on new epoch.
- Contracts use `interop.Hash160` and other type aliases instead of `[]byte`.

### Removed

- Multi signatures in side chain now collected with native notary contract, 
  thus side chain contracts do not use ballots anymore.

### Updated

- NEO Go to testnet compatible v0.94.0.

## [0.4.0] - 2021-02-15 - Seonyudo (선유도, 仙遊島)

### Fixed

- Old ballots are now removed before processing new ballot.

### Added

- Methods in container contract to store container size estimations.
- `common` package that contains shared code for all contracts.
- Contracts migration methods.

### Updated

- NEO Go to preview5 compatible v0.93.0.
- Contract manifests.

## [0.3.1] - 2020-12-29

Preview4-testnet version of NeoFS contracts.

## 0.3.0 - 2020-12-29

Preview4 compatible contracts.

[0.9.2]: https://github.com/nspcc-dev/neofs-contract/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.3.0...v0.3.1
