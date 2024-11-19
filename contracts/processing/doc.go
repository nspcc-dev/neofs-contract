/*
Package processing contains Processing contract which is deployed to main chain.

Processing contract pays for all multisignature transaction executions when notary
service is enabled in the main chain. Notary service prepares multisigned transactions,
however their senders should have GAS to succeed. It is inconvenient to
ask Alphabet nodes to pay for these transactions: nodes can change over time,
some nodes will spend GAS faster. It leads to economic instability.

Processing contract exists to solve this issue. At the Withdraw invocation of
NeoFS contract, a user pays fee directly to this contract. This fee is used to
pay for Cheque invocation of NeoFS contract that returns main chain GAS back
to the user. The address of the Processing contract is used as the first signer in
the multisignature transaction. Therefore, NeoVM executes Verify method of the
contract and if invocation is verified, Processing contract pays for the
execution.

# Contract notifications

Processing contract does not produce notifications to process.
*/
package processing

/*
Contract storage model.

# Summary
Key-value storage format:
 - 'neofsScriptHash' -> interop.Hash160
   NeoFS contract reference

# Setting
To handle some events, the contract refers to other contracts.
*/
