package tests

import (
	"bytes"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"path"
	"slices"
	"strconv"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/config/netmode"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/core/native/nativenames"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/unwrap"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/vm/vmstate"
	"github.com/nspcc-dev/neofs-contract/common"
	"github.com/nspcc-dev/neofs-contract/contracts/container/containerconst"
	"github.com/nspcc-dev/neofs-contract/contracts/nns/recordtype"
	containerrpc "github.com/nspcc-dev/neofs-contract/rpc/container"
	"github.com/nspcc-dev/neofs-sdk-go/container"
	cid "github.com/nspcc-dev/neofs-sdk-go/container/id"
	containertest "github.com/nspcc-dev/neofs-sdk-go/container/test"
	netmaptest "github.com/nspcc-dev/neofs-sdk-go/netmap/test"
	"github.com/nspcc-dev/neofs-sdk-go/user"
	usertest "github.com/nspcc-dev/neofs-sdk-go/user/test"
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

// CORSRule describes one rule for the container’s CORS attribute. It is the same as in contract.
type CORSRule struct {
	AllowedMethods []string
	AllowedOrigins []string
	AllowedHeaders []string
	ExposeHeaders  []string
	MaxAgeSeconds  int64
}

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

type testContainer struct {
	id                     [32]byte
	value, sig, pub, token []byte
	owner                  user.ID
}

func dummyContainer(owner neotest.Signer) testContainer {
	ownerID := user.NewFromScriptHash(owner.ScriptHash())

	cnr := containertest.Container()
	cnr.SetOwner(ownerID)
	value := cnr.Marshal()

	return testContainer{
		id:    sha256.Sum256(value),
		value: value,
		sig:   randomBytes(64),
		pub:   randomBytes(33),
		token: randomBytes(42),
		owner: ownerID,
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
			ownerAcc := cnt.owner.ScriptHash()

			putArgs := []any{cnt.value, cnt.sig, cnt.pub, cnt.token}
			c.InvokeFail(t, "insufficient balance to create container", "put", putArgs...)

			balanceMint(t, cBal, acc, containerFee*1, []byte{})

			cAcc := c.WithSigners(acc)
			cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "put", putArgs...)

			txHash := c.Invoke(t, stackitem.Null{}, "put", putArgs...)
			res := c.GetTxExecResult(t, txHash)
			events := res.Events
			require.Len(t, events, 4)
			assertNotificationEvent(t, events[3], "Transfer", nil, ownerAcc[:], big.NewInt(1), cnt.id[:])

			c.Invoke(t, stackitem.Null{}, "alias", cnt.id[:])

			var cnr container.Container
			require.NoError(t, cnr.Unmarshal(cnt.value))

			assertGetInfo(t, c, cnt.id, cnr)

			t.Run("with nice names", func(t *testing.T) {
				ctrNNS := neotest.CompileFile(t, c.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
				nnsHash := ctrNNS.Hash

				balanceMint(t, cBal, acc, containerFee*1, []byte{})

				putArgs := []any{cnt.value, cnt.sig, cnt.pub, cnt.token, "mycnt", ""}
				t.Run("no fee for alias", func(t *testing.T) {
					c.InvokeFail(t, "insufficient balance to create container", "put", putArgs...)
				})

				balanceMint(t, cBal, acc, containerAliasFee*1, []byte{})
				txHash := c.Invoke(t, stackitem.Null{}, "put", putArgs...)

				res := c.GetTxExecResult(t, txHash)
				events := res.Events
				require.Len(t, events, 5)
				assertNotificationEvent(t, events[4], "Transfer", nil, ownerAcc[:], big.NewInt(1), cnt.id[:])

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
	ownerAcc := cnt.owner.ScriptHash()
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "delete",
		cnt.id[:], cnt.sig, cnt.token)

	txHash := c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)

	res := c.GetTxExecResult(t, txHash)
	events := res.Events
	require.Len(t, events, 2)
	assertNotificationEvent(t, events[0], "DeleteSuccess", cnt.id[:])
	assertNotificationEvent(t, events[1], "Transfer", ownerAcc[:], nil, big.NewInt(1), cnt.id[:])

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)
	})

	c.InvokeFail(t, containerconst.NotFoundError, "get", cnt.id[:])
	c.InvokeFail(t, containerconst.NotFoundError, "getInfo", cnt.id[:])
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

		for _, method := range []string{"getReportByNode", "getReportByAccount"} {
			t.Run(method, func(t *testing.T) {
				// separate value for node 1
				if method == "getReportByNode" {
					res, err = c.TestInvoke(t, method, anotherCnr.id[:], nodes[0].pub)
				} else {
					res, err = c.TestInvoke(t, method, anotherCnr.id[:], nodes[0].signer.ScriptHash())
				}

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
				if method == "getReportByNode" {
					res, err = c.TestInvoke(t, method, anotherCnr.id[:], nodes[1].pub)
				} else {
					res, err = c.TestInvoke(t, method, anotherCnr.id[:], nodes[1].signer.ScriptHash())
				}
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
		}
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
	cnr := containertest.Container()
	anyValidCnr := cnr.Marshal()
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
		ownerAddr := user.NewFromScriptHash(ownerAcc)
		cnr := cnr
		cnr.SetAttribute("__NEOFS__METAINFO_CONSISTENCY", "foo")
		cnr.SetAttribute("__NEOFS__NAME", "my-domain")
		cnr.SetOwner(ownerAddr)
		cnrBytes := cnr.Marshal()
		id := cid.NewFromMarshalledContainer(cnrBytes)

		balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee+aliasFee, []byte{})

		txHash := exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
			cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "my-domain", "", true)

		// storage
		getStorageItem := func(key []byte) []byte {
			return getContractStorageItem(t, exec, containerContract.Hash, key)
		}
		require.EqualValues(t, id[:], getStorageItem(slices.Concat([]byte{'o'}, ownerAddr[:], id[:])))
		require.EqualValues(t, cnrBytes, getStorageItem(slices.Concat([]byte{'x'}, id[:])))
		require.EqualValues(t, []byte{}, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
		require.EqualValues(t, []byte("my-domain.container"), getStorageItem(slices.Concat([]byte("nnsHasAlias"), id[:])))

		// NNS
		exec.CommitteeInvoker(nnsContract).Invoke(t, stackitem.NewArray([]stackitem.Item{
			stackitem.NewByteArray([]byte(base58.Encode(id[:]))),
		}), "getRecords", "my-domain.container", int64(recordtype.TXT))

		// notifications
		res := exec.GetTxExecResult(t, txHash)
		events := res.Events
		require.Len(t, events, 5)
		committeeAcc := committee.(neotest.MultiSigner).Single(0).Account().ScriptHash() // type asserted above
		assertNotificationEvent(t, events[0], "Transfer", nil, containerContract.Hash[:], big.NewInt(1), []byte("my-domain.container"))
		assertNotificationEvent(t, events[1], "Transfer", ownerAcc[:], committeeAcc[:], big.NewInt(fee+aliasFee))
		assertNotificationEvent(t, events[2], "TransferX", ownerAcc[:], committeeAcc[:], big.NewInt(fee+aliasFee), slices.Concat([]byte{0x10}, id[:]))
		assertNotificationEvent(t, events[3], "Created", id[:], ownerAddr[:])
		assertNotificationEvent(t, events[4], "Transfer", nil, ownerAcc[:], big.NewInt(1), id[:])
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
		cnr := cnr
		cnr.SetOwner(user.NewFromScriptHash(owner.ScriptHash()))
		cnrBytes := cnr.Marshal()

		exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "insufficient balance to create container", "create",
			cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, anyValidMetaOnChain)

		balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee-1, []byte{})

		exec.CommitteeInvoker(containerContract.Hash).InvokeFail(t, "insufficient balance to create container", "create",
			cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, anyValidMetaOnChain)
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
	ownerAddr := user.NewFromScriptHash(ownerAcc)
	cnr.SetOwner(ownerAddr)
	cnr.SetAttribute("__NEOFS__METAINFO_CONSISTENCY", "foo")
	cnrBytes := cnr.Marshal()
	id := cid.NewFromMarshalledContainer(cnrBytes)

	balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee, []byte{})

	inv := exec.CommitteeInvoker(containerContract.Hash)

	txHash := inv.Invoke(t, stackitem.Null{}, "create",
		cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, true)

	// storage
	getStorageItem := func(key []byte) []byte {
		return getContractStorageItem(t, exec, containerContract.Hash, key)
	}
	require.EqualValues(t, id[:], getStorageItem(slices.Concat([]byte{'o'}, ownerAddr[:], id[:])))
	require.EqualValues(t, cnrBytes, getStorageItem(slices.Concat([]byte{'x'}, id[:])))
	require.EqualValues(t, []byte{}, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
	// notifications
	res := exec.GetTxExecResult(t, txHash)
	events := res.Events
	require.Len(t, events, 4)
	committeeAcc := committee.(neotest.MultiSigner).Single(0).Account().ScriptHash() // type asserted above
	assertNotificationEvent(t, events[0], "Transfer", ownerAcc[:], committeeAcc[:], big.NewInt(fee))
	assertNotificationEvent(t, events[1], "TransferX", ownerAcc[:], committeeAcc[:], big.NewInt(fee), slices.Concat([]byte{0x10}, id[:]))
	assertNotificationEvent(t, events[2], "Created", id[:], ownerAddr[:])
	assertNotificationEvent(t, events[3], "Transfer", nil, ownerAcc[:], big.NewInt(1), id[:])

	assertGetInfo(t, inv, id, cnr)
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
	t.Run("lock", func(t *testing.T) {
		blockChain, signer := chain.NewSingleWithOptions(t, nil)
		exec := neotest.NewExecutor(t, blockChain, signer, signer)

		deployDefaultNNS(t, exec)
		netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
		containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
		deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
		deployProxyContract(t, exec)

		exec.DeployContract(t, containerContract, nil)

		inv := exec.CommitteeInvoker(containerContract.Hash)

		cnr := containertest.Container()

		curBlockTime := exec.TopBlock(t).Timestamp
		expMs := curBlockTime + 1000
		exp := expMs / 1000

		cnr.SetAttribute("__NEOFS__LOCK_UNTIL", strconv.FormatUint(exp, 10))
		id := cid.NewFromMarshalledContainer(cnr.Marshal())

		inv.Invoke(t, id[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

		inv.InvokeFail(t, containerconst.ErrorLocked+" until "+strconv.FormatUint(exp*1000, 10)+", now "+strconv.Itoa(int(curBlockTime+2)),
			"remove", id[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

		blk := exec.NewUnsignedBlock(t)
		blk.Timestamp = expMs
		require.NoError(t, exec.Chain.AddBlock(exec.SignBlock(blk)))

		inv.Invoke(t, nil, "remove", id[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
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
	cnr := dummyContainer(owner).value
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
	require.Len(t, events, 2)
	assertNotificationEvent(t, events[0], "Removed", id[:], ownerAddr)
	assertNotificationEvent(t, events[1], "Transfer", ownerAcc[:], nil, big.NewInt(1), id[:])
}

func TestContainerPutEACL(t *testing.T) {
	cnr := containertest.Container()
	anyValidCnr := cnr.Marshal()
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
	ownerAddr := user.NewFromScriptHash(ownerAcc)
	cnr.SetOwner(ownerAddr)
	cnrBytes := cnr.Marshal()
	id := sha256.Sum256(cnrBytes)
	copy(anyValidEACL[6:], id[:])

	balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee, []byte{})

	// TODO: store items manually instead instead of creation call.
	//  https://github.com/nspcc-dev/neo-go/issues/2926 would help
	exec.CommitteeInvoker(containerContract.Hash).Invoke(t, stackitem.Null{}, "create",
		cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, "", "", false)
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
	cnr := containertest.Container()
	anyValidCnr := cnr.Marshal()
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
	cnr := containertest.Container()
	anyValidCnr := cnr.Marshal()
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

func TestContainerCreateV2(t *testing.T) {
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)
	anyOID := randomBytes(32)

	blockChain, committee := chain.NewSingle(t)
	require.Implements(t, (*neotest.MultiSigner)(nil), committee)
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	const fee = 1000
	const aliasFee = 500
	const domainName = "my-domain"
	const fullDomain = domainName + ".container"

	nnsContract := deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", fee, "ContainerAliasFee", aliasFee)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	balanceContract := deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	proxyContractAddr := deployProxyContract(t, exec)
	exec.DeployContract(t, containerContract, nil)

	getStorageItem := func(key []byte) []byte {
		return getContractStorageItem(t, exec, containerContract.Hash, key)
	}

	exec.CommitteeInvoker(exec.NativeHash(t, nativenames.Gas)).Invoke(t, true, "transfer",
		exec.Validator.ScriptHash(), proxyContractAddr, int64(1_0000_0000), nil)

	inv := exec.CommitteeInvoker(containerContract.Hash)
	proxyInv := exec.NewInvoker(containerContract.Hash, neotest.NewContractSigner(proxyContractAddr, func(tx *transaction.Transaction) []any {
		return nil
	}))
	nnsInv := exec.CommitteeInvoker(nnsContract)

	testMeta := func(t *testing.T, id cid.ID) []byte {
		m := testMeta(id[:], anyOID)
		b, err := stackitem.Serialize(m)
		require.NoError(t, err)
		return b
	}

	owner := exec.NewAccount(t, 0)
	ownerAcc := owner.ScriptHash()
	ownerID := user.NewFromScriptHash(ownerAcc)

	cnr := containertest.Container()
	cnr.SetOwner(ownerID)

	cnrBytes := cnr.Marshal()
	id := cid.NewFromMarshalledContainer(cnrBytes)

	cnrFields := containerToStructFields(cnr)

	anyMetaSigs := []any{[]any{randomBytes(interop.SignatureLen)}}

	t.Run("no alphabet witness", func(t *testing.T) {
		inv := exec.NewInvoker(containerContract.Hash, exec.NewAccount(t))
		inv.InvokeFail(t, "alphabet witness check failed", "createV2",
			stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("invalid attributes", func(t *testing.T) {
		t.Run("lock", func(t *testing.T) {
			assert := func(t *testing.T, val string, exc string) {
				cnr := cnr
				cnr.SetAttribute("__NEOFS__LOCK_UNTIL", val)

				inv.InvokeFail(t, exc, "createV2",
					stackitem.NewStruct(containerToStructFields(cnr)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
			}

			t.Run("non-int", func(t *testing.T) {
				for _, val := range []string{} {
					assert(t, val, "at instruction 1 (SYSCALL): invalid format") // StdLib atoi exception
				}
			})

			t.Run("non-positive", func(t *testing.T) {
				assert(t, "-42", "invalid __NEOFS__LOCK_UNTIL attribute: non-positive value -42")
				assert(t, "0", "invalid __NEOFS__LOCK_UNTIL attribute: non-positive value 0")
			})

			t.Run("already passed", func(t *testing.T) {
				curBlockTime := exec.TopBlock(t).Timestamp
				curBlockTimeSec := strconv.FormatUint(curBlockTime/1000, 10)
				assert(t, curBlockTimeSec, "lock expiration time "+curBlockTimeSec+"000 is not later than current "+strconv.FormatUint(curBlockTime+1, 10))
			})
		})
	})

	t.Run("no deposit", func(t *testing.T) {
		inv.InvokeFail(t, "insufficient balance to create container", "createV2",
			stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee-1, []byte{})

	t.Run("insufficient deposit", func(t *testing.T) {
		inv.InvokeFail(t, "insufficient balance to create container", "createV2",
			stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, 1, []byte{})

	txHash := inv.Invoke(t, id[:], "createV2",
		stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	assertSuccessAPI := func(t *testing.T, id cid.ID, cnrFields []stackitem.Item, cnrBytes []byte, ownerID user.ID) {
		inv.Invoke(t, stackitem.NewStruct(cnrFields), "getInfo", id[:])
		inv.Invoke(t, stackitem.NewBuffer(cnrBytes), "getContainerData", id[:])
		inv.Invoke(t, stackitem.NewBuffer(ownerID[:]), "owner", id[:])
		inv.Invoke(t, stackitem.NewStruct([]stackitem.Item{
			stackitem.NewBuffer(cnrBytes),
			stackitem.NewBuffer([]byte{}),
			stackitem.NewBuffer([]byte{}),
			stackitem.NewBuffer([]byte{}),
		}), "get", id[:])
		zeroQuota := stackitem.NewStruct([]stackitem.Item{stackitem.Make(0), stackitem.Make(0)})
		inv.Invoke(t, zeroQuota, "userQuota", ownerID[:])
		inv.Invoke(t, zeroQuota, "containerQuota", id[:])

		assertContainersOf := func(t *testing.T, owner []byte) {
			s, err := inv.TestInvoke(t, "containersOf", owner)
			require.NoError(t, err)
			require.EqualValues(t, 1, s.Len())
			arr := s.ToArray()
			require.Len(t, arr, 1)
			iter, ok := arr[0].Value().(*storage.Iterator)
			require.True(t, ok)
			var ids [][]byte
			for iter.Next() {
				id, err := iter.Value().TryBytes()
				require.NoError(t, err)
				ids = append(ids, id)
			}
			require.Contains(t, ids, id[:])
		}
		assertContainersOf(t, ownerID[:])
		assertContainersOf(t, nil)
	}

	assertSuccessStorage := func(t *testing.T, id cid.ID, cnrFields []stackitem.Item, cnrBytes []byte, ownerID user.ID) {
		gotStruct, err := stackitem.Deserialize(getStorageItem(slices.Concat([]byte{0}, id[:])))
		require.NoError(t, err)

		require.Equal(t, stackitem.NewStruct(cnrFields), gotStruct)
		require.EqualValues(t, id[:], getStorageItem(slices.Concat([]byte{'o'}, ownerID[:], id[:])))
		require.EqualValues(t, cnrBytes, getStorageItem(slices.Concat([]byte{'x'}, id[:])))
	}

	assertSuccessNotifications := func(t *testing.T, txHash util.Uint256, fee int64, id cid.ID, domain string, ownerID user.ID) {
		res := exec.GetTxExecResult(t, txHash)
		events := res.Events
		if domain != "" {
			require.Len(t, events, 5)
		} else {
			require.Len(t, events, 4)
		}

		committeeAcc := committee.(neotest.MultiSigner).Single(0).Account().ScriptHash() // type asserted above

		if domain != "" {
			assertNotificationEvent(t, events[0], "Transfer", nil, containerContract.Hash[:], big.NewInt(1), []byte(fullDomain))
			events = events[1:]
		}

		ownerAcc := ownerID.ScriptHash()

		require.Equal(t, balanceContract, events[0].ScriptHash)
		assertNotificationEvent(t, events[0], "Transfer", ownerAcc[:], committeeAcc[:], big.NewInt(fee))
		assertNotificationEvent(t, events[1], "TransferX", ownerAcc[:], committeeAcc[:], big.NewInt(fee), slices.Concat([]byte{0x10}, id[:]))
		assertNotificationEvent(t, events[2], "Created", id[:], ownerID[:])
		require.Equal(t, containerContract.Hash, events[3].ScriptHash)
		assertNotificationEvent(t, events[3], "Transfer", nil, ownerAcc[:], big.NewInt(1), id[:])
	}

	// API
	assertSuccessAPI(t, id, cnrFields, cnrBytes, ownerID)
	inv.Invoke(t, 1, "totalSupply")
	inv.Invoke(t, stackitem.Make(1), "count")
	inv.Invoke(t, stackitem.Null{}, "alias", id[:])
	proxyInv.InvokeFail(t, "container does not support meta-on-chain", "submitObjectPut",
		testMeta(t, id), anyMetaSigs)
	nnsInv.InvokeFail(t, "token not found", "getRecords", domainName+".container", int64(recordtype.TXT))

	// storage
	assertSuccessStorage(t, id, cnrFields, cnrBytes, ownerID)
	require.Nil(t, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
	require.Nil(t, getStorageItem(slices.Concat([]byte("nnsHasAlias"), id[:])))

	// notifications
	assertSuccessNotifications(t, txHash, fee, id, "", ownerID)

	inv.Invoke(t, stackitem.Null{}, "remove",
		id[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	inv.InvokeFail(t, containerconst.ErrorDeleted, "createV2",
		stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	t.Run("NNS alias", func(t *testing.T) {
		cnr := containertest.Container()
		cnr.SetOwner(ownerID)

		var d container.Domain
		d.SetName(domainName)

		t.Run("missing zone", func(t *testing.T) {
			d.SetZone("custom-zone")
			cnr.WriteDomain(d)

			inv.InvokeFail(t, "TLD not found", "createV2",
				stackitem.NewStruct(containerToStructFields(cnr)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		d.SetZone("") // default

		cnr.WriteDomain(d)
		cnrFields := containerToStructFields(cnr)
		cnrBytes := cnr.Marshal()
		id := cid.NewFromMarshalledContainer(cnrBytes)

		balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee+aliasFee-1, []byte{})

		t.Run("insufficient deposit", func(t *testing.T) {
			inv.InvokeFail(t, "insufficient balance to create container", "createV2",
				stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, 1, []byte{})

		txHash := inv.Invoke(t, id[:], "createV2",
			stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

		// API
		assertSuccessAPI(t, id, cnrFields, cnrBytes, ownerID)
		inv.Invoke(t, stackitem.Make(fullDomain), "alias", id[:])

		proxyInv.InvokeFail(t, "container does not support meta-on-chain", "submitObjectPut",
			testMeta(t, id), anyMetaSigs)

		nnsInv.Invoke(t, stackitem.NewArray([]stackitem.Item{stackitem.Make(base58.Encode(id[:]))}), "getRecords",
			fullDomain, int64(recordtype.TXT))

		// storage
		assertSuccessStorage(t, id, cnrFields, cnrBytes, ownerID)
		require.Nil(t, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
		require.EqualValues(t, "my-domain.container", getStorageItem(slices.Concat([]byte("nnsHasAlias"), id[:])))

		// notifications
		assertSuccessNotifications(t, txHash, fee+aliasFee, id, fullDomain, ownerID)

		t.Run("name already token", func(t *testing.T) {
			inv.InvokeFail(t, "name is already taken", "createV2",
				stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
	})

	t.Run("consistent meta", func(t *testing.T) {
		cnr := containertest.Container()
		cnr.SetOwner(ownerID)
		cnr.SetAttribute("__NEOFS__METAINFO_CONSISTENCY", "any")

		cnrFields := containerToStructFields(cnr)
		cnrBytes := cnr.Marshal()
		id := cid.NewFromMarshalledContainer(cnrBytes)

		balanceMint(t, exec.CommitteeInvoker(balanceContract), owner, fee, []byte{})

		txHash := inv.Invoke(t, id[:], "createV2",
			stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

		// API
		assertSuccessAPI(t, id, cnrFields, cnrBytes, ownerID)
		inv.Invoke(t, stackitem.Null{}, "alias", id[:])
		proxyInv.Invoke(t, stackitem.Null{}, "submitObjectPut",
			testMeta(t, id), anyMetaSigs)

		// storage
		assertSuccessStorage(t, id, cnrFields, cnrBytes, ownerID)
		require.Equal(t, []byte{}, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
		require.Nil(t, getStorageItem(slices.Concat([]byte("nnsHasAlias"), id[:])))

		// notifications
		assertSuccessNotifications(t, txHash, fee, id, "", ownerID)
	})

	t.Run("contract owner", func(t *testing.T) {
		testContract := deployTestNEP11Receiver(t, exec)
		contractSigner := neotest.NewContractSigner(testContract, func(tx *transaction.Transaction) []any {
			return nil
		})

		ownerID := user.NewFromScriptHash(testContract)

		cnr := containertest.Container()
		cnr.SetOwner(ownerID)
		cnr.SetAttribute("__NEOFS__METAINFO_CONSISTENCY", "any")

		cnrFields := containerToStructFields(cnr)
		cnrBytes := cnr.Marshal()
		id := cid.NewFromMarshalledContainer(cnrBytes)

		balanceMint(t, exec.CommitteeInvoker(balanceContract), contractSigner, fee, []byte{})

		txHash := inv.Invoke(t, id[:], "createV2",
			stackitem.NewStruct(cnrFields), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

		// API
		assertSuccessAPI(t, id, cnrFields, cnrBytes, ownerID)
		inv.Invoke(t, stackitem.Null{}, "alias", id[:])
		proxyInv.Invoke(t, stackitem.Null{}, "submitObjectPut",
			testMeta(t, id), anyMetaSigs)

		// storage
		assertSuccessStorage(t, id, cnrFields, cnrBytes, ownerID)
		require.Equal(t, []byte{}, getStorageItem(slices.Concat([]byte{'m'}, id[:])))
		require.Nil(t, getStorageItem(slices.Concat([]byte("nnsHasAlias"), id[:])))

		// notifications
		assertSuccessNotifications(t, txHash, fee, id, "", ownerID)

		stack, err := exec.CommitteeInvoker(testContract).TestInvoke(t, "get")
		require.NoError(t, err)
		require.EqualValues(t, 1, stack.Len())
		assertEqualItemArray(t, []stackitem.Item{
			stackitem.Null{}, // from
			stackitem.NewBuffer(id[:]),
			stackitem.Null{}, // data
		}, stack.Pop().Array())
	})
}

func TestContainerSymbol(t *testing.T) {
	cnrContract, _, _ := newContainerInvoker(t, true)

	cnrContract.Invoke(t, "FSCNTR", "symbol")
}

func TestContainerDecimals(t *testing.T) {
	cnrContract, _, _ := newContainerInvoker(t, true)

	cnrContract.Invoke(t, 0, "decimals")
}

func TestContainerTotalSupply(t *testing.T) {
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	bc, committee := chain.NewSingle(t)
	exec := neotest.NewExecutor(t, bc, committee, committee)

	deployDefaultNNS(t, exec)
	nmContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	cnrContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, nmContract, cnrContract.Hash)
	deployProxyContract(t, exec)
	exec.DeployContract(t, cnrContract, nil)

	cnrInv := exec.CommitteeInvoker(cnrContract.Hash)

	assert := func(t *testing.T, expected int) {
		cnrInv.Invoke(t, expected, "totalSupply")
	}

	assert(t, 0)

	cnr1 := containertest.Container()
	id1 := cid.NewFromMarshalledContainer(cnr1.Marshal())

	cnrInv.Invoke(t, id1[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr1)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	assert(t, 1)

	cnr2 := containertest.Container()
	id2 := cid.NewFromMarshalledContainer(cnr2.Marshal())

	cnrInv.Invoke(t, id2[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr2)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	assert(t, 2)

	cnrInv.Invoke(t, stackitem.Null{}, "remove", id1[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	assert(t, 1)
}

func TestContainerBalanceOf(t *testing.T) {
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	bc, committee := chain.NewSingle(t)
	exec := neotest.NewExecutor(t, bc, committee, committee)

	deployDefaultNNS(t, exec)
	nmContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	cnrContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, nmContract, cnrContract.Hash)
	deployProxyContract(t, exec)
	exec.DeployContract(t, cnrContract, nil)

	cnrInv := exec.CommitteeInvoker(cnrContract.Hash)

	t.Run("invalid len", func(t *testing.T) {
		for _, tc := range [][]byte{
			{},
			randomBytes(19),
			randomBytes(21),
		} {
			sLen := strconv.Itoa(len(tc))

			t.Run(sLen, func(t *testing.T) {
				cnrInv.InvokeFail(t, "invalid owner len "+sLen, "balanceOf", tc)
			})
		}
	})

	assert := func(t *testing.T, owner user.ID, expected int) {
		ownerAcc := owner.ScriptHash()
		cnrInv.Invoke(t, expected, "balanceOf", ownerAcc[:])
	}

	create := func(t *testing.T, owner user.ID) cid.ID {
		cnr1 := containertest.Container()
		cnr1.SetOwner(owner)
		id := cid.NewFromMarshalledContainer(cnr1.Marshal())

		cnrInv.Invoke(t, id[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr1)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

		return id
	}

	rm := func(t *testing.T, id cid.ID) {
		cnrInv.Invoke(t, stackitem.Null{}, "remove", id[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	}

	owner1 := usertest.ID()
	owner2 := usertest.OtherID(owner1)

	assert(t, owner1, 0)
	assert(t, owner2, 0)

	id1_1 := create(t, owner1)

	assert(t, owner1, 1)

	create(t, owner1)

	assert(t, owner1, 2)

	id2_1 := create(t, owner2)

	assert(t, owner2, 1)

	rm(t, id1_1)

	assert(t, owner1, 1)

	rm(t, id2_1)

	assert(t, owner2, 0)
}

func TestContainerTokensOf(t *testing.T) {
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	bc, committee := chain.NewSingle(t)
	exec := neotest.NewExecutor(t, bc, committee, committee)

	deployDefaultNNS(t, exec)
	nmContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	cnrContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, nmContract, cnrContract.Hash)
	deployProxyContract(t, exec)
	exec.DeployContract(t, cnrContract, nil)

	cnrInv := exec.CommitteeInvoker(cnrContract.Hash)

	t.Run("invalid len", func(t *testing.T) {
		for _, tc := range [][]byte{
			{},
			randomBytes(19),
			randomBytes(21),
		} {
			sLen := strconv.Itoa(len(tc))

			t.Run(sLen, func(t *testing.T) {
				cnrInv.InvokeFail(t, "invalid owner len "+sLen, "tokensOf", tc)
			})
		}
	})

	assert := func(t *testing.T, owner user.ID, expected [][]byte) {
		ownerAcc := owner.ScriptHash()

		s, err := cnrInv.TestInvoke(t, "tokensOf", ownerAcc[:])
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())

		it, ok := s.Pop().Value().(*storage.Iterator)
		require.True(t, ok)

		var actual [][]byte
		for it.Next() {
			id, err := it.Value().TryBytes()
			require.NoError(t, err)

			actual = append(actual, id)
		}

		require.ElementsMatch(t, expected, actual)
	}

	create := func(t *testing.T, owner user.ID) cid.ID {
		cnr := containertest.Container()
		cnr.SetOwner(owner)
		id := cid.NewFromMarshalledContainer(cnr.Marshal())

		cnrInv.Invoke(t, id[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

		return id
	}

	rm := func(t *testing.T, id cid.ID) {
		cnrInv.Invoke(t, stackitem.Null{}, "remove", id[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	}

	owner1 := usertest.ID()
	owner2 := usertest.OtherID(owner1)

	assert(t, owner1, [][]byte{})
	assert(t, owner2, [][]byte{})

	id1 := create(t, owner1)

	assert(t, owner1, [][]byte{id1[:]})

	id2 := create(t, owner1)

	assert(t, owner1, [][]byte{id1[:], id2[:]})

	id3 := create(t, owner2)

	assert(t, owner2, [][]byte{id3[:]})

	rm(t, id1)

	assert(t, owner1, [][]byte{id2[:]})

	rm(t, id3)

	assert(t, owner2, [][]byte{})
}

func TestContainerTransfer(t *testing.T) {
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	bc, committee := chain.NewSingle(t)
	exec := neotest.NewExecutor(t, bc, committee, committee)

	deployDefaultNNS(t, exec)
	nmContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	cnrContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, nmContract, cnrContract.Hash)
	deployProxyContract(t, exec)
	exec.DeployContract(t, cnrContract, nil)

	usr1 := exec.NewAccount(t)
	usr1Acc := usr1.ScriptHash()
	usr2 := exec.NewAccount(t)
	usr2Acc := usr2.ScriptHash()

	cmtInv := exec.CommitteeInvoker(cnrContract.Hash)
	usr1Inv := exec.NewInvoker(cnrContract.Hash, usr1)
	usr2Inv := exec.NewInvoker(cnrContract.Hash, usr2)

	t.Run("invalid receiver len", func(t *testing.T) {
		for _, tc := range [][]byte{
			{},
			randomBytes(19),
			randomBytes(21),
		} {
			sLen := strconv.Itoa(len(tc))

			t.Run(sLen, func(t *testing.T) {
				cmtInv.InvokeFail(t, "invalid receiver len "+sLen, "transfer", tc, []byte("any_id"), nil)
			})
		}
	})

	cnr := containertest.Container()
	cnr.SetOwner(user.NewFromScriptHash(usr1.ScriptHash()))
	id := cid.NewFromMarshalledContainer(cnr.Marshal())

	cmtInv.InvokeFail(t, containerconst.NotFoundError, "transfer", usr2Acc, id[:], nil)

	cmtInv.Invoke(t, id[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	assertGetInfo(t, cmtInv, id, cnr)
	cmtInv.Invoke(t, stackitem.NewBuffer(cnr.Marshal()), "getContainerData", id[:])
	cmtInv.Invoke(t, stackitem.NewBuffer(usr1Acc[:]), "ownerOf", id[:])
	cmtInv.Invoke(t, 1, "balanceOf", usr1Acc)
	cmtInv.Invoke(t, 0, "balanceOf", usr2Acc)

	t.Run("no witness", func(t *testing.T) {
		t.Run("owner", func(t *testing.T) {
			usr2Inv.Invoke(t, false, "transfer", usr2Acc, id[:], nil)
		})
		t.Run("receiver", func(t *testing.T) {
			usr1Inv.Invoke(t, false, "transfer", usr2Acc, id[:], nil)
		})
		t.Run("neither owner nor receiver", func(t *testing.T) {
			cmtInv.Invoke(t, false, "transfer", usr2Acc, id[:], nil)
		})
	})

	t.Run("self-transfer", func(t *testing.T) {
		txHash := usr1Inv.Invoke(t, true, "transfer", usr1Acc, id[:], nil)

		cmtInv.Invoke(t, stackitem.NewBuffer(usr1Acc[:]), "ownerOf", id[:])
		cmtInv.Invoke(t, 1, "balanceOf", usr1Acc)
		cmtInv.Invoke(t, 0, "balanceOf", usr2Acc)

		events := exec.GetTxExecResult(t, txHash).Events
		require.Len(t, events, 1)
		assertNotificationEvent(t, events[0], "Transfer", usr1Acc[:], usr1Acc[:], big.NewInt(1), id[:])
	})

	txHash := exec.NewInvoker(cnrContract.Hash, usr1, usr2).
		Invoke(t, true, "transfer", usr2Acc, id[:], nil)

	cnr.SetOwner(user.NewFromScriptHash(usr2Acc))
	assertGetInfo(t, cmtInv, id, cnr)
	cmtInv.Invoke(t, stackitem.NewBuffer(cnr.Marshal()), "getContainerData", id[:])
	cmtInv.Invoke(t, stackitem.NewBuffer(usr2Acc[:]), "ownerOf", id[:])
	cmtInv.Invoke(t, 0, "balanceOf", usr1Acc)
	cmtInv.Invoke(t, 1, "balanceOf", usr2Acc)

	events := exec.GetTxExecResult(t, txHash).Events
	require.Len(t, events, 1)
	assertNotificationEvent(t, events[0], "Transfer", usr1Acc[:], usr2Acc[:], big.NewInt(1), id[:])

	// contract receiver
	testContract := deployTestNEP11Receiver(t, exec)

	txHash = exec.NewInvoker(cnrContract.Hash, usr2, neotest.NewContractSigner(testContract, func(tx *transaction.Transaction) []any {
		return nil
	})).Invoke(t, true, "transfer", testContract, id[:], "foo")

	cnr.SetOwner(user.NewFromScriptHash(testContract))
	assertGetInfo(t, cmtInv, id, cnr)
	cmtInv.Invoke(t, stackitem.NewBuffer(cnr.Marshal()), "getContainerData", id[:])
	cmtInv.Invoke(t, stackitem.NewBuffer(testContract[:]), "ownerOf", id[:])
	cmtInv.Invoke(t, 0, "balanceOf", usr1Acc)
	cmtInv.Invoke(t, 0, "balanceOf", usr2Acc)
	cmtInv.Invoke(t, 1, "balanceOf", testContract)

	events = exec.GetTxExecResult(t, txHash).Events
	require.Len(t, events, 1)
	assertNotificationEvent(t, events[0], "Transfer", usr2Acc[:], testContract[:], big.NewInt(1), id[:])

	stack, err := exec.CommitteeInvoker(testContract).TestInvoke(t, "get")
	require.NoError(t, err)
	require.EqualValues(t, 1, stack.Len())
	assertEqualItemArray(t, []stackitem.Item{stackitem.NewBuffer(usr2Acc[:]), stackitem.NewBuffer(id[:])}, stack.Pop().Array())

	// after removal
	cmtInv.Invoke(t, stackitem.Null{}, "remove", id[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	usr2Inv.InvokeFail(t, containerconst.ErrorDeleted, "transfer", usr1Acc, id[:], nil)
}

func TestContainerOwnerOf(t *testing.T) {
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	bc, committee := chain.NewSingle(t)
	exec := neotest.NewExecutor(t, bc, committee, committee)

	deployDefaultNNS(t, exec)
	nmContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	cnrContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, nmContract, cnrContract.Hash)
	deployProxyContract(t, exec)
	exec.DeployContract(t, cnrContract, nil)

	cnrInv := exec.CommitteeInvoker(cnrContract.Hash)

	assertNotFound := func(t *testing.T, id cid.ID) {
		cnrInv.InvokeFail(t, containerconst.NotFoundError, "ownerOf", id[:])
	}

	assertOK := func(t *testing.T, id cid.ID, owner user.ID) {
		ownerAcc := owner.ScriptHash()
		cnrInv.Invoke(t, stackitem.NewBuffer(ownerAcc[:]), "ownerOf", id[:])
	}

	create := func(t *testing.T, cnr container.Container, id cid.ID) {
		cnrInv.Invoke(t, id[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	}

	owner1 := usertest.ID()
	owner2 := usertest.OtherID(owner1)

	cnr := containertest.Container()
	cnr.SetOwner(owner1)
	id1 := cid.NewFromMarshalledContainer(cnr.Marshal())

	assertNotFound(t, id1)

	create(t, cnr, id1)

	assertOK(t, id1, owner1)

	cnr.SetOwner(owner2)
	id2 := cid.NewFromMarshalledContainer(cnr.Marshal())

	assertNotFound(t, id2)

	create(t, cnr, id2)

	assertOK(t, id2, owner2)

	cnr.SetOwner(owner2)

	cnrInv.Invoke(t, stackitem.Null{}, "remove", id1[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	cnrInv.InvokeFail(t, containerconst.ErrorDeleted, "ownerOf", id1[:])
}

func TestContainerTokens(t *testing.T) {
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	bc, committee := chain.NewSingle(t)
	exec := neotest.NewExecutor(t, bc, committee, committee)

	deployDefaultNNS(t, exec)
	nmContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	cnrContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, nmContract, cnrContract.Hash)
	deployProxyContract(t, exec)
	exec.DeployContract(t, cnrContract, nil)

	cnrInv := exec.CommitteeInvoker(cnrContract.Hash)

	assert := func(t *testing.T, ids []cid.ID) {
		s, err := cnrInv.TestInvoke(t, "tokens")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())

		it, ok := s.Pop().Value().(*storage.Iterator)
		require.True(t, ok)

		var actual [][]byte
		for it.Next() {
			id, err := it.Value().TryBytes()
			require.NoError(t, err)

			actual = append(actual, id)
		}

		var expected [][]byte
		for i := range ids {
			expected = append(expected, ids[i][:])
		}

		require.ElementsMatch(t, expected, actual)
	}

	assert(t, []cid.ID{})

	create := func(t *testing.T, cnr container.Container, id cid.ID) {
		cnrInv.Invoke(t, id[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	}

	var ids []cid.ID
	for range 5 {
		cnr := containertest.Container()
		id := cid.NewFromMarshalledContainer(cnr.Marshal())

		create(t, cnr, id)

		ids = append(ids, id)
	}

	assert(t, ids)

	cnrInv.Invoke(t, stackitem.Null{}, "remove", ids[0][:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	assert(t, ids[1:])
}

func TestContainerProperties(t *testing.T) {
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	bc, committee := chain.NewSingle(t)
	exec := neotest.NewExecutor(t, bc, committee, committee)

	deployDefaultNNS(t, exec)
	nmContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	cnrContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, nmContract, cnrContract.Hash)
	deployProxyContract(t, exec)
	exec.DeployContract(t, cnrContract, nil)

	cnrInv := exec.CommitteeInvoker(cnrContract.Hash)

	assert := func(t *testing.T, id cid.ID, expected map[string]any) {
		s, err := cnrInv.TestInvoke(t, "properties", id[:])
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())

		m, ok := s.Pop().Item().(*stackitem.Map)
		require.True(t, ok)

		actual := make(map[string]any)
		for _, el := range m.Value().([]stackitem.MapElement) {
			kb, err := el.Key.TryBytes()
			require.NoError(t, err)

			actual[string(kb)] = el.Value.Value()
		}

		require.Len(t, actual, len(expected))

		for k, expVal := range expected {
			gotVal, ok := actual[k]
			require.True(t, ok, k)
			require.EqualValues(t, expVal, gotVal, k)
		}
	}

	owner := usertest.ID()

	var cnr container.Container
	cnr.Init()
	cnr.SetOwner(owner)
	cnr.SetBasicACL(containertest.BasicACL())
	cnr.SetPlacementPolicy(netmaptest.PlacementPolicy())
	cnr.SetAttribute("k1", "v1")
	cnr.SetAttribute("k2", "v2")
	cnr.SetAttribute("name", "any")

	id := cid.NewFromMarshalledContainer(cnr.Marshal())

	cnrInv.InvokeFail(t, containerconst.NotFoundError, "properties", id[:])

	create := func(t *testing.T, cnr container.Container, id cid.ID) {
		cnrInv.Invoke(t, id[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr)), anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	}

	create(t, cnr, id)

	assert(t, id, map[string]any{
		"name": id.String(),
		"k1":   "v1",
		"k2":   "v2",
	})

	cnr.SetName("my-container")

	id = cid.NewFromMarshalledContainer(cnr.Marshal())

	create(t, cnr, id)

	assert(t, id, map[string]any{
		"name": "my-container",
		"k1":   "v1",
		"k2":   "v2",
	})

	cnrInv.Invoke(t, stackitem.Null{}, "remove", id[:], anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

	cnrInv.InvokeFail(t, containerconst.ErrorDeleted, "properties", id[:])
}

func containerToStructFields(cnr container.Container) []stackitem.Item {
	var attrs []stackitem.Item
	for k, v := range cnr.Attributes() {
		attrs = append(attrs, stackitem.NewStruct([]stackitem.Item{
			stackitem.Make(k), stackitem.Make(v),
		}))
	}

	ownerID := cnr.Owner()

	return []stackitem.Item{
		stackitem.NewStruct([]stackitem.Item{
			stackitem.Make(cnr.Version().Major()),
			stackitem.Make(cnr.Version().Minor()),
		}),
		stackitem.Make(ownerID[1:][:20]),
		stackitem.Make(cnr.ProtoMessage().Nonce),
		stackitem.Make(uint32(cnr.BasicACL())),
		stackitem.NewArray(attrs),
		stackitem.NewByteArray(cnr.PlacementPolicy().Marshal()),
	}
}

func assertGetInfo(t testing.TB, inv *neotest.ContractInvoker, id cid.ID, cnr container.Container) {
	vmStack, err := inv.TestInvoke(t, "getInfo", id[:])
	require.NoError(t, err)
	invRes, err := unwrap.Item(&result.Invoke{
		State: vmstate.Halt.String(),
		Stack: vmStack.ToArray(),
	}, nil)
	require.NoError(t, err)
	require.IsType(t, []stackitem.Item(nil), invRes.Value())
	assertEqualItemArray(t, containerToStructFields(cnr), invRes.Value().([]stackitem.Item))
}

func TestSetAttribute(t *testing.T) {
	const anyValidUntil = math.MaxUint32
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	blockChain, committee := chain.NewSingle(t)
	require.Implements(t, (*neotest.MultiSigner)(nil), committee)
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)

	committeeInvoker := exec.CommitteeInvoker(containerContract.Hash)

	owner := exec.NewAccount(t, 200_0000_0000)
	ownerAcc := owner.ScriptHash()
	ownerAddr := user.NewFromScriptHash(ownerAcc)
	cnr := containertest.Container()
	cnr.SetOwner(ownerAddr)

	cnrBytes := cnr.Marshal()
	cID := cid.NewFromMarshalledContainer(cnrBytes)
	inv := exec.CommitteeInvoker(containerContract.Hash)

	t.Run("no alphabet witness", func(t *testing.T) {
		inv := exec.NewInvoker(containerContract.Hash, exec.NewAccount(t))
		inv.InvokeFail(t, "alphabet witness check failed", "setAttribute",
			cID[:], "any_attribute", "any_value", anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("invalid container id", func(t *testing.T) {
		var rules []CORSRule

		pl, err := json.Marshal(rules)
		require.NoError(t, err)

		inv.InvokeFail(t, "container does not exist", "setAttribute", []byte{1, 2, 3}, "CORS", pl,
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("create container", func(t *testing.T) {
		const anyValidDomainName = ""
		const anyValidDomainZone = ""

		_ = committeeInvoker.Invoke(t, stackitem.Null{}, "create",
			cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, false)

		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("passed valid until", func(t *testing.T) {
		curBlockTime := exec.TopBlock(t).Timestamp
		validUntil := curBlockTime / 1000

		inv.InvokeFail(t, "request is valid until "+strconv.FormatUint(validUntil, 10)+"000, now "+strconv.FormatUint(curBlockTime+1, 10),
			"setAttribute", cID[:], "any_attribute", "any_value", curBlockTime/1000,
			anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("update not whitelisted", func(t *testing.T) {
		inv.InvokeFail(t, "attribute is immutable", "setAttribute", cID[:], "my-attribute-123", "attribute-value",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("empty name", func(t *testing.T) {
		inv.InvokeFail(t, "name is empty", "setAttribute", cID[:], "", "attribute-value",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("empty value", func(t *testing.T) {
		inv.InvokeFail(t, "value is empty", "setAttribute", cID[:], "my-attribute-123", "",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("store and remove CORS", func(t *testing.T) {
		var rules = []CORSRule{
			{
				AllowedMethods: []string{"GET"},
				AllowedOrigins: []string{"*"},
				AllowedHeaders: []string{"*"},
				ExposeHeaders:  []string{},
			},
		}

		pl, err := json.Marshal(rules)
		require.NoError(t, err)

		var cnr2 container.Container
		cnr.CopyTo(&cnr2)

		txHash := inv.Invoke(t, nil, "setAttribute", cID[:], "CORS", pl,
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		res := inv.GetTxExecResult(t, txHash)
		require.Len(t, res.Events, 1)
		assertNotificationEvent(t, res.Events[0], "AttributeChanged", cID[:], []byte("CORS"))

		cnr2.SetAttribute("CORS", string(pl))
		assertGetInfo(t, inv, cID, cnr2)

		inv.Invoke(t, nil, "removeAttribute", cID[:], "CORS",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		res = inv.GetTxExecResult(t, txHash)
		require.Len(t, res.Events, 1)
		assertNotificationEvent(t, res.Events[0], "AttributeChanged", cID[:], []byte("CORS"))
		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("lock", func(t *testing.T) {
		cp := cnr
		cnr.CopyTo(&cp)
		cnr := cp

		id := cid.NewFromMarshalledContainer(cnr.Marshal())
		committeeInvoker.Invoke(t, id[:], "createV2", stackitem.NewStruct(containerToStructFields(cnr)), nil, nil, anyValidSessionToken)

		assertFail := func(t *testing.T, val string, exc string) {
			inv.InvokeFail(t, exc, "setAttribute", id[:], "__NEOFS__LOCK_UNTIL", val,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		}

		t.Run("non-int", func(t *testing.T) {
			for _, val := range []string{} {
				assertFail(t, val, "at instruction 1 (SYSCALL): invalid format") // StdLib atoi exception
			}
		})

		t.Run("non-positive", func(t *testing.T) {
			assertFail(t, "-42", "invalid __NEOFS__LOCK_UNTIL attribute: non-positive value -42")
			assertFail(t, "0", "invalid __NEOFS__LOCK_UNTIL attribute: non-positive value 0")
		})

		t.Run("already passed", func(t *testing.T) {
			curBlockTime := exec.TopBlock(t).Timestamp
			curBlockTimeSec := strconv.FormatUint(curBlockTime/1000, 10)
			assertFail(t, curBlockTimeSec, "lock expiration time "+curBlockTimeSec+"000 is not later than current "+strconv.FormatUint(curBlockTime+1, 10))
		})

		curBlockTime := exec.TopBlock(t).Timestamp
		expMs := curBlockTime + 2000
		exp := expMs / 1000
		expStr := strconv.Itoa(int(exp))

		txHash := inv.Invoke(t, nil, "setAttribute", id[:], "__NEOFS__LOCK_UNTIL", expStr,
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		res := inv.GetTxExecResult(t, txHash)
		require.Len(t, res.Events, 1)
		assertNotificationEvent(t, res.Events[0], "AttributeChanged", cID[:], []byte("__NEOFS__LOCK_UNTIL"))

		cnr.SetAttribute("__NEOFS__LOCK_UNTIL", expStr)
		assertGetInfo(t, inv, cID, cnr)

		t.Run("change", func(t *testing.T) {
			newVal := strconv.Itoa(int(exp - 1))
			assertFail(t, newVal, "lock expiration time "+newVal+" is not later than already set "+expStr)

			newVal = strconv.Itoa(int(exp + 1))
			txHash = inv.Invoke(t, nil, "setAttribute", id[:], "__NEOFS__LOCK_UNTIL", newVal,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
			res = inv.GetTxExecResult(t, txHash)
			require.Len(t, res.Events, 1)
			assertNotificationEvent(t, res.Events[0], "AttributeChanged", cID[:], []byte("__NEOFS__LOCK_UNTIL"))

			cnr.SetAttribute("__NEOFS__LOCK_UNTIL", newVal)
			assertGetInfo(t, inv, cID, cnr)
		})
	})
}

func TestRemoveAttribute(t *testing.T) {
	const anyValidUntil = math.MaxUint32
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	blockChain, committee := chain.NewSingle(t)
	require.Implements(t, (*neotest.MultiSigner)(nil), committee)
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)

	owner := exec.NewAccount(t, 200_0000_0000)
	ownerAcc := owner.ScriptHash()
	ownerAddr := user.NewFromScriptHash(ownerAcc)
	cnr := containertest.Container()
	cnr.SetOwner(ownerAddr)

	cnrBytes := cnr.Marshal()
	cID := cid.NewFromMarshalledContainer(cnrBytes)
	inv := exec.CommitteeInvoker(containerContract.Hash)

	t.Run("no alphabet witness", func(t *testing.T) {
		inv := exec.NewInvoker(containerContract.Hash, exec.NewAccount(t))
		inv.InvokeFail(t, "alphabet witness check failed", "removeAttribute",
			cID[:], "any_attribute", anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("invalid container id", func(t *testing.T) {
		inv.InvokeFail(t, "container does not exist", "removeAttribute", []byte{1, 2, 3}, "CORS",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("create container", func(t *testing.T) {
		const anyValidDomainName = ""
		const anyValidDomainZone = ""

		committeeInvoker := exec.CommitteeInvoker(containerContract.Hash)

		_ = committeeInvoker.Invoke(t, stackitem.Null{}, "create",
			cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, false)

		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("passed valid until", func(t *testing.T) {
		curBlockTime := exec.TopBlock(t).Timestamp
		validUntil := curBlockTime / 1000

		inv.InvokeFail(t, "request is valid until "+strconv.FormatUint(validUntil, 10)+"000, now "+strconv.FormatUint(curBlockTime+1, 10),
			"removeAttribute", cID[:], "any_attribute", curBlockTime/1000,
			anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("invalid name", func(t *testing.T) {
		inv.InvokeFail(t, "name is empty", "removeAttribute", cID[:], "",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("attribute is immutable", func(t *testing.T) {
		inv.InvokeFail(t, "attribute is immutable", "removeAttribute", cID[:], "random-name",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("store and remove CORS", func(t *testing.T) {
		var rules = []CORSRule{
			{
				AllowedMethods: []string{"GET"},
				AllowedOrigins: []string{"*"},
				AllowedHeaders: []string{"*"},
				ExposeHeaders:  []string{},
			},
		}

		pl, err := json.Marshal(rules)
		require.NoError(t, err)

		txHash := inv.Invoke(t, nil, "setAttribute", cID[:], "CORS", pl,
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		res := inv.GetTxExecResult(t, txHash)
		require.Len(t, res.Events, 1)
		assertNotificationEvent(t, res.Events[0], "AttributeChanged", cID[:], []byte("CORS"))

		var cnr2 container.Container
		cnr.CopyTo(&cnr2)
		cnr2.SetAttribute("CORS", string(pl))
		assertGetInfo(t, inv, cID, cnr2)

		txHash = inv.Invoke(t, nil, "removeAttribute", cID[:], "CORS",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		res = inv.GetTxExecResult(t, txHash)
		require.Len(t, res.Events, 1)
		assertNotificationEvent(t, res.Events[0], "AttributeChanged", cID[:], []byte("CORS"))
		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("lock", func(t *testing.T) {
		curBlockTime := exec.TopBlock(t).Timestamp
		expMs := curBlockTime + 1000
		exp := expMs / 1000
		expStr := strconv.Itoa(int(exp))

		txHash := inv.Invoke(t, nil, "setAttribute", cID[:], "__NEOFS__LOCK_UNTIL", expStr,
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		res := inv.GetTxExecResult(t, txHash)
		require.Len(t, res.Events, 1)
		assertNotificationEvent(t, res.Events[0], "AttributeChanged", cID[:], []byte("__NEOFS__LOCK_UNTIL"))

		t.Run("not yet passed", func(t *testing.T) {
			inv.InvokeFail(t, "lock expiration time "+strconv.Itoa(int(exp*1000))+" has not passed yet, now "+strconv.Itoa(int(curBlockTime+2)),
				"removeAttribute", cID[:], "__NEOFS__LOCK_UNTIL",
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		blk := exec.NewUnsignedBlock(t)
		blk.Timestamp = expMs
		require.NoError(t, exec.Chain.AddBlock(exec.SignBlock(blk)))

		txHash = inv.Invoke(t, nil, "removeAttribute", cID[:], "__NEOFS__LOCK_UNTIL",
			anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		res = inv.GetTxExecResult(t, txHash)
		require.Len(t, res.Events, 1)
		assertNotificationEvent(t, res.Events[0], "AttributeChanged", cID[:], []byte("__NEOFS__LOCK_UNTIL"))

		assertGetInfo(t, inv, cID, cnr)
	})
}

func TestSetCORSAttribute(t *testing.T) {
	const anyValidUntil = math.MaxUint32
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	blockChain, committee := chain.NewSingle(t)
	require.Implements(t, (*neotest.MultiSigner)(nil), committee)
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)

	owner := exec.NewAccount(t, 200_0000_0000)
	ownerAcc := owner.ScriptHash()
	ownerAddr := user.NewFromScriptHash(ownerAcc)
	cnr := containertest.Container()
	cnr.SetOwner(ownerAddr)
	cnrBytes := cnr.Marshal()
	cID := cid.NewFromMarshalledContainer(cnrBytes)
	inv := exec.CommitteeInvoker(containerContract.Hash)

	t.Run("create container", func(t *testing.T) {
		const anyValidDomainName = ""
		const anyValidDomainZone = ""

		committeeInvoker := exec.CommitteeInvoker(containerContract.Hash)

		_ = committeeInvoker.Invoke(t, stackitem.Null{}, "create",
			cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, false)

		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("allowedMethods", func(t *testing.T) {
		t.Run("empty allowed methods list", func(t *testing.T) {
			inv.InvokeFail(t, "invalid rule #0: empty rule", "setAttribute", cID[:], "CORS", `[{}]`,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		t.Run("empty allowed methods list", func(t *testing.T) {
			inv.InvokeFail(t, "AllowedMethods must be defined", "setAttribute", cID[:], "CORS", `[{"field":[]}]`,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
		t.Run("empty allowed methods list", func(t *testing.T) {
			inv.InvokeFail(t, "AllowedOrigins must be defined", "setAttribute", cID[:], "CORS", `[{"AllowedMethods":["GET"]}]`,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
		t.Run("empty allowed methods list", func(t *testing.T) {
			inv.InvokeFail(t, "AllowedHeaders must be defined", "setAttribute", cID[:], "CORS", `[{"AllowedMethods":["GET"],"AllowedOrigins":["*"]}]`,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
		t.Run("empty allowed methods list", func(t *testing.T) {
			inv.InvokeFail(t, "ExposeHeaders must be defined", "setAttribute", cID[:], "CORS", `[{"AllowedMethods":["GET"],"AllowedOrigins":["*"],"AllowedHeaders":["*"]}]`,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		t.Run("empty allowed methods list", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "AllowedMethods is empty", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		t.Run("invalid allowed method", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"METHOD"},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "invalid rule #0: invalid method METHOD", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		t.Run("empty allowed method", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET", ""},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "invalid rule #0: empty method #1", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
	})

	t.Run("allowedOrigins", func(t *testing.T) {
		t.Run("empty origins list", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "AllowedOrigins is empty", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		t.Run("empty origins", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{""},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "invalid rule #0: empty origin", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		t.Run("to many *", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{"**"},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "invalid rule #0: invalid origin #0: must contain no more than one *", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
	})

	t.Run("allowedHeaders", func(t *testing.T) {
		t.Run("empty headers list ok", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{"*"},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.Invoke(t, nil, "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		t.Run("empty header", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{"*"},
					AllowedHeaders: []string{""},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "invalid rule #0: empty allowed header #0", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})

		t.Run("to many *", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{"*"},
					AllowedHeaders: []string{"**"},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "invalid rule #0: invalid allow header #0: must contain no more than one *", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
	})

	t.Run("exposeHeaders", func(t *testing.T) {
		t.Run("empty header", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{"*"},
					AllowedHeaders: []string{""},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "invalid rule #0: empty allowed header #0", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
	})

	t.Run("maxAgeSeconds", func(t *testing.T) {
		t.Run("negative", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{"*"},
					AllowedHeaders: []string{"*"},
					ExposeHeaders:  []string{},
					MaxAgeSeconds:  -21,
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.InvokeFail(t, "MaxAgeSeconds must be >= 0", "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		})
	})

	t.Run("check valid CORS", func(t *testing.T) {
		t.Run("store CORS", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET"},
					AllowedOrigins: []string{"*"},
					AllowedHeaders: []string{"*"},
					ExposeHeaders:  []string{},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.Invoke(t, nil, "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

			cnr.SetAttribute("CORS", string(pl))

			assertGetInfo(t, inv, cID, cnr)
		})

		t.Run("update CORS", func(t *testing.T) {
			var rules = []CORSRule{
				{
					AllowedMethods: []string{"GET", "POST"},
					AllowedOrigins: []string{"*"},
					AllowedHeaders: []string{"*"},
					ExposeHeaders:  []string{},
				},
			}

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.Invoke(t, nil, "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

			cnr.SetAttribute("CORS", string(pl))

			assertGetInfo(t, inv, cID, cnr)
		})

		t.Run("clean CORS", func(t *testing.T) {
			var rules []CORSRule

			pl, err := json.Marshal(rules)
			require.NoError(t, err)

			inv.Invoke(t, nil, "setAttribute", cID[:], "CORS", pl,
				anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)

			cnr.SetAttribute("CORS", string(pl))

			assertGetInfo(t, inv, cID, cnr)
		})
	})
}

func TestSetS3TAGSAttribute(t *testing.T) {
	const anyValidUntil = math.MaxUint32
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	blockChain, committee := chain.NewSingle(t)
	require.Implements(t, (*neotest.MultiSigner)(nil), committee)
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)

	owner := exec.NewAccount(t, 200_0000_0000)
	ownerAcc := owner.ScriptHash()
	ownerAddr := user.NewFromScriptHash(ownerAcc)
	cnr := containertest.Container()
	cnr.SetOwner(ownerAddr)
	cnrBytes := cnr.Marshal()
	cID := cid.NewFromMarshalledContainer(cnrBytes)
	inv := exec.CommitteeInvoker(containerContract.Hash)

	t.Run("create container", func(t *testing.T) {
		const anyValidDomainName = ""
		const anyValidDomainZone = ""

		committeeInvoker := exec.CommitteeInvoker(containerContract.Hash)

		_ = committeeInvoker.Invoke(t, stackitem.Null{}, "create",
			cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, false)

		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("store and remove S3_TAGS", func(t *testing.T) {
		var tags = map[string]string{
			"key": "value",
		}

		pl, err := json.Marshal(tags)
		require.NoError(t, err)

		var cnr2 container.Container
		cnr.CopyTo(&cnr2)

		inv.Invoke(t, nil, "setAttribute", cID[:], "S3_TAGS", pl, anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		cnr2.SetAttribute("S3_TAGS", string(pl))
		assertGetInfo(t, inv, cID, cnr2)

		inv.Invoke(t, nil, "removeAttribute", cID[:], "S3_TAGS", anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("key is empty", func(t *testing.T) {
		var tags = map[string]string{
			"k1": "value1",
			"":   "value2",
			"k3": "value3",
		}

		pl, err := json.Marshal(tags)
		require.NoError(t, err)

		inv.InvokeFail(t, "tag key is empty", "setAttribute", cID[:], "S3_TAGS", pl, anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("value is empty", func(t *testing.T) {
		var tags = map[string]string{
			"k1": "value1",
			"k2": "value2",
			"k3": "",
		}

		pl, err := json.Marshal(tags)
		require.NoError(t, err)

		inv.InvokeFail(t, "tag k3 value is empty", "setAttribute", cID[:], "S3_TAGS", pl, anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})
}

func TestSetS3Attributes(t *testing.T) {
	const anyValidUntil = math.MaxUint32
	anyValidInvocScript := randomBytes(10)
	anyValidVerifScript := randomBytes(10)
	anyValidSessionToken := randomBytes(10)

	blockChain, committee := chain.NewSingle(t)
	require.Implements(t, (*neotest.MultiSigner)(nil), committee)
	exec := neotest.NewExecutor(t, blockChain, committee, committee)

	deployDefaultNNS(t, exec)
	netmapContract := deployNetmapContract(t, exec, "ContainerFee", 0)
	containerContract := neotest.CompileFile(t, exec.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	deployBalanceContract(t, exec, netmapContract, containerContract.Hash)
	deployProxyContract(t, exec)

	exec.DeployContract(t, containerContract, nil)

	owner := exec.NewAccount(t, 200_0000_0000)
	ownerAcc := owner.ScriptHash()
	ownerAddr := user.NewFromScriptHash(ownerAcc)
	cnr := containertest.Container()
	cnr.SetOwner(ownerAddr)
	cnrBytes := cnr.Marshal()
	cID := cid.NewFromMarshalledContainer(cnrBytes)
	inv := exec.CommitteeInvoker(containerContract.Hash)

	t.Run("create container", func(t *testing.T) {
		const anyValidDomainName = ""
		const anyValidDomainZone = ""

		committeeInvoker := exec.CommitteeInvoker(containerContract.Hash)

		_ = committeeInvoker.Invoke(t, stackitem.Null{}, "create",
			cnrBytes, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken, anyValidDomainName, anyValidDomainZone, false)

		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("store and remove S3_SETTINGS", func(t *testing.T) {
		pl := "{}"

		var cnr2 container.Container
		cnr.CopyTo(&cnr2)

		inv.Invoke(t, nil, "setAttribute", cID[:], "S3_SETTINGS", pl, anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		cnr2.SetAttribute("S3_SETTINGS", pl)
		assertGetInfo(t, inv, cID, cnr2)

		inv.Invoke(t, nil, "removeAttribute", cID[:], "S3_SETTINGS", anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("store and remove S3_NOTIFICATIONS", func(t *testing.T) {
		pl := "{}"

		var cnr2 container.Container
		cnr.CopyTo(&cnr2)

		inv.Invoke(t, nil, "setAttribute", cID[:], "S3_NOTIFICATIONS", pl, anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		cnr2.SetAttribute("S3_NOTIFICATIONS", pl)
		assertGetInfo(t, inv, cID, cnr2)

		inv.Invoke(t, nil, "removeAttribute", cID[:], "S3_NOTIFICATIONS", anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
		assertGetInfo(t, inv, cID, cnr)
	})

	t.Run("invalid S3_SETTINGS", func(t *testing.T) {
		pl := "invalid JSON"
		inv.InvokeFail(t, "", "setAttribute", cID[:], "S3_SETTINGS", pl, anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})

	t.Run("invalid S3_NOTIFICATIONS", func(t *testing.T) {
		pl := "invalid JSON"
		inv.InvokeFail(t, "", "setAttribute", cID[:], "S3_NOTIFICATIONS", pl, anyValidUntil, anyValidInvocScript, anyValidVerifScript, anyValidSessionToken)
	})
}
