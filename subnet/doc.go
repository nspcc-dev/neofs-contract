/*
Package subnet contains implementation of Subnet contract deployed in NeoFS
sidechain.

Subnet contract stores and manages NeoFS subnetwork states. It allows registering
and deleting subnetworks, limiting access to them, and defining a list of the Storage
Nodes that can be included in them.

# Contract notifications

Put notification. This notification is produced when a new subnetwork is
registered by invoking Put method.

	Put
	  - name: id
	    type: ByteArray
	  - name: ownerKey
	    type: PublicKey
	  - name: info
	    type: ByteArray

Delete notification. This notification is produced when some subnetwork is
deleted by invoking Delete method.

	Delete
	  - name: id
	    type: ByteArray

RemoveNode notification. This notification is produced when some node is deleted
by invoking RemoveNode method.

	RemoveNode
	  - name: subnetID
	    type: ByteArray
	  - name: node
	    type: PublicKey
*/
package subnet

/*
Contract storage model.

# Summary
Current conventions:
 <id>: binary-encoded unique identifier of the subnet

Key-value storage format:
 - 'o<id>' -> interop.PublicKey
   public keys of the subnet owners
 - 'i<id>' -> []byte
   subnet descriptors encoded into NeoFS API binary protocol format
 - 'n<id>' + interop.PublicKey -> 1
   public keys of nodes forming the subnet cluster
 - 'a<id>' + interop.PublicKey -> 1
   public keys of administrators managing topology of the subnet cluster
 - 'u<id>' + interop.PublicKey -> 1
   public keys of clients using the subnet to store data
 - 'm<id>' + interop.PublicKey -> 1
   public keys of administrators managing clients of the subnet

# Registration
Contract stores information about all subnets and their owners presented in the
NeoFS network for which the contract is deployed.

# Topology management
Contract records the parties responsible for managing subnets' cluster
topology.

# Client management
Contract records the parties responsible for managing subnet clients which store
data in the subnet cluster.
*/
