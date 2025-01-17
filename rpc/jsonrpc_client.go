// Copyright (C) 2024, AllianceBlock. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/requester"
	"github.com/ava-labs/hypersdk/rpc"
	"github.com/ava-labs/hypersdk/utils"

	nconsts "github.com/nuklai/nuklaivm/consts"
	"github.com/nuklai/nuklaivm/emission"
	"github.com/nuklai/nuklaivm/genesis"
	_ "github.com/nuklai/nuklaivm/registry" // ensure registry populated
)

type JSONRPCClient struct {
	requester *requester.EndpointRequester

	networkID uint32
	chainID   ids.ID
	g         *genesis.Genesis
	assetsL   sync.Mutex
	assets    map[ids.ID]*AssetReply
}

// New creates a new client object.
func NewJSONRPCClient(uri string, networkID uint32, chainID ids.ID) *JSONRPCClient {
	uri = strings.TrimSuffix(uri, "/")
	uri += JSONRPCEndpoint
	req := requester.New(uri, nconsts.Name)
	return &JSONRPCClient{
		requester: req,
		networkID: networkID,
		chainID:   chainID,
		assets:    map[ids.ID]*AssetReply{},
	}
}

func (cli *JSONRPCClient) Genesis(ctx context.Context) (*genesis.Genesis, error) {
	if cli.g != nil {
		return cli.g, nil
	}

	resp := new(GenesisReply)
	err := cli.requester.SendRequest(
		ctx,
		"genesis",
		nil,
		resp,
	)
	if err != nil {
		return nil, err
	}
	cli.g = resp.Genesis
	return resp.Genesis, nil
}

func (cli *JSONRPCClient) Tx(ctx context.Context, id ids.ID) (bool, bool, int64, uint64, error) {
	resp := new(TxReply)
	err := cli.requester.SendRequest(
		ctx,
		"tx",
		&TxArgs{TxID: id},
		resp,
	)
	switch {
	// We use string parsing here because the JSON-RPC library we use may not
	// allows us to perform errors.Is.
	case err != nil && strings.Contains(err.Error(), ErrTxNotFound.Error()):
		return false, false, -1, 0, nil
	case err != nil:
		return false, false, -1, 0, err
	}
	return true, resp.Success, resp.Timestamp, resp.Fee, nil
}

func (cli *JSONRPCClient) Asset(
	ctx context.Context,
	asset ids.ID,
	useCache bool,
) (bool, []byte, uint8, []byte, uint64, string, bool, error) {
	cli.assetsL.Lock()
	r, ok := cli.assets[asset]
	cli.assetsL.Unlock()
	if ok && useCache {
		return true, r.Symbol, r.Decimals, r.Metadata, r.Supply, r.Owner, r.Warp, nil
	}
	resp := new(AssetReply)
	err := cli.requester.SendRequest(
		ctx,
		"asset",
		&AssetArgs{
			Asset: asset,
		},
		resp,
	)
	switch {
	// We use string parsing here because the JSON-RPC library we use may not
	// allows us to perform errors.Is.
	case err != nil && strings.Contains(err.Error(), ErrAssetNotFound.Error()):
		return false, nil, 0, nil, 0, "", false, nil
	case err != nil:
		return false, nil, 0, nil, 0, "", false, err
	}
	cli.assetsL.Lock()
	cli.assets[asset] = resp
	cli.assetsL.Unlock()
	return true, resp.Symbol, resp.Decimals, resp.Metadata, resp.Supply, resp.Owner, resp.Warp, nil
}

func (cli *JSONRPCClient) Balance(ctx context.Context, addr string, asset ids.ID) (uint64, error) {
	resp := new(BalanceReply)
	err := cli.requester.SendRequest(
		ctx,
		"balance",
		&BalanceArgs{
			Address: addr,
			Asset:   asset,
		},
		resp,
	)
	return resp.Amount, err
}

func (cli *JSONRPCClient) Loan(
	ctx context.Context,
	asset ids.ID,
	destination ids.ID,
) (uint64, error) {
	resp := new(LoanReply)
	err := cli.requester.SendRequest(
		ctx,
		"loan",
		&LoanArgs{
			Asset:       asset,
			Destination: destination,
		},
		resp,
	)
	return resp.Amount, err
}

func (cli *JSONRPCClient) EmissionInfo(ctx context.Context) (uint64, uint64, uint64, uint64, uint64, emission.EmissionAccount, emission.EpochTracker, error) {
	resp := new(EmissionReply)
	err := cli.requester.SendRequest(
		ctx,
		"emissionInfo",
		nil,
		resp,
	)
	if err != nil {
		return 0, 0, 0, 0, 0, emission.EmissionAccount{}, emission.EpochTracker{}, err
	}

	return resp.CurrentBlockHeight, resp.TotalSupply, resp.MaxSupply, resp.TotalStaked, resp.RewardsPerEpoch, resp.EmissionAccount, resp.EpochTracker, err
}

func (cli *JSONRPCClient) AllValidators(ctx context.Context) ([]*emission.Validator, error) {
	resp := new(ValidatorsReply)
	err := cli.requester.SendRequest(
		ctx,
		"allValidators",
		nil,
		resp,
	)
	if err != nil {
		return []*emission.Validator{}, err
	}
	return resp.Validators, err
}

func (cli *JSONRPCClient) StakedValidators(ctx context.Context) ([]*emission.Validator, error) {
	resp := new(ValidatorsReply)
	err := cli.requester.SendRequest(
		ctx,
		"stakedValidators",
		nil,
		resp,
	)
	if err != nil {
		return []*emission.Validator{}, err
	}
	return resp.Validators, err
}

func (cli *JSONRPCClient) ValidatorStake(ctx context.Context, nodeID ids.NodeID) (uint64, uint64, uint64, uint64, codec.Address, codec.Address, error) {
	resp := new(ValidatorStakeReply)
	err := cli.requester.SendRequest(
		ctx,
		"validatorStake",
		&ValidatorStakeArgs{
			NodeID: nodeID,
		},
		resp,
	)
	if err != nil {
		return 0, 0, 0, 0, codec.EmptyAddress, codec.EmptyAddress, err
	}
	return resp.StakeStartBlock, resp.StakeEndBlock, resp.StakedAmount, resp.DelegationFeeRate, resp.RewardAddress, resp.OwnerAddress, err
}

func (cli *JSONRPCClient) UserStake(ctx context.Context, owner codec.Address, nodeID ids.NodeID) (uint64, uint64, uint64, codec.Address, codec.Address, error) {
	resp := new(UserStakeReply)
	err := cli.requester.SendRequest(
		ctx,
		"userStake",
		&UserStakeArgs{
			Owner:  owner,
			NodeID: nodeID,
		},
		resp,
	)
	if err != nil {
		return 0, 0, 0, codec.EmptyAddress, codec.EmptyAddress, err
	}
	return resp.StakeStartBlock, resp.StakeEndBlock, resp.StakedAmount, resp.RewardAddress, resp.OwnerAddress, err
}

func (cli *JSONRPCClient) WaitForBalance(
	ctx context.Context,
	addr string,
	asset ids.ID,
	min uint64,
) error {
	exists, symbol, decimals, _, _, _, _, err := cli.Asset(ctx, asset, true)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s does not exist", asset)
	}

	return rpc.Wait(ctx, func(ctx context.Context) (bool, error) {
		balance, err := cli.Balance(ctx, addr, asset)
		if err != nil {
			return false, err
		}
		shouldExit := balance >= min
		if !shouldExit {
			utils.Outf(
				"{{yellow}}waiting for %s %s on %s{{/}}\n",
				utils.FormatBalance(min, decimals),
				symbol,
				addr,
			)
		}
		return shouldExit, nil
	})
}

func (cli *JSONRPCClient) WaitForTransaction(ctx context.Context, txID ids.ID) (bool, uint64, error) {
	var success bool
	var fee uint64
	if err := rpc.Wait(ctx, func(ctx context.Context) (bool, error) {
		found, isuccess, _, ifee, err := cli.Tx(ctx, txID)
		if err != nil {
			return false, err
		}
		success = isuccess
		fee = ifee
		return found, nil
	}); err != nil {
		return false, 0, err
	}
	return success, fee, nil
}

var _ chain.Parser = (*Parser)(nil)

type Parser struct {
	networkID uint32
	chainID   ids.ID
	genesis   *genesis.Genesis
}

func (p *Parser) Rules(t int64) chain.Rules {
	return p.genesis.Rules(t, p.networkID, p.chainID)
}

func (*Parser) Registry() (chain.ActionRegistry, chain.AuthRegistry) {
	return nconsts.ActionRegistry, nconsts.AuthRegistry
}

func (cli *JSONRPCClient) Parser(ctx context.Context) (chain.Parser, error) {
	g, err := cli.Genesis(ctx)
	if err != nil {
		return nil, err
	}
	return &Parser{cli.networkID, cli.chainID, g}, nil
}
