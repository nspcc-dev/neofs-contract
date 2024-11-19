package tests

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"path"
	"slices"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
	"github.com/nspcc-dev/neofs-contract/contracts/nns/recordtype"
	"github.com/stretchr/testify/require"
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
	regContractNNS(t, e, "container", c.Hash)
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
	if !autohashes {
		deployContainerContract(t, e, &ctrNetmap.Hash, &ctrBalance.Hash, &nnsHash)
	} else {
		_ = deployNeoFSIDContract(t, e) // Needed in this case to autoresolve.
		deployContainerContractInternal(t, e, nil)
	}
	return e.CommitteeInvoker(ctrContainer.Hash), e.CommitteeInvoker(ctrBalance.Hash), e.CommitteeInvoker(ctrNetmap.Hash)
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
	t.Run("check with `list`", func(t *testing.T) {
		s, err := c.TestInvoke(t, "list", nil)
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())

		if len(expected) == 0 {
			_, ok := s.Top().Item().(stackitem.Null)
			require.True(t, ok)
			return
		}

		arr, ok := s.Top().Value().([]stackitem.Item)
		require.True(t, ok)
		require.Equal(t, len(expected), len(arr))

		actual := make([][]byte, 0, len(expected))
		for i := range arr {
			id, ok := arr[i].Value().([]byte)
			require.True(t, ok)
			actual = append(actual, id)
		}
		require.ElementsMatch(t, expected, actual)
	})
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
					c.InvokeFail(t, "insufficient balance to create container", "putNamed", putArgs...)
				})

				balanceMint(t, cBal, acc, containerAliasFee*1, []byte{})
				c.Invoke(t, stackitem.Null{}, "putNamed", putArgs...)

				expected := stackitem.NewArray([]stackitem.Item{
					stackitem.NewByteArray([]byte(base58.Encode(cnt.id[:]))),
				})
				cNNS := c.CommitteeInvoker(nnsHash)
				cNNS.Invoke(t, expected, "resolve", "mycnt."+containerDomain, int64(recordtype.TXT))
				c.Invoke(t, stackitem.NewByteArray([]byte("mycnt."+containerDomain)), "alias", cnt.id[:])

				t.Run("name is already taken", func(t *testing.T) {
					c.InvokeFail(t, "name is already taken", "putNamed", putArgs...)
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
					c2.Invoke(t, stackitem.Null{}, "putNamed", putArgs...)

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
		stackitem.NewByteArray(cnt.value),
		stackitem.NewByteArray(cnt.sig),
		stackitem.NewByteArray(cnt.pub),
		stackitem.NewByteArray(cnt.token),
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
		stackitem.NewByteArray(e.value),
		stackitem.NewByteArray(e.sig),
		stackitem.NewByteArray(e.pub),
		stackitem.NewByteArray(e.token),
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
	for i := range nodes {
		var cAcc = new(neotest.ContractInvoker)
		*cAcc = *cNm
		cAcc.Signers = append(cAcc.Signers, nodes[i].signer)
		cAcc.Invoke(t, stackitem.Null{}, "addPeer", nodes[i].raw)
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
