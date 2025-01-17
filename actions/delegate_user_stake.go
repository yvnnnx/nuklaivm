// Copyright (C) 2024, AllianceBlock. All rights reserved.
// See the file LICENSE for licensing terms.

package actions

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	hconsts "github.com/ava-labs/hypersdk/consts"
	"github.com/ava-labs/hypersdk/state"
	"github.com/ava-labs/hypersdk/utils"
	"github.com/nuklai/nuklaivm/emission"
	"github.com/nuklai/nuklaivm/storage"

	nconsts "github.com/nuklai/nuklaivm/consts"
)

var _ chain.Action = (*DelegateUserStake)(nil)

type DelegateUserStake struct {
	NodeID          []byte        `json:"nodeID"`          // Node ID of the validator to stake to
	StakeStartBlock uint64        `json:"stakeStartBlock"` // Block height at which the stake should be made
	StakeEndBlock   uint64        `json:"stakeEndBlock"`   // Block height at which the stake should end
	StakedAmount    uint64        `json:"stakedAmount"`    // Amount of NAI staked
	RewardAddress   codec.Address `json:"rewardAddress"`   // Address to receive rewards
}

func (*DelegateUserStake) GetTypeID() uint8 {
	return nconsts.DelegateUserStakeID
}

func (s *DelegateUserStake) StateKeys(actor codec.Address, _ ids.ID) []string {
	nodeID, _ := ids.ToNodeID(s.NodeID)
	return []string{
		string(storage.BalanceKey(actor, ids.Empty)),
		string(storage.DelegateUserStakeKey(actor, nodeID)),
		string(storage.RegisterValidatorStakeKey(nodeID)),
	}
}

func (*DelegateUserStake) StateKeysMaxChunks() []uint16 {
	return []uint16{storage.BalanceChunks, storage.DelegateUserStakeChunks, storage.RegisterValidatorStakeChunks}
}

func (*DelegateUserStake) OutputsWarpMessage() bool {
	return false
}

func (s *DelegateUserStake) Execute(
	ctx context.Context,
	_ chain.Rules,
	mu state.Mutable,
	_ int64,
	actor codec.Address,
	_ ids.ID,
	_ bool,
) (bool, uint64, []byte, *warp.UnsignedMessage, error) {
	nodeID, err := ids.ToNodeID(s.NodeID)
	if err != nil {
		return false, DelegateUserStakeComputeUnits, OutputInvalidNodeID, nil, nil
	}

	// Check if the validator the user is trying to delegate to is registered for staking
	exists, _, _, _, _, _, _, _ := storage.GetRegisterValidatorStake(ctx, mu, nodeID)
	if !exists {
		return false, RegisterValidatorStakeComputeUnits, OutputValidatorNotYetRegistered, nil, nil
	}

	// Check if the user has already delegated to this validator node before
	exists, _, _, _, _, _, _ = storage.GetDelegateUserStake(ctx, mu, actor, nodeID)
	if exists {
		return false, DelegateUserStakeComputeUnits, OutputUserAlreadyStaked, nil, nil
	}

	stakingConfig := emission.GetStakingConfig()

	// Check if the staked amount is a valid amount
	if s.StakedAmount < stakingConfig.MinDelegatorStake {
		return false, DelegateUserStakeComputeUnits, OutputDelegateStakedAmountInvalid, nil, nil
	}

	// Check if stakeEndBlock is greater than stakeStartBlock
	if s.StakeEndBlock <= s.StakeStartBlock {
		return false, DelegateUserStakeComputeUnits, OutputInvalidStakeEndBlock, nil, nil
	}

	// Get the emission instance
	emissionInstance := emission.GetEmission()

	// Check if stakeStartBlock is smaller than the current block height
	if s.StakeStartBlock < emissionInstance.GetLastAcceptedBlockHeight() {
		return false, DelegateUserStakeComputeUnits, OutputInvalidStakeStartBlock, nil, nil
	}

	// Delegate in Emission Balancer
	err = emissionInstance.DelegateUserStake(nodeID, actor, s.StakeStartBlock, s.StakeEndBlock, s.StakedAmount)
	if err != nil {
		return false, DelegateUserStakeComputeUnits, utils.ErrBytes(err), nil, nil
	}

	if err := storage.SubBalance(ctx, mu, actor, ids.Empty, s.StakedAmount); err != nil {
		return false, DelegateUserStakeComputeUnits, utils.ErrBytes(err), nil, nil
	}
	if err := storage.SetDelegateUserStake(ctx, mu, actor, nodeID, s.StakeStartBlock, s.StakeEndBlock, s.StakedAmount, s.RewardAddress); err != nil {
		return false, DelegateUserStakeComputeUnits, utils.ErrBytes(err), nil, nil
	}
	return true, DelegateUserStakeComputeUnits, nil, nil, nil
}

func (*DelegateUserStake) MaxComputeUnits(chain.Rules) uint64 {
	return DelegateUserStakeComputeUnits
}

func (*DelegateUserStake) Size() int {
	return hconsts.NodeIDLen + 3*hconsts.Uint64Len + codec.AddressLen
}

func (s *DelegateUserStake) Marshal(p *codec.Packer) {
	p.PackBytes(s.NodeID)
	p.PackUint64(s.StakeStartBlock)
	p.PackUint64(s.StakeEndBlock)
	p.PackUint64(s.StakedAmount)
	p.PackAddress(s.RewardAddress)
}

func UnmarshalDelegateUserStake(p *codec.Packer, _ *warp.Message) (chain.Action, error) {
	var stake DelegateUserStake
	p.UnpackBytes(hconsts.NodeIDLen, true, &stake.NodeID)
	stake.StakeStartBlock = p.UnpackUint64(true)
	stake.StakeEndBlock = p.UnpackUint64(true)
	stake.StakedAmount = p.UnpackUint64(true)
	p.UnpackAddress(&stake.RewardAddress)
	return &stake, p.Err()
}

func (*DelegateUserStake) ValidRange(chain.Rules) (int64, int64) {
	// Returning -1, -1 means that the action is always valid.
	return -1, -1
}
