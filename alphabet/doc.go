/*
Alphabet contract is a contract deployed in NeoFS side chain.

Alphabet contract is designed to support GAS producing and voting for new
validators in the side chain. NEO token is required to produce GAS and vote for
a new committee. If can be distributed among alphabet nodes of Inner Ring.
However, some of them may be malicious and some NEO can be lost. It will lead
to side chain economic destabilization. To avoid it, all 100 000 000 NEO are
distributed among all alphabet contracts.

To identify alphabet contracts, they are named with letters of the Glagolitic.
Names are set at contract deploy. Alphabet nodes of Inner Ring communicate with
one of the alphabetical contracts to emit GAS. To vote for a new list of side
chain committee, alphabet nodes of Inner Ring create multisignature transactions
for each alphabet contract.

Contract notifications

Alphabet contract does not produce notifications to process.
*/
package alphabet
