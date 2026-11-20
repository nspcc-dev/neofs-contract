/*
Package accessbox implements the Access Box contract.

The contract stores opaque access-box payloads used by S3 Gateway and Console.
Payloads and their metadata are public blockchain state. The contract does not
decode payloads, so it preserves bytes produced by clients exactly.

# Contract notifications

Access Box contract does not produce notifications to process.
*/
package accessbox

/*
Contract storage model.

Current conventions:
 <id>: 16-byte access-box identifier derived from its creation transaction
 <owner>: access-box owner script hash
 <kind>: one-byte access-box kind

# Summary
Key-value storage format:
 - 'p<id>' -> []byte
   Opaque access-box payload.
 - 'r<id>' -> std.Serialize(Record)
   Public access-box metadata.
 - 'u<id>' -> []byte
   Expiry timestamp. This is the expiry index scanned by PruneExpired.
 - 'o<owner><kind><id>' -> <id>
   Owner index. Kind 0 indexes every record of an owner.
 - 'v<revoker>' -> 1
   Access-box revoker index. Revokers can revoke records for any owner.
 - 'V' -> int
   Number of registered revokers. At most 100 revokers can be registered.

The revoker list is empty after deployment. Committee can add and remove
revokers. A revoker may revoke records for any owner.

# Lifetime

The contract accepts records with expiry no later than one year from the block
time. Expired payloads are not returned by Get. Metadata is retained until a
record is explicitly revoked or PruneExpired removes it.

# Fee

The optional AccessBoxFee Netmap configuration value is the amount paid to
each current Alphabet member when a record is created. A missing value means
zero, which keeps upgraded networks compatible until they configure a fee.
*/
