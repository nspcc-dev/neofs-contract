/*
Balance contract is a contract deployed in NeoFS side chain.

Balance contract stores all NeoFS account balances. It is NEP-17 compatible
contract so in can be tracked and controlled by N3 compatible network
monitors and wallet software.

This contract is used to store all micro transactions in the sidechain, such as
data audit settlements or container fee payments. It is inefficient to make such
small payment transactions in main chain. To process small transfers, balance
contract has higher (12) decimal precision than native GAS contract.

NeoFS balances are synchronized with main chain operations. Deposit produce
minting of NEOFS tokens in Balance contract. Withdraw locks some NEOFS tokens
in special lock account. When NeoFS contract transfers GAS assets back to the
user, lock account is destroyed with burn operation.

Contract notifications

Transfer notification. This is NEP-17 standard notification.

  Transfer:
    - name: from
      type: Hash160
    - name: to
      type: Hash160
    - name: amount
      type: Integer

TransferX notification. This is enhanced transfer notification with details.

  TransferX:
    - name: from
      type: Hash160
    - name: to
      type: Hash160
    - name: amount
      type: Integer
    - name: details
      type: ByteArray

Lock notification. This notification is produced when Lock account has been
created. It contains information about main chain transaction that produced
asset lock, address of lock account and NeoFS epoch number until lock account
is valid. Alphabet nodes of the Inner Ring catch notification and initialize
Cheque method invocation of the NeoFS contract.

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

Mint notification. This notification is produced when user balance is
replenished from deposit in the main chain.

  Mint:
   - name: to
     type: Hash160
   - name: amount
     type: Integer


Burn notification. This notification is produced after user balance is reduced
when NeoFS contract transferred GAS assets back to the user.

  Burn:
    - name: from
      type: Hash160
    - name: amount
      type: Integer
*/
package balance
