/*
Package reputation implements Reputation contract which is deployed to FS chain.

Storage nodes collect reputation data while communicating with other nodes.
This data is exchanged and the end result (global trust values) is stored in
the contract as opaque data.

# Contract notifications

Reputation contract does not produce notifications to process.
*/
package reputation

/*
Contract storage model.

Current conventions:
 <peer>: binary unique identifier of the NeoFS Reputation system participant
 <epoch>: little-endian unsigned integer NeoFS epoch

# Summary
Key-value storage format:
 - 'c<epoch><peer>' -> int
   Number of values got from calculated by fixed peer at fixed NeoFS epoch
 - 'r<epoch><peer>' + count -> []byte
   binary-encoded global trust values submitted calculated at fixed epoch by
   particular peer. All such values are counted starting from 0.

# Trust
Contract stores trust values collected within NeoFS Reputation system lifetime.
*/
