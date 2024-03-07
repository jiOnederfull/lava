package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lavanet/lava/protocol/chainlib"
	"github.com/lavanet/lava/protocol/chaintracker"
	"github.com/lavanet/lava/protocol/common"
	"github.com/lavanet/lava/protocol/lavaprotocol"
	"github.com/lavanet/lava/protocol/lavasession"
	"github.com/lavanet/lava/protocol/metrics"
	"github.com/lavanet/lava/protocol/provideroptimizer"
	"github.com/lavanet/lava/protocol/rpcconsumer"
	"github.com/lavanet/lava/protocol/rpcprovider"
	"github.com/lavanet/lava/protocol/rpcprovider/reliabilitymanager"
	"github.com/lavanet/lava/protocol/rpcprovider/rewardserver"
	"github.com/lavanet/lava/utils"
	"github.com/lavanet/lava/utils/rand"
	"github.com/lavanet/lava/utils/sigs"
	"github.com/stretchr/testify/require"

	spectypes "github.com/lavanet/lava/x/spec/types"
)

var (
	seed       int64
	randomizer *sigs.ZeroReader
)

func TestMain(m *testing.M) {
	// This code will run once before any test cases are executed.
	seed = time.Now().Unix()
	rand.SetSpecificSeed(seed)
	randomizer = sigs.NewZeroReader(seed)
	// Run the actual tests
	exitCode := m.Run()
	if exitCode != 0 {
		utils.LavaFormatDebug("failed tests seed", utils.Attribute{Key: "seed", Value: seed})
	}
	os.Exit(exitCode)
}

func isServerUp(url string) bool {
	client := http.Client{
		Timeout: 20 * time.Millisecond,
	}

	resp, err := client.Get(url)
	if err != nil {
		return false
	}

	defer resp.Body.Close()

	return resp.ContentLength > 0
}

func checkServerStatusWithTimeout(url string, totalTimeout time.Duration) bool {
	startTime := time.Now()

	for time.Since(startTime) < totalTimeout {
		if isServerUp(url) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}

	return false
}

func createInMemoryRewardDb(specs []string) (*rewardserver.RewardDB, error) {
	rewardDB := rewardserver.NewRewardDB()
	for _, spec := range specs {
		db := rewardserver.NewMemoryDB(spec)
		err := rewardDB.AddDB(db)
		if err != nil {
			return nil, err
		}
	}
	return rewardDB, nil
}

func createRpcConsumer(t *testing.T, ctx context.Context, specId string, apiInterface string, account sigs.Account, consumerListenAddress string, epoch uint64, pairingList map[uint64]*lavasession.ConsumerSessionsWithProvider, requiredResponses int, lavaChainID string) *rpcconsumer.RPCConsumerServer {
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle the incoming request and provide the desired response
		w.WriteHeader(http.StatusOK)
	})
	chainParser, _, chainFetcher, _, err := chainlib.CreateChainLibMocks(ctx, specId, apiInterface, serverHandler, "../../", nil)
	require.NoError(t, err)
	require.NotNil(t, chainParser)
	require.NotNil(t, chainFetcher)

	rpcConsumerServer := &rpcconsumer.RPCConsumerServer{}
	rpcEndpoint := &lavasession.RPCEndpoint{
		NetworkAddress:  consumerListenAddress,
		ChainID:         specId,
		ApiInterface:    apiInterface,
		TLSEnabled:      false,
		HealthCheckPath: "",
		Geolocation:     1,
	}
	consumerStateTracker := &mockConsumerStateTracker{}
	finalizationConsensus := lavaprotocol.NewFinalizationConsensus(rpcEndpoint.ChainID)
	_, averageBlockTime, _, _ := chainParser.ChainBlockStats()
	baseLatency := common.AverageWorldLatency / 2
	optimizer := provideroptimizer.NewProviderOptimizer(provideroptimizer.STRATEGY_BALANCED, averageBlockTime, baseLatency, 2)
	consumerSessionManager := lavasession.NewConsumerSessionManager(rpcEndpoint, optimizer, nil, nil)
	consumerSessionManager.UpdateAllProviders(epoch, pairingList)

	consumerConsistency := rpcconsumer.NewConsumerConsistency(specId)
	consumerCmdFlags := common.ConsumerCmdFlags{}
	rpcsonumerLogs, err := metrics.NewRPCConsumerLogs(nil, nil)
	require.NoError(t, err)
	err = rpcConsumerServer.ServeRPCRequests(ctx, rpcEndpoint, consumerStateTracker, chainParser, finalizationConsensus, consumerSessionManager, requiredResponses, account.SK, lavaChainID, nil, rpcsonumerLogs, account.Addr, consumerConsistency, nil, consumerCmdFlags, false, nil, nil)
	require.NoError(t, err)
	// wait for consumer server to be up
	consumerUp := checkServerStatusWithTimeout("http://"+consumerListenAddress, time.Millisecond*50)
	require.True(t, consumerUp)

	return rpcConsumerServer
}

func createRpcProvider(t *testing.T, ctx context.Context, consumerAddress string, specId string, apiInterface string, providerListenAddress string, account sigs.Account, epoch uint64, lavaChainID string, addons []string) (*rpcprovider.RPCProviderServer, *ReplySetter) {
	replySetter := ReplySetter{
		status:       http.StatusOK,
		replyDataBuf: []byte(`{"reply": "REPLY-STUB"}`),
	}
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle the incoming request and provide the desired response
		w.WriteHeader(replySetter.status)
		fmt.Fprint(w, string(replySetter.replyDataBuf))
	})
	chainParser, chainRouter, chainFetcher, _, err := chainlib.CreateChainLibMocks(ctx, specId, apiInterface, serverHandler, "../../", addons)
	require.NoError(t, err)
	require.NotNil(t, chainParser)
	require.NotNil(t, chainFetcher)
	require.NotNil(t, chainRouter)

	rpcProviderServer := &rpcprovider.RPCProviderServer{}
	rpcProviderEndpoint := &lavasession.RPCProviderEndpoint{
		NetworkAddress: lavasession.NetworkAddressData{
			Address:    providerListenAddress,
			KeyPem:     "",
			CertPem:    "",
			DisableTLS: true,
		},
		ChainID:      lavaChainID,
		ApiInterface: apiInterface,
		Geolocation:  1,
		NodeUrls: []common.NodeUrl{
			{
				Url:               "",
				InternalPath:      "",
				AuthConfig:        common.AuthConfig{},
				IpForwarding:      false,
				Timeout:           0,
				Addons:            addons,
				SkipVerifications: []string{},
			},
		},
	}
	rewardDB, err := createInMemoryRewardDb([]string{specId})
	require.NoError(t, err)
	_, averageBlockTime, blocksToFinalization, blocksInFinalizationData := chainParser.ChainBlockStats()
	mockProviderStateTracker := mockProviderStateTracker{consumerAddressForPairing: consumerAddress, averageBlockTime: averageBlockTime}
	rws := rewardserver.NewRewardServer(&mockProviderStateTracker, nil, rewardDB, "badger_test", 1, 10, nil)

	blockMemorySize, err := mockProviderStateTracker.GetEpochSizeMultipliedByRecommendedEpochNumToCollectPayment(ctx)
	require.NoError(t, err)
	providerSessionManager := lavasession.NewProviderSessionManager(rpcProviderEndpoint, blockMemorySize)
	providerPolicy := rpcprovider.GetAllAddonsAndExtensionsFromNodeUrlSlice(rpcProviderEndpoint.NodeUrls)
	chainParser.SetPolicy(providerPolicy, specId, apiInterface)

	blocksToSaveChainTracker := uint64(blocksToFinalization + blocksInFinalizationData)
	chainTrackerConfig := chaintracker.ChainTrackerConfig{
		BlocksToSave:        blocksToSaveChainTracker,
		AverageBlockTime:    averageBlockTime,
		ServerBlockMemory:   rpcprovider.ChainTrackerDefaultMemory + blocksToSaveChainTracker,
		NewLatestCallback:   nil,
		ConsistencyCallback: nil,
		Pmetrics:            nil,
	}
	mockChainFetcher := NewMockChainFetcher(1000, 10, nil)
	chainTracker, err := chaintracker.NewChainTracker(ctx, mockChainFetcher, chainTrackerConfig)
	require.NoError(t, err)
	reliabilityManager := reliabilitymanager.NewReliabilityManager(chainTracker, &mockProviderStateTracker, account.Addr.String(), chainRouter, chainParser)
	rpcProviderServer.ServeRPCRequests(ctx, rpcProviderEndpoint, chainParser, rws, providerSessionManager, reliabilityManager, account.SK, nil, chainRouter, &mockProviderStateTracker, account.Addr, lavaChainID, rpcprovider.DEFAULT_ALLOWED_MISSING_CU, nil, nil)
	listener := rpcprovider.NewProviderListener(ctx, rpcProviderEndpoint.NetworkAddress, "/health")
	err = listener.RegisterReceiver(rpcProviderServer, rpcProviderEndpoint)
	require.NoError(t, err)
	chainParser.Activate()
	chainTracker.RegisterForBlockTimeUpdates(chainParser)
	return rpcProviderServer, &replySetter
}

func TestConsumerProviderBasic(t *testing.T) {
	ctx := context.Background()
	// can be any spec and api interface
	specId := "LAV1"
	apiInterface := spectypes.APIInterfaceTendermintRPC
	epoch := uint64(100)
	requiredResponses := 1
	lavaChainID := "lava"

	numProviders := 1

	consumerListenAddress := "localhost:21111"
	pairingList := map[uint64]*lavasession.ConsumerSessionsWithProvider{}
	type providerData struct {
		account       sigs.Account
		listenAddress string
		server        *rpcprovider.RPCProviderServer
		replySetter   *ReplySetter
	}
	providers := []providerData{}
	for i := 0; i < numProviders; i++ {
		// providerListenAddress := "localhost:111" + strconv.Itoa(i)
		providerListenAddress := "localhost:111" + strconv.Itoa(i)
		account := sigs.GenerateDeterministicFloatingKey(randomizer)
		providerDataI := providerData{account: account, listenAddress: providerListenAddress}
		providers = append(providers, providerDataI)
		pairingList[uint64(i)] = &lavasession.ConsumerSessionsWithProvider{
			PublicLavaAddress: account.Addr.String(),
			Endpoints: []*lavasession.Endpoint{
				{
					NetworkAddress: providerListenAddress,
					Enabled:        true,
					Geolocation:    1,
				},
			},
			Sessions:         map[int64]*lavasession.SingleConsumerSession{},
			MaxComputeUnits:  10000,
			UsedComputeUnits: 0,
			PairingEpoch:     epoch,
		}
	}
	consumerAccount := sigs.GenerateDeterministicFloatingKey(randomizer)
	for i := 0; i < numProviders; i++ {
		ctx := context.Background()
		providerDataI := providers[i]
		providers[i].server, providers[i].replySetter = createRpcProvider(t, ctx, consumerAccount.Addr.String(), specId, apiInterface, providerDataI.listenAddress, providerDataI.account, epoch, lavaChainID, nil)
	}
	rpcconsumerServer := createRpcConsumer(t, ctx, specId, apiInterface, consumerAccount, consumerListenAddress, epoch, pairingList, requiredResponses, lavaChainID)
	require.NotNil(t, rpcconsumerServer)
	client := http.Client{}
	resp, err := client.Get("http://" + consumerListenAddress + "/status")
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	resp.Body.Close()
}
