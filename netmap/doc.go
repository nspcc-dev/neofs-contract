/*
Package netmap contains implementation of the Netmap contract for NeoFS systems.

Netmap contract stores and manages NeoFS network map, Storage node candidates
and epoch number counter. In notary disabled environment, contract also stores
a list of Inner Ring node keys.

# Contract notifications

AddPeer notification. This notification is produced when a Storage node sends
a bootstrap request by invoking AddPeer method.

	AddPeer
	  - name: nodeInfo
	    type: ByteArray

UpdateState notification. This notification is produced when a Storage node wants
to change its state (go offline) by invoking UpdateState method. Supported
states: (2) -- offline.

	UpdateState
	  - name: state
	    type: Integer
	  - name: publicKey
	    type: PublicKey

NewEpoch notification. This notification is produced when a new epoch is applied
in the network by invoking NewEpoch method.

	NewEpoch
	  - name: epoch
	    type: Integer
*/
package netmap

/*
Contract storage model.

# Summary
Key-value storage format:
 - 'snapshotEpoch' -> int
   current epoch
 - 'snapshotBlock' -> int
   block which "ticked" the current epoch
 - 'snapshotCount' -> int
   number of stored network maps including current one
 - 'snapshot_<ID>' -> std.Serialize([]storageNode)
   network map by snapshot ID
 - 'snapshotCurrent' -> int
   ID of the snapshot representing current network map
 - 'candidate<public_key>' -> std.Serialize(netmapNode)
   information about the particular network map candidate
 - 'containerScriptHash' -> 20-byte script hash
   Container contract reference
 - 'balanceScriptHash' -> 20-byte script hash
   Balance contract reference
 - 'notary' -> bool
   is notary mode disabled
 - 'innerring' -> []interop.PublicKey
   public keys of the Inner Ring members
 - 'config<name>' -> []byte
   value of the particular NeoFS network parameter

# Setting
Contract can be deployed in notary and notary-disabled mode. In notary-disabled
mode contract stores the Inner Ring members.

To handle some events, the contract refers to other contracts.

# Epoch
Contract stores the current (last) NeoFS timestamp for the network within which
the contract is deployed.

# Network maps
Contract records set of network parties representing the network map. Current
network map is updated on each epoch tick. Contract also holds limited number of
previous network maps (SNAPSHOT_LIMIT). Timestamped network maps are called
snapshots. Snapshots are identified by the numerical ring [0:SNAPSHOT_LIMIT).

# Network map candidates
Contract stores information about the network parties which were requested to be
added to the network map.

# Network configuration
Contract stores NeoFS network configuration declared in the NeoFS API protocol.
*/
