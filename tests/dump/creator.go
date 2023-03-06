package dump

import (
	"encoding/csv"
	"encoding/json"
	"fmt"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
)

// Creator dumps states of the Neo smart contracts. Output file format:
//
//	'<label>-<block>-contracts.json': JSON array of contracts' states
//	'<label>-<block>-storage.csv': CSV of contracts' storages
//
// Storage CSV are 'name,key,value' where name stands for contract name and
// binary key-value are base64-encoded.
//
// Use IterateDumps to access existing dumps.
type Creator struct {
	dumpStreams

	contracts []dumpContractState

	storageItemsCSV *csv.Writer
}

// NewCreator returns Creator which dumps contracts into given directory. The
// dump is identified by specified ID. Resulting Creator should be closed when
// finished working with it.
//
// NewCreator fails if dump with provided ID already exists.
func NewCreator(dir string, id ID) (*Creator, error) {
	var res Creator

	err := initDumpStreams(&res.dumpStreams, dir, id, false)
	if err != nil {
		return nil, err
	}

	res.storageItemsCSV = csv.NewWriter(res.dumpStreams.storageItems)

	return &res, nil
}

// AddContract adds given state of the named Neo contract to the resulting dump
// and returns StorageWriter for the contract storage. After all needed
// contracts are added, they should be flushed via Flush method.
func (x *Creator) AddContract(name string, st state.Contract) *StorageWriter {
	x.contracts = append(x.contracts, dumpContractState{
		Name:  name,
		State: st,
	})

	return &StorageWriter{
		name: name,
		csv:  x.storageItemsCSV,
	}
}

// Flush flushes accumulated dump to the file system.
func (x *Creator) Flush() error {
	jEnc := json.NewEncoder(x.dumpStreams.contracts)
	jEnc.SetIndent("", " ")

	err := jEnc.Encode(x.contracts)
	if err != nil {
		return fmt.Errorf("encode contract states to JSON: %w", err)
	}

	x.storageItemsCSV.Flush()

	err = x.storageItemsCSV.Error()
	if err != nil {
		return fmt.Errorf("flush CSV data: %w", err)
	}

	return nil
}

// Close releases underlying resources of the Creator and makes it unusable.
func (x *Creator) Close() {
	x.close()
}

// StorageWriter writes data into the superior contract's storage dump.
type StorageWriter struct {
	name string
	csv  *csv.Writer
}

// Write saves given binary key-value into the contract dump as storage item.
func (x *StorageWriter) Write(key, value []byte) error {
	err := x.csv.Write([]string{
		x.name,
		_encoding.EncodeToString(key),
		_encoding.EncodeToString(value),
	})
	if err != nil {
		return fmt.Errorf("write storage item as CSV data: %w", err)
	}

	return nil
}
