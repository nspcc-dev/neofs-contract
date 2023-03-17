package dump

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nspcc-dev/neo-go/pkg/core/state"
)

// ID is a unique identifier of the dump prepared according to the model
// described in the current package.
type ID struct {
	// Label of the dump source (e.g. testnet, mainnet).
	Label string
	// Blockchain height at which the state was pulled.
	Block uint32
}

// String returns hyphen-separated ID fields.
func (x ID) String() string {
	return x.Label + sep + strconv.FormatUint(uint64(x.Block), 10)
}

// decodes ID fields from the hyphen-separated string.
func (x *ID) decodeString(s string) error {
	ss := strings.Split(s, sep)
	if len(ss) < 2 {
		return fmt.Errorf("expected '%s'-separated string with at least 2 items", sep)
	}

	n, err := strconv.ParseUint(ss[1], 10, 32)
	if err != nil {
		return fmt.Errorf("decode block number from '%s': %w", ss[1], err)
	}

	x.Label = ss[0]
	x.Block = uint32(n)

	return nil
}

// global encoding of binary values.
var _encoding = base64.StdEncoding

// dumpContractState is a JSON-encoded information about the dumped contract.
type dumpContractState struct {
	Name  string         `json:"name"`
	State state.Contract `json:"state"`
}

// dumpStreams groups data streams for contracts' states and storages.
type dumpStreams struct {
	contracts, storageItems io.ReadWriteCloser
}

// close closes all streams.
func (x *dumpStreams) close() {
	_ = x.storageItems.Close()
	_ = x.contracts.Close()
}

const (
	// word separator used in dump file naming
	sep = "-"
	// suffix of file with contracts' states
	statesFileSuffix = "contracts.json"
)

// initDumpStreams opens data streams for the dump files located in the
// specified directory. If read flag is set, streams are read-only. Otherwise,
// files must not exist, and streams are write only.
func initDumpStreams(d *dumpStreams, dir string, id ID, read bool) error {
	var err error

	pathStorage := filepath.Join(dir, strings.Join([]string{id.String(), "storage.csv"}, sep))
	if !read {
		if err = checkFileNotExists(pathStorage); err != nil {
			return err
		}
	}

	pathContracts := filepath.Join(dir, strings.Join([]string{id.String(), statesFileSuffix}, sep))
	if !read {
		if err = checkFileNotExists(pathContracts); err != nil {
			return err
		}
	}

	var flag int
	var perm os.FileMode

	if read {
		flag = os.O_RDONLY
	} else {
		flag = os.O_CREATE | os.O_WRONLY
		perm = 0600
	}

	d.storageItems, err = os.OpenFile(pathStorage, flag, perm)
	if err != nil {
		return fmt.Errorf("open file with storage items: %w", err)
	}

	d.contracts, err = os.OpenFile(pathContracts, flag, perm)
	if err != nil {
		return fmt.Errorf("open file with contract states: %w", err)
	}

	return nil
}
