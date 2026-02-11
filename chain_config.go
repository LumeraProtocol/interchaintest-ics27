// chain_config.go - Unified configuration for all Lumera versions
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

// ChainVersion defines which Lumera version to use
type ChainVersion string

const (
	// V1_9_1 includes crisis module, uses legacy x/params for consensus
	V1_9_1 ChainVersion = "v1.9.1"
	// V1_10_1 removes crisis module, uses x/consensus for consensus params
	V1_10_1 ChainVersion = "v1.10.1"
)

// GetLumeraChainConfig returns a chain config for the specified version
func GetLumeraChainConfig(version ChainVersion, useLocalImage bool) ibc.ChainConfig {
	image := ibc.DockerImage{
		Repository: "ghcr.io/lumeraprotocol/lumerad",
		Version:    string(version),
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
		ModifyGenesis:       getModifyGenesisFunc(version),
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

	// Default configs for backward compatibility
	LumeraConfig = GetLumeraChainConfig(V1_10_1, false)
)

// getModifyGenesisFunc returns the appropriate genesis modifier based on version
func getModifyGenesisFunc(version ChainVersion) func(ibc.ChainConfig, []byte) ([]byte, error) {
	switch version {
	case V1_9_1:
		return modifyLumeraGenesisV1_9_1
	case V1_10_1:
		return modifyLumeraGenesisV1_10_1
	default:
		return modifyLumeraGenesisV1_10_1
	}
}

// modifyLumeraGenesisV1_9_1 configures genesis for Lumera v1.9.1 and earlier.
// Follows the same minimal-modification approach as start-lumera-standalone.sh:
// trust lumerad init defaults, only fix denoms + remove unsupported modules.
func modifyLumeraGenesisV1_9_1(config ibc.ChainConfig, genesis []byte) ([]byte, error) {
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

	// v1.9.1 has crisis module — ensure it uses correct denom
	if err := ensureCrisisModule(appState); err != nil {
		return nil, err
	}
	// Remove unsupported modules
	delete(appState, "nft")
	// Sync claims total from CSV
	if err := setClaimsTotalFromCSV(appState); err != nil {
		return nil, err
	}

	return json.MarshalIndent(g, "", "  ")
}

// modifyLumeraGenesisV1_10_1 configures genesis for Lumera v1.10.0 and v1.10.1.
// Follows the same minimal-modification approach as start-lumera-standalone.sh:
// trust lumerad init defaults, only fix denoms + remove unsupported modules.
func modifyLumeraGenesisV1_10_1(config ibc.ChainConfig, genesis []byte) ([]byte, error) {
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

	// v1.10.x removed crisis module
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

// ensureCrisisModule ensures crisis module is present (required for v1.9.1 and earlier)
func ensureCrisisModule(appState map[string]interface{}) error {
	if _, ok := appState["crisis"]; !ok {
		appState["crisis"] = map[string]interface{}{
			"constant_fee": map[string]interface{}{
				"denom":  "ulume",
				"amount": "1000000000",
			},
		}
	}
	return nil
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

// setConsensusParams configures consensus params in x/consensus module (used by v1.10.x)
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
