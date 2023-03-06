/*
Package dump provides I/O operations for collected states of the Neo smart
contracts.

State collection (including storage) allows you to emulate work with "live"
contract. First of all, it is in demand for testing. For state reproducibility,
it is necessary to be able to persist (dump) information about the contract
along with its data, as well as read ready-made dumps.

The package works with dumps stored in the file system using human-readable
encoding.
*/
package dump
