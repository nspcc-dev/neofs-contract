/*
Package neofs contains implementation of NeoFS contract deployed in NeoFS mainchain.

NeoFS contract is an entry point to NeoFS users. This contract stores all NeoFS
related GAS, registers new Inner Ring candidates and produces notifications
to control the sidechain.

While mainchain committee controls the list of Alphabet nodes in native
RoleManagement contract, NeoFS can't change more than 1\3 keys at a time.
NeoFS contract contains the actual list of Alphabet nodes in the sidechain.

Network configuration is also stored in NeoFS contract. All changes in
configuration are mirrored in the sidechain with notifications.

# Contract notifications

Deposit notification. This notification is produced when user transfers native
GAS to the NeoFS contract address. The same amount of NEOFS token will be
minted in Balance contract in the sidechain.

	Deposit:
	  - name: from
	    type: Hash160
	  - name: amount
	    type: Integer
	  - name: receiver
	    type: Hash160
	  - name: txHash
	    type: Hash256

Withdraw notification. This notification is produced when a user wants to
withdraw GAS from the internal NeoFS balance and has paid fee for that.

	Withdraw:
	  - name: user
	    type: Hash160
	  - name: amount
	    type: Integer
	  - name: txHash
	    type: Hash256

Cheque notification. This notification is produced when NeoFS contract
has successfully transferred assets back to the user after withdraw.

	Cheque:
	  - name: id
	    type: ByteArray
	  - name: user
	    type: Hash160
	  - name: amount
	    type: Integer
	  - name: lockAccount
	    type: ByteArray

Bind notification. This notification is produced when a user wants to bind
public keys with the user account (OwnerID). Keys argument is an array of ByteArray.

	Bind:
	  - name: user
	    type: ByteArray
	  - name: keys
	    type: Array

Unbind notification. This notification is produced when a user wants to unbind
public keys with the user account (OwnerID). Keys argument is an array of ByteArray.

	Unbind:
	  - name: user
	    type: ByteArray
	  - name: keys
	    type: Array

AlphabetUpdate notification. This notification is produced when Alphabet nodes
have updated their lists in the contract. Alphabet argument is an array of ByteArray. It
contains public keys of new alphabet nodes.

	AlphabetUpdate:
	  - name: id
	    type: ByteArray
	  - name: alphabet
	    type: Array

SetConfig notification. This notification is produced when Alphabet nodes update
NeoFS network configuration value.

	SetConfig
	  - name: id
	    type: ByteArray
	  - name: key
	    type: ByteArray
	  - name: value
	    type: ByteArray
*/
package neofs

/*
Contract storage model.

# Summary
Key-value storage format:
 - 'notary' -> bool
   is notary mode disabled
 - 'ballots' -> std.Serialize([]Ballot)
   collected ballots for pending voting if notary disabled (here Ballot is a
   structure defined in common package)
 - 'processingScriptHash' -> interop.Hash160
   Processing contract reference
 - 'candidates' + interop.PublicKey -> 1
   each participant who is considered for entry into the Inner Ring
 - 'alphabet' -> []interop.PublicKey
   list of the NeoFS Alphabet members

# Setting
Contract can be deployed in notary and notary-disabled mode.

To handle some events, the contract refers to other contracts.

# Network configuration
Contract storage configuration of the NeoFS network within which the contract is
deployed.

# Inner Ring Contract accumulates candidates for the Inner Ring. It also holds
current NeoFS Alphabet.

# Voting
Contract collects voting data in notary-disabled installation.
*/
