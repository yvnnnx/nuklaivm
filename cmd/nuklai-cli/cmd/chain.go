// Copyright (C) 2024, AllianceBlock. All rights reserved.
// See the file LICENSE for licensing terms.

package cmd

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/spf13/cobra"

	nrpc "github.com/nuklai/nuklaivm/rpc"
)

var chainCmd = &cobra.Command{
	Use: "chain",
	RunE: func(*cobra.Command, []string) error {
		return ErrMissingSubcommand
	},
}

var importChainCmd = &cobra.Command{
	Use: "import",
	RunE: func(_ *cobra.Command, args []string) error {
		return handler.Root().ImportChain()
	},
}

var importANRChainCmd = &cobra.Command{
	Use: "import-anr",
	RunE: func(_ *cobra.Command, args []string) error {
		return handler.Root().ImportANR()
	},
}

var importAvalancheCliChainCmd = &cobra.Command{
	Use: "import-cli [path]",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return ErrInvalidArgs
		}
		return nil
	},
	RunE: func(_ *cobra.Command, args []string) error {
		return handler.ImportCLI(args[0])
	},
}

var setChainCmd = &cobra.Command{
	Use: "set",
	RunE: func(*cobra.Command, []string) error {
		return handler.Root().SetDefaultChain()
	},
}

var chainInfoCmd = &cobra.Command{
	Use: "info",
	RunE: func(_ *cobra.Command, args []string) error {
		return handler.Root().PrintChainInfo()
	},
}

var watchChainCmd = &cobra.Command{
	Use: "watch",
	RunE: func(_ *cobra.Command, args []string) error {
		var cli *nrpc.JSONRPCClient
		return handler.Root().WatchChain(hideTxs, func(uri string, networkID uint32, chainID ids.ID) (chain.Parser, error) {
			cli = nrpc.NewJSONRPCClient(uri, networkID, chainID)
			return cli.Parser(context.TODO())
		}, func(tx *chain.Transaction, result *chain.Result) {
			if cli == nil {
				// Should never happen
				return
			}
			handleTx(cli, tx, result)
		})
	},
}
