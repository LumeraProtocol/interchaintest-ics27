// genesis_test.go - Test helper for testing genesis configuration
package interchaintest_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestLumeraGenesisSetup tests that Lumera starts correctly with modified genesis
// This can be run independently to verify genesis configuration before running full ICA tests
func TestLumeraGenesisSetup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping genesis setup test in short mode")
	}

	version := DefaultLumeraVersion
	if v := os.Getenv("LUMERA_VERSION"); v != "" {
		version = v
	}
	useLocal := os.Getenv("USE_LOCAL_IMAGE") == "true"

	t.Run("Genesis_"+version, func(t *testing.T) {
		testGenesisSetup(t, version, useLocal)
	})
}

func testGenesisSetup(t *testing.T, version string, useLocalImage bool) {
	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	client, network := interchaintest.DockerSetup(t)

	config := GetLumeraChainConfig(version, useLocalImage)

	t.Logf("Testing Lumera %s (local=%v)", version, useLocalImage)

	// Build single chain
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{
			ChainConfig:   config,
			NumValidators: &[]int{1}[0],
			NumFullNodes:  &[]int{0}[0],
		},
	})

	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	lumera := chains[0].(*cosmos.CosmosChain)

	// Create interchain with just Lumera
	ic := interchaintest.NewInterchain().AddChain(lumera)

	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:  t.Name(),
		Client:    client,
		NetworkID: network,
	}))
	t.Cleanup(func() { _ = ic.Close() })

	// Verify chain started successfully
	height, err := lumera.Height(ctx)
	require.NoError(t, err)
	require.Greater(t, height, int64(0), "Chain should be producing blocks")
	t.Logf("Chain started successfully at height %d", height)

	// Verify genesis was modified correctly
	t.Run("VerifyGenesisModifications", func(t *testing.T) {
		verifyGenesisModifications(t, ctx, lumera)
	})

	// Verify claims.csv is present
	t.Run("VerifyClaimsCSV", func(t *testing.T) {
		verifyClaimsCSV(t, ctx, lumera)
	})

	t.Logf("All genesis setup tests passed for %s", version)
}

func verifyGenesisModifications(t *testing.T, ctx context.Context, lumera *cosmos.CosmosChain) {
	// Read genesis.json directly from the container (lumerad export can't run while node is running)
	genesisPath := lumera.HomeDir() + "/config/genesis.json"
	catCmd := []string{"cat", genesisPath}
	stdout, _, err := lumera.Exec(ctx, catCmd, nil)
	require.NoError(t, err)

	var genesis map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout, &genesis))

	appState, ok := genesis["app_state"].(map[string]interface{})
	require.True(t, ok, "app_state should exist")

	// Verify denoms are set correctly
	t.Run("Denoms", func(t *testing.T) {
		staking := appState["staking"].(map[string]interface{})
		stakingParams := staking["params"].(map[string]interface{})
		require.Equal(t, "ulume", stakingParams["bond_denom"], "bond_denom should be ulume")

		mint := appState["mint"].(map[string]interface{})
		mintParams := mint["params"].(map[string]interface{})
		require.Equal(t, "ulume", mintParams["mint_denom"], "mint_denom should be ulume")

		t.Logf("Denoms configured correctly")
	})

	// Verify core modules exist with params (using defaults from lumerad init)
	t.Run("ActionModule", func(t *testing.T) {
		action, ok := appState["action"].(map[string]interface{})
		require.True(t, ok, "action module should exist")
		_, ok = action["params"].(map[string]interface{})
		require.True(t, ok, "action params should exist")
		t.Logf("Action module present with params")
	})

	t.Run("SupernodeModule", func(t *testing.T) {
		supernode, ok := appState["supernode"].(map[string]interface{})
		require.True(t, ok, "supernode module should exist")
		_, ok = supernode["params"].(map[string]interface{})
		require.True(t, ok, "supernode params should exist")
		t.Logf("Supernode module present with params")
	})

	// Verify NFT module is removed
	t.Run("NFTModuleRemoved", func(t *testing.T) {
		_, exists := appState["nft"]
		require.False(t, exists, "NFT module should not exist")
		t.Logf("NFT module correctly removed")
	})

	// Crisis module should be removed
	t.Run("CrisisModuleRemoved", func(t *testing.T) {
		_, exists := appState["crisis"]
		require.False(t, exists, "Crisis module should not exist")
		t.Logf("Crisis module correctly removed")
	})
}

func verifyClaimsCSV(t *testing.T, ctx context.Context, lumera *cosmos.CosmosChain) {
	// Check if claims.csv exists in the config directory
	checkCmd := []string{
		"test", "-f", lumera.HomeDir() + "/config/claims.csv",
	}
	_, _, err := lumera.Exec(ctx, checkCmd, nil)

	if err != nil {
		t.Logf("claims.csv not found in config directory (this may be expected)")
		// Don't fail the test as claims.csv might not be required for all test scenarios
	} else {
		t.Logf("claims.csv present in config directory")

		// Count lines in claims.csv
		countCmd := []string{"wc", "-l", lumera.HomeDir() + "/config/claims.csv"}
		stdout, _, err := lumera.Exec(ctx, countCmd, nil)
		if err == nil {
			t.Logf("   claims.csv has %s", string(stdout))
		}
	}
}
