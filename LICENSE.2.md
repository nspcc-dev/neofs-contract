This repository provides several different components that use different
licenses. Contracts, tests and commands use GPLv3 (see [LICENSE](LICENSE)),
while RPC bindings (95% of which are autogenerated code) and other
code intended to be used as a library to integrate with these contracts
is distributed under Apache 2.0 license (see [LICENSE-APACHE](LICENSE-APACHE)).

Specifically, GPLv3 is used by code in these folders:
 * cmd
 * common
 * contracts/*/
 * tests
 * scripts

Apache 2.0 is used in:
 * rpc
 * deploy
 * contracts (top-level Go package with binaries, contracts themselves are GPLv3)