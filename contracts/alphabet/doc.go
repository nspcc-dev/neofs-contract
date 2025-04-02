/*
Package alphabet implements Alphabet contract which is deployed to FS chain.

Alphabet contract is designed to support GAS production and vote for new
validators in FS chain. NEO token is required to produce GAS and vote for
a new committee. It can be distributed among alphabet nodes of the Inner Ring.
However, some of them may be malicious, and some NEO can be lost. It will destabilize
the economics of FS chain. To avoid it, all 100,000,000 NEO are
distributed among all alphabet contracts.

To identify alphabet contracts, they are named, names are set at contract deploy.
Alphabet nodes of the Inner Ring communicate with one of the alphabetical contracts
to emit GAS. To vote for a new list of side chain committee, alphabet nodes of
the Inner Ring create multisignature transactions for each alphabet contract.

# Contract notifications

Alphabet contract does not produce notifications to process.
*/
package alphabet

/*
Contract storage model.

# Summary
Key-value storage format:
 - 'netmapScriptHash' -> interop.Hash160
   Netmap contract reference
 - 'proxyScriptHash' -> interop.Hash160
   Proxy contract reference
 - 'name' -> string
   name of the contract set at its deployment
 - 'index' -> int
   member index in the Alphabet list
 - 'threshold' -> int
   currently unused value

# Setting
To handle some events, the contract refers to other contracts.

# Membership
Contracts are named and positioned in the Alphabet list of FS chain.
*/
