/*
Package accessbox implements the Access Box contract.

The contract stores opaque access-box entries for NeoFS applications and
clients. It does not decode entry keys or values. All records, categories, and
entries are public blockchain state.

# Contract notifications

Put emits `AccessBoxCreated` after it stores the new record, indexes, and
initial entries. The notification has one argument: the generated 16-byte
access-box ID.
*/
package accessbox

/*
Contract storage model.

Current conventions:
 <id>: 16-byte access-box identifier derived from its creation transaction
 <owner>: access-box owner script hash
 <category>: caller-defined non-empty string, no longer than 16 bytes
 <key>: caller-defined non-empty entry key, no longer than 47 bytes
 <value>: caller-defined non-empty entry value, no longer than 65,000 bytes

An access box has at most 100 entries. The aggregate size of its entry values
is at most 256 KiB. Put accepts from one to 100 initial keys which all store
the same value. Further entries can be added one at a time with Append until
either limit is reached. These limits keep revocation and expiry cleanup
bounded.

# Summary

Key-value storage format:
 - 'r<id>' -> std.Serialize(Record)
   Public access-box record.
 - 'e<id><key>' -> []byte
   Opaque access-box entry.
 - 'u<expiry><id>' -> []byte
   Expiry index. Expiry is an eight-byte big-endian Unix timestamp in
   milliseconds, so keys sort by expiry and PruneExpired always starts with
   the earliest record.
 - 'o<owner>\x00<id>' -> std.Serialize(Record)
   Owner index over all categories.
 - 'o<owner>\x01<category length><category><id>' -> std.Serialize(Record)
   Owner index over one category. The category length prevents one category
   prefix from matching another category.

# Listing

List returns an iterator over records for an owner and optional category. Each
item is a Record. Clients consume it through normal Neo RPC iterator mechanisms
(sessions or expansion). List intentionally has no contract paging or cursor
API; clients that expose a paged API decide how to paginate the results.

RevokeOwner and PruneExpired retain their record limit and also remove at most
256 entries per invocation. If a call reaches that entry limit, the caller can
invoke it again to continue with the remaining records.

# Authorization

Only the owner can create an access box or append an entry. The owner or
committee can remove an entry, revoke one access box, or revoke access boxes
for an owner.

# Lifetime

The contract accepts records with expiry no later than one year from the block
time. Get does not return entries from expired records. List and GetInfo still
return their record information until the record is explicitly revoked or
PruneExpired removes it.

# Fee

The optional AccessBoxFee Netmap configuration value is the amount paid to
each current Alphabet member when an access box is created. A missing value
means zero, which keeps upgraded networks compatible until they configure a
fee. Appending or removing entries does not charge AccessBoxFee.
*/
