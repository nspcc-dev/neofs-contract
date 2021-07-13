/*
NeoFSID contract is a contract deployed in NeoFS side chain.

NeoFSID contract used to store connection between OwnerID and it's public keys.
OwnerID is a 25-byte N3 wallet address that can be produced from public key.
It is one-way conversion. In simple cases NeoFS verifies ownership by checking
signature and relation between public key and OwnerID.

In more complex cases, user can use public keys unrelated to OwnerID to maintain
secure access to the data. NeoFSID contract stores relation between OwnerID and
arbitrary public keys. Data owner can bind or unbind public key with it's account
by invoking Bind or Unbind methods of NeoFS contract in main chain. After that,
Alphabet nodes produce multi signed AddKey and RemoveKey invocations of NeoFSID
contract.

Contract notifications

NeoFSID contract does not produce notifications to process.
*/
package neofsid
