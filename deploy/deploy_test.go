package deploy

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/config/netmode"
	"github.com/nspcc-dev/neo-go/pkg/consensus"
	"github.com/nspcc-dev/neo-go/pkg/core"
	"github.com/nspcc-dev/neo-go/pkg/core/block"
	"github.com/nspcc-dev/neo-go/pkg/core/mempoolevent"
	"github.com/nspcc-dev/neo-go/pkg/core/native/noderoles"
	"github.com/nspcc-dev/neo-go/pkg/core/storage"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/fixedn"
	"github.com/nspcc-dev/neo-go/pkg/neorpc"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/network"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient"
	"github.com/nspcc-dev/neo-go/pkg/services/notary"
	"github.com/nspcc-dev/neo-go/pkg/services/rpcsrv"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	embeddedcontracts "github.com/nspcc-dev/neofs-contract/contracts"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNeoFSRuntimeTransactionModifier(t *testing.T) {
	t.Run("invalid invocation result state", func(t *testing.T) {
		var res result.Invoke
		res.State = "FAULT" // any non-HALT

		err := neoFSRuntimeTransactionModifier(func() uint32 { return 0 })(&res, new(transaction.Transaction))
		require.Error(t, err)
	})

	var validRes result.Invoke
	validRes.State = "HALT"

	for _, tc := range []struct {
		curHeight     uint32
		expectedNonce uint32
		expectedVUB   uint32
	}{
		{curHeight: 0, expectedNonce: 0, expectedVUB: 100},
		{curHeight: 1, expectedNonce: 0, expectedVUB: 100},
		{curHeight: 99, expectedNonce: 0, expectedVUB: 100},
		{curHeight: 100, expectedNonce: 100, expectedVUB: 200},
		{curHeight: 199, expectedNonce: 100, expectedVUB: 200},
		{curHeight: 200, expectedNonce: 200, expectedVUB: 300},
		{curHeight: math.MaxUint32 - 50, expectedNonce: 100 * (math.MaxUint32 / 100), expectedVUB: math.MaxUint32},
	} {
		m := neoFSRuntimeTransactionModifier(func() uint32 { return tc.curHeight })

		var tx transaction.Transaction

		err := m(&validRes, &tx)
		require.NoError(t, err, tc)
		require.EqualValues(t, tc.expectedNonce, tx.Nonce, tc)
		require.EqualValues(t, tc.expectedVUB, tx.ValidUntilBlock, tc)
	}
}

type deployBlockchain struct {
	*rpcclient.Internal
	*core.Blockchain
}

// SubscribeToNewHeaders implements [deploy.Blockchain] interface.
func (d deployBlockchain) SubscribeToNewHeaders() (<-chan *block.Header, error) {
	ch := make(chan *block.Header)

	_, err := d.ReceiveHeadersOfAddedBlocks(nil, ch)
	if err != nil {
		return nil, err
	}

	return ch, nil
}

// SubscribeToNotaryRequests implements [Blockchain] interface.
func (d deployBlockchain) SubscribeToNotaryRequests() (<-chan *result.NotaryRequestEvent, error) {
	var (
		ch     = make(chan *result.NotaryRequestEvent)
		evtype = mempoolevent.TransactionAdded
	)

	_, err := d.ReceiveNotaryRequests(&neorpc.NotaryRequestFilter{Type: &evtype}, ch)
	if err != nil {
		return nil, err
	}

	return ch, nil
}

func TestContractAutodeploy(t *testing.T) {
	validatorAcc, err := wallet.NewAccount()
	require.NoError(t, err)

	var validatorMulti = new(wallet.Account)
	*validatorMulti = *validatorAcc
	err = validatorMulti.ConvertMultisig(1, []*keys.PublicKey{validatorAcc.PublicKey()})
	require.NoError(t, err)

	var (
		tmpDir     = t.TempDir()
		walletPath = filepath.Join(tmpDir, "wallet.json")
		wlt        = wallet.NewInMemoryWallet()
	)

	err = validatorAcc.Encrypt("", keys.NEP2ScryptParams())
	require.NoError(t, err)
	wlt.Accounts = append(wlt.Accounts, validatorAcc)
	wlt.SetPath(walletPath)
	require.NoError(t, wlt.Save())

	var (
		cfg = config.Config{
			ApplicationConfiguration: config.ApplicationConfiguration{
				RPC: config.RPC{
					BasicService: config.BasicService{
						Enabled: true,
					},
					MaxGasInvoke: fixedn.Fixed8FromInt64(50),
				},
				P2PNotary: config.P2PNotary{
					Enabled: true,
					UnlockWallet: config.Wallet{
						Path:     walletPath,
						Password: "",
					},
				},
				Consensus: config.Consensus{
					Enabled: true,
					UnlockWallet: config.Wallet{
						Path:     walletPath,
						Password: "",
					},
				},
			},
			ProtocolConfiguration: config.ProtocolConfiguration{
				Magic:           netmode.UnitTestNet,
				MaxTimePerBlock: 20 * time.Second,
				Genesis: config.Genesis{
					MaxTraceableBlocks:          1000,
					MaxValidUntilBlockIncrement: 1000 / 2,
					TimePerBlock:                50 * time.Millisecond,
					Roles: map[noderoles.Role]keys.PublicKeys{
						noderoles.P2PNotary: {validatorAcc.PublicKey()},
					},
				},
				P2PSigExtensions:   true,
				StandbyCommittee:   []string{hex.EncodeToString(validatorAcc.PublicKey().Bytes())},
				ValidatorsCount:    1,
				VerifyTransactions: true,
			},
		}
		logger = zaptest.NewLogger(t)
		store  = storage.NewMemoryStore()
	)

	bc, err := core.NewBlockchain(store, config.Blockchain{ProtocolConfiguration: cfg.ProtocolConfiguration}, logger)
	require.NoError(t, err)
	go bc.Run()
	t.Cleanup(bc.Close)

	serverConfig, err := network.NewServerConfig(config.Config{ProtocolConfiguration: cfg.ProtocolConfiguration})
	require.NoError(t, err)
	serverConfig.UserAgent = fmt.Sprintf(config.UserAgentFormat, "something")
	netSrv, err := network.NewServer(serverConfig, bc, bc.GetStateSyncModule(), logger)
	require.NoError(t, err)
	cons, err := consensus.NewService(consensus.Config{
		Logger:                logger,
		Broadcast:             netSrv.BroadcastExtensible,
		Chain:                 bc,
		BlockQueue:            netSrv.GetBlockQueue(),
		ProtocolConfiguration: cfg.ProtocolConfiguration,
		RequestTx:             netSrv.RequestTx,
		StopTxFlow:            netSrv.StopTxFlow,
		Wallet:                cfg.ApplicationConfiguration.Consensus.UnlockWallet,
	})
	require.NoError(t, err)
	netSrv.AddConsensusService(cons, cons.OnPayload, cons.OnTransaction)
	netSrv.Start()

	errCh := make(chan error, 2)
	rpcServer := rpcsrv.New(bc, cfg.ApplicationConfiguration.RPC, netSrv, nil, logger, errCh)
	rpcServer.Start()
	t.Cleanup(rpcServer.Shutdown)

	rpcClient, err := rpcclient.NewInternal(context.TODO(), rpcServer.RegisterLocal)
	require.NoError(t, err)
	require.NoError(t, rpcClient.Init())

	notarySrv, err := notary.NewNotary(notary.Config{
		Chain:   bc,
		Log:     logger,
		MainCfg: cfg.ApplicationConfiguration.P2PNotary,
	}, cfg.ProtocolConfiguration.Magic, netSrv.GetNotaryPool(), netSrv.RelayTxn)
	require.NoError(t, err)
	bc.SetNotary(notarySrv)
	notarySrv.Start()
	t.Cleanup(notarySrv.Shutdown)
	require.True(t, notarySrv.IsAuthorized())

	var (
		bcActor   = deployBlockchain{rpcClient, bc}
		deployPrm = Prm{
			Blockchain:               bcActor,
			LocalAccount:             validatorAcc,
			Logger:                   logger,
			ValidatorMultiSigAccount: validatorMulti,
		}
	)

	err = readEmbeddedContracts(&deployPrm)
	require.NoError(t, err)
	deployPrm.NNS.SystemEmail = "nonexistent@nspcc.io"

	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	err = Deploy(ctx, deployPrm)
	cancel()
	require.NoError(t, err)
}

// Copy-pasted from IR.
func readEmbeddedContracts(deployPrm *Prm) error {
	cs, err := embeddedcontracts.GetFS()
	if err != nil {
		return fmt.Errorf("read embedded contracts: %w", err)
	}

	mRequired := map[string]*CommonDeployPrm{
		"NameService":        &deployPrm.NNS.Common,
		"NeoFS Alphabet":     &deployPrm.AlphabetContract.Common,
		"NeoFS Balance":      &deployPrm.BalanceContract.Common,
		"NeoFS Container":    &deployPrm.ContainerContract.Common,
		"NeoFS Netmap":       &deployPrm.NetmapContract.Common,
		"NeoFS Notary Proxy": &deployPrm.ProxyContract.Common,
		"NeoFS Reputation":   &deployPrm.ReputationContract.Common,
	}

	for i := range cs {
		p, ok := mRequired[cs[i].Manifest.Name]
		if ok {
			p.Manifest = cs[i].Manifest
			p.NEF = cs[i].NEF

			delete(mRequired, cs[i].Manifest.Name)
		}
	}

	if len(mRequired) > 0 {
		missing := make([]string, 0, len(mRequired))
		for name := range mRequired {
			missing = append(missing, name)
		}

		return fmt.Errorf("some contracts are required but not embedded: %v", missing)
	}

	return nil
}
