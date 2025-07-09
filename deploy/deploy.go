package deploy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"

	"github.com/nspcc-dev/neo-go/pkg/core/block"
	"github.com/nspcc-dev/neo-go/pkg/core/native/noderoles"
	"github.com/nspcc-dev/neo-go/pkg/core/state"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neorpc/result"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/actor"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/invoker"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/notary"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/rolemgmt"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/manifest"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/nef"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"go.uber.org/zap"
)

// Blockchain groups services provided by particular Neo blockchain network
// representing FS chain that are required for its deployment.
type Blockchain interface {
	// RPCActor groups functions needed to compose and send transactions (incl.
	// Notary service requests) to the blockchain.
	notary.RPCActor

	// GetCommittee returns list of public keys owned by Neo blockchain committee
	// members. Resulting list is non-empty, unique and unsorted.
	GetCommittee() (keys.PublicKeys, error)

	// GetContractStateByID returns network state of the smart contract by its ID.
	// GetContractStateByID returns error with 'Unknown contract' substring if
	// requested contract is missing.
	GetContractStateByID(id int32) (*state.Contract, error)

	// GetContractStateByHash is similar to GetContractStateByID but accepts address.
	// GetContractStateByHash may return non-nil state.Contract along with an error.
	GetContractStateByHash(util.Uint160) (*state.Contract, error)

	// SubscribeToNewHeaders opens stream of the new headers persisted in the
	// blockchain and returns channel to read them. The channel is closed only when
	// connection to the blockchain is lost and there will be no more events. Caller
	// subscribes once, regularly reads events from the channel and is resistant to
	// event replay.
	SubscribeToNewHeaders() (<-chan *block.Header, error)

	// SubscribeToNotaryRequests opens stream of the notary request events from the
	// blockchain and returns channel to read them. The channel is closed only when
	// connection to the blockchain is lost and there will be no more events. Caller
	// subscribes once, regularly reads events from the channel and is resistant to
	// event replay.
	SubscribeToNotaryRequests() (<-chan *result.NotaryRequestEvent, error)
}

// CommonDeployPrm groups common deployment parameters of the smart contract.
type CommonDeployPrm struct {
	NEF      nef.File
	Manifest manifest.Manifest
}

// NNSPrm groups deployment parameters of the NeoFS NNS contract.
type NNSPrm struct {
	Common      CommonDeployPrm
	SystemEmail string
}

// AlphabetContractPrm groups deployment parameters of the NeoFS Alphabet contract.
type AlphabetContractPrm struct {
	Common CommonDeployPrm
}

// BalanceContractPrm groups deployment parameters of the NeoFS Balance contract.
type BalanceContractPrm struct {
	Common CommonDeployPrm
}

// ContainerContractPrm groups deployment parameters of the Container contract.
type ContainerContractPrm struct {
	Common CommonDeployPrm
}

// NetmapContractPrm groups deployment parameters of the Netmap contract.
type NetmapContractPrm struct {
	Common CommonDeployPrm
	Config NetworkConfiguration
}

// ProxyContractPrm groups deployment parameters of the NeoFS Proxy contract.
type ProxyContractPrm struct {
	Common CommonDeployPrm
}

// ReputationContractPrm groups deployment parameters of the NeoFS Reputation contract.
type ReputationContractPrm struct {
	Common CommonDeployPrm
}

// Prm groups all parameters of the FS chain deployment procedure.
type Prm struct {
	// Writes progress into the log.
	Logger *zap.Logger

	// Particular Neo blockchain instance to be used as FS chain.
	Blockchain Blockchain

	// Local process account used for transaction signing (must be unlocked).
	LocalAccount *wallet.Account

	// Validator multi-sig account to spread initial GAS to network
	// participants (must be unlocked).
	ValidatorMultiSigAccount *wallet.Account

	NNS NNSPrm

	AlphabetContract   AlphabetContractPrm
	BalanceContract    BalanceContractPrm
	ContainerContract  ContainerContractPrm
	NetmapContract     NetmapContractPrm
	ProxyContract      ProxyContractPrm
	ReputationContract ReputationContractPrm
}

// Deploy initializes Neo network represented by given Prm.Blockchain as FS
// chain and makes it full-featured for NeoFS storage system operation.
//
// Deploy aborts only by context or when a fatal error occurs. Deployment
// progress is logged in detail. It is expected that some situations can be
// changed/fixed on the chain from the outside, so Deploy adapts flexibly and
// does not stop at the moment.
//
// Deployment process is detailed in NeoFS docs. Summary of stages:
//  1. NNS contract deployment
//  2. launch of a notary service for the committee
//  3. initial GAS distribution between committee members
//  4. Alphabet initialization (incl. registration as candidates to validators)
//  5. deployment/update of the NeoFS system contracts
//  6. distribution of all available NEO between the Alphabet contracts
//
// See project documentation for details.
func Deploy(ctx context.Context, prm Prm) error {
	// wrap the parent context into the context of the current function so that
	// transaction wait routines do not leak
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	committee, err := prm.Blockchain.GetCommittee()
	if err != nil {
		return fmt.Errorf("get Neo committee of the network: %w", err)
	}

	slices.SortFunc(committee, (*keys.PublicKey).Cmp)

	// determine a leader
	localPrivateKey := prm.LocalAccount.PrivateKey()
	localPublicKey := localPrivateKey.PublicKey()
	localAccCommitteeIndex := -1

	for i := range committee {
		if committee[i].Equal(localPublicKey) {
			localAccCommitteeIndex = i
			break
		}
	}

	if localAccCommitteeIndex < 0 {
		return errors.New("local account does not belong to any Neo committee member")
	}

	simpleLocalActor, err := actor.NewSimple(prm.Blockchain, prm.LocalAccount)
	if err != nil {
		return fmt.Errorf("init transaction sender from single local account: %w", err)
	}

	committeeLocalActor, err := newCommitteeNotaryActor(prm.Blockchain, prm.LocalAccount, committee)
	if err != nil {
		return fmt.Errorf("create Notary service client sending transactions to be signed by the committee: %w", err)
	}

	chNewBlock := make(chan struct{}, 1)

	err = listenCommitteeNotaryRequests(ctx, listenCommitteeNotaryRequestsPrm{
		logger:               prm.Logger,
		blockchain:           prm.Blockchain,
		localAcc:             prm.LocalAccount,
		committee:            committee,
		validatorMultiSigAcc: prm.ValidatorMultiSigAccount,
	})
	if err != nil {
		return fmt.Errorf("start listener of committee notary requests: %w", err)
	}
	go autoReplenishNotaryBalance(ctx, prm.Logger, prm.Blockchain, prm.LocalAccount, chNewBlock)

	monitor, err := newBlockchainMonitor(prm.Logger, prm.Blockchain, chNewBlock)
	if err != nil {
		return fmt.Errorf("init blockchain monitor: %w", err)
	}

	notaryRolesSet, alphaRolesSet, err := checkCommitteeRoles(prm.Blockchain, monitor, committee)
	if err != nil {
		return fmt.Errorf("pre-check committee roles: %w", err)
	}
	transfersDone := notaryRolesSet // currently the same
	if notaryRolesSet {
		// if it is possible to transfer GAS before any network settings,
		// it should be done as it speeds up expensive operations

		prm.Logger.Info("making initial transfer of funds to the committee...")

		err = makeInitialTransferToCommittee(ctx, makeInitialGASTransferToCommitteePrm{
			logger:               prm.Logger,
			blockchain:           prm.Blockchain,
			monitor:              monitor,
			committee:            committee,
			localAcc:             prm.LocalAccount,
			validatorMultiSigAcc: prm.ValidatorMultiSigAccount,
			tryTransfer:          localAccCommitteeIndex == 0,
		})
		if err != nil {
			return fmt.Errorf("initial transfer funds to the committee: %w", err)
		}

		prm.Logger.Info("initial transfer to the committee successfully done")
	}

	deployNNSPrm := deployNNSContractPrm{
		logger:        prm.Logger,
		blockchain:    prm.Blockchain,
		monitor:       monitor,
		localAcc:      prm.LocalAccount,
		localNEF:      prm.NNS.Common.NEF,
		localManifest: prm.NNS.Common.Manifest,
		systemEmail:   prm.NNS.SystemEmail,
		tryDeploy:     localAccCommitteeIndex == 0, // see below
	}

	// if local node is the first committee member (Az) => deploy NNS contract,
	// otherwise just wait. This will avoid duplication of contracts. This also
	// makes the procedure more centralized, however, in practice, at the start of
	// the network, all members are expected to be healthy and active.

	prm.Logger.Info("initializing NNS contract on the chain...")

	nnsOnChainAddress, err := initNNSContract(ctx, deployNNSPrm)
	if err != nil {
		return fmt.Errorf("init NNS contract on the chain: %w", err)
	}

	prm.Logger.Info("NNS contract successfully initialized on the chain", zap.Stringer("address", nnsOnChainAddress))

	if !notaryRolesSet {
		prm.Logger.Info("enable Notary service for the committee...")

		err = enableNotary(ctx, enableNotaryPrm{
			logger:                 prm.Logger,
			blockchain:             prm.Blockchain,
			monitor:                monitor,
			nnsOnChainAddress:      nnsOnChainAddress,
			systemEmail:            prm.NNS.SystemEmail,
			committee:              committee,
			localAcc:               prm.LocalAccount,
			localAccCommitteeIndex: localAccCommitteeIndex,
		})
		if err != nil {
			return fmt.Errorf("enable Notary service for the committee: %w", err)
		}

		prm.Logger.Info("Notary service successfully enabled for the committee")
	} else {
		prm.Logger.Debug("notary roles are already set")
	}

	if !transfersDone {
		prm.Logger.Info("making initial transfer of funds to the committee...")

		err = makeInitialTransferToCommittee(ctx, makeInitialGASTransferToCommitteePrm{
			logger:               prm.Logger,
			blockchain:           prm.Blockchain,
			monitor:              monitor,
			committee:            committee,
			localAcc:             prm.LocalAccount,
			validatorMultiSigAcc: prm.ValidatorMultiSigAccount,
			tryTransfer:          localAccCommitteeIndex == 0,
		})
		if err != nil {
			return fmt.Errorf("initial transfer funds to the committee: %w", err)
		}

		prm.Logger.Info("initial transfer to the committee successfully done")
	}

	if !alphaRolesSet {
		prm.Logger.Info("initializing NeoFS Alphabet...")

		err = designateNeoFSAlphabet(ctx, initAlphabetPrm{
			logger:     prm.Logger,
			blockchain: prm.Blockchain,
			monitor:    monitor,
			committee:  committee,
			localAcc:   prm.LocalAccount,
		})
		if err != nil {
			return fmt.Errorf("init NeoFS Alphabet: %w", err)
		}

		prm.Logger.Info("NeoFS Alphabet successfully initialized")
	} else {
		prm.Logger.Debug("alphabet roles are already set")
	}

	syncPrm := syncNeoFSContractPrm{
		logger:              prm.Logger,
		blockchain:          prm.Blockchain,
		monitor:             monitor,
		localAcc:            prm.LocalAccount,
		nnsContract:         nnsOnChainAddress,
		systemEmail:         prm.NNS.SystemEmail,
		committee:           committee,
		simpleLocalActor:    simpleLocalActor,
		committeeLocalActor: committeeLocalActor,
	}

	localAccLeads := localAccCommitteeIndex == 0

	// we attempt to deploy contracts (except Alphabet ones) by single committee
	// member (1st for simplicity) to reduce the likelihood of contract duplication
	// in the chain and better predictability of the final address (the address is a
	// function from the sender of the deploying transaction). While this approach
	// is centralized, we still expect any node incl. 1st one to be "healthy".
	// Updates are done concurrently.
	syncPrm.tryDeploy = localAccLeads

	var notaryDisabledExtraUpdateArg bool

	// Deploy NeoFS contracts in strict order. Contracts dependent on others come
	// after.

	err = prefillNNS(ctx, prm.ContainerContract, syncPrm)
	if err != nil {
		return fmt.Errorf("prefilling NNS contract: %w", err)
	}
	prm.Logger.Info("prefilled NNS contract")

	// 1. Proxy
	//
	// It's required for Notary service to work, and also pays for subsequent
	// contract updates.
	syncPrm.localNEF = prm.ProxyContract.Common.NEF
	syncPrm.localManifest = prm.ProxyContract.Common.Manifest
	syncPrm.domainName = domainProxy
	syncPrm.buildExtraDeployArgs = noExtraDeployArgs
	syncPrm.isProxy = true

	prm.Logger.Info("synchronizing Proxy contract with the chain...")

	proxyContractAddress, err := syncNeoFSContract(ctx, syncPrm)
	if err != nil {
		return fmt.Errorf("sync Proxy contract with the chain: %w", err)
	}

	prm.Logger.Info("Proxy contract successfully synchronized", zap.Stringer("address", proxyContractAddress))

	// use on-chain address of the Proxy contract to update all others
	syncPrm.isProxy = false
	syncPrm.proxyContract = proxyContractAddress

	prm.Logger.Info("replenishing the the Proxy contract's balance...")

	err = transferGASToProxy(ctx, transferGASToProxyPrm{
		logger:        prm.Logger,
		blockchain:    prm.Blockchain,
		monitor:       monitor,
		proxyContract: proxyContractAddress,
		committee:     committee,
		localAcc:      prm.LocalAccount,
		tryTransfer:   localAccLeads,
	})
	if err != nil {
		return fmt.Errorf("replenish balance of the Proxy contract: %w", err)
	}

	prm.Logger.Info("Proxy balance successfully replenished")

	prm.Logger.Info("initializing vote for NeoFS Alphabet members to role of validators...")

	err = initVoteForAlphabet(ctx, initVoteForAlphabetPrm{
		logger:        prm.Logger,
		blockchain:    prm.Blockchain,
		monitor:       monitor,
		committee:     committee,
		localAcc:      prm.LocalAccount,
		proxyContract: proxyContractAddress,
	})
	if err != nil {
		return fmt.Errorf("init vote for NeoFS Alphabet members to role of validators: %w", err)
	}

	prm.Logger.Info("vote for NeoFS Alphabet to role of validators successfully initialized")

	// NNS (update)
	//
	// Special contract which is always deployed first, but its update depends on
	// Proxy contract.
	prm.Logger.Info("updating on-chain NNS contract...")

	err = updateNNSContract(ctx, updateNNSContractPrm{
		logger:        prm.Logger,
		blockchain:    prm.Blockchain,
		monitor:       monitor,
		localAcc:      prm.LocalAccount,
		localNEF:      prm.NNS.Common.NEF,
		localManifest: prm.NNS.Common.Manifest,
		systemEmail:   prm.NNS.SystemEmail,
		committee:     committee,
		proxyContract: proxyContractAddress,
	})
	if err != nil {
		return fmt.Errorf("update NNS contract on the chain: %w", err)
	}

	prm.Logger.Info("on-chain NNS contract successfully updated")

	// 2. Netmap
	//
	// Required for:
	//  - Balance
	//  - Container
	netConfig := []any{
		[]byte(MaxObjectSizeConfig), encodeUintConfig(prm.NetmapContract.Config.MaxObjectSize),
		[]byte(BasicIncomeRateConfig), encodeUintConfig(prm.NetmapContract.Config.StoragePrice),
		[]byte(EpochDurationConfig), encodeUintConfig(prm.NetmapContract.Config.EpochDuration),
		[]byte(ContainerFeeConfig), encodeUintConfig(prm.NetmapContract.Config.ContainerFee),
		[]byte(ContainerAliasFeeConfig), encodeUintConfig(prm.NetmapContract.Config.ContainerAliasFee),
		[]byte(EigenTrustIterationsConfig), encodeUintConfig(prm.NetmapContract.Config.EigenTrustIterations),
		[]byte(EigenTrustAlphaConfig), encodeFloatConfig(prm.NetmapContract.Config.EigenTrustAlpha),
		[]byte(InnerRingCandidateFeeConfig), encodeUintConfig(prm.NetmapContract.Config.IRCandidateFee),
		[]byte(WithdrawFeeConfig), encodeUintConfig(prm.NetmapContract.Config.WithdrawalFee),
		[]byte(HomomorphicHashingDisabledKey), encodeBoolConfig(prm.NetmapContract.Config.HomomorphicHashingDisabled),
		[]byte(MaintenanceModeAllowedConfig), encodeBoolConfig(prm.NetmapContract.Config.MaintenanceModeAllowed),
	}

	for i := range prm.NetmapContract.Config.Raw {
		netConfig = append(netConfig, []byte(prm.NetmapContract.Config.Raw[i].Name), prm.NetmapContract.Config.Raw[i].Value)
	}

	syncPrm.localNEF = prm.NetmapContract.Common.NEF
	syncPrm.localManifest = prm.NetmapContract.Common.Manifest
	syncPrm.domainName = domainNetmap
	syncPrm.buildExtraDeployArgs = func() ([]any, error) {
		return []any{
			notaryDisabledExtraUpdateArg,
			util.Uint160{}, // Balance contract address legacy
			util.Uint160{}, // Container contract address legacy
			[]any(nil),     // keys, currently unused
			netConfig,
		}, nil
	}

	prm.Logger.Info("synchronizing Netmap contract with the chain...")

	netmapContractAddress, err := syncNeoFSContract(ctx, syncPrm)
	if err != nil {
		return fmt.Errorf("sync Netmap contract with the chain: %w", err)
	}

	prm.Logger.Info("Netmap contract successfully synchronized", zap.Stringer("address", netmapContractAddress))

	// 3. Balance
	syncPrm.localNEF = prm.BalanceContract.Common.NEF
	syncPrm.localManifest = prm.BalanceContract.Common.Manifest
	syncPrm.domainName = domainBalance
	syncPrm.deployWitness = WitnessValidators
	syncPrm.validatorsDeployAllowedContracts = []util.Uint160{netmapContractAddress}
	syncPrm.buildExtraDeployArgs = noExtraDeployArgs

	prm.Logger.Info("synchronizing Balance contract with the chain...")

	balanceContractAddress, err := syncNeoFSContract(ctx, syncPrm)
	if err != nil {
		return fmt.Errorf("sync Balance contract with the chain: %w", err)
	}

	prm.Logger.Info("Balance contract successfully synchronized", zap.Stringer("address", balanceContractAddress))

	syncPrm.deployWitness = 0
	syncPrm.validatorsDeployAllowedContracts = nil

	// 4. Reputation
	syncPrm.localNEF = prm.ReputationContract.Common.NEF
	syncPrm.localManifest = prm.ReputationContract.Common.Manifest
	syncPrm.domainName = domainReputation
	syncPrm.buildExtraDeployArgs = noExtraDeployArgs

	prm.Logger.Info("synchronizing Reputation contract with the chain...")

	reputationContractAddress, err := syncNeoFSContract(ctx, syncPrm)
	if err != nil {
		return fmt.Errorf("sync Reputation contract with the chain: %w", err)
	}

	prm.Logger.Info("Reputation contract successfully synchronized", zap.Stringer("address", reputationContractAddress))

	// 5. Container
	syncPrm.localNEF = prm.ContainerContract.Common.NEF
	syncPrm.localManifest = prm.ContainerContract.Common.Manifest
	syncPrm.domainName = domainContainer
	syncPrm.deployWitness = WitnessValidatorsAndCommittee
	syncPrm.validatorsDeployAllowedContracts = []util.Uint160{netmapContractAddress}
	syncPrm.buildExtraDeployArgs = func() ([]any, error) {
		return []any{
			notaryDisabledExtraUpdateArg,
			netmapContractAddress,
			balanceContractAddress,
			util.Uint160{}, // neofsid address
			nnsOnChainAddress,
			domainContainers,
		}, nil
	}

	prm.Logger.Info("synchronizing Container contract with the chain...")

	syncPrm.prefilledNNSRecord = true
	containerContractAddress, err := syncNeoFSContract(ctx, syncPrm)
	if err != nil {
		return fmt.Errorf("sync Container contract with the chain: %w", err)
	}

	prm.Logger.Info("Container contract successfully synchronized", zap.Stringer("address", containerContractAddress))

	syncPrm.deployWitness = 0
	syncPrm.validatorsDeployAllowedContracts = nil
	syncPrm.prefilledNNSRecord = false

	// 6. Alphabet
	syncPrm.localNEF = prm.AlphabetContract.Common.NEF
	syncPrm.localManifest = prm.AlphabetContract.Common.Manifest

	var alphabetContracts []util.Uint160

	for ind := range committee {
		syncPrm.tryDeploy = ind == localAccCommitteeIndex // each member deploys its own Alphabet contract
		if !syncPrm.tryDeploy {
			temp := committee[ind].GetScriptHash()
			syncPrm.designatedDeployer = &temp
		}
		syncPrm.domainName = calculateAlphabetContractAddressDomain(ind)
		syncPrm.buildExtraDeployArgs = func() ([]any, error) {
			return []any{
				notaryDisabledExtraUpdateArg,
				netmapContractAddress,
				proxyContractAddress,
				syncPrm.domainName,
				ind,
				len(committee),
			}, nil
		}

		prm.Logger.Info("synchronizing Alphabet contract with the chain...", zap.Int("index", ind))

		alphabetContractAddress, err := syncNeoFSContract(ctx, syncPrm)
		if err != nil {
			return fmt.Errorf("sync Alphabet contract #%d with the chain: %w", ind, err)
		}

		prm.Logger.Info("Alphabet contract successfully synchronized",
			zap.Int("index", ind), zap.Stringer("address", alphabetContractAddress))

		alphabetContracts = append(alphabetContracts, alphabetContractAddress)
	}

	// note: this instruction has no effect anymore, but saves future code added
	// below from potential problems
	syncPrm.designatedDeployer = nil

	prm.Logger.Info("distributing NEO to the Alphabet contracts...")

	err = distributeNEOToAlphabetContracts(ctx, distributeNEOToAlphabetContractsPrm{
		logger:            prm.Logger,
		blockchain:        prm.Blockchain,
		monitor:           monitor,
		proxyContract:     proxyContractAddress,
		committee:         committee,
		localAcc:          prm.LocalAccount,
		alphabetContracts: alphabetContracts,
	})
	if err != nil {
		return fmt.Errorf("distribute NEO to the Alphabet contracts: %w", err)
	}

	prm.Logger.Info("NEO distribution to the Alphabet contracts successfully completed")

	return nil
}

func noExtraDeployArgs() ([]any, error) { return nil, nil }

func encodeUintConfig(v uint64) []byte {
	return stackitem.NewBigInteger(new(big.Int).SetUint64(v)).Bytes()
}

func encodeFloatConfig(v float64) []byte {
	return []byte(strconv.FormatFloat(v, 'f', -1, 64))
}

func encodeBoolConfig(v bool) []byte {
	return stackitem.NewBool(v).Bytes()
}

// returns actor.TransactionCheckerModifier which checks that invocation
// finished with 'HALT' state and, if so, sets transaction's nonce and
// ValidUntilBlock to 100*N and 100*(N+1) correspondingly, where
// 100*N <= current height < 100*(N+1).
func neoFSRuntimeTransactionModifier(getBlockchainHeight func() uint32) actor.TransactionCheckerModifier {
	return func(r *result.Invoke, tx *transaction.Transaction) error {
		err := actor.DefaultCheckerModifier(r, tx)
		if err != nil {
			return err
		}

		curHeight := getBlockchainHeight()
		const span = 100
		n := curHeight / span

		tx.Nonce = n * span

		if math.MaxUint32-span > tx.Nonce {
			tx.ValidUntilBlock = tx.Nonce + span
		} else {
			tx.ValidUntilBlock = math.MaxUint32
		}

		return nil
	}
}

// first is for notary, second is for alphabet.
func checkCommitteeRoles(b Blockchain, m *blockchainMonitor, committee keys.PublicKeys) (bool, bool, error) {
	roleContract := rolemgmt.NewReader(invoker.New(b, nil))

	notaryRole, err := checkRole(noderoles.P2PNotary, roleContract, m, committee)
	if err != nil {
		return false, false, fmt.Errorf("%s role check: %w", noderoles.P2PNotary, err)
	}

	alphaRole, err := checkRole(noderoles.NeoFSAlphabet, roleContract, m, committee)
	if err != nil {
		return false, false, fmt.Errorf("%s role check: %w", noderoles.NeoFSAlphabet, err)
	}

	return notaryRole, alphaRole, nil
}

func checkRole(role noderoles.Role, roleContract *rolemgmt.ContractReader, m *blockchainMonitor, committee keys.PublicKeys) (bool, error) {
	currentRoles, err := roleContract.GetDesignatedByRole(role, m.currentHeight())
	if err != nil {
		return false, fmt.Errorf("reading role: %w", err)
	}

	if len(currentRoles) < len(committee) {
		return false, nil
	}

	for i := range committee {
		if !currentRoles.Contains(committee[i]) {
			return false, nil
		}
	}

	return true, nil
}
