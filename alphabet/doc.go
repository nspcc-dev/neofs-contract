/*
Package alphabet contains implementation of Alphabet contract deployed in NeoFS
sidechain.

Alphabet contract is designed to support GAS production and vote for new
validators in the sidechain. NEO token is required to produce GAS and vote for
a new committee. It can be distributed among alphabet nodes of the Inner Ring.
However, some of them may be malicious, and some NEO can be lost. It will destabilize
the economic of the sidechain. To avoid it, all 100,000,000 NEO are
distributed among all alphabet contracts.

To identify alphabet contracts, they are named with letters of the Glagolitic alphabet.
Names are set at contract deploy. Alphabet nodes of the Inner Ring communicate with
one of the alphabetical contracts to emit GAS. To vote for a new list of side
chain committee, alphabet nodes of the Inner Ring create multisignature transactions
for each alphabet contract.

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
   name (Glagolitic letter) of the contract
 - 'index' -> int
   member index in the Alphabet list
 - 'threshold' -> int
   currently unused value

# Setting
To handle some events, the contract refers to other contracts.

# Membership
Contracts are named and positioned in the Alphabet list of the NeoFS Sidechain.
*/
