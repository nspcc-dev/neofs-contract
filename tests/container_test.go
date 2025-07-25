package tests

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math"
	"math/big"
	"path"
	"slices"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/config/netmode"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/core/native/nativenames"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
	"github.com/nspcc-dev/neofs-contract/contracts/nns/recordtype"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const containerPath = "../contracts/container"
const containerDomain = "container"

const (
	containerFee      = 0_0100_0000
	containerAliasFee = 0_0050_0000
)

func deployContainerContract(t *testing.T, e *neotest.Executor, addrNetmap, addrBalance, addrNNS *util.Uint160) util.Uint160 {
	args := make([]any, 6)
	args[0] = int64(0)
	args[1] = addrNetmap
	args[2] = addrBalance
	args[3] = util.Uint160{} // not needed for now
	args[4] = addrNNS

	return deployContainerContractInternal(t, e, args)
}

func deployContainerContractInternal(t *testing.T, e *neotest.Executor, args []any) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	e.DeployContract(t, c, args)

	// Container contract cannot be deployed without Proxy knowing its hash
	// so its hash must always be precalculated externally and be registered
	// in NNS beforehand, that is why there is no NNS registering in
	// `deployContainer*` functions ever

	return c.Hash
}

func newContainerInvoker(t *testing.T, autohashes bool) (*neotest.ContractInvoker, *neotest.ContractInvoker, *neotest.ContractInvoker) {
	e := newExecutor(t)

	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))

	nnsHash := deployDefaultNNS(t, e)
	deployNetmapContract(t, e, containerconst.RegistrationFeeKey, int64(containerFee),
		containerconst.AliasFeeKey, int64(containerAliasFee))
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	deployProxyContract(t, e)
	if !autohashes {
		deployContainerContract(t, e, &ctrNetmap.Hash, &ctrBalance.Hash, &nnsHash)
	} else {
		deployContainerContractInternal(t, e, nil)
	}
	return e.CommitteeInvoker(ctrContainer.Hash), e.CommitteeInvoker(ctrBalance.Hash), e.CommitteeInvoker(ctrNetmap.Hash)
}

func newProxySponsorInvoker(t *testing.T) (*neotest.ContractInvoker, *neotest.ContractInvoker, *neotest.ContractInvoker) {
	e := newExecutor(t)

	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	ctrProxy := neotest.CompileFile(t, e.CommitteeHash, proxyPath, path.Join(proxyPath, "config.yml"))

	nnsHash := deployDefaultNNS(t, e)
	deployNetmapContract(t, e, containerconst.RegistrationFeeKey, int64(containerFee),
		containerconst.AliasFeeKey, int64(containerAliasFee))
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	deployProxyContract(t, e)
	deployContainerContract(t, e, &ctrNetmap.Hash, &ctrBalance.Hash, &nnsHash)

	gasHash, err := e.Chain.GetNativeContractScriptHash(nativenames.Gas)
	require.NoError(t, err)
	gasInv := e.CommitteeInvoker(gasHash).WithSigners(e.Validator)
	gasInv.Invoke(t, true, "transfer",
		e.Validator.ScriptHash(), ctrProxy.Hash,
		int64(10_0000_0000), nil)

	return e.NewInvoker(ctrContainer.Hash, neotest.NewContractSigner(ctrProxy.Hash, func(tx *transaction.Transaction) []any {
		return []any{}
	}), e.NewAccount(t)), e.CommitteeInvoker(ctrContainer.Hash), e.CommitteeInvoker(ctrBalance.Hash)
}

func setContainerOwner(c []byte, acc neotest.Signer) {
	owner, _ := base58.Decode(address.Uint160ToString(acc.ScriptHash()))
	copy(c[6:], owner)
}

type testContainer struct {
	id                     [32]byte
	value, sig, pub, token []byte
}

func dummyContainer(owner neotest.Signer) testContainer {
	value := randomBytes(100)
	value[1] = 0 // zero offset
	setContainerOwner(value, owner)

	return testContainer{
		id:    sha256.Sum256(value),
		value: value,
		sig:   randomBytes(64),
		pub:   randomBytes(33),
		token: randomBytes(42),
	}
}

func TestContainerCount(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t, false)

	checkCount := func(t *testing.T, expected int64) {
		s, err := c.TestInvoke(t, "count")
		require.NoError(t, err)
		bi := s.Pop().BigInt()
		require.True(t, bi.IsInt64())
		require.Equal(t, int64(expected), bi.Int64())
	}

	checkCount(t, 0)
	acc1, cnt1 := addContainer(t, c, cBal)
	checkCount(t, 1)

	_, cnt2 := addContainer(t, c, cBal)
	checkCount(t, 2)

	// Same owner.
	cnt3 := dummyContainer(acc1)
	balanceMint(t, cBal, acc1, containerFee*1, []byte{})
	c.Invoke(t, stackitem.Null{}, "put", cnt3.value, cnt3.sig, cnt3.pub, cnt3.token)
	checkContainerList(t, c, [][]byte{cnt1.id[:], cnt2.id[:], cnt3.id[:]})

	c.Invoke(t, stackitem.Null{}, "delete", cnt1.id[:], cnt1.sig, cnt1.token)
	checkCount(t, 2)
	checkContainerList(t, c, [][]byte{cnt2.id[:], cnt3.id[:]})

	c.Invoke(t, stackitem.Null{}, "delete", cnt2.id[:], cnt2.sig, cnt2.token)
	checkCount(t, 1)
	checkContainerList(t, c, [][]byte{cnt3.id[:]})

	c.Invoke(t, stackitem.Null{}, "delete", cnt3.id[:], cnt3.sig, cnt3.token)
	checkCount(t, 0)
	checkContainerList(t, c, [][]byte{})
}

func checkContainerList(t *testing.T, c *neotest.ContractInvoker, expected [][]byte) {
	t.Run("check with `containersOf`", func(t *testing.T) {
		s, err := c.TestInvoke(t, "containersOf", nil)
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())

		iter, ok := s.Top().Value().(*storage.Iterator)
		require.True(t, ok)

		actual := make([][]byte, 0, len(expected))
		for iter.Next() {
			id, ok := iter.Value().Value().([]byte)
			require.True(t, ok)
			actual = append(actual, id)
		}
		require.ElementsMatch(t, expected, actual)
	})
}

func TestContainerPut(t *testing.T) {
	for autohashes, name := range map[bool]string{
		false: "standard deploy",
		true:  "deploy with no hashes",
	} {
		t.Run(name, func(t *testing.T) {
			c, cBal, _ := newContainerInvoker(t, autohashes)

			acc := c.NewAccount(t)
			cnt := dummyContainer(acc)

			putArgs := []any{cnt.value, cnt.sig, cnt.pub, cnt.token}
			c.InvokeFail(t, "insufficient balance to create container", "put", putArgs...)

			balanceMint(t, cBal, acc, containerFee*1, []byte{})

			cAcc := c.WithSigners(acc)
			cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "put", putArgs...)

			c.Invoke(t, stackitem.Null{}, "put", putArgs...)
			c.Invoke(t, stackitem.Null{}, "alias", cnt.id[:])

			t.Run("with nice names", func(t *testing.T) {
				ctrNNS := neotest.CompileFile(t, c.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
				nnsHash := ctrNNS.Hash

				balanceMint(t, cBal, acc, containerFee*1, []byte{})

				putArgs := []any{cnt.value, cnt.sig, cnt.pub, cnt.token, "mycnt", ""}
				t.Run("no fee for alias", func(t *testing.T) {
					c.InvokeFail(t, "insufficient balance to create container", "put", putArgs...)
				})

				balanceMint(t, cBal, acc, containerAliasFee*1, []byte{})
				c.Invoke(t, stackitem.Null{}, "put", putArgs...)

				expected := stackitem.NewArray([]stackitem.Item{
					stackitem.NewByteArray([]byte(base58.Encode(cnt.id[:]))),
				})
				cNNS := c.CommitteeInvoker(nnsHash)
				cNNS.Invoke(t, expected, "resolve", "mycnt."+containerDomain, int64(recordtype.TXT))
				c.Invoke(t, stackitem.NewByteArray([]byte("mycnt."+containerDomain)), "alias", cnt.id[:])

				t.Run("name is already taken", func(t *testing.T) {
					c.InvokeFail(t, "name is already taken", "put", putArgs...)
				})

				c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)
				cNNS.Invoke(t, stackitem.NewArray([]stackitem.Item{}), "resolve", "mycnt."+containerDomain, int64(recordtype.TXT))

				t.Run("register in advance", func(t *testing.T) {
					cnt.value[len(cnt.value)-1]++
					cnt.id = sha256.Sum256(cnt.value)

					cNNS.Invoke(t, stackitem.Null{}, "registerTLD",
						"cdn", "whateveriwant@world.com", int64(0), int64(0), int64(100_000), int64(0))

					cNNS.Invoke(t, true, "register",
						"domain.cdn", c.CommitteeHash,
						"whateveriwant@world.com", int64(0), int64(0), int64(100_000), int64(0))

					balanceMint(t, cBal, acc, (containerFee+containerAliasFee)*1, []byte{})

					putArgs := []any{cnt.value, cnt.sig, cnt.pub, cnt.token, "domain", "cdn"}
					c2 := c.WithSigners(c.Committee, acc)
					c2.Invoke(t, stackitem.Null{}, "put", putArgs...)

					expected = stackitem.NewArray([]stackitem.Item{
						stackitem.NewByteArray([]byte(base58.Encode(cnt.id[:])))})
					cNNS.Invoke(t, expected, "resolve", "domain.cdn", int64(recordtype.TXT))
				})
			})
		})
	}
}

func addContainer(t *testing.T, c, cBal *neotest.ContractInvoker) (neotest.Signer, testContainer) {
	acc := c.NewAccount(t)
	cnt := dummyContainer(acc)

	balanceMint(t, cBal, acc, containerFee*1, []byte{})
	c.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token)
	return acc, cnt
}

func TestContainerDelete(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t, false)

	acc, cnt := addContainer(t, c, cBal)
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "delete",
		cnt.id[:], cnt.sig, cnt.token)

	c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)
	})

	c.InvokeFail(t, containerconst.NotFoundError, "get", cnt.id[:])
	// Try to put the same container again (replay attack).
	balanceMint(t, cBal, acc, containerFee*1, []byte{})
	c.InvokeFail(t, containerconst.ErrorDeleted, "put", cnt.value, cnt.sig, cnt.pub, cnt.token)
}

func TestContainerOwner(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t, false)

	acc, cnt := addContainer(t, c, cBal)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.InvokeFail(t, containerconst.NotFoundError, "owner", id[:])
	})

	owner, _ := base58.Decode(address.Uint160ToString(acc.ScriptHash()))
	c.Invoke(t, stackitem.NewBuffer(owner), "owner", cnt.id[:])
}

func TestContainerGet(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t, false)

	_, cnt := addContainer(t, c, cBal)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.InvokeFail(t, containerconst.NotFoundError, "get", id[:])
	})

	expected := stackitem.NewStruct([]stackitem.Item{
		stackitem.NewBuffer(cnt.value),
		stackitem.NewBuffer([]byte{}),
		stackitem.NewBuffer([]byte{}),
		stackitem.NewBuffer([]byte{}),
	})
	c.Invoke(t, expected, "get", cnt.id[:])
}

type eacl struct {
	value []byte
	sig   []byte
	pub   []byte
	token []byte
}

func dummyEACL(containerID [32]byte) eacl {
	e := make([]byte, 50)
	copy(e[6:], containerID[:])
	return eacl{
		value: e,
		sig:   randomBytes(64),
		pub:   randomBytes(33),
		token: randomBytes(42),
	}
}

func TestContainerSetEACL(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t, false)

	acc, cnt := addContainer(t, c, cBal)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		e := dummyEACL(id)
		c.InvokeFail(t, containerconst.NotFoundError, "setEACL", e.value, e.sig, e.pub, e.token)
	})

	e := dummyEACL(cnt.id)
	setArgs := []any{e.value, e.sig, e.pub, e.token}
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "setEACL", setArgs...)

	c.Invoke(t, stackitem.Null{}, "setEACL", setArgs...)

	expected := stackitem.NewStruct([]stackitem.Item{
		stackitem.NewBuffer(e.value),
		stackitem.NewBuffer([]byte{}),
		stackitem.NewBuffer([]byte{}),
		stackitem.NewBuffer([]byte{}),
	})
	c.Invoke(t, expected, "eACL", cnt.id[:])

	replaceEACLArg := func(eACL []byte) []any {
		res := make([]any, len(setArgs))
		copy(res, setArgs)
		res[0] = eACL
		return res
	}

	checkInvalidEACL := func(eACL []byte, exception string) {
		c.InvokeFail(t, exception, "setEACL", replaceEACLArg(eACL)...)
	}

	const missingVersionException = "missing version field in eACL BLOB"
	const missingContainerException = "missing container ID field in eACL BLOB"

	checkInvalidEACL([]byte{}, missingVersionException)
	checkInvalidEACL([]byte{0}, missingVersionException)
	checkInvalidEACL([]byte{0, 0, 0}, missingContainerException)

	const offset = byte(20) // any

	// first byte can be any since protobuf is not completely decoded
	prefix := append([]byte{0, offset}, make([]byte, offset+4)...)

	checkInvalidEACL(prefix, missingContainerException)
	checkInvalidEACL(append(prefix, make([]byte, len(cnt.id)-1)...), missingContainerException)
	c.Invoke(t, stackitem.Null{}, "setEACL", replaceEACLArg(append(prefix, cnt.id[:]...))...)
	c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)
	c.InvokeFail(t, containerconst.NotFoundError, "eACL", cnt.id[:])
}

func TestContainerSizeEstimation(t *testing.T) {
	c, cBal, cNm := newContainerInvoker(t, false)

	_, cnt := addContainer(t, c, cBal)
	nodes := []testNodeInfo{
		newStorageNode(t, c),
		newStorageNode(t, c),
		newStorageNode(t, c),
	}
	// Never clean up the map.
	cNm.Invoke(t, stackitem.Null{}, "setCleanupThreshold", 0)
	for i := range nodes {
		var cAcc = new(neotest.ContractInvoker)
		*cAcc = *cNm
		cAcc.Signers = append(cAcc.Signers, nodes[i].signer)
		// It's not correct to have nodes in different maps, but it's good enough
		// to test both map versions wrt estimations.
		if i%2 == 0 {
			cAcc.Invoke(t, stackitem.Null{}, "addPeer", nodes[i].raw)
		} else {
			nodeStruct := stackitem.NewStruct([]stackitem.Item{
				stackitem.NewArray([]stackitem.Item{stackitem.Make("grpcs://192.0.2.100:8090")}),
				stackitem.NewMapWithValue([]stackitem.MapElement{
					{Key: stackitem.Make("key"), Value: stackitem.Make("value")},
					{Key: stackitem.Make("Capacity"), Value: stackitem.Make("100500")},
				}),
				stackitem.NewByteArray(nodes[i].pub),
				stackitem.Make(nodes[i].state),
			})
			cAcc.Invoke(t, stackitem.Null{}, "addNode", nodeStruct)
		}
	}

	// putContainerSize retrieves storage nodes from the previous snapshot,
	// so epoch must be incremented twice.
	cNm.Invoke(t, stackitem.Null{}, "newEpoch", int64(1))
	cNm.Invoke(t, stackitem.Null{}, "newEpoch", int64(2))

	t.Run("must be witnessed by key in the argument", func(t *testing.T) {
		c.WithSigners(nodes[1].signer).InvokeFail(t, common.ErrWitnessFailed, "putContainerSize",
			int64(2), cnt.id[:], int64(123), nodes[0].pub)
	})

	t.Run("incorrect key fails", func(t *testing.T) {
		_, err := c.TestInvoke(t, "getContainerSize", cnt.id[:]) // Incorrect length.
		require.Error(t, err)
		_, err = c.TestInvoke(t, "getContainerSize", append([]byte("0"), cnt.id[:]...)) // Incorrect prefix.
		require.Error(t, err)
	})
	c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
		int64(2), cnt.id[:], int64(123), nodes[0].pub)
	estimations := []estimation{{nodes[0].pub, 123}}
	checkEstimations(t, c, 2, cnt, estimations...)

	c.WithSigners(nodes[1].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
		int64(2), cnt.id[:], int64(42), nodes[1].pub)
	estimations = append(estimations, estimation{nodes[1].pub, int64(42)})
	checkEstimations(t, c, 2, cnt, estimations...)

	t.Run("add estimation for a different epoch", func(t *testing.T) {
		c.WithSigners(nodes[2].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
			int64(1), cnt.id[:], int64(777), nodes[2].pub)
		checkEstimations(t, c, 1, cnt, estimation{nodes[2].pub, 777})
		checkEstimations(t, c, 2, cnt, estimations...)
	})

	c.WithSigners(nodes[2].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
		int64(3), cnt.id[:], int64(888), nodes[2].pub)
	checkEstimations(t, c, 3, cnt, estimation{nodes[2].pub, 888})

	// Remove old estimations.
	for i := int64(1); i <= containerconst.CleanupDelta; i++ {
		cNm.Invoke(t, stackitem.Null{}, "newEpoch", 2+i)
		checkEstimations(t, c, 2, cnt, estimations...)
		checkEstimations(t, c, 3, cnt, estimation{nodes[2].pub, 888})
	}

	epoch := int64(2 + containerconst.CleanupDelta + 1)
	cNm.Invoke(t, stackitem.Null{}, "newEpoch", epoch)
	checkEstimations(t, c, 2, cnt, estimations...) // not yet removed
	checkEstimations(t, c, 3, cnt, estimation{nodes[2].pub, 888})

	c.WithSigners(nodes[1].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
		epoch, cnt.id[:], int64(999), nodes[1].pub)

	checkEstimations(t, c, 2, cnt, estimations[:1]...)
	checkEstimations(t, c, epoch, cnt, estimation{nodes[1].pub, int64(999)})

	// Estimation from node 0 should be cleaned during epoch tick.
	for i := int64(1); i <= containerconst.TotalCleanupDelta-containerconst.CleanupDelta; i++ {
		cNm.Invoke(t, stackitem.Null{}, "newEpoch", epoch+i)
	}
	checkEstimations(t, c, 2, cnt)
	checkEstimations(t, c, epoch, cnt, estimation{nodes[1].pub, int64(999)})
}

type estimation struct {
	from []byte
	size int64
}

func checkEstimations(t *testing.T, c *neotest.ContractInvoker, epoch int64, cnt testContainer, estimations ...estimation) {
	// Check that listed estimations match expected
	listEstimations := getListEstimations(t, c, epoch, cnt)
	requireEstimationsMatch(t, estimations, listEstimations)

	// Check that iterated estimations match expected
	iterEstimations := getIterEstimations(t, c, epoch, cnt.id[:])
	requireEstimationsMatch(t, estimations, iterEstimations)
}

func getListEstimations(t *testing.T, c *neotest.ContractInvoker, epoch int64, cnt testContainer) []estimation {
	s, err := c.TestInvoke(t, "listContainerSizes", epoch)
	require.NoError(t, err)

	var id []byte

	// When there are no estimations, listContainerSizes can also return nothing.
	item := s.Top().Item()
	switch it := item.(type) {
	case stackitem.Null:
		require.Equal(t, stackitem.Null{}, it)
		return make([]estimation, 0)
	case *stackitem.Array:
		id, err = it.Value().([]stackitem.Item)[0].TryBytes()
		require.NoError(t, err)
	default:
		require.FailNow(t, "invalid return type for listContainerSizes")
	}

	s, err = c.TestInvoke(t, "getContainerSize", id)
	require.NoError(t, err)

	// Here and below we assume that all estimations in the contract are related to our container
	sizes := s.Top().Array()
	require.Equal(t, cnt.id[:], sizes[0].Value())

	return convertStackToEstimations(sizes[1].Value().([]stackitem.Item))
}

func getIterEstimations(t *testing.T, c *neotest.ContractInvoker, epoch int64, cid []byte) []estimation {
	iterStack, err := c.TestInvoke(t, "iterateAllContainerSizes", epoch)
	require.NoError(t, err)
	iter := iterStack.Pop().Value().(*storage.Iterator)

	// Iterator contains pairs: key + estimation (as stack item).
	pairs := iteratorToArray(iter)
	estimationItems := make([]stackitem.Item, len(pairs))
	for i, pair := range pairs {
		pairItems := pair.Value().([]stackitem.Item)
		estimationItems[i] = pairItems[1]

		// Assuming a single container.
		cntId := pairItems[0].Value().([]byte)
		require.Less(t, len(cid), len(cntId))
		require.Equal(t, cid, cntId[:len(cid)])
	}
	cidSpecificIter, err := c.TestInvoke(t, "iterateContainerSizes", epoch, cid)
	require.NoError(t, err)
	iter = cidSpecificIter.Pop().Value().(*storage.Iterator)

	// Iterator contains pairs: key + estimation (as stack item).
	cidSpecificArray := iteratorToArray(iter)
	require.Equal(t, estimationItems, cidSpecificArray)

	return convertStackToEstimations(estimationItems)
}

func convertStackToEstimations(stackItems []stackitem.Item) []estimation {
	estimations := make([]estimation, 0, len(stackItems))
	for _, item := range stackItems {
		value := item.Value().([]stackitem.Item)
		from := value[0].Value().([]byte)
		size := value[1].Value().(*big.Int)

		estimation := estimation{from: from, size: size.Int64()}
		estimations = append(estimations, estimation)
	}
	return estimations
}

func requireEstimationsMatch(t *testing.T, expected []estimation, actual []estimation) {
	require.Equal(t, len(expected), len(actual))
	for _, e := range expected {
		found := false
		for _, a := range actual {
			if found = bytes.Equal(e.from, a.from); found {
				require.Equal(t, e.size, a.size)
				break
			}
		}
		require.True(t, found, "expected estimation from %x to be present", e.from)
	}
}

func TestContainerList(t *testing.T) {
	c, _, _ := newContainerInvoker(t, false)

	t.Run("happy path", func(t *testing.T) {
		cID := make([]byte, sha256.Size)
		_, err := rand.Read(cID)
		require.NoError(t, err)

		const nodesNumber = 1024
		const numberOfVectors = 4
		var containerList []any
		for range nodesNumber {
			s := c.NewAccount(t).(neotest.SingleSigner)
			containerList = append(containerList, s.Account().PublicKey().Bytes())
		}

		var replicas []uint8
		var vectors [][]any
		for i := range numberOfVectors {
			vector := containerList[nodesNumber/numberOfVectors*i : nodesNumber/numberOfVectors*(i+1)]
			vectors = append(vectors, vector)
			replicas = append(replicas, uint8(i+1))

			c.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cID, i, vector)
		}
		c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID, replicas)

		stack, err := c.TestInvoke(t, "replicasNumbers", cID)
		require.NoError(t, err)

		replicasFromContract := stackIteratorToUint8Array(t, stack)
		require.Equal(t, replicas, replicasFromContract)

		for i := range numberOfVectors {
			stack, err := c.TestInvoke(t, "nodes", cID, uint8(i))
			require.NoError(t, err)

			fromContract := stackIteratorToBytesArray(t, stack)
			require.ElementsMatch(t, vectors[i], fromContract)
		}

		// change container
		for i := nodesNumber / 2; i < nodesNumber; i++ {
			s := c.NewAccount(t).(neotest.SingleSigner)
			containerList[i] = s.Account().PublicKey().Bytes()
		}

		c.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cID, 0, containerList)
		c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID, []uint8{1})
		stack, err = c.TestInvoke(t, "nodes", cID, 0)
		require.NoError(t, err)

		fromContract := stackIteratorToBytesArray(t, stack)
		require.ElementsMatch(t, containerList, fromContract)

		// clean container up
		c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID, nil)

		for i := range numberOfVectors {
			stack, err = c.TestInvoke(t, "nodes", cID, i)
			require.NoError(t, err)

			fromContract = stackIteratorToBytesArray(t, stack)
			require.Empty(t, fromContract)
		}

		// cleaning cleaned container
		c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID, nil)

		for i := range numberOfVectors {
			stack, err = c.TestInvoke(t, "nodes", cID, i)
			require.NoError(t, err)

			fromContract = stackIteratorToBytesArray(t, stack)
			require.Empty(t, fromContract)
		}

		stack, err = c.TestInvoke(t, "replicasNumbers", cID)
		require.NoError(t, err)

		replicasFromContract = stackIteratorToUint8Array(t, stack)
		require.Empty(t, replicasFromContract)
	})

	t.Run("unknown placement vector", func(t *testing.T) {
		const requestedIndex = 1
		c.InvokeFail(t, fmt.Sprintf("invalid placement vector: %d index not found but %d requested", requestedIndex-1, requestedIndex),
			"addNextEpochNodes", make([]byte, 32), requestedIndex, nil)
	})

	t.Run("invalid container ID", func(t *testing.T) {
		badCID := []byte{1, 2, 3, 4, 5}

		c.InvokeFail(t, fmt.Sprintf("%s: length: %d", containerconst.ErrorInvalidContainerID, len(badCID)), "addNextEpochNodes", badCID, 0, nil)
		c.InvokeFail(t, fmt.Sprintf("%s: length: %d", containerconst.ErrorInvalidContainerID, len(badCID)), "commitContainerListUpdate", badCID, 0)
		c.InvokeFail(t, fmt.Sprintf("%s: length: %d", containerconst.ErrorInvalidContainerID, len(badCID)), "nodes", badCID, 0)
	})

	t.Run("invalid public key", func(t *testing.T) {
		cID := make([]byte, sha256.Size)
		_, err := rand.Read(cID)
		require.NoError(t, err)

		badPublicKey := []byte{1, 2, 3, 4, 5}

		c.InvokeFail(t, fmt.Sprintf("%s: length: %d", containerconst.ErrorInvalidPublicKey, len(badPublicKey)), "addNextEpochNodes", cID, 0, []any{badPublicKey})
	})

	t.Run("not alphabet", func(t *testing.T) {
		cID := make([]byte, sha256.Size)
		_, err := rand.Read(cID)
		require.NoError(t, err)

		notAlphabet := c.Executor.NewInvoker(c.Hash, c.NewAccount(t))
		notAlphabet.InvokeFail(t, common.ErrAlphabetWitnessFailed, "addNextEpochNodes", cID, 0, nil)
		notAlphabet.InvokeFail(t, common.ErrAlphabetWitnessFailed, "commitContainerListUpdate", cID, 0)
	})
}

func stackIteratorToBytesArray(t *testing.T, stack *vm.Stack) [][]byte {
	i, ok := stack.Pop().Value().(*storage.Iterator)
	require.True(t, ok)

	res := make([][]byte, 0)
	for i.Next() {
		b, err := i.Value().TryBytes()
		require.NoError(t, err)

		res = append(res, b)
	}

	return res
}

func stackIteratorToUint8Array(t *testing.T, stack *vm.Stack) []uint8 {
	i, ok := stack.Pop().Value().(*storage.Iterator)
	require.True(t, ok)

	res := make([]uint8, 0)
	for i.Next() {
		b, err := i.Value().TryInteger()
		require.NoError(t, err)

		res = append(res, uint8(b.Uint64()))
	}

	return res
}

func TestVerifyPlacementSignature(t *testing.T) {
	testData := make([]byte, 1024)
	_, err := rand.Read(testData)
	require.NoError(t, err)
	cID := make([]byte, sha256.Size)
	_, err = rand.Read(cID)
	require.NoError(t, err)

	c, _, _ := newContainerInvoker(t, false)

	const nodesNumber = 1024
	const numberOfVectors = 4
	vectors := make([][]any, numberOfVectors)
	vectorsSigners := make([][]*keys.PrivateKey, numberOfVectors)
	for i := range nodesNumber {
		vecNum := i / (nodesNumber / numberOfVectors)
		s := c.NewAccount(t).(neotest.SingleSigner)

		vectors[vecNum] = append(vectors[vecNum], s.Account().PublicKey().Bytes())
		vectorsSigners[vecNum] = append(vectorsSigners[vecNum], s.Account().PrivateKey())
	}
	var replicas []uint8
	for i := range numberOfVectors {
		replicas = append(replicas, uint8(i+1))
		c.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cID, i, vectors[i])
	}
	c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID, replicas)

	sigs := make([]any, numberOfVectors)
	for i, rep := range replicas {
		// neo-go client's "generic" magic for args
		var vectorSigs []any
		for j := range rep {
			vectorSigs = append(vectorSigs, vectorsSigners[i][j].Sign(testData))
		}

		sigs[i] = vectorSigs
	}

	t.Run("happy path", func(t *testing.T) {
		stack, err := c.TestInvoke(t, "verifyPlacementSignatures", cID, testData, sigs)
		require.NoError(t, err)
		stackWithBool(t, stack, true)

		// "right" signatures not in the first place
		slices.Reverse(sigs[numberOfVectors-1].([]any))

		stack, err = c.TestInvoke(t, "verifyPlacementSignatures", cID, testData, sigs)
		require.NoError(t, err)
		stackWithBool(t, stack, true)
	})

	t.Run("not enough vectors", func(t *testing.T) {
		stack, err := c.TestInvoke(t, "verifyPlacementSignatures", cID, testData, sigs[:len(vectors)-1])
		require.NoError(t, err)
		stackWithBool(t, stack, false)
	})

	t.Run("incorrect signature", func(t *testing.T) {
		k, err := keys.NewPrivateKey()
		require.NoError(t, err)

		const problemVector = 1
		sigs[problemVector].([]any)[replicas[problemVector]-1] = k.Sign(testData)

		stack, err := c.TestInvoke(t, "verifyPlacementSignatures", cID, testData, sigs)
		require.NoError(t, err)
		stackWithBool(t, stack, false)
	})

	t.Run("not enough signatures", func(t *testing.T) {
		const problemVector = 1
		sigs[problemVector] = sigs[problemVector].([]any)[:replicas[problemVector]-1]

		stack, err := c.TestInvoke(t, "verifyPlacementSignatures", cID, testData, sigs)
		require.NoError(t, err)
		stackWithBool(t, stack, false)
	})
}

func stackWithBool(t *testing.T, stack *vm.Stack, v bool) {
	require.Equal(t, 1, stack.Len())

	res, ok := stack.Pop().Value().(bool)
	require.True(t, ok)
	require.Equal(t, v, res)
}

func TestPutMeta(t *testing.T) {
	cSingleWithProxy, cCommitee, cBal := newProxySponsorInvoker(t)
	var sigs []any
	for range 5 {
		sigs = append(sigs, make([]byte, 64))
	}

	t.Run("meta disabled", func(t *testing.T) {
		acc := cCommitee.NewAccount(t)
		cnt := dummyContainer(acc)
		balanceMint(t, cBal, acc, containerFee*1, []byte{})

		metaInfo, err := stackitem.Serialize(stackitem.NewMapWithValue(
			[]stackitem.MapElement{{Key: stackitem.Make("cid"), Value: stackitem.Make(cnt.id[:])}}))
		require.NoError(t, err)

		cCommitee.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token, "", "", false)
		cSingleWithProxy.InvokeFail(t, "container does not support meta-on-chain", "submitObjectPut", metaInfo, sigs)

		expected := stackitem.NewStruct([]stackitem.Item{
			stackitem.NewBuffer(cnt.value),
			stackitem.NewBuffer([]byte{}),
			stackitem.NewBuffer([]byte{}),
			stackitem.NewBuffer([]byte{}),
		})
		cCommitee.Invoke(t, expected, "get", cnt.id[:])
	})

	t.Run("meta enabled", func(t *testing.T) {
		acc := cCommitee.NewAccount(t)
		cnt := dummyContainer(acc)
		oid := randomBytes(sha256.Size)
		balanceMint(t, cBal, acc, containerFee*1, []byte{})
		cCommitee.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token, "", "", true)

		t.Run("correct meta data", func(t *testing.T) {
			meta := testMeta(cnt.id[:], oid)
			rawMeta, err := stackitem.Serialize(meta)
			require.NoError(t, err)

			hash := cSingleWithProxy.Invoke(t, stackitem.Null{}, "submitObjectPut", rawMeta, sigs)
			res := cSingleWithProxy.GetTxExecResult(t, hash)
			require.Len(t, res.Events, 1)
			require.Equal(t, "ObjectPut", res.Events[0].Name)
			notificationArgs := res.Events[0].Item.Value().([]stackitem.Item)
			require.Len(t, notificationArgs, 3)
			require.Equal(t, cnt.id[:], notificationArgs[0].Value().([]byte))
			require.Equal(t, oid, notificationArgs[1].Value().([]byte))

			metaValuesExp := meta.Value().([]stackitem.MapElement)
			metaValuesGot := notificationArgs[2].Value().([]stackitem.MapElement)
			require.Equal(t, metaValuesExp, metaValuesGot)
		})

		t.Run("additional testing values", func(t *testing.T) {
			// meta-on-chain feature is in progress, it may or may not include additional
			// values passed through the contract, therefore, it should be allowed to
			// accept unknown map KV pairs

			meta := testMeta(cnt.id[:], oid)
			meta.Add(stackitem.Make("test"), stackitem.Make("test"))
			rawMeta, err := stackitem.Serialize(meta)
			require.NoError(t, err)

			hash := cSingleWithProxy.Invoke(t, stackitem.Null{}, "submitObjectPut", rawMeta, sigs)
			res := cSingleWithProxy.GetTxExecResult(t, hash)
			require.Len(t, res.Events, 1)
			require.Equal(t, "ObjectPut", res.Events[0].Name)
			notificationArgs := res.Events[0].Item.Value().([]stackitem.Item)
			require.Len(t, notificationArgs, 3)
			require.Equal(t, cnt.id[:], notificationArgs[0].Value().([]byte))
			require.Equal(t, oid, notificationArgs[1].Value().([]byte))

			metaValuesExp := meta.Value().([]stackitem.MapElement)
			metaValuesGot := notificationArgs[2].Value().([]stackitem.MapElement)
			require.Equal(t, metaValuesExp, metaValuesGot)
		})

		t.Run("not signed by Proxy", func(t *testing.T) {
			meta := testMeta(cnt.id[:], oid)
			rawMeta, err := stackitem.Serialize(meta)
			require.NoError(t, err)

			cCommitee.InvokeFail(t, "not signed by Proxy contract", "submitObjectPut", rawMeta, sigs)
		})

		t.Run("missing required values", func(t *testing.T) {
			testFunc := func(key string) {
				meta := testMeta(cnt.id[:], oid)
				meta.Drop(meta.Index(stackitem.Make(key)))
				raw, err := stackitem.Serialize(meta)
				require.NoError(t, err)
				cSingleWithProxy.InvokeFail(t, fmt.Sprintf("'%s' not found", key), "submitObjectPut", raw, sigs)
			}

			testFunc("oid")
			testFunc("size")
			testFunc("validUntil")
			testFunc("network")
		})

		t.Run("incorrect values", func(t *testing.T) {
			testFunc := func(key string, newVal any) {
				meta := testMeta(cnt.id[:], oid)
				meta.Add(stackitem.Make(key), stackitem.Make(newVal))
				raw, err := stackitem.Serialize(meta)
				require.NoError(t, err)
				cSingleWithProxy.InvokeFail(t, "incorrect", "submitObjectPut", raw, sigs)
			}

			testFunc("oid", []byte{1})
			testFunc("validUntil", 1) // tested chain will have some blocks for sure
			testFunc("network", netmode.UnitTestNet+1)
			testFunc("type", math.MaxInt64)
			testFunc("firstPart", []byte{1})
			testFunc("previousPart", []byte{1})
			testFunc("deleted", []any{[]byte{1}})
			testFunc("locked", []any{[]byte{1}})
		})
	})
}

func testMeta(cid, oid []byte) *stackitem.Map {
	return stackitem.NewMapWithValue(
		[]stackitem.MapElement{
			{Key: stackitem.Make("network"), Value: stackitem.Make(netmode.UnitTestNet)},
			{Key: stackitem.Make("cid"), Value: stackitem.Make(cid)},
			{Key: stackitem.Make("oid"), Value: stackitem.Make(oid)},
			{Key: stackitem.Make("firstPart"), Value: stackitem.Make(oid)},
			{Key: stackitem.Make("size"), Value: stackitem.Make(123)},
			{Key: stackitem.Make("deleted"), Value: stackitem.Make([]any{randomBytes(sha256.Size)})},
			{Key: stackitem.Make("locked"), Value: stackitem.Make([]any{randomBytes(sha256.Size)})},
			{Key: stackitem.Make("validUntil"), Value: stackitem.Make(math.MaxInt)},
		})
}

func TestContainerAlias(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t, false)

	t.Run("missing container", func(t *testing.T) {
		c.InvokeFail(t, containerconst.NotFoundError, "alias", make([]byte, 32))
	})

	t.Run("no alias", func(t *testing.T) {
		acc, cnt := addContainer(t, c, cBal)
		cAcc := c.WithSigners(acc)

		cAcc.Invoke(t, stackitem.Null{}, "alias", cnt.id[:])
	})

	t.Run("good", func(t *testing.T) {
		acc, cnt := addContainer(t, c, cBal)
		balanceMint(t, cBal, acc, containerFee+containerAliasFee, []byte{})
		c.Invoke(t, stackitem.Null{}, "putNamed", cnt.value, cnt.sig, cnt.pub, cnt.token, "mycnt", "")

		c.Invoke(t, stackitem.NewByteArray([]byte("mycnt."+containerDomain)), "alias", cnt.id[:])
	})
}

func TestContainerCreate(t *testing.T) {
	anyValidCnr := randomBytes(100)
	anyValidCnr[1] = 0 // owner offset fix
	ch := sha256.Sum256(anyValidCnr)
	anyValidCnrID := ch[:]
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)
	const anyValidDomainName = ""
	const anyValidDomainZone = ""
	const anyValidMetaOnChain = false
	t.Run("invalid container", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)
		t.Run("wrong owner offset", func(t *testing.T) {
			cnr := randomBytes(10)
			cnr[1] = 100
			exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "invalid offset", "create",
				cnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, anyValidMetaOnChain)
		})
	})
	t.Run("no alphabet witness", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)
		otherAcc := exec.NewAccount(t)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		exec.NewInvoker(containerContract.Hash, otherAcc).InvokeFail(t, "alphabet witness check failed", "create",
			anyValidCnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, anyValidMetaOnChain)
	})
	t.Run("already deleted", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
			anyValidCnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, anyValidMetaOnChain)

		// TODO: do not mix test with the other ops. Instead, preset contract storage.
		//  It will be easier with https://github.com/nspcc-dev/neo-go/issues/2926.
		exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "remove",
			anyValidCnrID, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

		exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "container was previously deleted", "create",
			anyValidCnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, anyValidMetaOnChain)
	})
	t.Run("domain register failure", func(t *testing.T) {
		const fee = 1000
		const aliasFee = 500
		blockChain, committee := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		require.Implements(t, (*neotest.MultiSigner)(nil), committee)
		exec := neotest.NewExecutor(t, blockChain, committee, committee)

		nnsContract := deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", fee, "ContainerAliasFee", aliasFee)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		balanceContract := deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		owner := exec.NewAccount(t, 0)
		ownerAcc := owner.ScriptHash()
		ownerAddr, _ := base58.Decode(address.Uint160ToString(ownerAcc))
		cnr := slices.Clone(anyValidCnr)
		copy(cnr[6:], ownerAddr)
		setContainerOwner(cnr, owner)
		id := sha256.Sum256(cnr)

		balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee+aliasFee, []byte{})

		txHash := exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
			cnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "my-domain", "", true)

		// storage
		getStorageItem := func(key []byte) []byte {
			return getContractStorageItem(t, exec, containerContract.Hash, key)
		}
		require.EqualValues(t, id[:], getStorageItem(slices.Concat([]byte{'o'}, ownerAddr, id[:])))
		require.EqualValues(t, cnr, getStorageItem(slices.Concat([]byte{'x'}, id[:])))
		require.EqualValues(t, []byte{}, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
		require.EqualValues(t, []byte("my-domain.container"), getStorageItem(slices.Concat([]byte("nnsHasAlias"), id[:])))

		// NNS
		exec.CommitteeInvoker(nnsContract).Invoke(t, stackitem.NewArray([]stackitem.Item{
			stackitem.NewByteArray([]byte(base58.Encode(id[:]))),
		}), "getRecords", "my-domain.container", int64(recordtype.TXT))

		// notifications
		res := exec.GetTxExecResult(t, txHash)
		events := res.Events
		require.Len(t, events, 4)
		committeeAcc := committee.(neotest.MultiSigner).Single(0).Account().ScriptHash() // type asserted above
		assertNotificationEvent(t, events[0], "Transfer", nil, containerContract.Hash[:], big.NewInt(1), []byte("my-domain.container"))
		assertNotificationEvent(t, events[1], "Transfer", ownerAcc[:], committeeAcc[:], big.NewInt(fee+aliasFee))
		assertNotificationEvent(t, events[2], "TransferX", ownerAcc[:], committeeAcc[:], big.NewInt(fee+aliasFee), slices.Concat([]byte{0x10}, id[:]))
		assertNotificationEvent(t, events[3], "Created", id[:], ownerAddr)
	})
	t.Run("not enough funds", func(t *testing.T) {
		const fee = 1000
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", fee)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		balanceContract := deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		owner := exec.NewAccount(t, 0)
		cnr := slices.Clone(anyValidCnr)
		setContainerOwner(cnr, owner)

		exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "insufficient balance to create container", "create",
			cnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, anyValidMetaOnChain)

		balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee-1, []byte{})

		exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "insufficient balance to create container", "create",
			cnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, anyValidMetaOnChain)
	})
	t.Run("payment failure", func(t *testing.T) {
		t.Skip("TODO")
	})

	const fee = 1000
	blockChain, committee := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
	require.Implements(t, (*neotest.MultiSigner)(nil), committee)
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", fee)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	balanceContract := deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)

	owner := exec.NewAccount(t, 0)
	ownerAcc := owner.ScriptHash()
	ownerAddr, _ := base58.Decode(address.Uint160ToString(ownerAcc))
	cnr := slices.Clone(anyValidCnr)
	copy(cnr[6:], ownerAddr)
	setContainerOwner(cnr, owner)
	id := sha256.Sum256(cnr)

	balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee, []byte{})

	txHash := exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
		cnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, true)
	// storage
	getStorageItem := func(key []byte) []byte {
		return getContractStorageItem(t, exec, containerContract.Hash, key)
	}
	require.EqualValues(t, id[:], getStorageItem(slices.Concat([]byte{'o'}, ownerAddr, id[:])))
	require.EqualValues(t, cnr, getStorageItem(slices.Concat([]byte{'x'}, id[:])))
	require.EqualValues(t, []byte{}, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
	// notifications
	res := exec.GetTxExecResult(t, txHash)
	events := res.Events
	require.Len(t, events, 3)
	committeeAcc := committee.(neotest.MultiSigner).Single(0).Account().ScriptHash() // type asserted above
	assertNotificationEvent(t, events[0], "Transfer", ownerAcc[:], committeeAcc[:], big.NewInt(fee))
	assertNotificationEvent(t, events[1], "TransferX", ownerAcc[:], committeeAcc[:], big.NewInt(fee), slices.Concat([]byte{0x10}, id[:]))
	assertNotificationEvent(t, events[2], "Created", id[:], ownerAddr)
}

func TestContainerRemove(t *testing.T) {
	anyValidCnrID := randomBytes(32)
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)
	t.Run("no alphabet witness", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)
		otherAcc := exec.NewAccount(t)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		exec.NewInvoker(containerContract.Hash, otherAcc).InvokeFail(t, "alphabet witness check failed", "remove",
			anyValidCnrID, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})
	t.Run("invalid ID", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		for _, tc := range []struct {
			name, err string
			ln        int
		}{
			{name: "empty", err: "invalid container ID length", ln: 0},
			{name: "undersize", err: "invalid container ID length", ln: 31},
			{name: "oversize", err: "invalid container ID length", ln: 33},
		} {
			t.Run(tc.name, func(t *testing.T) {
				exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "", "remove",
					randomBytes(tc.ln), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
			})
		}
	})
	t.Run("broken container in storage", func(t *testing.T) {
		t.Skip("TODO https://github.com/nspcc-dev/neo-go/issues/2926")
	})

	const createFee = 1000
	blockChain, committee := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", createFee)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	balanceContract := deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)

	// TODO: store items manually instead instead of creation call.
	//  https://github.com/nspcc-dev/neo-go/issues/2926 would help
	owner := exec.NewAccount(t, 0)
	ownerAcc := owner.ScriptHash()
	ownerAddr, _ := base58.Decode(address.Uint160ToString(ownerAcc))
	cnr := randomBytes(100)
	cnr[1] = 0 // owner offset fix
	copy(cnr[6:], ownerAddr)
	setContainerOwner(cnr, owner)
	id := sha256.Sum256(cnr)

	balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, createFee, []byte{})

	exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
		cnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "", "", true)
	txHash := exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "remove",
		id[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	// storage
	getStorageItem := func(key []byte) []byte {
		return getContractStorageItem(t, exec, containerContract.Hash, key)
	}
	require.Nil(t, getStorageItem(slices.Concat([]byte{'o'}, ownerAddr, id[:])))
	require.Nil(t, getStorageItem(slices.Concat([]byte{'x'}, id[:])))
	require.Nil(t, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
	require.Nil(t, getStorageItem(slices.Concat([]byte("eACL"), id[:])))
	for k, v := range contractStorageItems(t, exec, containerContract.Hash, slices.Concat([]byte{'n'}, id[:])) {
		t.Fatalf("SN item not removed: key %x, val %x", k, v)
	}
	for k, v := range contractStorageItems(t, exec, containerContract.Hash, slices.Concat([]byte{'u'}, id[:])) {
		t.Fatalf("next epoch SN item not removed: key %x, val %x", k, v)
	}
	for k, v := range contractStorageItems(t, exec, containerContract.Hash, slices.Concat([]byte{'r'}, id[:])) {
		t.Fatalf("replica num item not removed: key %x, val %x", k, v)
	}
	require.Equal(t, []byte{}, getStorageItem(slices.Concat([]byte{'d'}, id[:])))
	// notifications
	res := exec.GetTxExecResult(t, txHash)
	events := res.Events
	require.Len(t, events, 1)
	assertNotificationEvent(t, events[0], "Removed", id[:], ownerAddr)
}

func TestContainerPutEACL(t *testing.T) {
	anyValidCnr := randomBytes(100)
	anyValidCnr[1] = 0 // owner offset fix
	ch := sha256.Sum256(anyValidCnr)
	anyValidCnrID := ch[:]
	anyValidEACL := randomBytes(100)
	anyValidEACL[1] = 0 // CID offset fix
	copy(anyValidEACL[6:], anyValidCnrID)
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)
	t.Run("invalid eACL", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
			anyValidCnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "", "", false)
		t.Run("no fields", func(t *testing.T) {
			for ln := range 2 {
				exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "missing version field in eACL BLOB", "putEACL",
					make([]byte, ln), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
			}
		})
		t.Run("cut container ID", func(t *testing.T) {
			for idLn := range 32 {
				exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "missing container ID field in eACL BLOB", "putEACL",
					anyValidEACL[:6+idLn], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
			}
		})
	})
	t.Run("no alphabet witness", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)
		otherAcc := exec.NewAccount(t)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
			anyValidCnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "", "", false)

		exec.NewInvoker(containerContract.Hash, otherAcc).InvokeFail(t, "alphabet witness check failed", "putEACL",
			anyValidEACL, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})
	t.Run("missing container", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
		exec := neotest.NewExecutor(t, blockChain, signer, signer)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "container does not exist", "putEACL",
			anyValidEACL, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	const fee = 1000
	blockChain, committee := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
	require.Implements(t, (*neotest.MultiSigner)(nil), committee)
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", fee)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	balanceContract := deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)

	owner := exec.NewAccount(t, 0)
	ownerAcc := owner.ScriptHash()
	ownerAddr, _ := base58.Decode(address.Uint160ToString(ownerAcc))
	cnr := slices.Clone(anyValidCnr)
	copy(cnr[6:], ownerAddr)
	setContainerOwner(cnr, owner)
	id := sha256.Sum256(cnr)
	copy(anyValidEACL[6:], id[:])

	balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee, []byte{})

	// TODO: store items manually instead instead of creation call.
	//  https://github.com/nspcc-dev/neo-go/issues/2926 would help
	exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
		cnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "", "", false)
	txHash := exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "putEACL",
		anyValidEACL, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	// storage
	getStorageItem := func(key []byte) []byte {
		return getContractStorageItem(t, exec, containerContract.Hash, key)
	}
	require.EqualValues(t, anyValidEACL, getStorageItem(slices.Concat([]byte("eACL"), id[:])))
	// notifications
	res := exec.GetTxExecResult(t, txHash)
	events := res.Events
	require.Len(t, events, 1)
	assertNotificationEvent(t, events[0], "EACLChanged", id[:])
}

func TestGetContainerData(t *testing.T) {
	anyValidCnr := randomBytes(100)
	anyValidCnr[1] = 0 // owner offset fix
	ch := sha256.Sum256(anyValidCnr)
	anyValidCnrID := ch[:]
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	blockChain, committee := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)
	inv := exec.CommitteeInvoker(containerContract.Hash)

	t.Run("missing", func(t *testing.T) {
		inv.InvokeFail(t, "container does not exist", "getContainerData", anyValidCnrID)
	})

	inv.Invoke(t, stackitem.Null{}, "create",
		anyValidCnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "", "", false)

	inv.Invoke(t, stackitem.NewBuffer(anyValidCnr), "getContainerData", anyValidCnrID)
}

func TestGetEACLData(t *testing.T) {
	anyValidCnr := randomBytes(100)
	anyValidCnr[1] = 0 // owner offset fix
	ch := sha256.Sum256(anyValidCnr)
	anyValidCnrID := ch[:]
	anyValidEACL := randomBytes(100)
	anyValidEACL[1] = 0 // CID offset fix
	copy(anyValidEACL[6:], anyValidCnrID)
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	blockChain, committee := chain.NewSingleWithOptions(t, &chain.Options{Logger: zap.NewNop()})
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)
	inv := exec.CommitteeInvoker(containerContract.Hash)

	t.Run("missing container", func(t *testing.T) {
		inv.InvokeFail(t, "container does not exist", "getEACLData", anyValidCnrID)
	})

	inv.Invoke(t, stackitem.Null{}, "create",
		anyValidCnr, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "", "", false)

	t.Run("missing", func(t *testing.T) {
		inv.Invoke(t, stackitem.Null{}, "getEACLData", anyValidCnrID)
	})

	exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "putEACL",
		anyValidEACL, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	inv.Invoke(t, stackitem.NewBuffer(anyValidEACL), "getEACLData", anyValidCnrID)
}
