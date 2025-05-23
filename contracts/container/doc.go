/*
Package container implements Container contract which is deployed to FS chain.

Container contract stores and manages containers, extended ACLs and container
size estimations. Contract does not perform sanity or signature checks of
containers or extended ACLs, it is done by Alphabet nodes of the Inner Ring.
Alphabet nodes approve it by invoking the same Put or SetEACL methods with
the same arguments.

# Contract notifications

containerPut notification. This notification is produced when a user wants to
create a new container. Alphabet nodes of the Inner Ring catch the notification and
validate container data, signature and token if present.

	containerPut:
	  - name: container
	    type: ByteArray
	  - name: signature
	    type: Signature
	  - name: publicKey
	    type: PublicKey
	  - name: token
	    type: ByteArray

containerDelete notification. This notification is produced when a container owner
wants to delete a container. Alphabet nodes of the Inner Ring catch the notification
and validate container ownership, signature and token if present.

	containerDelete:
	  - name: containerID
	    type: ByteArray
	  - name: signature
	    type: Signature
	  - name: token
	    type: ByteArray

nodesUpdate notification. This notification is produced when a container roster
is changed. Triggered only by the Alphabet at the beginning of epoch.

	name: NodesUpdate
	  - name: ContainerID
	    type: hash256

setEACL notification. This notification is produced when a container owner wants
to update an extended ACL of a container. Alphabet nodes of the Inner Ring catch
the notification and validate container ownership, signature and token if
present.

	setEACL:
	  - name: eACL
	    type: ByteArray
	  - name: signature
	    type: Signature
	  - name: publicKey
	    type: PublicKey
	  - name: token
	    type: ByteArray

StartEstimation notification. This notification is produced when Storage nodes
should exchange estimation values of container sizes among other Storage nodes.

	StartEstimation:
	  - name: epoch
	    type: Integer

StopEstimation notification. This notification is produced when Storage nodes
should calculate average container size based on received estimations and store
it in Container contract.

	StopEstimation:
	  - name: epoch
	    type: Integer
*/
package container

/*
Contract storage model.

# Summary
Current conventions:
 <cid>: 32-byte container identifier (SHA-256 hashes of container data)
 <owner>: 25-byte NEO3 account of owner of the particular container
 <epoch>: little-endian unsigned integer NeoFS epoch

Key-value storage format:
 - 'netmapScriptHash' -> interop.Hash160
   Netmap contract reference
 - 'balanceScriptHash' -> interop.Hash160
   Balance contract reference
 - 'nnsScriptHash' -> interop.Hash160
   NNS contract reference
 - 'nnsRoot' -> interop.Hash160
   NNS root domain zone for containers
 - 'x<cid>' -> []byte
   container descriptors encoded into NeoFS API binary protocol format
 - 'd<cid>' -> ""
   deleted container IDs with no values for replay protection
 - 'o<owner><cid>' -> <cid>
   user-by-user containers
 - 'nnsHasAlias<cid>' -> string
   domains registered for containers in the NNS
 - 'cnr<epoch><cid>' + [10]byte -> std.Serialize(estimation)
   estimation of the container size sent by the storage node. Key suffix is first
   10 bytes of RIPEMD-160 hash of the storage node's public key
   (interop.PublicKey). Here estimation is a type.
 - 'est' + [20]byte -> []<epoch>
   list of NeoFS epochs when particular storage node sent estimations. Suffix is
   RIPEMD-160 hash of the storage node's public key (interop.PublicKey).
 - 'n<cid><placement_index><counter>' -> interop.PublicKey
   one of the container nodes' public key, counter is 2-bytes long BE
 - 'u<cid><placement_index><counter>' -> interop.PublicKey
   one of the container nodes' public key _for the next epoch_, they will become
   the current ones (with the 'n' prefix) once the Alphabet handles epoch update.
   Counter is 2-bytes long BE
 - 'r'<cid><placement_index> -> int (not bigger than uint8)
   REP clause from placement policy for <placement index>
 - 'eACL<cid>' -> []byte
   container eACLs encoded into NeoFS API binary protocol format

# Setting
To handle some events, the contract refers to other contracts.

# Containers
Contract stores information about all containers (incl. extended ACL tables)
presented in the NeoFS network for which the contract is deployed. For
performance optimization, container are additionally indexed by their owners.

# NNS
Contract tracks container-related domains registered in the NNS. By default
"container" TLD is used (unless overridden on deploy).

# Size estimations
Contract stores containers' size estimations came from NeoFS storage nodes.
*/
