/*
Audit contract is a contract deployed in NeoFS side chain.

Inner Ring nodes perform an audit of the registered containers in every epoch.
If container contains StorageGroup objects, then the Inner Ring node initializes
a series of audit checks. Based on the results of these checks, the Inner Ring
node creates a DataAuditResult structure for the container. The content of this
structure makes it possible to determine which storage nodes were examined and
the status of these checks. Based on this information, container owner is
charged for data storage.

Audit contract is used as reliable and verifiable storage for all
DataAuditResult structures. At the end of the data audit routine, the Inner Ring
nodes send a stable marshaled version of the DataAuditResult structure to the
contract. When Alphabet nodes of the Inner Ring perform settlement operations,
they list and get these AuditResultStructures from the audit contract.

Contract notifications

Alphabet contract does not produce notifications to process.
*/
package audit
