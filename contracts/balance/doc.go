/*
Package balance implements Balance contract which is deployed to FS chain.

Balance contract stores all NeoFS account balances. It is a NEP-17 compatible
contract, so it can be tracked and controlled by N3 compatible network
monitors and wallet software.

This contract is used to store all micro transactions in FS chain, such as
data audit settlements or container fee payments. It is inefficient to make such
small payment transactions in main chain. To process small transfers, balance
contract has higher (12) decimal precision than native GAS contract.

NeoFS balances are synchronized with main chain operations. Deposit produces
minting of NEOFS tokens in Balance contract. Withdraw locks some NEOFS tokens
in a special lock account. When NeoFS contract transfers GAS assets back to the
user, the lock account is destroyed with burn operation.

# Contract notifications

Transfer notification. This is a NEP-17 standard notification.

	Transfer:
	  - name: from
	    type: Hash160
	  - name: to
	    type: Hash160
	  - name: amount
	    type: Integer

TransferX notification. This is an enhanced transfer notification with details.

	TransferX:
	  - name: from
	    type: Hash160
	  - name: to
	    type: Hash160
	  - name: amount
	    type: Integer
	  - name: details
	    type: ByteArray

Lock notification. This notification is produced when a lock account is
created. It contains information about main chain transaction that has produced
the asset lock, the address of the lock account and the NeoFS epoch number until which the
lock account is valid. Alphabet nodes of the Inner Ring catch notification and initialize
Cheque method invocation of NeoFS contract.

	Lock:
	  - name: txID
	    type: ByteArray
	  - name: from
	    type: Hash160
	  - name: to
	    type: Hash160
	  - name: amount
	    type: Integer
	  - name: until
	    type: Integer
*/
package balance

/*
Contract storage model.

# Summary
Key-value storage format:
 - 'MainnetGAS' -> int
   total amount of main chain GAS deployed in the NeoFS network in Fixed12
 - a<interop.Hash160> -> std.Serialize(Account)
   balance sheet of all NeoFS users (here Account is a structure defined in current package)

# Accounting
Contract stores information about all NeoFS accounts.
*/
