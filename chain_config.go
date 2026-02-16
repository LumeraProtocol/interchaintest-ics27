// chain_config.go - Chain configuration for Lumera interchaintest
package interchaintest_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"

	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
)

// DefaultLumeraVersion is used when LUMERA_VERSION env var is not set.
const DefaultLumeraVersion = "v1.10.1"

// GetLumeraChainConfig returns a chain config for the given version.
// version is the Docker image tag (e.g. "v1.10.1").
func GetLumeraChainConfig(version string, useLocalImage bool) ibc.ChainConfig {
	image := ibc.DockerImage{
		Repository: "ghcr.io/lumeraprotocol/lumerad",
		Version:    version,
		UIDGID:     "1025:1025",
	}

	if useLocalImage {
		image.Repository = "lumerad-local"
		image.Version = "local"
	}

	return ibc.ChainConfig{
		Type:                "cosmos",
		Name:                "lumera",
		ChainID:             "lumera-testnet-2",
		Images:              []ibc.DockerImage{image},
		Bin:                 "lumerad",
		Bech32Prefix:        "lumera",
		Denom:               "ulume",
		GasPrices:           "0.025ulume",
		GasAdjustment:       1.5,
		TrustingPeriod:      "336h",
		ModifyGenesis:       modifyLumeraGenesis,
		AdditionalStartArgs: []string{"--claims-path", "/tmp/claims.csv"},
	}
}

var (
	OsmosisImage = ibc.DockerImage{
		Repository: "ghcr.io/strangelove-ventures/heighliner/osmosis",
		Version:    "v25.0.0",
		UIDGID:     "1025:1025",
	}

	OsmosisConfig = ibc.ChainConfig{
		Type:           "cosmos",
		Name:           "osmosis",
		ChainID:        "osmosis-test-1",
		Images:         []ibc.DockerImage{OsmosisImage},
		Bin:            "osmosisd",
		Bech32Prefix:   "osmo",
		Denom:          "uosmo",
		GasPrices:      "0.025uosmo",
		GasAdjustment:  1.5,
		TrustingPeriod: "336h",
	}

	// Default config for backward compatibility
	LumeraConfig = GetLumeraChainConfig(DefaultLumeraVersion, false)
)

// modifyLumeraGenesis configures genesis for Lumera.
// Follows the minimal-modification approach: trust lumerad init defaults,
// only fix denoms + remove unsupported modules.
func modifyLumeraGenesis(config ibc.ChainConfig, genesis []byte) ([]byte, error) {
	genesis, err := cosmos.ModifyGenesis([]cosmos.GenesisKV{
		cosmos.NewGenesisKV("app_state.staking.params.bond_denom", config.Denom),
		cosmos.NewGenesisKV("app_state.mint.params.mint_denom", config.Denom),
		// ICA host: allow all message types so ICA-submitted txs are executed
		cosmos.NewGenesisKV("app_state.interchainaccounts.host_genesis_state.params.host_enabled", true),
		cosmos.NewGenesisKV("app_state.interchainaccounts.host_genesis_state.params.allow_messages", []string{"*"}),
	})(config, genesis)
	if err != nil {
		return nil, err
	}

	g := make(map[string]interface{})
	if err := json.Unmarshal(genesis, &g); err != nil {
		return nil, fmt.Errorf("failed to unmarshal genesis: %w", err)
	}

	appState, ok := g["app_state"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("app_state not found in genesis")
	}

	// Crisis module was removed in v1.10.x
	delete(appState, "crisis")
	// Remove unsupported modules
	delete(appState, "nft")
	// v1.10.x uses x/consensus module for consensus params
	if err := setConsensusParams(appState); err != nil {
		return nil, err
	}
	// Sync claims total from CSV
	if err := setClaimsTotalFromCSV(appState); err != nil {
		return nil, err
	}

	return json.MarshalIndent(g, "", "  ")
}

// setClaimsTotalFromCSV reads claims.csv and sets total_claimable_amount in genesis to match
func setClaimsTotalFromCSV(appState map[string]interface{}) error {
	// Find claims.csv relative to this source file
	_, thisFile, _, _ := runtime.Caller(0)
	claimsPath := filepath.Join(filepath.Dir(thisFile), "claims.csv")

	f, err := os.Open(claimsPath)
	if err != nil {
		// claims.csv not available on host — skip
		return nil
	}
	defer f.Close()

	total := new(big.Int)
	r := csv.NewReader(f)
	for {
		record, err := r.Read()
		if err != nil {
			break
		}
		if len(record) < 2 {
			continue
		}
		amount := new(big.Int)
		if _, ok := amount.SetString(record[1], 10); !ok {
			continue
		}
		total.Add(total, amount)
	}

	if total.Sign() == 0 {
		return nil
	}

	claim, ok := appState["claim"].(map[string]interface{})
	if !ok {
		claim = make(map[string]interface{})
		appState["claim"] = claim
	}
	claim["total_claimable_amount"] = total.String()

	return nil
}

// setConsensusParams configures consensus params in x/consensus module
func setConsensusParams(appState map[string]interface{}) error {
	consensus, ok := appState["consensus"].(map[string]interface{})
	if !ok {
		consensus = make(map[string]interface{})
		appState["consensus"] = consensus
	}

	params := map[string]interface{}{
		"block": map[string]interface{}{
			"max_bytes": "22020096",
			"max_gas":   "-1",
		},
		"evidence": map[string]interface{}{
			"max_age_num_blocks": "100000",
			"max_age_duration":   "172800000000000",
			"max_bytes":          "1048576",
		},
		"validator": map[string]interface{}{
			"pub_key_types": []string{"ed25519"},
		},
		"version": map[string]interface{}{
			"app": "0",
		},
	}

	consensus["params"] = params
	return nil
}
