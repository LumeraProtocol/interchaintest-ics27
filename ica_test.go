// ica_test.go — End-to-end test for ICS-27 (Interchain Accounts) between Osmosis
// and Lumera. Proves that a user on Osmosis (controller chain) can register an
// interchain account on Lumera (host chain) and use it to submit a cascade
// storage action (MsgRequestAction) — the full cross-chain flow.
//
// Test topology:
//
//	Osmosis (controller) ──IBC──▶ Lumera (host)
//	   │                              │
//	   │  1. register ICA             │
//	   │  2. send-tx (ICA packet) ──▶ │  3. execute MsgRequestAction
//	   │                              │  4. action created on-chain
//
// The buildpacket helper tool (tools/buildpacket/) is compiled and executed as
// a subprocess. It lives in a separate Go module to avoid ibc-go v8 vs v10
// init() conflicts with interchaintest.
package interchaintest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

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

// ibcPath is the relayer path name linking Osmosis and Lumera.
const ibcPath = "osmo-lumera"

// TestOsmosisLumeraICA spins up Osmosis + Lumera in Docker, connects them via
// IBC, registers an interchain account, and executes a cascade action through it.
func TestOsmosisLumeraICA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ICA e2e test in short mode")
	}

	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	client, network := interchaintest.DockerSetup(t)

	// Choose Lumera version and whether to use local image
	// Override via LUMERA_VERSION env var (default defined in Makefile / DefaultLumeraVersion)
	version := DefaultLumeraVersion
	if v := os.Getenv("LUMERA_VERSION"); v != "" {
		version = v
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
	// We generate a mnemonic (rather than letting interchaintest create one)
	// because the same key must later be imported into the buildpacket tool's
	// keyring to sign the cascade MsgRequestAction on behalf of the ICA.
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
	// This initiates the ICS-27 channel handshake. The relayer will complete
	// INIT → TRY → ACK → CONFIRM asynchronously in the background.
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
		Code   int    `json:"code"`
		RawLog string `json:"raw_log"`
	}
	require.NoError(t, json.Unmarshal(stdout, &registerTxResp), "failed to parse register ICA tx response: %s", string(stdout))
	require.Equal(t, 0, registerTxResp.Code, "Register ICA tx failed: %s", registerTxResp.RawLog)

	// ── Step 2: Poll until the ICA address is registered ──
	// The relayer completes the channel handshake asynchronously; poll instead
	// of waiting a fixed number of blocks.
	var icaAddr string
	require.Eventually(t, func() bool {
		addr, err := tryQueryICAAddress(ctx, osmosis, connectionID, user.FormattedAddress())
		if err != nil || addr == "" {
			return false
		}
		icaAddr = addr
		return true
	}, 2*time.Minute, 3*time.Second, "ICA address was not registered in time")
	t.Logf("ICA address on Lumera: %s", icaAddr)

	// ── Step 3: Fund ICA via direct bank send on Lumera ──
	// The ICA address exists on Lumera but has no tokens. We fund it directly
	// on the host chain so it can pay gas for the MsgRequestAction later.
	fundICA(t, ctx, lumera, icaAddr)

	// ── Step 4: Execute MsgRequestAction via ICA ──
	t.Run("ExecuteAction", func(t *testing.T) {
		testExecuteActionViaICA(t, ctx, osmosis, lumera, r, eRep, user, connectionID, icaAddr, mnemonic)
	})
}

// tryQueryICAAddress queries the ICA address, returning ("", err) if not yet available.
func tryQueryICAAddress(ctx context.Context, controller *cosmos.CosmosChain, connectionID, owner string) (string, error) {
	cmd := []string{
		controller.Config().Bin, "q", "interchain-accounts", "controller",
		"interchain-account", owner, connectionID,
		"--node", controller.GetRPCAddress(),
		"--output", "json",
	}
	stdout, _, err := controller.Exec(ctx, cmd, nil)
	if err != nil {
		return "", err
	}

	var resp struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return "", fmt.Errorf("unmarshal ICA query response: %w", err)
	}
	return resp.Address, nil
}

// findICAChannel returns the channel ID for the ICA controller port on the given chain.
func findICAChannel(t *testing.T, ctx context.Context, r ibc.Relayer, eRep *testreporter.RelayerExecReporter, chainID string) string {
	t.Helper()
	channels, err := r.GetChannels(ctx, eRep, chainID)
	require.NoError(t, err)

	for _, ch := range channels {
		if strings.HasPrefix(ch.PortID, "icacontroller-") {
			t.Logf("Found ICA channel: %s (port: %s)", ch.ChannelID, ch.PortID)
			return ch.ChannelID
		}
	}
	t.Fatalf("no ICA controller channel found on chain %s (channels: %+v)", chainID, channels)
	return ""
}

// fundICA creates a funder wallet on Lumera and sends tokens directly to the
// ICA address. This is a host-chain-local operation (no IBC involved) — it
// simply ensures the ICA has enough gas to execute messages sent via ICS-27.
func fundICA(
	t *testing.T, ctx context.Context,
	lumera *cosmos.CosmosChain,
	icaAddr string,
) {
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
		"--output", "json",
	}
	stdout, _, err := lumera.Exec(ctx, sendCmd, nil)
	require.NoError(t, err)

	var sendResp struct {
		Code   int    `json:"code"`
		RawLog string `json:"raw_log"`
	}
	require.NoError(t, json.Unmarshal(stdout, &sendResp), "failed to parse bank send response: %s", string(stdout))
	require.Equal(t, 0, sendResp.Code, "bank send to ICA failed: %s", sendResp.RawLog)

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

var (
	buildpacketOnce   sync.Once
	buildpacketBinary string
	buildpacketErr    error
)

// buildBuildpacketTool compiles the buildpacket helper binary once and returns
// the path to the resulting executable. Subsequent calls reuse the cached binary.
func buildBuildpacketTool(t *testing.T) string {
	t.Helper()
	buildpacketOnce.Do(func() {
		toolDir := buildpacketToolDir()
		// Use os.TempDir so the binary outlives any single t.TempDir()
		binary := filepath.Join(os.TempDir(), "buildpacket-test")

		cmd := exec.Command("go", "build", "-o", binary, ".")
		cmd.Dir = toolDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildpacketErr = fmt.Errorf("failed to build buildpacket tool: %s", string(out))
			return
		}
		buildpacketBinary = binary
	})
	require.NoError(t, buildpacketErr)
	return buildpacketBinary
}

// testExecuteActionViaICA builds and submits a cascade MsgRequestAction through
// the ICA channel. The flow is:
//  1. Create a test file (simulates user data for cascade storage)
//  2. Run the buildpacket tool to construct MsgRequestAction + wrap it in an ICA CosmosTx packet
//  3. Submit the packet from Osmosis via "send-tx" (controller → host)
//  4. Wait for the relayer to deliver + execute the packet on Lumera
//  5. Verify that the action was created on Lumera with the correct type
func testExecuteActionViaICA(
	t *testing.T, ctx context.Context,
	osmosis, lumera *cosmos.CosmosChain,
	r ibc.Relayer, eRep *testreporter.RelayerExecReporter,
	user ibc.Wallet, connectionID, icaAddr, mnemonic string,
) {
	// ── Create a test file for cascade storage ──
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
	// This is a separate Go binary (tools/buildpacket/) that uses the Lumera
	// SDK to build a real MsgRequestAction with cascade metadata. It must be
	// a separate module because the Lumera SDK depends on ibc-go/v10, which
	// conflicts with interchaintest's ibc-go/v8 at init() time.
	toolBinary := buildBuildpacketTool(t)

	// ── Get Lumera host gRPC address ──
	// The buildpacket tool connects to Lumera's gRPC to query chain state
	// (e.g. account sequence) needed to construct the message.
	grpcAddr := lumera.GetHostGRPCAddress()
	require.NotEmpty(t, grpcAddr, "Lumera host gRPC address must be available")
	grpcAddr = strings.Replace(grpcAddr, "0.0.0.0", "localhost", 1)
	t.Logf("Lumera gRPC address: %s", grpcAddr)

	// ── Run buildpacket tool to create ICA packet with real SDK data ──
	toolCtx, toolCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer toolCancel()
	toolCmd := exec.CommandContext(toolCtx, toolBinary,
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

	// Write the ICA packet JSON into the Osmosis container's filesystem so
	// the osmosisd CLI can read it as a file argument to send-tx.
	packetFile := "ica_packet.json"
	err = osmosis.GetNode().WriteFile(ctx, packetJSON, packetFile)
	require.NoError(t, err)

	// ── Send the ICA packet from Osmosis (controller) ──
	// This broadcasts a tx on Osmosis that wraps our CosmosTx packet. The
	// relayer will pick it up and deliver it to Lumera for execution.
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

	// ── Wait for the relayer to deliver the ICA packet to Lumera ──
	err = testutil.WaitForBlocks(ctx, 10, osmosis, lumera)
	require.NoError(t, err)

	// Explicitly flush any remaining packets on the ICA channel to ensure
	// delivery. The channel is found dynamically by its "icacontroller-" port
	// prefix (not hardcoded) since channel IDs depend on creation order.
	icaChanID := findICAChannel(t, ctx, r, eRep, osmosis.Config().ChainID)
	require.NoError(t, r.Flush(ctx, eRep, ibcPath, icaChanID))
	err = testutil.WaitForBlocks(ctx, 5, osmosis, lumera)
	require.NoError(t, err)

	// ── Verify action was created on Lumera ──
	verifyActionCreated(t, ctx, lumera, icaAddr)
}

// verifyActionCreated queries the action module on Lumera and asserts that an
// action with the expected creator (the ICA address) and type CASCADE exists.
// This confirms the full ICS-27 round-trip: controller tx → relay → host execution.
func verifyActionCreated(t *testing.T, ctx context.Context, lumera *cosmos.CosmosChain, creator string) {
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
