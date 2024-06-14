/*
Package contracts embeds compiled NeoFS contracts and provides access to them.
*/
package contracts

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/manifest"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/nef"
)

const (
	neofsDir      = "neofs"      // not deployed to FS chain.
	processingDir = "processing" // not deployed to FS chain.

	nnsDir        = "nns"
	alphabetDir   = "alphabet"
	auditDir      = "audit"
	balanceDir    = "balance"
	containerDir  = "container"
	neofsIDDir    = "neofsid"
	netmapDir     = "netmap"
	proxyDir      = "proxy"
	reputationDir = "reputation"

	nefName      = "contract.nef"
	manifestName = "manifest.json"
)

// Contract groups information about Neo contract stored in the current package.
type Contract struct {
	NEF      nef.File
	Manifest manifest.Manifest
}

var (
	//go:embed */contract.nef */manifest.json
	_fs embed.FS

	errInvalidNEF      = errors.New("invalid NEF")
	errInvalidManifest = errors.New("invalid manifest")

	fsContracts = []string{
		nnsDir,
		proxyDir,
		auditDir,
		netmapDir,
		balanceDir,
		reputationDir,
		neofsIDDir,
		containerDir,
		alphabetDir,
	}
	mainContracts = []string{
		neofsDir,
		processingDir,
	}
)

// GetFS returns current set of NeoFS chain contracts stored in the package.
// They're returned in the order they're supposed to be deployed starting
// from NNS.
func GetFS() ([]Contract, error) {
	return read(_fs, fsContracts)
}

// GetMain returns current set of mainnet contracts stored in the package.
// They're returned in the order they're supposed to be deployed starting
// from NeoFS contract.
func GetMain() ([]Contract, error) {
	return read(_fs, mainContracts)
}

// read same as Read by allows to override source fs.FS.
func read(_fs fs.FS, dirs []string) ([]Contract, error) {
	var res = make([]Contract, 0, len(dirs))

	for i := range dirs {
		c, err := readContractFromDir(_fs, dirs[i])
		if err != nil {
			return nil, fmt.Errorf("read contract %s: %w", dirs[i], err)
		}

		res = append(res, c)
	}

	return res, nil
}

func readContractFromDir(_fs fs.FS, dir string) (Contract, error) {
	var c Contract

	// Only embedded FS is supported now and it uses "/" even on Windows,
	// so filepath.Join() is not applicable.
	fNEF, err := _fs.Open(dir + "/" + nefName)
	if err != nil {
		return c, fmt.Errorf("open NEF: %w", err)
	}
	defer fNEF.Close()

	fManifest, err := _fs.Open(dir + "/" + manifestName)
	if err != nil {
		return c, fmt.Errorf("open manifest: %w", err)
	}
	defer fManifest.Close()

	bReader := io.NewBinReaderFromIO(fNEF)
	c.NEF.DecodeBinary(bReader)
	if bReader.Err != nil {
		return c, fmt.Errorf("%w: %w", errInvalidNEF, bReader.Err)
	}

	err = json.NewDecoder(fManifest).Decode(&c.Manifest)
	if err != nil {
		return c, fmt.Errorf("%w: %w", errInvalidManifest, err)
	}

	return c, nil
}
