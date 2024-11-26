/*
Package netmap contains implementation of the Netmap contract for NeoFS systems.

Netmap contract stores and manages NeoFS network map, storage node candidates
and epoch number counter. Currently it maintains two lists simultaneously, both
in old BLOB-based format and new structured one. Nodes and IR are encouraged to
use both during transition period, old format will eventually be discontinued.

# Contract notifications

AddNode notification. This notification is emitted when a storage node joins
the candidate list via AddNode method (using new format).

	AddNode
	  - name: publicKey
	    type: PublicKey
	  - name: addresses
	    type: Array
	  - name: attributes
	    type: Map

AddPeer notification. This notification is produced when a Storage node sends
a bootstrap request by invoking AddPeer method.

	AddPeer
	  - name: nodeInfo
	    type: ByteArray

UpdateStateSuccess notification. This notification is produced when a storage
node or Alphabet changes node state by invoking UpdateState or DeleteNode
methods. Supported states: online/offline/maintenance.

	UpdateStateSuccess
	  - name: publicKey
	    type: PublicKey
	  - name: state
	    type: Integer

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
 - 'snapshot_<ID>' -> std.Serialize([]Node)
   network map by snapshot ID (where Node is a type)
 - 'snapshotCurrent' -> int
   ID of the snapshot representing current network map
 - 'candidate<public_key>' -> std.Serialize(Node)
   information about the particular network map candidate (where Node is a type)
 - 'containerScriptHash' -> 20-byte script hash
   Container contract reference
 - 'balanceScriptHash' -> 20-byte script hash
   Balance contract reference
 - 'config<name>' -> []byte
   value of the particular NeoFS network parameter
 - '2<public_key>' -> std.Serialize(Node2)
   Candidate list in modern structured format.
 - 'p<epoch><public_key>' -> std.Serialize(Node2)
   Per-epoch node list, epoch is encoded as 4-byte BE integer.

# Setting
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
