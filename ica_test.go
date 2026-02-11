// ica_test.go
package interchaintest_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cosmossdk.io/math"

	"github.com/cosmos/go-bip39"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/relayer"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const ibcPath = "osmo-lumera"

func TestOsmosisLumeraICA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ICA e2e test in short mode")
	}

	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	client, network := interchaintest.DockerSetup(t)

	// Choose Lumera version and whether to use local image
	// Set USE_LOCAL_IMAGE=true to use locally built image
	// Set LUMERA_VERSION=v1.9.1 to test older version (default is v1.10.1)
	version := V1_10_1
	if v := os.Getenv("LUMERA_VERSION"); v != "" {
		version = ChainVersion(v)
	}
	useLocal := os.Getenv("USE_LOCAL_IMAGE") == "true"

	lumeraConfig := GetLumeraChainConfig(version, useLocal)

	t.Logf("Testing with Lumera %s (local image: %v)", version, useLocal)

	// ── Build chains ──
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{ChainConfig: OsmosisConfig, NumValidators: &[]int{1}[0], NumFullNodes: &[]int{0}[0]},
		{ChainConfig: lumeraConfig, NumValidators: &[]int{1}[0], NumFullNodes: &[]int{0}[0]},
	})

	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	osmosis, lumera := chains[0].(*cosmos.CosmosChain), chains[1].(*cosmos.CosmosChain)

	// ── Build relayer ──
	r := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t),
		relayer.StartupFlags("-b", "100"),
	).Build(t, client, network)

	// ── Create interchain ──
	ic := interchaintest.NewInterchain().
		AddChain(osmosis).
		AddChain(lumera).
		AddRelayer(r, "relayer").
		AddLink(interchaintest.InterchainLink{
			Chain1:  osmosis,
			Chain2:  lumera,
			Relayer: r,
			Path:    ibcPath,
		})

	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:  t.Name(),
		Client:    client,
		NetworkID: network,
	}))
	t.Cleanup(func() { _ = ic.Close() })

	// ── Start relayer ──
	require.NoError(t, r.StartRelayer(ctx, eRep, ibcPath))
	t.Cleanup(func() { _ = r.StopRelayer(ctx, eRep) })

	// ── Get connection IDs ──
	connections, err := r.GetConnections(ctx, eRep, osmosis.Config().ChainID)
	require.NoError(t, err)
	require.NotEmpty(t, connections)
	osmosisConnectionID := connections[0].ID

	// ── Fund user on Osmosis ──
	// Generate a mnemonic so the same key can be imported into the
	// buildpacket tool's keyring for the Lumera SDK cascade client.
	entropy, err := bip39.NewEntropy(256)
	require.NoError(t, err)
	mnemonic, err := bip39.NewMnemonic(entropy)
	require.NoError(t, err)

	osmosisUser, err := interchaintest.GetAndFundTestUserWithMnemonic(
		ctx, "ica-user", mnemonic, math.NewInt(10_000_000_000), osmosis,
	)
	require.NoError(t, err)

	// ── Sub-tests ──
	t.Run("RegisterICA", func(t *testing.T) {
		testRegisterICA(t, ctx, osmosis, lumera, r, eRep, osmosisUser, osmosisConnectionID, mnemonic)
	})
}

func testRegisterICA(
	t *testing.T, ctx context.Context,
	osmosis, lumera *cosmos.CosmosChain,
	r ibc.Relayer, eRep *testreporter.RelayerExecReporter,
	user ibc.Wallet, connectionID, mnemonic string,
) {
	// ── Step 1: Register ICA from Osmosis ──
	registerCmd := []string{
		osmosis.Config().Bin, "tx", "interchain-accounts", "controller",
		"register", connectionID,
		"--from", user.KeyName(),
		"--gas", "auto",
		"--gas-adjustment", "1.5",
		"--gas-prices", osmosis.Config().GasPrices,
		"-y",
		"--chain-id", osmosis.Config().ChainID,
		"--node", osmosis.GetRPCAddress(),
		"--home", osmosis.HomeDir(),
		"--keyring-backend", "test",
		"--output", "json",
	}
	stdout, _, err := osmosis.Exec(ctx, registerCmd, nil)
	require.NoError(t, err)
	t.Logf("Register ICA tx: %s", string(stdout))

	// Verify tx was accepted on-chain
	var registerTxResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(stdout, &registerTxResp); err == nil {
		require.Equal(t, 0, registerTxResp.Code, "Register ICA tx should succeed, got code %d", registerTxResp.Code)
	}

	// ── Step 2: Wait for ICA channel to open ──
	// The relayer is running and will complete the channel handshake automatically
	err = testutil.WaitForBlocks(ctx, 15, osmosis, lumera)
	require.NoError(t, err)

	// ── Step 3: Query the ICA address on Lumera ──
	icaAddr := queryICAAddress(t, ctx, osmosis, connectionID, user.FormattedAddress())
	require.NotEmpty(t, icaAddr, "ICA address should not be empty")
	t.Logf("ICA address on Lumera: %s", icaAddr)

	// ── Step 4: Fund ICA via direct bank send on Lumera ──
	fundICA(t, ctx, lumera, icaAddr)

	// ── Step 5: Execute MsgRequestAction via ICA ──
	t.Run("ExecuteAction", func(t *testing.T) {
		testExecuteActionViaICA(t, ctx, osmosis, lumera, r, eRep, user, connectionID, icaAddr, mnemonic)
	})
}

func queryICAAddress(t *testing.T, ctx context.Context, controller *cosmos.CosmosChain, connectionID, owner string) string {
	cmd := []string{
		controller.Config().Bin, "q", "interchain-accounts", "controller",
		"interchain-account", owner, connectionID,
		"--node", controller.GetRPCAddress(),
		"--output", "json",
	}
	stdout, _, err := controller.Exec(ctx, cmd, nil)
	require.NoError(t, err)

	var resp struct {
		Address string `json:"address"`
	}
	require.NoError(t, json.Unmarshal(stdout, &resp))
	return resp.Address
}

func fundICA(
	t *testing.T, ctx context.Context,
	lumera *cosmos.CosmosChain,
	icaAddr string,
) {
	// Fund the ICA with LUME via direct send on Lumera
	lumeraUsers := interchaintest.GetAndFundTestUsers(t, ctx, "funder", math.NewInt(50_000_000_000), lumera)
	funder := lumeraUsers[0]

	sendCmd := []string{
		lumera.Config().Bin, "tx", "bank", "send",
		funder.KeyName(), icaAddr, "10000000000ulume",
		"--from", funder.KeyName(),
		"--gas", "auto",
		"--gas-adjustment", "1.5",
		"--gas-prices", lumera.Config().GasPrices,
		"-y",
		"--chain-id", lumera.Config().ChainID,
		"--node", lumera.GetRPCAddress(),
		"--home", lumera.HomeDir(),
		"--keyring-backend", "test",
	}
	_, _, err := lumera.Exec(ctx, sendCmd, nil)
	require.NoError(t, err)

	err = testutil.WaitForBlocks(ctx, 3, lumera)
	require.NoError(t, err)

	// Verify balance
	bal, err := lumera.GetBalance(ctx, icaAddr, "ulume")
	require.NoError(t, err)
	require.True(t, bal.GT(math.ZeroInt()), "ICA should have LUME balance")
	t.Logf("ICA balance: %s ulume", bal.String())
}

// buildpacketToolDir returns the absolute path to the tools/buildpacket directory.
func buildpacketToolDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "tools", "buildpacket")
}

// buildBuildpacketTool compiles the buildpacket helper binary and returns
// the path to the resulting executable.
func buildBuildpacketTool(t *testing.T) string {
	t.Helper()
	toolDir := buildpacketToolDir()
	binary := filepath.Join(t.TempDir(), "buildpacket")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = toolDir
	cmd.Stderr = os.Stderr
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to build buildpacket tool: %s", string(out))
	return binary
}

func testExecuteActionViaICA(
	t *testing.T, ctx context.Context,
	osmosis, lumera *cosmos.CosmosChain,
	r ibc.Relayer, eRep *testreporter.RelayerExecReporter,
	user ibc.Wallet, connectionID, icaAddr, mnemonic string,
) {
	// ── Create a real test file for cascade ──
	tmpFile, err := os.CreateTemp("", "ica-cascade-test-*.bin")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	testData := make([]byte, 1024) // 1 KB payload
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	_, err = tmpFile.Write(testData)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	// ── Build the buildpacket helper tool ──
	toolBinary := buildBuildpacketTool(t)

	// ── Get Lumera host gRPC address ──
	grpcAddr := lumera.GetHostGRPCAddress()
	require.NotEmpty(t, grpcAddr, "Lumera host gRPC address must be available")
	grpcAddr = strings.Replace(grpcAddr, "0.0.0.0", "localhost", 1)
	t.Logf("Lumera gRPC address: %s", grpcAddr)

	// ── Run buildpacket tool to create ICA packet with real SDK data ──
	toolCmd := exec.CommandContext(ctx, toolBinary,
		"--mnemonic", mnemonic,
		"--ica-address", icaAddr,
		"--grpc-addr", grpcAddr,
		"--chain-id", lumera.Config().ChainID,
		"--file", tmpFile.Name(),
		"--owner-hrp", "osmo",
	)
	var stderrBuf strings.Builder
	toolCmd.Stderr = &stderrBuf
	packetJSON, err := toolCmd.Output()
	t.Logf("buildpacket stderr:\n%s", stderrBuf.String())
	require.NoError(t, err, "buildpacket tool failed: %s", stderrBuf.String())
	require.NotEmpty(t, packetJSON, "buildpacket produced empty output")
	t.Logf("ICA packet data: %s", string(packetJSON))

	// Write packet data file to the node's volume
	packetFile := "ica_packet.json"
	err = osmosis.GetNode().WriteFile(ctx, packetJSON, packetFile)
	require.NoError(t, err)

	// ── Send via ICA ──
	packetFilePath := osmosis.HomeDir() + "/" + packetFile
	sendTxCmd := []string{
		osmosis.Config().Bin, "tx", "interchain-accounts", "controller",
		"send-tx", connectionID, packetFilePath,
		"--from", user.KeyName(),
		"--gas", "auto",
		"--gas-adjustment", "2.0",
		"--gas-prices", osmosis.Config().GasPrices,
		"-y",
		"--chain-id", osmosis.Config().ChainID,
		"--node", osmosis.GetRPCAddress(),
		"--home", osmosis.HomeDir(),
		"--keyring-backend", "test",
		"--output", "json",
	}
	stdout, _, err := osmosis.Exec(ctx, sendTxCmd, nil)
	require.NoError(t, err)
	t.Logf("ICA SendTx broadcast result: %s", string(stdout))

	// Parse broadcast response and get tx hash
	var broadcastResp struct {
		TxHash string `json:"txhash"`
		Code   int    `json:"code"`
		RawLog string `json:"raw_log"`
	}
	require.NoError(t, json.Unmarshal(stdout, &broadcastResp), "Failed to parse send-tx response: %s", string(stdout))
	require.Equal(t, 0, broadcastResp.Code, "send-tx broadcast failed: %s", broadcastResp.RawLog)
	t.Logf("ICA SendTx tx hash: %s", broadcastResp.TxHash)

	// Wait for tx to be included in a block, then check execution result
	err = testutil.WaitForBlocks(ctx, 3, osmosis)
	require.NoError(t, err)

	queryTxCmd := []string{
		osmosis.Config().Bin, "q", "tx", broadcastResp.TxHash,
		"--node", osmosis.GetRPCAddress(),
		"--output", "json",
	}
	txResult, _, err := osmosis.Exec(ctx, queryTxCmd, nil)
	if err != nil {
		t.Logf("WARNING: could not query tx result: %v", err)
	} else {
		var txResp struct {
			Code   int    `json:"code"`
			RawLog string `json:"raw_log"`
		}
		if err := json.Unmarshal(txResult, &txResp); err == nil {
			t.Logf("ICA SendTx execution: code=%d raw_log=%s", txResp.Code, txResp.RawLog)
			require.Equal(t, 0, txResp.Code, "ICA SendTx execution failed: %s", txResp.RawLog)
		}
	}

	// ── Wait for relay + execution ──
	err = testutil.WaitForBlocks(ctx, 10, osmosis, lumera)
	require.NoError(t, err)

	// Flush ICA channel packets
	require.NoError(t, r.Flush(ctx, eRep, ibcPath, "channel-1"))
	err = testutil.WaitForBlocks(ctx, 5, osmosis, lumera)
	require.NoError(t, err)

	// ── Verify action was created on Lumera ──
	verifyActionCreated(t, ctx, lumera, icaAddr)
}

func verifyActionCreated(t *testing.T, ctx context.Context, lumera *cosmos.CosmosChain, creator string) {
	// Query actions by creator or list recent actions
	queryCmd := []string{
		lumera.Config().Bin, "q", "action", "list-actions",
		"--node", lumera.GetRPCAddress(),
		"--output", "json",
	}
	stdout, _, err := lumera.Exec(ctx, queryCmd, nil)
	require.NoError(t, err)
	t.Logf("list-actions response: %s", string(stdout))
	t.Logf("Expected creator: %s", creator)

	var resp struct {
		Actions []struct {
			Creator    string `json:"creator"`
			ActionID   string `json:"actionID"`
			ActionType string `json:"actionType"`
			State      string `json:"state"`
		} `json:"actions"`
	}
	require.NoError(t, json.Unmarshal(stdout, &resp))
	t.Logf("Found %d actions total", len(resp.Actions))

	// Find the action created by our ICA
	found := false
	for _, a := range resp.Actions {
		t.Logf("  Action: ID=%s Creator=%s Type=%s State=%s", a.ActionID, a.Creator, a.ActionType, a.State)
		if a.Creator == creator {
			found = true
			t.Logf("Matched ICA action: ID=%s Type=%s State=%s", a.ActionID, a.ActionType, a.State)
			require.Equal(t, "ACTION_TYPE_CASCADE", a.ActionType)
			break
		}
	}
	require.True(t, found, "Action created by ICA address should exist on Lumera")
}
