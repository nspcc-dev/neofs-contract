# Changelog
Changelog for NeoFS Contract

## [Unreleased]

### Added
- `create`, `remove` and `putEACL` methods to Container contract (#478)
- `getEpochBlock`, `getEpochTime` and `lastEpochTime` methods to Netmap contract (#484)

### Changed
- Deployment code uses header subscription instead of block subscription now (#471)
- Go 1.23+ is required now (#472)
- Minimal supported version for upgrades is 0.18.0 now (#482)
- All contracts follow NEP-22 standard now (#459)

### Updated

### Removed
- neofsid contract (#474)
- bind/unbind methods of neofs contract (#475)

### Fixed

## [0.21.0] - 2025-02-19

### Added
- Container node lists to the container contract (#438)
- Container placement verifications to the container contract (#440)
- NNS renew method with time parameter (#442)
- SetAdmin and Renew events in NNS contract (#442)
- New node format in the netmap contract with no limits to the map size (#445, #461, #462, #464)
- Experimental object metadata events in the container contract (#448, #451, #456, #457)
- Auto-cleaning for inactive nodes in the netmap contract (#460)

### Changed
- Minimal Go version to 1.22 (#353, #387)
- NNS contract now returns an empty array from resolve and getRecord if there are no records (#420)

### Updated
- NeoGo dependency to 0.108.0 (#450, #465)
- golang.org/x/crypto dependency from 0.26.0 to 0.31.0 (#452, #453, #454)

### Removed
- github.com/urfave/cli dependency (#433)

## [0.20.0] - 2024-07-30

### Added
- `verify` method for Alphabet contracts (#386)
- Contract deployment code (#395, #410, #417)
- Functions to deal with address records and New* constructors for nns wrapper (#397)
- Script comparing some of NeoFS contract states (#399)
- Script to compare main/fs chain deposit state (#400)
- Binary contracts provided as Go package (#401)
- Support for NEP-18 addresses in NNS wrapper (#392)
- Contract-specific constants and some types to RPC bindings (#402)
- Prefixes to balance contract storage scheme (#406)
- `admin` to `properties()` result of NNS (#419)

### Changed
- Contracts moved into a separate directory (#378)
- Licensing documentation (#391, #395, #401)
- Release archive uses contract.nef and manifest.json file names (#401)
- NNS now returns more specific errors for invalid domains (#419)

### Updated
- NeoGo dependency to 0.106.3 (#389, #398, #401, #421)
- golang.org/x/crypto dependency from 0.14.0 to 0.17.0 (#383)
- Minimal Go version to 1.20 (#389)
- google.golang.org/protobuf dependency from 1.31.0 to 1.33.0 (#393)

### Fixed
- Outdated NNS record preventing container deletion (#403)
- Container contract allowed for Put replays (#404)
- Potential overflow of NNS record IDs (now limited to 16 entries, #419)
- CNAME resolve results included CNAME record itself (#419)
- NNS `isAvailable` returning `true` when conflicting records are known (#419)

## [0.19.1] - 2023-11-28

### Fixed
- Version to 0.19.1 (#376)

## [0.19.0] - 2023-11-28

### Added
- Subscription to new epoch events in netmap contract (#368)

### Updated
- `neo-go` to `v0.104.0` (#367, #374)

### Removed
- Obsolete subnet contract (#364)
- Unused deployment/update options (#373)
- Most of deploy configurations (#373)

### Fixed
- Expired TLD registration (#366)
- reputation contract documentation (#369)

## [0.18.0] - 2023-09-26

### Added
- EACL validation in container.setEACL (#330)
- Contract storage model documentation (#320)
- Bump minimum required go version up to 1.18 (#346)
- Ability to register predefined TLDs at NNS deployment (#344)
- RPC bindings generation (#345)
- Method to get container name by its ID (#360)
- Convenience methods for NNS contract (#361)

### Updated
- NNS TLD registration is possible for committee only now (#344, #357)
- NNS TLDs are no longer proper NFTs (#344)

### Removed
- Old unused (notary-disabled) events (#341)
- Unused Burn/Mint balance contract events (#358)

### Fixed
- Migration of non-notary network to notarized one with stale votes (#333)
- nns.getAllRecords missing 'safe' mark (#355)
- Stale EACL record left after container deletion (#359)

## [0.17.0] - 2023-04-06

### Added
- methods to iterate over containers and their sizes (#293, #300, #326)
- `cmd/dump` app that pulls state and data of contracts from remote networks (#324)
- `tests/migration` framework for storage migration testing (#324)
- Dumps of the NeoFS MainNet and TestNet contracts (#324)

### Updated
- `neo-go` to `v0.101.0`
- `neo-go/pkg/interop` to `v0.0.0-20230208100456-1d6e48ee78e5`
- `stretchr/testify` to `v1.8.2`
- NNS contract now uses 10 years for the default domain expiration (#296)
- contract documentation (#275, #317)

### Removed
- Support for non-notary settings (#303)
- `updateInnerRing` of the Netmap contract (#303)

### Fixed
- Migration of contracts to previous versions (#324)
- Potential panic in container contract's `getContainerSize` (#321)

### Updating from v0.16.0
When updating a non-notary installation:
- read Inner Ring set using `innerRingList` method of the Netmap contract and
  install it as NeoFSAlphabet role in RoleManagement one
- if an update is aborted due to pending votes, try again later
- replace calling of removed `updateInnerRing` and deprecated `innerRingList`
  methods of the Netmap contract with RoleManagement contract API

## [0.16.0] - 2022-10-17 - Anmado (안마도, 鞍馬島)

### Added
- Support `MAINTENANCE` state of storage nodes (#269)

### Changed
- `netmap.Snapshot` and all similar methods return (#269)

### Updated
- NNS contract now sets domain expiration based on `register` arguments (#262)

### Fixed
- NNS `renew` now can only be done by the domain owner

### Updating from v0.15.5
Update deployed `Netmap` contract using `Update` method: storage of the contract
has been incompatibly changed.

## [0.15.5] - 2022-08-23

### Updated
- Update neo-go to v0.99.2 (#261)
- Makefile now takes only `v*` tags into account (#255)

## [0.15.4] - 2022-07-27
Only a version bump to update manifest.

## [0.15.3] - 2022-07-22

### Added
- Allow to build archive from source (#250)

### Changed
- Update neo-go to the latest version
- Use proper type for integer constants (#248)

## [0.15.2] - 2022-06-07

### Added
- `container.Count` method (#242)

### Changed
- Update neo-go to v0.99.0 (#246)

## [0.15.1] - 2022-04-13

### Fixed
- Max domain name fragement length (#238)

### Added
- `netmap.UpdateSnapshotCount` method (#232)
- Notifications of successful container and storage node operations (#236)

### Changed
- Update neo-go to v0.98.2 (#234)

## [0.15.0] - 2022-03-23 - Heuksando (흑산도, 黑山島)

### Fixed
- Split `UpdateState` method to allow Alphabet nodes remove storage nodes from
  network map based on consensus decision in notary-enabled environment (#225)

### Changed
- Increase from 2 to 10 stored network maps in netmap contract (#224)
- Use public keys instead of `IRNode` structures in neofs and netmap contracts
  (#222)

## [0.14.2] - 2022-02-07

### Fixed
- Remove duplicate records in NNS contract (#196)

### Changed
- Evict container estimations on every put (#215)
- Update neo-go to v0.98.1

## [0.14.1] - 2022-01-24

### Fixed
- Remove migration routine for reputation contract update (#220)
- Remove version check for subnet contract update (#220)

### Added
- Append version to `Update` arguments for subnet contract (#220)

## [0.14.0] - 2022-01-14 - Geojedo (거제도, 巨濟島)

### Fixed
- Sync `Update` method signature in NNS contract (#197)
- Use current block index in all `GetDisgnatedByRole` invocations (#209)

### Added
- Version check during contract update (#204)

### Changed
- Use `storage.RemovePrefix` in subnet contract (#199)

### Removed
- Netmap contract hash usage in proxy contract (#205)
- Legacy contract owner records from contract storage (#202)

## [0.13.2] - 2021-12-14

### Fixed
- Reputation contract migration (#201)

## [0.13.1] - 2021-12-08

### Fixed
- Specify container contract as owner of all container related domain zones
  (#194)

## [0.13.0] - 2021-12-07 - Sinjido (신지도, 薪智島)

Support of subnetwork contract from NeoFS API v2.11.0.

### Fixed
- Records with duplicate values are not allowed in NNS anymore (#165)
- Allow multiple `reputation.Put`, `container.PutCotnainerSize`, 
  `neofsid.AddKey`, `neofsid.RemoveKey`, `neofs.InnerRingCandidateAdd`, 
  `neofs.InnerRingCandidateRemove` invocations in one block (#101)
- `netmap.UpdateState` checks both node and alphabet signatures in notary
  enabled environment (#154)

### Added
- Version method in NNS contract (#158)
- Subnet contract (#122)
- `netmap.Register` method for notary enabled environment (#154)

### Changed
- Container contract throws panic if required container is missing (#142)
- Container contract does not throw panic if deleting container is already 
  removed (#142)
- NNS stores root as regular TLD (#139)
- Use testing framework from neo-go (#161)
- Allow hyphen in domain names in NNS (#180)
- Panic messages do not heave method name prefix anymore (#179)
- `OnNEP17Payment` method calls `Abort` instead of panic (#179)
- Allow arbitrary-level domains in NNS (#172)
- Refactor (#169)

## [0.12.2] - 2021-11-26

### Fixed
- Domain owner check in container contract (#156)
- Missing NNS related keys in container contract (#181)

### Added
- Update functions now provide contract version in data (#164)

## [0.12.1] - 2021-10-19

### Fixed
- Sanity checks for notary enabled environment in container contract (#149)

### Added
- NeoFS global configuration parameter `ContainerAliasFee`. This parameter
  used as additional fee for container registration with nice name alias (#148).

### Changed
- `netmap.AddPeer` method can update `NodeInfo` structures (#146)
- `netmap.Update` allows to redefine any key-value pair of global config (#151)

## [0.12.0] - 2021-10-15 - Udo (우도, 牛島)

NNS update with native container names in container contract.

### Fixed
- Safe methods list in reputation contract manifest (#144)

### Added
- SOA record type support in NNS (#125)
- Test framework for N3 contracts written in go (#137)
- Unit tests for container and NNS contracts (#135, #137)
- `PutNamed` method in container contract that registers domain in NNS (#135)

### Changed
- NNS contract supports multiple records of the same type (#125)

## [0.11.0] - 2021-09-22 - Mungapdo (문갑도, 文甲島)

Contract owners are removed, now side chain committee is in charge of contract
update routine.

### Fixed
- Container contract does not throw PICKITEM panic when trying access 
  non-existent container, instead panics with user-friendly message (#121)

### Changed
- NNS contract has been updated to the latest version from Neo upstream (#123)
- Container contract does not throw panic on deleting non-existent container
  (#121)
- Migrate methods renamed to Update (#128)
- Contracts now throw panic if update routine fails (#130)
- Side chain committee is able to update contracts (#107) 

### Removed
- Contract owner arguments at deploy stage (#107)

## [0.10.1] - 2021-07-29

### Changed
- `Version` method returns encoded semver value (#98)

### Removed
- `InitConfig` methods from neofs and netmap contracts. Network configuration
  now provided as contract deploy parameter. (#115)

## [0.10.0] - 2021-07-23 - Wando (완도, 莞島)

### Fixed
- Alphabet contract does not emit GAS to proxy contract and does not check
  proxy contract script hash length at deploy stage if notary disabled (#106)

### Added
- Netmap contract stores block height when last epoch was applied and provides
  `LastEpochBlock` method to get it (#110)
- NNS contract (#108)  
- Enhanced documentation for autodoc tools (#105)

### Changed
- Update neo-go to v0.96.0

### Removed
- v0.9.1 to v0.9.2 migration code (#104)

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

[Unreleased]: https://github.com/nspcc-dev/neofs-contract/compare/v0.21.0...master
[0.21.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.20.0...v0.21.0
[0.20.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.19.1...v0.20.0
[0.19.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.19.0...v0.19.1
[0.19.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.18.0...v0.19.0
[0.18.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.17.0...v0.18.0
[0.17.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.16.0...v0.17.0
[0.16.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.15.5...v0.16.0
[0.15.5]: https://github.com/nspcc-dev/neofs-contract/compare/v0.15.4...v0.15.5
[0.15.4]: https://github.com/nspcc-dev/neofs-contract/compare/v0.15.3...v0.15.4
[0.15.3]: https://github.com/nspcc-dev/neofs-contract/compare/v0.15.2...v0.15.3
[0.15.2]: https://github.com/nspcc-dev/neofs-contract/compare/v0.15.1...v0.15.2
[0.15.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.15.0...v0.15.1
[0.15.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.14.2...v0.15.0
[0.14.2]: https://github.com/nspcc-dev/neofs-contract/compare/v0.14.1...v0.14.2
[0.14.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.14.0...v0.14.1
[0.14.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.13.2...v0.14.0
[0.13.2]: https://github.com/nspcc-dev/neofs-contract/compare/v0.13.1...v0.13.2
[0.13.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.13.0...v0.13.1
[0.13.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.12.2...v0.13.0
[0.12.2]: https://github.com/nspcc-dev/neofs-contract/compare/v0.12.1...v0.12.2
[0.12.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.12.0...v0.12.1
[0.12.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.10.1...v0.11.0
[0.10.1]: https://github.com/nspcc-dev/neofs-contract/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/nspcc-dev/neofs-contract/compare/v0.9.2...v0.10.0
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
