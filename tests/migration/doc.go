/*
Package migration provides framework to test migration of the NeoFS smart contracts.

Smart contracts store sensitive system data of the NeoFS network. The contracts
are updated on the fly, so data migration must be performed accurately, without
backward compatibility loss. The package provides services of Neo blockchain and
particular contract needed for testing. Test blockchain environment can be based
on "real" data from the remote NeoFS blockchain instances.
*/
package migration
