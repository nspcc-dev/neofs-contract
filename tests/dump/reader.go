package dump

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
)

// IterateDumps iterates over all contracts collected by the Creator model in
// the specified directory, and passes ID and Reader of each dump into f.
func IterateDumps(dir string, f func(ID, *Reader)) error {
	var id ID
	var r Reader
	var streams dumpStreams

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, e error) error {
		if errors.Is(e, fs.ErrNotExist) {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		name := d.Name()

		err := id.decodeString(name)
		if err != nil {
			return fmt.Errorf("decode dump ID from file name '%s': %w", d.Name(), err)
		}

		if !strings.HasSuffix(name, statesFileSuffix) {
			return nil
		}

		err = initDumpStreams(&streams, dir, id, true)
		if err != nil {
			return fmt.Errorf("init dump streams ('%s'): %w", name, err)
		}

		err = r.fromDumpStreams(streams.contracts, streams.storageItems)
		if err != nil {
			return fmt.Errorf("init dump reader ('%s'): %w", name, err)
		}

		streams.close()

		f(id, &r)

		return nil
	})
}

type kv struct{ k, v []byte }

// Reader reads contracts collected in the superior dump.
type Reader struct {
	states   []dumpContractState
	mStorage map[string][]kv
}

func (x *Reader) fromDumpStreams(rContracts, rStorageItems io.Reader) error {
	err := json.NewDecoder(rContracts).Decode(&x.states)
	if err != nil {
		return fmt.Errorf("decode contract states from JSON: %w", err)
	}

	var rec []string
	var _kv kv

	_csv := csv.NewReader(rStorageItems)
	_csv.FieldsPerRecord = 3
	_csv.ReuseRecord = true

	if x.mStorage != nil {
		clear(x.mStorage)
	} else {
		x.mStorage = make(map[string][]kv)
	}

	for {
		rec, err = _csv.Read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read next CSV record: %w", err)
		}

		// out-of-range safety guaranteed by csv settings
		_kv.k, err = _encoding.DecodeString(rec[1])
		if err != nil {
			return fmt.Errorf("decode storage item key: %w", err)
		}

		_kv.v, err = _encoding.DecodeString(rec[2])
		if err != nil {
			return fmt.Errorf("decode storage item value: %w", err)
		}

		x.mStorage[rec[0]] = append(x.mStorage[rec[0]], _kv)
	}
}

// IterateContractStates iterates over all contracts from the superior dump and
// passes their states into f.
func (x *Reader) IterateContractStates(f func(name string, _state state.Contract)) (err error) {
	for i := range x.states {
		f(x.states[i].Name, x.states[i].State)
	}
	return nil
}

// IterateContractStorages iterates over all contracts from the superior dump
// and passes their storage items into f.
func (x *Reader) IterateContractStorages(f func(name string, key, value []byte)) error {
	for name, kvs := range x.mStorage {
		for i := range kvs {
			f(name, kvs[i].k, kvs[i].v)
		}
	}
	return nil
}
