// Copyright (C) 2024, AllianceBlock. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	runner_sdk "github.com/ava-labs/avalanche-network-runner/client"
	"github.com/ava-labs/avalanche-network-runner/rpcpb"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/crypto/ed25519"
	hrpc "github.com/ava-labs/hypersdk/rpc"
	hutils "github.com/ava-labs/hypersdk/utils"

	"github.com/nuklai/nuklaivm/actions"
	"github.com/nuklai/nuklaivm/auth"
	nconsts "github.com/nuklai/nuklaivm/consts"
	nrpc "github.com/nuklai/nuklaivm/rpc"
)

const (
	startAmount = uint64(10000000000000000000)
	sendAmount  = uint64(5000)

	healthPollInterval = 10 * time.Second
)

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "nuklaivm e2e test suites")
}

var (
	requestTimeout time.Duration

	networkRunnerLogLevel string
	avalanchegoLogLevel   string
	gRPCEp                string
	gRPCGatewayEp         string

	execPath  string
	pluginDir string

	vmGenesisPath    string
	vmConfigPath     string
	vmConfig         string
	subnetConfigPath string
	outputPath       string

	mode string

	logsDir string

	blockchainIDA string
	blockchainIDB string

	trackSubnetsOpt runner_sdk.OpOption

	numValidators uint
)

func init() {
	flag.DurationVar(
		&requestTimeout,
		"request-timeout",
		120*time.Second,
		"timeout for transaction issuance and confirmation",
	)

	flag.StringVar(
		&networkRunnerLogLevel,
		"network-runner-log-level",
		"info",
		"gRPC server endpoint",
	)

	flag.StringVar(
		&avalanchegoLogLevel,
		"avalanchego-log-level",
		"info",
		"log level for avalanchego",
	)

	flag.StringVar(
		&gRPCEp,
		"network-runner-grpc-endpoint",
		"0.0.0.0:8080",
		"gRPC server endpoint",
	)
	flag.StringVar(
		&gRPCGatewayEp,
		"network-runner-grpc-gateway-endpoint",
		"0.0.0.0:8081",
		"gRPC gateway endpoint",
	)

	flag.StringVar(
		&execPath,
		"avalanchego-path",
		"",
		"avalanchego executable path",
	)

	flag.StringVar(
		&pluginDir,
		"avalanchego-plugin-dir",
		"",
		"avalanchego plugin directory",
	)

	flag.StringVar(
		&vmGenesisPath,
		"vm-genesis-path",
		"",
		"VM genesis file path",
	)

	flag.StringVar(
		&vmConfigPath,
		"vm-config-path",
		"",
		"VM configfile path",
	)

	flag.StringVar(
		&subnetConfigPath,
		"subnet-config-path",
		"",
		"Subnet configfile path",
	)

	flag.StringVar(
		&outputPath,
		"output-path",
		"",
		"output YAML path to write local cluster information",
	)

	flag.StringVar(
		&mode,
		"mode",
		"test",
		"'test' to shut down cluster after tests, 'run' to skip tests and only run without shutdown",
	)

	flag.UintVar(
		&numValidators,
		"num-validators",
		5,
		"number of validators per blockchain",
	)
}

const (
	modeTest      = "test"
	modeFullTest  = "full-test" // runs state sync
	modeRun       = "run"
	modeRunSingle = "run-single"
)

var anrCli runner_sdk.Client

var _ = ginkgo.BeforeSuite(func() {
	gomega.Expect(mode).Should(gomega.Or(
		gomega.Equal(modeTest),
		gomega.Equal(modeFullTest),
		gomega.Equal(modeRun),
		gomega.Equal(modeRunSingle),
	))
	gomega.Expect(numValidators).Should(gomega.BeNumerically(">", 0))
	logLevel, err := logging.ToLevel(networkRunnerLogLevel)
	gomega.Expect(err).Should(gomega.BeNil())
	logFactory := logging.NewFactory(logging.Config{
		DisplayLevel: logLevel,
		LogLevel:     logLevel,
	})
	log, err := logFactory.Make("main")
	gomega.Expect(err).Should(gomega.BeNil())

	anrCli, err = runner_sdk.New(runner_sdk.Config{
		Endpoint:    gRPCEp,
		DialTimeout: 10 * time.Second,
	}, log)
	gomega.Expect(err).Should(gomega.BeNil())

	hutils.Outf(
		"{{green}}sending 'start' with binary path:{{/}} %q (%q)\n",
		execPath,
		nconsts.ID,
	)

	// Load config data
	if len(vmConfigPath) > 0 {
		configData, err := os.ReadFile(vmConfigPath)
		gomega.Expect(err).Should(gomega.BeNil())
		vmConfig = string(configData)
	} else {
		vmConfig = "{}"
	}

	// Start cluster
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	resp, err := anrCli.Start(
		ctx,
		execPath,
		runner_sdk.WithPluginDir(pluginDir),
		// We don't disable PUT gossip here because the E2E test adds multiple
		// non-validating nodes (which will fall behind).
		runner_sdk.WithGlobalNodeConfig(fmt.Sprintf(`{
				"log-level":"%s",
				"log-display-level":"%s",
				"proposervm-use-current-height":true,
				"throttler-inbound-validator-alloc-size":"10737418240",
				"throttler-inbound-at-large-alloc-size":"10737418240",
				"throttler-inbound-node-max-processing-msgs":"100000",
				"throttler-inbound-bandwidth-refill-rate":"1073741824",
				"throttler-inbound-bandwidth-max-burst-size":"1073741824",
				"throttler-inbound-cpu-validator-alloc":"100000",
				"throttler-inbound-disk-validator-alloc":"10737418240000",
				"throttler-outbound-validator-alloc-size":"10737418240",
				"throttler-outbound-at-large-alloc-size":"10737418240",
				"consensus-on-accept-gossip-validator-size":"10",
				"consensus-on-accept-gossip-peer-size":"10",
				"network-compression-type":"zstd",
				"consensus-app-concurrency":"512",
				"profile-continuous-enabled":true,
				"profile-continuous-freq":"1m",
				"http-host":"",
				"http-allowed-origins": "*",
				"http-allowed-hosts": "*"
			}`,
			avalanchegoLogLevel,
			avalanchegoLogLevel,
		)),
	)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
	hutils.Outf(
		"{{green}}successfully started cluster:{{/}} %s {{green}}subnets:{{/}} %+v\n",
		resp.ClusterInfo.RootDataDir,
		resp.GetClusterInfo().GetSubnets(),
	)
	logsDir = resp.GetClusterInfo().GetRootDataDir()

	// Name 10 new validators (which should have BLS key registered)
	subnetA := []string{}
	subnetB := []string{}
	for i := 1; i <= int(numValidators)*2; i++ {
		n := fmt.Sprintf("node%d-bls", i)
		if i <= int(numValidators) {
			subnetA = append(subnetA, n)
		} else {
			subnetB = append(subnetB, n)
		}
	}
	specs := []*rpcpb.BlockchainSpec{
		{
			VmName:      nconsts.Name,
			Genesis:     vmGenesisPath,
			ChainConfig: vmConfigPath,
			SubnetSpec: &rpcpb.SubnetSpec{
				SubnetConfig: subnetConfigPath,
				Participants: subnetA,
			},
		},
		{
			VmName:      nconsts.Name,
			Genesis:     vmGenesisPath,
			ChainConfig: vmConfigPath,
			SubnetSpec: &rpcpb.SubnetSpec{
				SubnetConfig: subnetConfigPath,
				Participants: subnetB,
			},
		},
	}
	if mode == modeRunSingle {
		specs = specs[0:1]
	}

	// Create 2 subnets
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	sresp, err := anrCli.CreateBlockchains(
		ctx,
		specs,
	)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())

	blockchainIDA = sresp.ChainIds[0]
	subnetIDA := sresp.ClusterInfo.CustomChains[blockchainIDA].SubnetId
	hutils.Outf(
		"{{green}}successfully added chain:{{/}} %s {{green}}subnet:{{/}} %s {{green}}participants:{{/}} %+v\n",
		blockchainIDA,
		subnetIDA,
		subnetA,
	)

	if mode == modeRunSingle {
		trackSubnetsOpt = runner_sdk.WithGlobalNodeConfig(fmt.Sprintf(`{"%s":"%s"}`,
			config.TrackSubnetsKey,
			subnetIDA,
		))
	} else {
		blockchainIDB = sresp.ChainIds[1]
		subnetIDB := sresp.ClusterInfo.CustomChains[blockchainIDB].SubnetId
		hutils.Outf(
			"{{green}}successfully added chain:{{/}} %s {{green}}subnet:{{/}} %s {{green}}participants:{{/}} %+v\n",
			blockchainIDB,
			subnetIDB,
			subnetB,
		)
		trackSubnetsOpt = runner_sdk.WithGlobalNodeConfig(fmt.Sprintf(`{"%s":"%s,%s"}`,
			config.TrackSubnetsKey,
			subnetIDA,
			subnetIDB,
		))
	}

	gomega.Expect(blockchainIDA).Should(gomega.Not(gomega.BeEmpty()))
	if mode != modeRunSingle {
		gomega.Expect(blockchainIDB).Should(gomega.Not(gomega.BeEmpty()))
	}
	gomega.Expect(logsDir).Should(gomega.Not(gomega.BeEmpty()))

	cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
	status, err := anrCli.Status(cctx)
	ccancel()
	gomega.Expect(err).Should(gomega.BeNil())
	nodeInfos := status.GetClusterInfo().GetNodeInfos()

	instancesA = []instance{}
	for _, nodeName := range subnetA {
		info := nodeInfos[nodeName]
		u := fmt.Sprintf("%s/ext/bc/%s", info.Uri, blockchainIDA)
		bid, err := ids.FromString(blockchainIDA)
		gomega.Expect(err).Should(gomega.BeNil())
		nodeID, err := ids.NodeIDFromString(info.GetId())
		gomega.Expect(err).Should(gomega.BeNil())
		hcli := hrpc.NewJSONRPCClient(u)

		// After returning healthy, the node may not respond right away
		//
		// TODO: figure out why
		var networkID uint32
		for i := 0; i < 10; i++ {
			networkID, _, _, err = hcli.Network(context.TODO())
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
		}
		gomega.Expect(err).Should(gomega.BeNil())

		instancesA = append(instancesA, instance{
			nodeID: nodeID,
			uri:    u,
			hcli:   hcli,
			ncli:   nrpc.NewJSONRPCClient(u, networkID, bid),
		})

		// Let's copy the node's staking.cert and staking.key to a temporary directory
		destDir := fmt.Sprintf("/tmp/nuklaivm/nodes/%s/", info.GetName())
		err = copyNodeInfo(info.GetDbDir(), destDir)
		gomega.Expect(err).Should(gomega.BeNil())
	}

	if mode != modeRunSingle {
		instancesB = []instance{}
		for _, nodeName := range subnetB {
			info := nodeInfos[nodeName]
			u := fmt.Sprintf("%s/ext/bc/%s", info.Uri, blockchainIDB)
			bid, err := ids.FromString(blockchainIDB)
			gomega.Expect(err).Should(gomega.BeNil())
			nodeID, err := ids.NodeIDFromString(info.GetId())
			gomega.Expect(err).Should(gomega.BeNil())
			hcli := hrpc.NewJSONRPCClient(u)

			// After returning healthy, the node may not respond right away
			//
			// TODO: figure out why
			var networkID uint32
			for i := 0; i < 10; i++ {
				networkID, _, _, err = hcli.Network(context.TODO())
				if err != nil {
					time.Sleep(1 * time.Second)
					continue
				}
			}
			gomega.Expect(err).Should(gomega.BeNil())

			instancesB = append(instancesB, instance{
				nodeID: nodeID,
				uri:    u,
				hcli:   hcli,
				ncli:   nrpc.NewJSONRPCClient(u, networkID, bid),
			})

			// Let's copy the node's staking.cert and staking.key to a temporary directory
			destDir := fmt.Sprintf("/tmp/nuklaivm/nodes/%s/", info.GetName())
			err = copyNodeInfo(info.GetDbDir(), destDir)
			gomega.Expect(err).Should(gomega.BeNil())
		}
	}

	// Load default pk
	privBytes, err := codec.LoadHex(
		"323b1d8f4eed5f0da9da93071b034f2dce9d2d22692c172f3cb252a64ddfafd01b057de320297c29ad0c1f589ea216869cf1938d88c9fbd70d6748323dbf2fa7", //nolint:lll
		ed25519.PrivateKeyLen,
	)
	gomega.Ω(err).Should(gomega.BeNil())
	priv = ed25519.PrivateKey(privBytes)
	factory = auth.NewED25519Factory(priv)
	rsender = auth.NewED25519Address(priv.PublicKey())
	sender = codec.MustAddressBech32(nconsts.HRP, rsender)
	hutils.Outf("\n{{yellow}}$ loaded address:{{/}} %s\n\n", sender)
})

var (
	priv    ed25519.PrivateKey
	factory *auth.ED25519Factory
	rsender codec.Address
	sender  string

	instancesA []instance
	instancesB []instance
)

type instance struct {
	nodeID ids.NodeID
	uri    string
	hcli   *hrpc.JSONRPCClient
	ncli   *nrpc.JSONRPCClient
}

var _ = ginkgo.AfterSuite(func() {
	switch mode {
	case modeTest, modeFullTest:
		hutils.Outf("{{red}}shutting down cluster{{/}}\n")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		_, err := anrCli.Stop(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())

	case modeRun:
		hutils.Outf("{{yellow}}skipping cluster shutdown{{/}}\n\n")
		hutils.Outf("{{cyan}}Blockchain A:{{/}} %s\n", blockchainIDA)
		for _, member := range instancesA {
			hutils.Outf("%s URI: %s\n", member.nodeID, member.uri)
		}
		hutils.Outf("\n{{cyan}}Blockchain B:{{/}} %s\n", blockchainIDB)
		for _, member := range instancesB {
			hutils.Outf("%s URI: %s\n", member.nodeID, member.uri)
		}

	case modeRunSingle:
		hutils.Outf("{{yellow}}skipping cluster shutdown{{/}}\n\n")
		hutils.Outf("{{cyan}}Blockchain:{{/}} %s\n", blockchainIDA)
		for _, member := range instancesA {
			hutils.Outf("%s URI: %s\n", member.nodeID, member.uri)
		}
	}
	gomega.Expect(anrCli.Close()).Should(gomega.BeNil())
})

var _ = ginkgo.Describe("[Ping]", func() {
	ginkgo.It("can ping A", func() {
		for _, inst := range instancesA {
			hcli := inst.hcli
			ok, err := hcli.Ping(context.Background())
			gomega.Ω(ok).Should(gomega.BeTrue())
			gomega.Ω(err).Should(gomega.BeNil())
		}
	})

	ginkgo.It("can ping B", func() {
		for _, inst := range instancesB {
			hcli := inst.hcli
			ok, err := hcli.Ping(context.Background())
			gomega.Ω(ok).Should(gomega.BeTrue())
			gomega.Ω(err).Should(gomega.BeNil())
		}
	})
})

var _ = ginkgo.Describe("[Network]", func() {
	ginkgo.It("can get network A", func() {
		for _, inst := range instancesA {
			hcli := inst.hcli
			_, _, chainID, err := hcli.Network(context.Background())
			gomega.Ω(chainID).ShouldNot(gomega.Equal(ids.Empty))
			gomega.Ω(err).Should(gomega.BeNil())
		}
	})

	ginkgo.It("can get network B", func() {
		for _, inst := range instancesB {
			hcli := inst.hcli
			_, _, chainID, err := hcli.Network(context.Background())
			gomega.Ω(chainID).ShouldNot(gomega.Equal(ids.Empty))
			gomega.Ω(err).Should(gomega.BeNil())
		}
	})
})

var _ = ginkgo.Describe("[Test]", func() {
	switch mode {
	case modeRun, modeRunSingle:
		hutils.Outf("{{yellow}}skipping tests{{/}}\n")
		return
	}

	ginkgo.It("transfer in a single node (raw)", func() {
		nativeBalance, err := instancesA[0].ncli.Balance(context.TODO(), sender, ids.Empty)
		gomega.Ω(err).Should(gomega.BeNil())
		gomega.Ω(nativeBalance).Should(gomega.Equal(startAmount))

		other, err := ed25519.GeneratePrivateKey()
		gomega.Ω(err).Should(gomega.BeNil())
		aother := auth.NewED25519Address(other.PublicKey())

		ginkgo.By("issue Transfer to the first node", func() {
			// Generate transaction
			parser, err := instancesA[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, maxFee, err := instancesA[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    aother,
					Value: sendAmount,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}generated transaction{{/}}\n")

			// Broadcast and wait for transaction
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, fee, err := instancesA[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found transaction{{/}}\n")

			// Check sender balance
			balance, err := instancesA[0].ncli.Balance(context.Background(), sender, ids.Empty)
			gomega.Ω(err).Should(gomega.BeNil())
			hutils.Outf(
				"{{yellow}}start=%d fee=%d send=%d balance=%d{{/}}\n",
				startAmount,
				maxFee,
				sendAmount,
				balance,
			)
			gomega.Ω(balance).Should(gomega.Equal(startAmount - fee - sendAmount))
			hutils.Outf("{{yellow}}fetched balance{{/}}\n")
		})

		ginkgo.By("check if Transfer has been accepted from all nodes", func() {
			for _, inst := range instancesA {
				color.Blue("checking %q", inst.uri)

				// Ensure all blocks processed
				for {
					_, h, _, err := inst.hcli.Accepted(context.Background())
					gomega.Ω(err).Should(gomega.BeNil())
					if h > 0 {
						break
					}
					time.Sleep(1 * time.Second)
				}

				// Check balance of recipient
				balance, err := inst.ncli.Balance(context.Background(), codec.MustAddressBech32(nconsts.HRP, aother), ids.Empty)
				gomega.Ω(err).Should(gomega.BeNil())
				gomega.Ω(balance).Should(gomega.Equal(sendAmount))
			}
		})
	})

	ginkgo.It("performs a warp transfer of the native asset", func() {
		other, err := ed25519.GeneratePrivateKey()
		gomega.Ω(err).Should(gomega.BeNil())
		aother := codec.MustAddressBech32(nconsts.HRP, auth.NewED25519Address(other.PublicKey()))
		source, err := ids.FromString(blockchainIDA)
		gomega.Ω(err).Should(gomega.BeNil())
		destination, err := ids.FromString(blockchainIDB)
		gomega.Ω(err).Should(gomega.BeNil())
		otherFactory := auth.NewED25519Factory(other)

		var txID ids.ID
		ginkgo.By("submitting an export action on source", func() {
			otherBalance, err := instancesA[0].ncli.Balance(context.Background(), aother, ids.Empty)
			gomega.Ω(err).Should(gomega.BeNil())
			senderBalance, err := instancesA[0].ncli.Balance(context.Background(), sender, ids.Empty)
			gomega.Ω(err).Should(gomega.BeNil())

			parser, err := instancesA[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesA[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.ExportAsset{
					To:          auth.NewED25519Address(other.PublicKey()),
					Asset:       ids.Empty,
					Value:       sendAmount,
					Return:      false,
					Destination: destination,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID = tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)

			// Broadcast and wait for transaction
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, fee, err := instancesA[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found warp export transaction{{/}}\n")

			// Check loans and balances
			amount, err := instancesA[0].ncli.Loan(context.Background(), ids.Empty, destination)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(amount).Should(gomega.Equal(sendAmount))
			aotherBalance, err := instancesA[0].ncli.Balance(context.Background(), aother, ids.Empty)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(otherBalance).Should(gomega.Equal(aotherBalance))
			asenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(asenderBalance).Should(gomega.Equal(senderBalance - sendAmount - fee))
		})

		ginkgo.By("fund other account with native", func() {
			parser, err := instancesB[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesB[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    auth.NewED25519Address(other.PublicKey()),
					Asset: ids.Empty,
					Value: 500_000_000,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID := tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)

			// Broadcast transaction (wait for after all broadcast)
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")

			// Confirm transaction is accepted
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, _, err := instancesB[0].ncli.WaitForTransaction(ctx, txID)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found transaction %s on B{{/}}\n", txID)
		})

		ginkgo.By("submitting an import action on destination", func() {
			bIDA, err := ids.FromString(blockchainIDA)
			gomega.Ω(err).Should(gomega.BeNil())
			newAsset := actions.ImportedAssetID(ids.Empty, bIDA)
			nativeOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			newOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newOtherBalance).Should(gomega.Equal(uint64(0)))
			nativeSenderBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			newSenderBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				sender,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newSenderBalance).Should(gomega.Equal(uint64(0)))

			var (
				msg                     *warp.Message
				subnetWeight, sigWeight uint64
			)
			for {
				msg, subnetWeight, sigWeight, err = instancesA[0].hcli.GenerateAggregateWarpSignature(
					context.Background(),
					txID,
				)
				if sigWeight == subnetWeight && err == nil {
					break
				}
				if err == nil {
					hutils.Outf(
						"{{yellow}}waiting for signature weight:{{/}} %d {{yellow}}observed:{{/}} %d\n",
						subnetWeight,
						sigWeight,
					)
				} else {
					hutils.Outf("{{red}}found error:{{/}} %v\n", err)
				}
				time.Sleep(1 * time.Second)
			}
			hutils.Outf(
				"{{green}}fetched signature weight:{{/}} %d {{green}}total weight:{{/}} %d\n",
				sigWeight,
				subnetWeight,
			)
			gomega.Ω(subnetWeight).Should(gomega.Equal(sigWeight))

			parser, err := instancesB[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesB[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				msg,
				&actions.ImportAsset{},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID = tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, fee, err := instancesB[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found warp import transaction{{/}}\n")

			// Check asset info and balance
			aNativeOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(nativeOtherBalance).Should(gomega.Equal(aNativeOtherBalance))
			aNewOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNewOtherBalance).Should(gomega.Equal(sendAmount))
			aNativeSenderBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNativeSenderBalance).Should(gomega.Equal(nativeSenderBalance - fee))
			aNewSenderBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				sender,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNewSenderBalance).Should(gomega.Equal(uint64(0)))
			exists, symbol, decimals, metadata, supply, owner, warp, err := instancesB[0].ncli.Asset(context.Background(), newAsset, false)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(exists).Should(gomega.BeTrue())
			gomega.Ω(string(symbol)).Should(gomega.Equal(nconsts.Symbol))
			gomega.Ω(decimals).Should(gomega.Equal(uint8(nconsts.Decimals)))
			gomega.Ω(metadata).Should(gomega.Equal(actions.ImportedAssetMetadata(ids.Empty, bIDA)))
			gomega.Ω(supply).Should(gomega.Equal(sendAmount))
			gomega.Ω(owner).Should(gomega.Equal(codec.MustAddressBech32(nconsts.HRP, codec.EmptyAddress)))
			gomega.Ω(warp).Should(gomega.BeTrue())
		})

		ginkgo.By("submitting an invalid export action to new destination", func() {
			bIDA, err := ids.FromString(blockchainIDA)
			gomega.Ω(err).Should(gomega.BeNil())
			newAsset := actions.ImportedAssetID(ids.Empty, bIDA)
			parser, err := instancesB[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesB[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.ExportAsset{
					To:          rsender,
					Asset:       newAsset,
					Value:       100,
					Return:      false,
					Destination: ids.GenerateTestID(),
				},
				otherFactory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)

			// Broadcast and wait for transaction
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, _, err := instancesB[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeFalse())

			// Confirm balances are unchanged
			newOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newOtherBalance).Should(gomega.Equal(sendAmount))
		})

		ginkgo.By("submitting first (2000) return export action on destination", func() {
			bIDA, err := ids.FromString(blockchainIDA)
			gomega.Ω(err).Should(gomega.BeNil())
			newAsset := actions.ImportedAssetID(ids.Empty, bIDA)
			parser, err := instancesB[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesB[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.ExportAsset{
					To:          rsender,
					Asset:       newAsset,
					Value:       2000,
					Return:      true,
					Destination: source,
					Reward:      100,
				},
				otherFactory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID = tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)

			// Broadcast and wait for transaction
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, _, err := instancesB[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found warp export transaction{{/}}\n")

			// Check balances and asset info
			amount, err := instancesB[0].ncli.Loan(context.Background(), newAsset, source)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(amount).Should(gomega.Equal(uint64(0)))
			otherBalance, err := instancesB[0].ncli.Balance(context.Background(), aother, newAsset)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(otherBalance).Should(gomega.Equal(uint64(2900)))
			exists, symbol, decimals, metadata, supply, owner, warp, err := instancesB[0].ncli.Asset(context.Background(), newAsset, false)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(exists).Should(gomega.BeTrue())
			gomega.Ω(string(symbol)).Should(gomega.Equal(nconsts.Symbol))
			gomega.Ω(decimals).Should(gomega.Equal(uint8(nconsts.Decimals)))
			gomega.Ω(metadata).Should(gomega.Equal(actions.ImportedAssetMetadata(ids.Empty, bIDA)))
			gomega.Ω(supply).Should(gomega.Equal(uint64(2900)))
			gomega.Ω(owner).Should(gomega.Equal(codec.MustAddressBech32(nconsts.HRP, codec.EmptyAddress)))
			gomega.Ω(warp).Should(gomega.BeTrue())
		})

		ginkgo.By("submitting first import action on source", func() {
			bIDA, err := ids.FromString(blockchainIDA)
			gomega.Ω(err).Should(gomega.BeNil())
			newAsset := actions.ImportedAssetID(ids.Empty, bIDA)
			nativeOtherBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				aother,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			newOtherBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newOtherBalance).Should(gomega.Equal(uint64(0)))
			nativeSenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			newSenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newSenderBalance).Should(gomega.Equal(uint64(0)))

			var (
				msg                     *warp.Message
				subnetWeight, sigWeight uint64
			)
			for {
				msg, subnetWeight, sigWeight, err = instancesB[0].hcli.GenerateAggregateWarpSignature(
					context.Background(),
					txID,
				)
				if sigWeight == subnetWeight && err == nil {
					break
				}
				if err == nil {
					hutils.Outf(
						"{{yellow}}waiting for signature weight:{{/}} %d {{yellow}}observed:{{/}} %d\n",
						subnetWeight,
						sigWeight,
					)
				} else {
					hutils.Outf("{{red}}found error:{{/}} %v\n", err)
				}
				time.Sleep(1 * time.Second)
			}
			hutils.Outf(
				"{{green}}fetched signature weight:{{/}} %d {{green}}total weight:{{/}} %d\n",
				sigWeight,
				subnetWeight,
			)
			gomega.Ω(subnetWeight).Should(gomega.Equal(sigWeight))

			parser, err := instancesA[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesA[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				msg,
				&actions.ImportAsset{},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID = tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, fee, err := instancesA[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found warp import transaction{{/}}\n")

			// Check balances and loan
			aNativeOtherBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				aother,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(nativeOtherBalance).Should(gomega.Equal(aNativeOtherBalance))
			aNewOtherBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNewOtherBalance).Should(gomega.Equal(uint64(0)))
			aNativeSenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNativeSenderBalance).
				Should(gomega.Equal(nativeSenderBalance - fee + 2000 + 100))
			aNewSenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNewSenderBalance).Should(gomega.Equal(uint64(0)))
			amount, err := instancesA[0].ncli.Loan(context.Background(), ids.Empty, destination)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(amount).Should(gomega.Equal(uint64(2900)))
		})

		ginkgo.By("submitting second (2900) return export action on destination", func() {
			bIDA, err := ids.FromString(blockchainIDA)
			gomega.Ω(err).Should(gomega.BeNil())
			newAsset := actions.ImportedAssetID(ids.Empty, bIDA)
			parser, err := instancesB[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesB[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.ExportAsset{
					To:          auth.NewED25519Address(other.PublicKey()),
					Asset:       newAsset,
					Value:       2900,
					Return:      true,
					Destination: source,
				},
				otherFactory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID = tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)

			// Broadcast and wait for transaction
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, _, err := instancesB[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found warp export transaction{{/}}\n")

			// Check balances and asset info
			amount, err := instancesB[0].ncli.Loan(context.Background(), newAsset, source)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(amount).Should(gomega.Equal(uint64(0)))
			otherBalance, err := instancesB[0].ncli.Balance(context.Background(), aother, newAsset)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(otherBalance).Should(gomega.Equal(uint64(0)))
			exists, _, _, _, _, _, _, err := instancesB[0].ncli.Asset(context.Background(), newAsset, false)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(exists).Should(gomega.BeFalse())
		})

		ginkgo.By("submitting second import action on source", func() {
			bIDA, err := ids.FromString(blockchainIDA)
			gomega.Ω(err).Should(gomega.BeNil())
			newAsset := actions.ImportedAssetID(ids.Empty, bIDA)
			nativeOtherBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				aother,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			newOtherBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newOtherBalance).Should(gomega.Equal(uint64(0)))
			nativeSenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			newSenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newSenderBalance).Should(gomega.Equal(uint64(0)))

			var (
				msg                     *warp.Message
				subnetWeight, sigWeight uint64
			)
			for {
				msg, subnetWeight, sigWeight, err = instancesB[0].hcli.GenerateAggregateWarpSignature(
					context.Background(),
					txID,
				)
				if sigWeight == subnetWeight && err == nil {
					break
				}
				if err == nil {
					hutils.Outf(
						"{{yellow}}waiting for signature weight:{{/}} %d {{yellow}}observed:{{/}} %d\n",
						subnetWeight,
						sigWeight,
					)
				} else {
					hutils.Outf("{{red}}found error:{{/}} %v\n", err)
				}
				time.Sleep(1 * time.Second)
			}
			hutils.Outf(
				"{{green}}fetched signature weight:{{/}} %d {{green}}total weight:{{/}} %d\n",
				sigWeight,
				subnetWeight,
			)
			gomega.Ω(subnetWeight).Should(gomega.Equal(sigWeight))

			parser, err := instancesA[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesA[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				msg,
				&actions.ImportAsset{},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID = tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, fee, err := instancesA[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found warp import transaction{{/}}\n")

			// Check balances and loan
			aNativeOtherBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				aother,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNativeOtherBalance).Should(gomega.Equal(nativeOtherBalance + 2900))
			aNewOtherBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNewOtherBalance).Should(gomega.Equal(uint64(0)))
			aNativeSenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNativeSenderBalance).Should(gomega.Equal(nativeSenderBalance - fee))
			aNewSenderBalance, err := instancesA[0].ncli.Balance(
				context.Background(),
				sender,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNewSenderBalance).Should(gomega.Equal(uint64(0)))
			amount, err := instancesA[0].ncli.Loan(context.Background(), ids.Empty, destination)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(amount).Should(gomega.Equal(uint64(0)))
		})

		ginkgo.By("swaping into destination", func() {
			bIDA, err := ids.FromString(blockchainIDA)
			gomega.Ω(err).Should(gomega.BeNil())
			newAsset := actions.ImportedAssetID(ids.Empty, bIDA)
			parser, err := instancesA[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err := instancesA[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.ExportAsset{
					To:          auth.NewED25519Address(other.PublicKey()),
					Asset:       ids.Empty, // becomes newAsset
					Value:       2000,
					Return:      false,
					SwapIn:      100,
					AssetOut:    ids.Empty,
					SwapOut:     200,
					SwapExpiry:  time.Now().UnixMilli() + 100_000,
					Destination: destination,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID = tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)

			// Broadcast and wait for transaction
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			success, _, err := instancesA[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found warp export transaction{{/}}\n")

			// Record balances on destination
			nativeOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			newOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newOtherBalance).Should(gomega.Equal(uint64(0)))
			nativeSenderBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			newSenderBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				sender,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(newSenderBalance).Should(gomega.Equal(uint64(0)))

			var (
				msg                     *warp.Message
				subnetWeight, sigWeight uint64
			)
			for {
				msg, subnetWeight, sigWeight, err = instancesA[0].hcli.GenerateAggregateWarpSignature(
					context.Background(),
					txID,
				)
				if sigWeight == subnetWeight && err == nil {
					break
				}
				if err == nil {
					hutils.Outf(
						"{{yellow}}waiting for signature weight:{{/}} %d {{yellow}}observed:{{/}} %d\n",
						subnetWeight,
						sigWeight,
					)
				} else {
					hutils.Outf("{{red}}found error:{{/}} %v\n", err)
				}
				time.Sleep(1 * time.Second)
			}
			hutils.Outf(
				"{{green}}fetched signature weight:{{/}} %d {{green}}total weight:{{/}} %d\n",
				sigWeight,
				subnetWeight,
			)
			gomega.Ω(subnetWeight).Should(gomega.Equal(sigWeight))

			parser, err = instancesB[0].ncli.Parser(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, tx, _, err = instancesB[0].hcli.GenerateTransaction(
				context.Background(),
				parser,
				msg,
				&actions.ImportAsset{
					Fill: true,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			txID = tx.ID()
			hutils.Outf("{{yellow}}generated transaction:{{/}} %s\n", txID)
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
			ctx, cancel = context.WithTimeout(context.Background(), requestTimeout)
			success, fee, err := instancesB[0].ncli.WaitForTransaction(ctx, tx.ID())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(success).Should(gomega.BeTrue())
			hutils.Outf("{{yellow}}found warp import transaction{{/}}\n")

			// Check balances following swap
			aNativeOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNativeOtherBalance).Should(gomega.Equal(nativeOtherBalance + 200))
			aNewOtherBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				aother,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNewOtherBalance).Should(gomega.Equal(uint64(1900)))
			aNativeSenderBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				sender,
				ids.Empty,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNativeSenderBalance).
				Should(gomega.Equal(nativeSenderBalance - fee - 200))
			aNewSenderBalance, err := instancesB[0].ncli.Balance(
				context.Background(),
				sender,
				newAsset,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(aNewSenderBalance).Should(gomega.Equal(uint64(100)))
		})
	})

	// TODO: add custom asset test
	// TODO: test with only part of sig weight
	// TODO: attempt to mint a warp asset

	switch mode {
	case modeTest:
		hutils.Outf("{{yellow}}skipping bootstrap and state sync tests{{/}}\n")
		return
	}

	// Create blocks before bootstrapping starts
	count := 0
	ginkgo.It("supports issuance of 128 blocks", func() {
		count += generateBlocks(context.Background(), count, 128, instancesA, true)
	})

	// Ensure bootstrapping works
	var hsyncClient *hrpc.JSONRPCClient
	var nsyncClient *nrpc.JSONRPCClient
	ginkgo.It("can bootstrap a new node", func() {
		cluster, err := anrCli.AddNode(
			context.Background(),
			"bootstrap",
			execPath,
			trackSubnetsOpt,
			runner_sdk.WithChainConfigs(map[string]string{
				blockchainIDA: vmConfig,
			}),
		)
		gomega.Expect(err).To(gomega.BeNil())
		awaitHealthy(anrCli)

		nodeURI := cluster.ClusterInfo.NodeInfos["bootstrap"].Uri
		uri := nodeURI + fmt.Sprintf("/ext/bc/%s", blockchainIDA)
		bid, err := ids.FromString(blockchainIDA)
		gomega.Expect(err).To(gomega.BeNil())
		hutils.Outf("{{blue}}bootstrap node uri: %s{{/}}\n", uri)
		c := hrpc.NewJSONRPCClient(uri)
		hsyncClient = c
		networkID, _, _, err := hsyncClient.Network(context.TODO())
		gomega.Expect(err).Should(gomega.BeNil())
		nc := nrpc.NewJSONRPCClient(uri, networkID, bid)
		nsyncClient = nc
		instancesA = append(instancesA, instance{
			uri:  uri,
			hcli: c,
			ncli: nc,
		})
	})

	ginkgo.It("accepts transaction after it bootstraps", func() {
		acceptTransaction(hsyncClient, nsyncClient)
	})

	// Create blocks before state sync starts (state sync requires at least 256
	// blocks)
	//
	// We do 1024 so that there are a number of ranges of data to fetch.
	ginkgo.It("supports issuance of at least 1024 more blocks", func() {
		count += generateBlocks(context.Background(), count, 1024, instancesA, true)
		// TODO: verify all roots are equal
	})

	ginkgo.It("can state sync a new node when no new blocks are being produced", func() {
		cluster, err := anrCli.AddNode(
			context.Background(),
			"sync",
			execPath,
			trackSubnetsOpt,
			runner_sdk.WithChainConfigs(map[string]string{
				blockchainIDA: vmConfig,
			}),
		)
		gomega.Expect(err).To(gomega.BeNil())

		awaitHealthy(anrCli)

		nodeURI := cluster.ClusterInfo.NodeInfos["sync"].Uri
		uri := nodeURI + fmt.Sprintf("/ext/bc/%s", blockchainIDA)
		bid, err := ids.FromString(blockchainIDA)
		gomega.Expect(err).To(gomega.BeNil())
		hutils.Outf("{{blue}}sync node uri: %s{{/}}\n", uri)
		hsyncClient = hrpc.NewJSONRPCClient(uri)
		networkID, _, _, err := hsyncClient.Network(context.TODO())
		gomega.Expect(err).To(gomega.BeNil())
		nsyncClient = nrpc.NewJSONRPCClient(uri, networkID, bid)
	})

	ginkgo.It("accepts transaction after state sync", func() {
		acceptTransaction(hsyncClient, nsyncClient)
	})

	ginkgo.It("can pause a node", func() {
		// shuts down the node and keeps all db/conf data for a proper restart
		_, err := anrCli.PauseNode(
			context.Background(),
			"sync",
		)
		gomega.Expect(err).To(gomega.BeNil())

		awaitHealthy(anrCli)

		ok, err := hsyncClient.Ping(context.Background())
		gomega.Ω(ok).Should(gomega.BeFalse())
		gomega.Ω(err).Should(gomega.HaveOccurred())
	})

	ginkgo.It("supports issuance of 256 more blocks", func() {
		count += generateBlocks(context.Background(), count, 256, instancesA, true)
		// TODO: verify all roots are equal
	})

	ginkgo.It("can re-sync the restarted node", func() {
		_, err := anrCli.ResumeNode(
			context.Background(),
			"sync",
		)
		gomega.Expect(err).To(gomega.BeNil())

		awaitHealthy(anrCli)
	})

	ginkgo.It("accepts transaction after restarted node state sync", func() {
		acceptTransaction(hsyncClient, nsyncClient)
	})

	ginkgo.It("state sync while broadcasting transactions", func() {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			// Recover failure if exits
			defer ginkgo.GinkgoRecover()

			count += generateBlocks(ctx, count, 0, instancesA, false)
		}()

		// Give time for transactions to start processing
		time.Sleep(5 * time.Second)

		// Start syncing node
		cluster, err := anrCli.AddNode(
			context.Background(),
			"sync_concurrent",
			execPath,
			trackSubnetsOpt,
			runner_sdk.WithChainConfigs(map[string]string{
				blockchainIDA: vmConfig,
			}),
		)
		gomega.Expect(err).To(gomega.BeNil())
		awaitHealthy(anrCli)

		nodeURI := cluster.ClusterInfo.NodeInfos["sync_concurrent"].Uri
		uri := nodeURI + fmt.Sprintf("/ext/bc/%s", blockchainIDA)
		bid, err := ids.FromString(blockchainIDA)
		gomega.Expect(err).To(gomega.BeNil())
		hutils.Outf("{{blue}}sync node uri: %s{{/}}\n", uri)
		hsyncClient = hrpc.NewJSONRPCClient(uri)
		networkID, _, _, err := hsyncClient.Network(context.TODO())
		gomega.Expect(err).To(gomega.BeNil())
		nsyncClient = nrpc.NewJSONRPCClient(uri, networkID, bid)
		cancel()
	})

	ginkgo.It("accepts transaction after state sync concurrent", func() {
		acceptTransaction(hsyncClient, nsyncClient)
	})

	// TODO: restart all nodes (crisis simulation)
})

func awaitHealthy(cli runner_sdk.Client) {
	for {
		time.Sleep(healthPollInterval)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := cli.Health(ctx)
		cancel() // by default, health will wait to return until healthy
		if err == nil {
			return
		}
		hutils.Outf(
			"{{yellow}}waiting for health check to pass:{{/}} %v\n",
			err,
		)
	}
}

// generate blocks until either ctx is cancelled or the specified (!= 0) number of blocks is generated.
// if 0 blocks are specified, will just wait until ctx is cancelled.
func generateBlocks(
	ctx context.Context,
	cumulativeTxs int,
	blocksToGenerate uint64,
	instances []instance,
	failOnError bool,
) int {
	_, lastHeight, _, err := instances[0].hcli.Accepted(context.Background())
	gomega.Ω(err).Should(gomega.BeNil())
	parser, err := instances[0].ncli.Parser(context.Background())
	gomega.Ω(err).Should(gomega.BeNil())
	var targetHeight uint64
	if blocksToGenerate != 0 {
		targetHeight = lastHeight + blocksToGenerate
	}
	for ctx.Err() == nil {
		// Generate transaction
		other, err := ed25519.GeneratePrivateKey()
		gomega.Ω(err).Should(gomega.BeNil())
		submit, _, _, err := instances[cumulativeTxs%len(instances)].hcli.GenerateTransaction(
			context.Background(),
			parser,
			nil,
			&actions.Transfer{
				To:    auth.NewED25519Address(other.PublicKey()),
				Value: 1,
			},
			factory,
		)
		if failOnError {
			gomega.Ω(err).Should(gomega.BeNil())
		} else if err != nil {
			hutils.Outf(
				"{{yellow}}unable to generate transaction:{{/}} %v\n",
				err,
			)
			time.Sleep(5 * time.Second)
			continue
		}

		// Broadcast transactions
		err = submit(context.Background())
		if failOnError {
			gomega.Ω(err).Should(gomega.BeNil())
		} else if err != nil {
			hutils.Outf(
				"{{yellow}}tx broadcast failed:{{/}} %v\n",
				err,
			)
			time.Sleep(5 * time.Second)
			continue
		}
		cumulativeTxs++
		_, height, _, err := instances[0].hcli.Accepted(context.Background())
		if failOnError {
			gomega.Ω(err).Should(gomega.BeNil())
		} else if err != nil {
			hutils.Outf(
				"{{yellow}}height lookup failed:{{/}} %v\n",
				err,
			)
			time.Sleep(5 * time.Second)
			continue
		}
		if targetHeight != 0 && height > targetHeight {
			break
		} else if height > lastHeight {
			lastHeight = height
			hutils.Outf("{{yellow}}height=%d count=%d{{/}}\n", height, cumulativeTxs)
		}

		// Sleep for a very small amount of time to avoid overloading the
		// network with transactions (can generate very fast)
		time.Sleep(10 * time.Millisecond)
	}
	return cumulativeTxs
}

func acceptTransaction(hcli *hrpc.JSONRPCClient, ncli *nrpc.JSONRPCClient) {
	parser, err := ncli.Parser(context.Background())
	gomega.Ω(err).Should(gomega.BeNil())
	for {
		// Generate transaction
		other, err := ed25519.GeneratePrivateKey()
		gomega.Ω(err).Should(gomega.BeNil())
		unitPrices, err := hcli.UnitPrices(context.Background(), false)
		gomega.Ω(err).Should(gomega.BeNil())
		submit, tx, maxFee, err := hcli.GenerateTransaction(
			context.Background(),
			parser,
			nil,
			&actions.Transfer{
				To:    auth.NewED25519Address(other.PublicKey()),
				Value: sendAmount,
			},
			factory,
		)
		gomega.Ω(err).Should(gomega.BeNil())
		hutils.Outf("{{yellow}}generated transaction{{/}} prices: %+v maxFee: %d\n", unitPrices, maxFee)

		// Broadcast and wait for transaction
		gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
		hutils.Outf("{{yellow}}submitted transaction{{/}}\n")
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		success, _, err := ncli.WaitForTransaction(ctx, tx.ID())
		cancel()
		if err != nil {
			hutils.Outf("{{red}}cannot find transaction: %v{{/}}\n", err)
			continue
		}
		gomega.Ω(success).Should(gomega.BeTrue())
		hutils.Outf("{{yellow}}found transaction{{/}}\n")
		break
	}
}

// copyNodeInfo handles the entire process of copying signer.key & staking.* files from the source directory
// to the destination directory, after stripping out "/db" from the source path.
func copyNodeInfo(sourceLogPath, destDir string) error {
	// Step 1: Strip out "/db" from the source path
	basePath := strings.TrimSuffix(sourceLogPath, "/db")

	// Ensure the destination directory exists, create it if it doesn't
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Define the full path for signer.key
	signerKeyPath := filepath.Join(basePath, "signer.key")

	// Step 2: Copy the signer.key file if it exists
	if _, err := os.Stat(signerKeyPath); err == nil {
		destSignerKeyPath := filepath.Join(destDir, "signer.key")
		if err := forceCopyFile(signerKeyPath, destSignerKeyPath); err != nil {
			return fmt.Errorf("failed to copy signer.key: %w", err)
		}
	} else if !os.IsNotExist(err) {
		// If the error is not due to the file not existing, return the error
		return fmt.Errorf("error checking signer.key: %w", err)
	}

	// Step 3: Copy files matching "staking.*" from source to destination, force overwrite
	return filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Match only files that start with "staking."
		if matched, _ := filepath.Match("staking.*", info.Name()); matched {
			destPath := filepath.Join(destDir, info.Name())
			return forceCopyFile(path, destPath)
		}
		return nil
	})
}

// forceCopyFile copies a file from src to dst, overwriting the dst file if it exists.
func forceCopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Use os.Create to open the destination file for writing, creating it if it doesn't exist
	// or truncating it if it does.
	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}
