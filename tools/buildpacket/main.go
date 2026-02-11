// buildpacket builds an ICA packet containing a real MsgRequestAction
// using the Lumera SDK's cascade client. It lives in a separate Go module
// to avoid the ibc-go/v8 vs v10 init() conflict with interchaintest.
//
// Usage:
//
//	go run . --mnemonic "..." --ica-address lumera1... --grpc-addr localhost:9090 \
//	         --chain-id lumera-testnet-2 --file /tmp/test.bin --owner-hrp osmo
//
// Outputs the ICA packet JSON to stdout (errors go to stderr).
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/LumeraProtocol/sdk-go/cascade"
	"github.com/LumeraProtocol/sdk-go/ica"
	sdkcrypto "github.com/LumeraProtocol/sdk-go/pkg/crypto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	gogoproto "github.com/cosmos/gogoproto/proto"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
)

func main() {
	mnemonic := flag.String("mnemonic", "", "BIP39 mnemonic for key derivation")
	icaAddress := flag.String("ica-address", "", "ICA address on Lumera (host chain)")
	grpcAddr := flag.String("grpc-addr", "", "Lumera gRPC address (host:port)")
	chainID := flag.String("chain-id", "", "Lumera chain ID")
	filePath := flag.String("file", "", "Path to the file to create action for")
	ownerHRP := flag.String("owner-hrp", "osmo", "Bech32 HRP for controller chain")
	flag.Parse()

	for _, check := range []struct{ name, val string }{
		{"mnemonic", *mnemonic},
		{"ica-address", *icaAddress},
		{"grpc-addr", *grpcAddr},
		{"chain-id", *chainID},
		{"file", *filePath},
	} {
		if strings.TrimSpace(check.val) == "" {
			fmt.Fprintf(os.Stderr, "--%s is required\n", check.name)
			os.Exit(1)
		}
	}

	ctx := context.Background()

	// Set up temporary keyring from mnemonic
	tmpDir, err := os.MkdirTemp("", "buildpacket-keyring-*")
	if err != nil {
		fatal("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	kr, err := sdkcrypto.NewMultiChainKeyring("lumera", "test", tmpDir)
	if err != nil {
		fatal("create keyring: %v", err)
	}

	keyName := "buildpacket-key"
	_, err = kr.NewAccount(keyName, *mnemonic, "", hd.CreateHDPath(118, 0, 0).String(), hd.Secp256k1)
	if err != nil {
		fatal("import key from mnemonic: %v", err)
	}

	lumeraAddr, err := sdkcrypto.AddressFromKey(kr, keyName, "lumera")
	if err != nil {
		fatal("derive lumera address: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Derived lumera address: %s\n", lumeraAddr)

	rec, err := kr.Key(keyName)
	if err != nil {
		fatal("get key record: %v", err)
	}
	pub, err := rec.GetPubKey()
	if err != nil {
		fatal("get pubkey: %v", err)
	}
	appPubkey := pub.Bytes()

	// Normalise 0.0.0.0 → localhost for host-side connections
	normalizedGRPC := strings.Replace(*grpcAddr, "0.0.0.0", "localhost", 1)
	fmt.Fprintf(os.Stderr, "Connecting to Lumera gRPC: %s\n", normalizedGRPC)

	cascadeClient, err := cascade.New(ctx, cascade.Config{
		ChainID:         *chainID,
		GRPCAddr:        normalizedGRPC,
		Address:         lumeraAddr,
		KeyName:         keyName,
		ICAOwnerKeyName: keyName,
		ICAOwnerHRP:     *ownerHRP,
	}, kr)
	if err != nil {
		fatal("create cascade client: %v", err)
	}
	defer func() { _ = cascadeClient.Close() }()

	// Build MsgRequestAction with real cascade metadata
	uploadOpts := &cascade.UploadOptions{}
	cascade.WithICACreatorAddress(*icaAddress)(uploadOpts)
	cascade.WithAppPubkey(appPubkey)(uploadOpts)

	msg, _, err := cascadeClient.CreateRequestActionMessage(ctx, lumeraAddr, *filePath, uploadOpts)
	if err != nil {
		fatal("CreateRequestActionMessage: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Built MsgRequestAction: creator=%s type=%s\n", msg.Creator, msg.ActionType)

	// Pack into ICA packet data
	msgAny, err := ica.PackRequestAny(msg)
	if err != nil {
		fatal("PackRequestAny: %v", err)
	}

	cosmosTx := &icatypes.CosmosTx{
		Messages: []*codectypes.Any{msgAny},
	}
	cosmosTxBytes, err := gogoproto.Marshal(cosmosTx)
	if err != nil {
		fatal("marshal CosmosTx: %v", err)
	}

	// Output ICA packet JSON to stdout
	fmt.Printf(`{"type":"TYPE_EXECUTE_TX","data":"%s","memo":""}`,
		base64.StdEncoding.EncodeToString(cosmosTxBytes))
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "buildpacket: "+format+"\n", args...)
	os.Exit(1)
}
