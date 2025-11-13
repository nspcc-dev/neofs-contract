package tests

import (
	"bytes"
	"crypto/elliptic"
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
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
	"github.com/nspcc-dev/neofs-contract/contracts/nns/recordtype"
	containerrpc "github.com/nspcc-dev/neofs-contract/rpc/container"
	cid "github.com/nspcc-dev/neofs-sdk-go/container/id"
	containertest "github.com/nspcc-dev/neofs-sdk-go/container/test"
	"github.com/nspcc-dev/neofs-sdk-go/user"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const containerPath = "../contracts/container"
const containerDomain = "container"

const (
	containerFee      = 0_0100_0000
	containerAliasFee = 0_0050_0000
	epochDuration     = 100 // in seconds
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
		containerconst.AliasFeeKey, int64(containerAliasFee), containerconst.EpochDurationKey, int64(epochDuration))
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

func TestContainerSizeReports(t *testing.T) {
	c, cBal, cNm := newContainerInvoker(t, false)
	nodes := []testNodeInfo{
		newStorageNode(t, c),
		newStorageNode(t, c),
		newStorageNode(t, c),
	}
	slices.SortFunc(nodes, func(a, b testNodeInfo) int {
		keyA, err := keys.NewPublicKeyFromBytes(a.pub, elliptic.P256())
		require.NoError(t, err)
		keyB, err := keys.NewPublicKeyFromBytes(b.pub, elliptic.P256())
		require.NoError(t, err)

		return bytes.Compare(keyA.GetScriptHash().BytesBE(), keyB.GetScriptHash().BytesBE())
	})

	nodesKeys := make([]any, 0, len(nodes))
	for _, node := range nodes {
		nodesKeys = append(nodesKeys, node.pub)
	}

	cnt := addContainerWithNodes(t, c, cBal, nodesKeys)

	t.Run("must be witnessed by key in the argument", func(t *testing.T) {
		c.WithSigners(nodes[1].signer).InvokeFail(t, common.ErrWitnessFailed, "putReport",
			cnt.id[:], int64(123), int64(456), nodes[0].pub)
	})

	t.Run("not a netmap node", func(t *testing.T) {
		notNode := c.NewAccount(t)

		c.WithSigners(notNode).InvokeFail(t, "must be invoked by storage node", "putReport",
			cnt.id[:], int64(123), int64(456), notNode.(neotest.SingleSigner).Account().PrivateKey().PublicKey().Bytes())
	})

	t.Run("max number of reports", func(t *testing.T) {
		const allowedReportsNumber = 3

		for range allowedReportsNumber {
			c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putReport",
				cnt.id[:], int64(123), int64(456), nodes[0].pub,
			)
		}

		c.WithSigners(nodes[0].signer).InvokeFail(t, "max number of reports", "putReport",
			cnt.id[:], int64(123), int64(456), nodes[0].pub,
		)

		cNm.Invoke(t, stackitem.Null{}, "newEpoch", int64(1))
		c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putReport",
			cnt.id[:], int64(123), int64(456), nodes[0].pub,
		)
	})

	t.Run("container info", func(t *testing.T) {
		type record struct{ size, objs int64 }
		type tc struct {
			name    string
			reports []record
			result  record
		}

		cases := []tc{
			{
				name:    "empty records",
				reports: nil,
				result:  record{size: 0, objs: 0},
			},
			{
				name:    "single report",
				reports: []record{{size: 1234, objs: 5678}},
				result:  record{size: 1234, objs: 5678},
			},
			{
				name:    "zero values",
				reports: []record{{size: 0, objs: 0}},
				result:  record{size: 0, objs: 0},
			},
			{
				name:    "multiple records",
				reports: []record{{size: 10, objs: 100}, {size: 20, objs: 200}, {size: 30, objs: 300}},
				result:  record{size: 60, objs: 600},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				anotherCnr := addContainerWithNodes(t, c, cBal, nodesKeys)

				for i, r := range tc.reports {
					c.WithSigners(nodes[i].signer).Invoke(t, stackitem.Null{}, "putReport",
						anotherCnr.id[:], r.size, r.objs, nodes[i].pub,
					)
				}

				res, err := c.TestInvoke(t, "getNodeReportSummary", anotherCnr.id[:])
				require.NoError(t, err, "receiving container info")

				var summ containerrpc.ContainerNodeReportSummary
				err = summ.FromStackItem(res.Top().Item())
				require.NoError(t, err)

				require.Equal(t, tc.result.size, summ.ContainerSize.Int64(), "sizes are not equal")
				require.Equal(t, tc.result.objs, summ.NumberOfObjects.Int64(), "object numbers are not equal")
			})
		}
	})

	t.Run("get estimations by node", func(t *testing.T) {
		anotherCnr := addContainerWithNodes(t, c, cBal, nodesKeys)

		const (
			nodeSize1 = 1234
			nodeObjs1 = 5678
			nodeSize2 = 4321
			nodeObjs2 = 8765
		)

		// node 1
		c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putReport",
			anotherCnr.id[:], nodeSize1, nodeObjs1, nodes[0].pub,
		)

		// node 2
		c.WithSigners(nodes[1].signer).Invoke(t, stackitem.Null{}, "putReport",
			anotherCnr.id[:], nodeSize2, nodeObjs2, nodes[1].pub,
		)

		// summaries for the whole container
		res, err := c.TestInvoke(t, "getNodeReportSummary", anotherCnr.id[:])
		require.NoError(t, err, "receiving estimations")
		var summ containerrpc.ContainerNodeReportSummary
		err = summ.FromStackItem(res.Pop().Item())
		require.NoError(t, err)
		require.Equal(t, int64(nodeSize1+nodeSize2), summ.ContainerSize.Int64(), "average sizes are not equal")
		require.Equal(t, int64(nodeObjs1+nodeObjs2), summ.NumberOfObjects.Int64(), "average object numbers are not equal")

		// separate value for node 1
		res, err = c.TestInvoke(t, "getReportByNode", anotherCnr.id[:], nodes[0].pub)
		require.NoError(t, err, "receiving estimations for node 1")
		var r containerrpc.ContainerNodeReport
		require.NoError(t, r.FromStackItem(res.Pop().Item()))
		pk := r.PublicKey.Bytes()
		size := r.ContainerSize
		objs := r.NumberOfObjects
		reportNumber := r.NumberOfReports

		require.Equal(t, nodes[0].pub, pk)
		require.Equal(t, int64(nodeSize1), size.Int64(), "node 1 sizes are not equal")
		require.Equal(t, int64(nodeObjs1), objs.Int64(), "node 1 object numbers are not equal")
		require.Equal(t, int64(1), reportNumber.Int64(), "unexpected node 1 report number")

		// separate value for node 2
		res, err = c.TestInvoke(t, "getReportByNode", anotherCnr.id[:], nodes[1].pub)
		require.NoError(t, err, "receiving estimations for node 1")
		require.NoError(t, r.FromStackItem(res.Pop().Item()))
		pk = r.PublicKey.Bytes()
		size = r.ContainerSize
		objs = r.NumberOfObjects
		reportNumber = r.NumberOfReports

		require.Equal(t, nodes[1].pub, pk)
		require.Equal(t, int64(nodeSize2), size.Int64(), "node 2 sizes are not equal")
		require.Equal(t, int64(nodeObjs2), objs.Int64(), "node 2 object numbers are not equal")
		require.Equal(t, int64(1), reportNumber.Int64(), "unexpected node 2 report number")
	})

	t.Run("billing values", func(t *testing.T) {
		const objsNumber = 1234

		cNm.Invoke(t, stackitem.Null{}, "setConfig", []byte{}, containerconst.EpochDurationKey, epochDuration)
		var currentEpoch = 100

		type (
			reportValue struct {
				reportedAt int64 // epoch duration is 100, this val should be in [0; 100] segment
				value      int64
			}

			testCase struct {
				name            string
				previousValue   int64
				reports         []reportValue
				expectedAverage int64
			}
		)

		cases := []testCase{
			{
				name:          "single report at the beginning",
				previousValue: 100,
				reports: []reportValue{
					{reportedAt: 0, value: 12345},
				},
				expectedAverage: 100*0/100 + 12345*100/100,
			},
			{
				name:          "single report in the middle",
				previousValue: 400,
				reports: []reportValue{
					{reportedAt: 50, value: 12345},
				},
				expectedAverage: 400*50/100 + 12345*50/100,
			},
			{
				name:          "single report at the end",
				previousValue: 200,
				reports: []reportValue{
					{reportedAt: 100, value: 300},
				},
				expectedAverage: 200*100/100 + 300*0/100,
			},
			{
				name:          "reports at the beginning",
				previousValue: 200,
				reports: []reportValue{
					{reportedAt: 0, value: 100},
					{reportedAt: 30, value: 200},
					{reportedAt: 80, value: 300},
				},
				expectedAverage: 200*0/100 + 100*30/100 + 200*50/100 + 300*20/100,
			},
			{
				name:          "reports in the middle",
				previousValue: 500,
				reports: []reportValue{
					{reportedAt: 20, value: 100},
					{reportedAt: 60, value: 200},
					{reportedAt: 90, value: 300},
				},
				expectedAverage: 500*20/100 + 100*40/100 + 200*30/100 + 300*10/100,
			},
			{
				name:          "reports at the end",
				previousValue: 600,
				reports: []reportValue{
					{reportedAt: 40, value: 100},
					{reportedAt: 70, value: 200},
					{reportedAt: 100, value: 300},
				},
				expectedAverage: 600*40/100 + 100*30/100 + 200*30/100,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				anotherCnr := addContainerWithNodes(t, c, cBal, nodesKeys)
				c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putReport", anotherCnr.id[:], tc.previousValue, objsNumber, nodes[0].pub)

				var (
					lastBlock    = c.TopBlock(t)
					newEpochTime = (lastBlock.Timestamp + 1000) / 1000 * 1000

					latestReportedValue = tc.reports[len(tc.reports)-1].value
					numberOfReports     = len(tc.reports)
					latestReportTime    int
				)

				currentEpoch++
				newEpochBlockTXs := []*transaction.Transaction{cNm.PrepareInvoke(t, "newEpoch", currentEpoch)}
				if tc.reports[0].reportedAt == 0 {
					if len(tc.reports) == 1 {
						latestReportTime = int(newEpochTime)
					}

					tx := c.WithSigners(nodes[0].signer).PrepareInvoke(t, "putReport", anotherCnr.id[:], tc.reports[0].value, objsNumber, nodes[0].pub)
					tx.SystemFee = 1_0000_0000
					newEpochBlockTXs = append(newEpochBlockTXs, tx)

					tc.reports = tc.reports[1:]
				}
				b := c.NewUnsignedBlock(t, newEpochBlockTXs...)
				b.Timestamp = newEpochTime
				c.SignBlock(b)
				require.NoError(t, c.Chain.AddBlock(b))
				for _, tx := range newEpochBlockTXs {
					c.CheckHalt(t, tx.Hash(), stackitem.Make(nil))
				}

				for i, r := range tc.reports {
					tx := c.WithSigners(nodes[0].signer).PrepareInvoke(t, "putReport", anotherCnr.id[:], r.value, objsNumber, nodes[0].pub)
					tx.SystemFee = 1_0000_0000
					b := c.NewUnsignedBlock(t, tx)
					if r.reportedAt != 0 {
						b.Timestamp = newEpochTime + uint64(r.reportedAt)*1000 // in milliseconds
					}
					c.SignBlock(b)
					require.NoError(t, c.Chain.AddBlock(b))
					c.CheckHalt(t, tx.Hash(), stackitem.Make(nil))

					if i == len(tc.reports)-1 {
						latestReportTime = int(b.Timestamp)
					}
				}

				res, err := c.TestInvoke(t, "iterateReports", anotherCnr.id[:])
				require.NoError(t, err)

				reports := parseNodeReportsFromStackIterator(t, res)
				require.Len(t, reports, 1)
				report := reports[0]
				require.EqualValues(t, numberOfReports, report.NumberOfReports.Int64())
				require.EqualValues(t, objsNumber, report.NumberOfObjects.Int64())
				require.EqualValues(t, latestReportedValue, report.ContainerSize.Int64())

				res, err = c.TestInvoke(t, "iterateBillingStats", anotherCnr.id[:])
				require.NoError(t, err)

				billingStats := parseEpochBillingStatFromStackIterator(t, res)
				require.Len(t, billingStats, 1)
				stat := billingStats[0]

				require.EqualValues(t, latestReportTime, stat.LastUpdateTime.Int64())
				require.EqualValues(t, currentEpoch, stat.LatestEpoch.Int64())
				require.EqualValues(t, latestReportedValue, stat.LatestContainerSize.Int64())
				require.EqualValues(t, tc.expectedAverage, stat.LatestEpochAverageSize.Int64())
			})
		}
	})

	t.Run("iterate container's reports", func(t *testing.T) {
		const reportersNumber = 3
		anotherCnr := addContainerWithNodes(t, c, cBal, nodesKeys)

		for i := range reportersNumber {
			c.WithSigners(nodes[i].signer).Invoke(t, stackitem.Null{}, "putReport",
				anotherCnr.id[:], i, i, nodes[i].pub,
			)
		}

		res, err := c.TestInvoke(t, "iterateReports", anotherCnr.id[:])
		require.NoError(t, err, "receiving reports iterator")

		reports := parseNodeReportsFromStackIterator(t, res)
		require.Len(t, reports, reportersNumber, "reporters number are not equal")
		for i := range reportersNumber {
			require.Equal(t, nodes[i].pub, reports[i].PublicKey.Bytes())
			require.Equal(t, int64(i), reports[i].ContainerSize.Int64())
			require.Equal(t, int64(i), reports[i].NumberOfObjects.Int64())
			require.Equal(t, int64(1), reports[i].NumberOfReports.Int64())
		}
	})

	t.Run("iterate all containers", func(t *testing.T) {
		// drop all the previous containers

		res, err := c.TestInvoke(t, "containersOf", nil)
		require.NoError(t, err)
		cIDs := iteratorToArray(res.Pop().Value().(*storage.Iterator))
		for _, cID := range cIDs {
			cIDRaw, err := cID.TryBytes()
			require.NoError(t, err)

			c.Invoke(t, stackitem.Null{}, "remove", cIDRaw, []byte{}, []byte{}, []byte{})
		}

		const numOfContainers = 3
		type cnrInfo struct {
			size int64
			objs int64
		}

		want := make(map[cid.ID]cnrInfo)
		for i := range numOfContainers {
			anotherCnr := addContainerWithNodes(t, c, cBal, nodesKeys)
			want[anotherCnr.id] = cnrInfo{size: int64(i), objs: int64(i)}

			c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putReport",
				anotherCnr.id[:], i, i, nodes[0].pub,
			)
		}

		res, err = c.TestInvoke(t, "iterateAllReportSummaries")
		require.NoError(t, err, "receiving all container infos iterator")

		it := res.Pop().Value().(*storage.Iterator)
		estimations := iteratorToArray(it)
		require.Len(t, estimations, numOfContainers, "unexpected infos number")
		for _, e := range estimations {
			kv := e.Value().([]stackitem.Item)
			cID, err := kv[0].TryBytes()
			require.NoError(t, err)
			require.Len(t, cID, cid.Size)

			v := kv[1].Value().([]stackitem.Item)
			size, err := v[0].TryInteger()
			require.NoError(t, err)
			objs, err := v[1].TryInteger()
			require.NoError(t, err)

			require.Equal(t, want[cid.ID(cID)].size, size.Int64())
			require.Equal(t, want[cid.ID(cID)].objs, objs.Int64())
		}
	})

	t.Run("cleanup container infos", func(t *testing.T) {
		// drop all the previous containers

		res, err := c.TestInvoke(t, "containersOf", nil)
		require.NoError(t, err)
		cIDs := iteratorToArray(res.Pop().Value().(*storage.Iterator))
		for _, cID := range cIDs {
			cIDRaw, err := cID.TryBytes()
			require.NoError(t, err)

			c.Invoke(t, stackitem.Null{}, "remove", cIDRaw, []byte{}, []byte{}, []byte{})
		}

		anotherCnr := addContainerWithNodes(t, c, cBal, nodesKeys)
		c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putReport",
			anotherCnr.id[:], 123, 455, nodes[0].pub,
		)
		res, err = c.TestInvoke(t, "iterateAllReportSummaries")
		require.NoError(t, err, "receiving all container infos iterator")
		it := res.Pop().Value().(*storage.Iterator)
		estimations := iteratorToArray(it)
		require.Len(t, estimations, 1)

		c.Invoke(t, stackitem.Null{}, "remove", anotherCnr.id[:], []byte{1}, []byte{1}, []byte{1})
		res, err = c.TestInvoke(t, "iterateAllReportSummaries")
		require.NoError(t, err, "receiving all container infos iterator")
		it = res.Pop().Value().(*storage.Iterator)
		estimations = iteratorToArray(it)
		require.Empty(t, estimations)
	})
}

func parseNodeReportsFromStackIterator(t *testing.T, stack *vm.Stack) []containerrpc.ContainerNodeReport {
	var res []containerrpc.ContainerNodeReport

	it := stack.Pop().Value().(*storage.Iterator)
	for it.Next() {
		var r containerrpc.ContainerNodeReport
		require.NoError(t, r.FromStackItem(it.Value()))
		res = append(res, r)
	}

	return res
}

func parseEpochBillingStatFromStackIterator(t *testing.T, stack *vm.Stack) []containerrpc.ContainerEpochBillingStat {
	var res []containerrpc.ContainerEpochBillingStat

	it := stack.Pop().Value().(*storage.Iterator)
	for it.Next() {
		var r containerrpc.ContainerEpochBillingStat
		require.NoError(t, r.FromStackItem(it.Value()))
		res = append(res, r)
	}

	return res
}

func addContainerWithNodes(t *testing.T, cnrInv, balInv *neotest.ContractInvoker, keys []any) testContainer {
	_, cnr := addContainer(t, cnrInv, balInv)

	cnrInv.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cnr.id[:], 0, keys)
	cnrInv.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cnr.id[:], []uint8{uint8(len(keys))})

	return cnr
}

func TestQuotas(t *testing.T) {
	var (
		c, cBal, cNm = newContainerInvoker(t, false)
		ownerAcc     = c.NewAccount(t)
		ownerSH      = ownerAcc.ScriptHash()
		owner        = user.NewFromScriptHash(ownerSH)
		cnr          = containertest.Container()
	)
	cnr.SetOwner(owner)
	rawCnr := cnr.Marshal()
	cID := cid.NewFromMarshalledContainer(rawCnr)
	balanceMint(t, cBal, ownerAcc, containerFee*1, []byte{})
	c.Invoke(t, stackitem.Null{}, "put", rawCnr, randomBytes(64), randomBytes(33), randomBytes(42))

	nodes := []testNodeInfo{
		newStorageNode(t, c),
		newStorageNode(t, c),
		newStorageNode(t, c),
	}
	nodesKeys := make([]any, 0, len(nodes))
	for _, node := range nodes {
		nodesKeys = append(nodesKeys, node.pub)
	}
	c.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cID[:], 0, nodesKeys)
	c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID[:], []uint8{uint8(len(nodesKeys))})

	cNm.Invoke(t, stackitem.Null{}, "newEpoch", int64(1))
	cNm.Invoke(t, stackitem.Null{}, "newEpoch", int64(2))

	const node1Report = 1000
	const node2Report = 10000
	c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putReport",
		cID[:], int64(node1Report), int64(node1Report), nodes[0].pub,
	)
	c.WithSigners(nodes[1].signer).Invoke(t, stackitem.Null{}, "putReport",
		cID[:], int64(node2Report), int64(node2Report), nodes[1].pub,
	)

	t.Run("witness checks", func(t *testing.T) {
		t.Run("container quota", func(t *testing.T) {
			c.WithSigners(c.NewAccount(t)).InvokeFail(t, common.ErrWitnessFailed, "setSoftContainerQuota", cID[:], 0)
			c.WithSigners(c.NewAccount(t)).InvokeFail(t, common.ErrWitnessFailed, "setHardContainerQuota", cID[:], 0)
		})

		t.Run("user quota", func(t *testing.T) {
			c.WithSigners(c.NewAccount(t)).InvokeFail(t, common.ErrWitnessFailed, "setSoftUserQuota", owner[:], 0)
			c.WithSigners(c.NewAccount(t)).InvokeFail(t, common.ErrWitnessFailed, "setHardUserQuota", owner[:], 0)
		})
	})

	t.Run("alphabet management", func(t *testing.T) {
		t.Run("turned on", func(t *testing.T) {
			cNm.Invoke(t, stackitem.Null{}, "setConfig", []byte{}, containerconst.AlphabetManagesQuotasKey, true)

			t.Run("container quota", func(t *testing.T) {
				c.WithSigners(ownerAcc).InvokeFail(t, common.ErrAlphabetWitnessFailed, "setSoftContainerQuota", cID[:], 0)
				c.WithSigners(ownerAcc).InvokeFail(t, common.ErrAlphabetWitnessFailed, "setHardContainerQuota", cID[:], 0)
			})

			t.Run("user quota", func(t *testing.T) {
				c.WithSigners(ownerAcc).InvokeFail(t, common.ErrAlphabetWitnessFailed, "setSoftUserQuota", owner[:], 0)
				c.WithSigners(ownerAcc).InvokeFail(t, common.ErrAlphabetWitnessFailed, "setHardUserQuota", owner[:], 0)
			})
		})

		t.Run("turned off", func(t *testing.T) {
			cNm.Invoke(t, stackitem.Null{}, "setConfig", []byte{}, containerconst.AlphabetManagesQuotasKey, false)

			t.Run("container quota", func(t *testing.T) {
				c.InvokeFail(t, common.ErrWitnessFailed, "setSoftContainerQuota", cID[:], 0)
				c.InvokeFail(t, common.ErrWitnessFailed, "setHardContainerQuota", cID[:], 0)
			})

			t.Run("user quota", func(t *testing.T) {
				c.InvokeFail(t, common.ErrWitnessFailed, "setSoftUserQuota", owner[:], 0)
				c.InvokeFail(t, common.ErrWitnessFailed, "setHardUserQuota", owner[:], 0)
			})
		})
	})

	t.Run("wrong input arg", func(t *testing.T) {
		t.Run("container quota", func(t *testing.T) {
			c.WithSigners(ownerAcc).InvokeFail(t, "invalid container id", "setSoftContainerQuota", []byte{0}, 0)
			c.WithSigners(ownerAcc).InvokeFail(t, "invalid container id", "setHardContainerQuota", []byte{0}, 0)
		})
		t.Run("user quota", func(t *testing.T) {
			c.WithSigners(ownerAcc).InvokeFail(t, "invalid user id", "setSoftUserQuota", []byte{0}, 0)
			c.WithSigners(ownerAcc).InvokeFail(t, "invalid user id", "setHardUserQuota", []byte{0}, 0)
		})
	})

	t.Run("default values", func(t *testing.T) {
		exp := stackitem.NewStruct([]stackitem.Item{
			stackitem.Make(0),
			stackitem.Make(0),
		})

		t.Run("container quota", func(t *testing.T) {
			c.Invoke(t, exp, "containerQuota", cID[:])
		})
		t.Run("container quota", func(t *testing.T) {
			c.Invoke(t, exp, "userQuota", owner[:])
		})
	})

	t.Run("happy path", func(t *testing.T) {
		softLimit := node2Report
		hardLimit := node1Report

		t.Run("container quota", func(t *testing.T) {
			txH := c.WithSigners(ownerAcc).Invoke(t, stackitem.Null{}, "setSoftContainerQuota", cID[:], softLimit)
			res := c.GetTxExecResult(t, txH)
			assertNotificationEvent(t, res.Events[0], "ContainerQuotaSet", cID[:], big.NewInt(int64(softLimit)), false)
			exp := stackitem.NewStruct([]stackitem.Item{
				stackitem.Make(softLimit),
				stackitem.Make(0),
			})
			c.Invoke(t, exp, "containerQuota", cID[:])

			txH = c.WithSigners(ownerAcc).Invoke(t, stackitem.Null{}, "setHardContainerQuota", cID[:], hardLimit)
			res = c.GetTxExecResult(t, txH)
			assertNotificationEvent(t, res.Events[0], "ContainerQuotaSet", cID[:], big.NewInt(int64(hardLimit)), true)
			exp = stackitem.NewStruct([]stackitem.Item{
				stackitem.Make(softLimit),
				stackitem.Make(hardLimit),
			})
			c.Invoke(t, exp, "containerQuota", cID[:])
		})
		t.Run("user quota", func(t *testing.T) {
			txH := c.WithSigners(ownerAcc).Invoke(t, stackitem.Null{}, "setSoftUserQuota", owner[:], softLimit)
			res := c.GetTxExecResult(t, txH)
			assertNotificationEvent(t, res.Events[0], "UserQuotaSet", owner[:], big.NewInt(int64(softLimit)), false)
			exp := stackitem.NewStruct([]stackitem.Item{
				stackitem.Make(softLimit),
				stackitem.Make(0),
			})
			c.Invoke(t, exp, "userQuota", owner[:])

			txH = c.WithSigners(ownerAcc).Invoke(t, stackitem.Null{}, "setHardUserQuota", owner[:], hardLimit)
			res = c.GetTxExecResult(t, txH)
			assertNotificationEvent(t, res.Events[0], "UserQuotaSet", owner[:], big.NewInt(int64(hardLimit)), true)
			exp = stackitem.NewStruct([]stackitem.Item{
				stackitem.Make(softLimit),
				stackitem.Make(hardLimit),
			})
			c.Invoke(t, exp, "userQuota", owner[:])
		})
	})
}

func TestTotalUserSpace(t *testing.T) {
	c, cBal, cNm := newContainerInvoker(t, false)
	node := newStorageNode(t, c)
	nodeKey := node.pub
	ownerAcc := c.NewAccount(t)
	ownerSH := ownerAcc.ScriptHash()
	owner := user.NewFromScriptHash(ownerSH)

	cNm.Invoke(t, stackitem.Null{}, "newEpoch", int64(1))

	t.Run("values", func(t *testing.T) {
		const numOfCnrs = 10
		cnrs := make([]cid.ID, 0, numOfCnrs)
		for range numOfCnrs {
			cnr := containertest.Container()
			cnr.SetOwner(owner)
			rawCnr := cnr.Marshal()
			cID := cid.NewFromMarshalledContainer(rawCnr)

			balanceMint(t, cBal, ownerAcc, containerFee*1, []byte{})
			c.Invoke(t, stackitem.Null{}, "put", rawCnr, randomBytes(64), randomBytes(33), randomBytes(42))

			c.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cID[:], 0, []any{nodeKey})
			c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID[:], []uint8{1})

			cnrs = append(cnrs, cID)
		}

		var sum int
		for i, cID := range cnrs {
			sum += i
			c.WithSigners(node.signer).Invoke(t, stackitem.Null{}, "putReport", cID[:], i, 1234, nodeKey)
			c.Invoke(t, stackitem.Make(sum), "getTakenSpaceByUser", owner[:])
		}

		// decrease values
		for i, cID := range cnrs {
			if i == 0 {
				continue
			}
			sum--
			c.WithSigners(node.signer).Invoke(t, stackitem.Null{}, "putReport", cID[:], i-1, 1234, nodeKey)
			c.Invoke(t, stackitem.Make(sum), "getTakenSpaceByUser", owner[:])
		}

		// container removal handling
		for _, cID := range cnrs {
			c.Invoke(t, stackitem.Null{}, "remove", cID[:], []byte{}, []byte{}, []byte{})
		}
		c.Invoke(t, stackitem.Make(0), "getTakenSpaceByUser", owner[:])
	})

	t.Run("netmap changes", func(t *testing.T) {
		cnr := containertest.Container()
		cnr.SetOwner(owner)
		rawCnr := cnr.Marshal()
		cID := cid.NewFromMarshalledContainer(rawCnr)

		balanceMint(t, cBal, ownerAcc, containerFee*1, []byte{})
		c.Invoke(t, stackitem.Null{}, "put", rawCnr, randomBytes(64), randomBytes(33), randomBytes(42))

		const nodesInNetmap = 10
		var (
			signers  []neotest.Signer
			oldNodes []any
		)
		for range nodesInNetmap {
			acc := c.NewAccount(t)
			key := acc.(neotest.SingleSigner).Account().PrivateKey().PublicKey().Bytes()

			signers = append(signers, acc)
			oldNodes = append(oldNodes, key)
		}
		c.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cID[:], 0, oldNodes)
		c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID[:], []uint8{1})

		const spaceMultiplier = 1000
		var sum int
		for i := range nodesInNetmap {
			sum += i * spaceMultiplier
			c.WithSigners(signers[i]).Invoke(t, stackitem.Null{}, "putReport", cID[:], i*spaceMultiplier, i*spaceMultiplier, oldNodes[i])
		}
		c.Invoke(t, stackitem.Make(sum), "getTakenSpaceByUser", owner[:])

		newNodes := oldNodes[:len(oldNodes)/2]
		var newSum int
		for i := range newNodes {
			newSum += i * spaceMultiplier
		}
		c.Invoke(t, stackitem.Null{}, "addNextEpochNodes", cID[:], 0, newNodes)
		c.Invoke(t, stackitem.Null{}, "commitContainerListUpdate", cID[:], []uint8{1})
		c.Invoke(t, stackitem.Make(newSum), "getTakenSpaceByUser", owner[:])
	})
}

func TestContainerList(t *testing.T) {
	c, _, _ := newContainerInvoker(t, false)

	t.Run("happy path", func(t *testing.T) {
		cID := make([]byte, sha256.Size)
		_, _ = rand.Read(cID)

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
		_, _ = rand.Read(cID)

		badPublicKey := []byte{1, 2, 3, 4, 5}

		c.InvokeFail(t, fmt.Sprintf("%s: length: %d", containerconst.ErrorInvalidPublicKey, len(badPublicKey)), "addNextEpochNodes", cID, 0, []any{badPublicKey})
	})

	t.Run("not alphabet", func(t *testing.T) {
		cID := make([]byte, sha256.Size)
		_, _ = rand.Read(cID)

		notAlphabet := c.NewInvoker(c.Hash, c.NewAccount(t))
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
	_, _ = rand.Read(testData)
	cID := make([]byte, sha256.Size)
	_, _ = rand.Read(cID)

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
	const (
		numOfVectors  = 5
		nodesInVector = 5
	)

	var sigs []any
	for range numOfVectors {
		vector := make([]any, 0, nodesInVector)
		for range nodesInVector {
			vector = append(vector, make([]byte, interop.SignatureLen))
		}
		sigs = append(sigs, vector)
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
