/*
Package audit implements Audit contract which is deployed to FS chain.

Inner Ring nodes perform audit of the registered containers during every epoch.
If a container contains StorageGroup objects, an Inner Ring node initializes
a series of audit checks. Based on the results of these checks, the Inner Ring
node creates a DataAuditResult structure for the container. The content of this
structure makes it possible to determine which storage nodes have been examined and
see the status of these checks. Regarding this information, the container owner is
charged for data storage.

Audit contract is used as a reliable and verifiable storage for all
DataAuditResult structures. At the end of data audit routine, Inner Ring
nodes send a stable marshaled version of the DataAuditResult structure to the
contract. When Alphabet nodes of the Inner Ring perform settlement operations,
they make a list and get these AuditResultStructures from the audit contract.

# Contract notifications

Audit contract does not produce notifications to process.
*/
package audit

/*
Contract storage model.

# Summary
Key-value storage format:
 - <epoch><cid><key> -> []byte
   Data audit results encoded into NeoFS API binary protocol format. Results are
   identified by triplet concatenation:
    1. little-endian unsigned integer NeoFS epoch when audit was performed
    2. 32-byte identifier of the NeoFS container under audit
    3. 24-byte prefix of SHA-256 hash of the auditor's (Inner Ring) public key

# Audit history
Contracts stores results of the NeoFS data audits performed by the Inner Ring.
*/
