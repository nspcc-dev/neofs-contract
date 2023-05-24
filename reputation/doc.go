/*
Package reputation contains implementation of Reputation contract deployed in NeoFS
sidechain.

Inner Ring nodes produce data audit for each container during each epoch. In the end,
nodes produce DataAuditResult structure that contains information about audit
progress. Reputation contract provides storage for such structures and simple
interface to iterate over available DataAuditResults on specified epoch.

During settlement process, Alphabet nodes fetch all DataAuditResult structures
from the epoch and execute balance transfers from data owners to Storage and
Inner Ring nodes if data audit succeeds.

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
