/*
Package proxy implements Proxy contract which is deployed to FS chain.

Proxy contract pays for all multisignature transaction executions when notary
service is enabled in FS chain. Notary service prepares multisigned transactions,
however their senders should have FS chain GAS to succeed. It is inconvenient to
ask Alphabet nodes to pay for these transactions: nodes can change over time,
some nodes will spend FS chain GAS faster. It leads to economic instability.

Proxy contract exists to solve this issue. While Alphabet contracts hold all
FS chain NEO, proxy contract holds most of FS chain GAS. Alphabet
contracts emit half of the available GAS to the proxy contract. The address of the
Proxy contract is used as the first signer in a multisignature transaction.
Therefore, NeoVM executes Verify method of the contract; and if invocation is
verified, Proxy contract pays for the execution.

# Contract notifications

Proxy contract does not produce notifications to process.
*/
package proxy

/*
Contract storage model.

At the moment, no data is stored in the contract.
*/
