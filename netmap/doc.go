/*
Netmap contract is a contract deployed in NeoFS side chain.

Netmap contract stores and manages NeoFS network map, Storage node candidates
and epoch number counter. In notary disabled environment, contract also stores
list of Inner Ring node keys.

Contract notifications

AddPeer notification. This notification is produced when Storage node sends
bootstrap request by invoking AddPeer method.

  AddPeer
    - name: nodeInfo
      type: ByteArray

UpdateState notification. This notification is produced when Storage node wants
to change it's state (go offline) by invoking UpdateState method. Supported
states: (2) -- offline.

  UpdateState
    - name: state
      type: Integer
    - name: publicKey
      type: PublicKey

NewEpoch notification. This notification is produced when new epoch is applied
in the network by invoking NewEpoch method.

  NewEpoch
    - name: epoch
      type: Integer
*/
package netmap
