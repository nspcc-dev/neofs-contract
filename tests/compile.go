package tests

import (
	"path"

	"github.com/nspcc-dev/neo-go/cli/smartcontract"
	"github.com/nspcc-dev/neo-go/pkg/compiler"
	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/manifest"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/nef"
	"github.com/nspcc-dev/neo-go/pkg/util"
)

// Contract contains contract info for deployment.
type Contract struct {
	Hash     util.Uint160
	NEF      *nef.File
	Manifest *manifest.Manifest
}

var contracts = map[string]*Contract{}

// ContractInfo compiles contract and returns it's NEF, manifest and hash.
func ContractInfo(sender util.Uint160, ctrPath string) (*Contract, error) {
	if c, ok := contracts[ctrPath]; ok {
		return c, nil
	}

	// nef.NewFile() cares about version a lot.
	config.Version = "0.90.0-test"

	avm, di, err := compiler.CompileWithDebugInfo(ctrPath, nil)
	if err != nil {
		return nil, err
	}

	ne, err := nef.NewFile(avm)
	if err != nil {
		return nil, err
	}

	conf, err := smartcontract.ParseContractConfig(path.Join(ctrPath, "config.yml"))
	if err != nil {
		return nil, err
	}

	o := &compiler.Options{}
	o.Name = conf.Name
	o.ContractEvents = conf.Events
	o.ContractSupportedStandards = conf.SupportedStandards
	o.Permissions = make([]manifest.Permission, len(conf.Permissions))
	for i := range conf.Permissions {
		o.Permissions[i] = manifest.Permission(conf.Permissions[i])
	}
	o.SafeMethods = conf.SafeMethods
	m, err := compiler.CreateManifest(di, o)
	if err != nil {
		return nil, err
	}

	c := &Contract{
		Hash:     state.CreateContractHash(sender, ne.Checksum, m.Name),
		NEF:      ne,
		Manifest: m,
	}
	contracts[ctrPath] = c
	return c, nil
}
