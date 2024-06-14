package contracts

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/nspcc-dev/neo-go/pkg/smartcontract/manifest"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/nef"
	"github.com/stretchr/testify/require"
)

func TestGetFS(t *testing.T) {
	c, err := GetFS()
	require.NoError(t, err)
	require.Equal(t, len(fsContracts), len(c))
}

func TestGetMain(t *testing.T) {
	c, err := GetMain()
	require.NoError(t, err)
	require.Equal(t, len(mainContracts), len(c))
}

func TestGetMissingFiles(t *testing.T) {
	_fs := fstest.MapFS{}

	// Missing NEF
	_, err := read(_fs, mainContracts)
	require.Error(t, err)

	// Missing manifest.
	_fs[neofsDir+"/"+nefName] = &fstest.MapFile{}
	_, err = read(_fs, mainContracts)
	require.Error(t, err)
}

func TestReadInvalidFormat(t *testing.T) {
	var (
		_fs          = fstest.MapFS{}
		nefPath      = neofsDir + "/" + nefName
		manifestPath = neofsDir + "/" + manifestName
	)

	_, validNEF := anyValidNEF(t)
	_, validManifest := anyValidManifest(t, "zero")

	_fs[nefPath] = &fstest.MapFile{Data: validNEF}
	_fs[manifestPath] = &fstest.MapFile{Data: validManifest}

	_, err := read(_fs, []string{neofsDir})
	require.NoError(t, err)

	_fs[nefPath] = &fstest.MapFile{Data: []byte("not a NEF")}
	_fs[manifestPath] = &fstest.MapFile{Data: validManifest}

	_, err = read(_fs, []string{neofsDir})
	require.ErrorIs(t, err, errInvalidNEF)

	_fs[nefPath] = &fstest.MapFile{Data: validNEF}
	_fs[manifestPath] = &fstest.MapFile{Data: []byte("not a manifest")}

	_, err = read(_fs, []string{neofsDir})
	require.ErrorIs(t, err, errInvalidManifest)
}

func anyValidNEF(tb testing.TB) (nef.File, []byte) {
	script := make([]byte, 32)

	_nef, err := nef.NewFile(script)
	require.NoError(tb, err)

	bNEF, err := _nef.Bytes()
	require.NoError(tb, err)

	return *_nef, bNEF
}

func anyValidManifest(tb testing.TB, name string) (manifest.Manifest, []byte) {
	_manifest := manifest.NewManifest(name)

	jManifest, err := json.Marshal(_manifest)
	require.NoError(tb, err)

	return *_manifest, jManifest
}
