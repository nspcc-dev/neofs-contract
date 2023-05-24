/*
Package neofsid contains implementation of NeoFSID contract deployed in NeoFS
sidechain.

NeoFSID contract is used to store connection between an OwnerID and its public keys.
OwnerID is a 25-byte N3 wallet address that can be produced from a public key.
It is one-way conversion. In simple cases, NeoFS verifies ownership by checking
signature and relation between a public key and an OwnerID.

In more complex cases, a user can use public keys unrelated to the OwnerID to maintain
secure access to the data. NeoFSID contract stores relation between an OwnerID and
arbitrary public keys. Data owner can bind a public key with its account or unbind it
by invoking Bind or Unbind methods of NeoFS contract in the mainchain. After that,
Alphabet nodes produce multisigned AddKey and RemoveKey invocations of NeoFSID
contract.

# Contract notifications

NeoFSID contract does not produce notifications to process.
*/
package neofsid

/*
Contract storage model.

# Summary
Key-value storage format:
 - 'netmapScriptHash' -> interop.Hash160
   Netmap contract reference (currently unused)
 - 'o' + ID + interop.PublicKey -> 1
   each key of the NeoFS user identified by 25-byte NEO3 account

# Keychains
Contract collects all keys of the NeoFS users except ones that may be directly
resolved into user ID.
*/
