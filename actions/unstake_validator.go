// Copyright (C) 2024, AllianceBlock. All rights reserved.
// See the file LICENSE for licensing terms.

package actions

import (
	"bytes"
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/consts"
	"github.com/ava-labs/hypersdk/state"
	"github.com/ava-labs/hypersdk/utils"
	"github.com/nuklai/nuklaivm/storage"

	mconsts "github.com/nuklai/nuklaivm/consts"
)

var _ chain.Action = (*UnstakeValidator)(nil)

type UnstakeValidator struct {
	Stake  ids.ID `json:"stake"`
	NodeID []byte `json:"nodeID"`
}

func (*UnstakeValidator) GetTypeID() uint8 {
	return mconsts.UnstakeValidatorID
}

func (u *UnstakeValidator) StateKeys(auth chain.Auth, _ ids.ID) []string {
	return []string{
		string(storage.BalanceKey(auth.Actor())),
		string(storage.StakeKey(u.Stake)),
	}
}

func (*UnstakeValidator) StateKeysMaxChunks() []uint16 {
	return []uint16{storage.BalanceChunks, storage.StakeChunks}
}

func (*UnstakeValidator) OutputsWarpMessage() bool {
	return false
}

func (u *UnstakeValidator) Execute(
	ctx context.Context,
	_ chain.Rules,
	mu state.Mutable,
	_ int64,
	auth chain.Auth,
	_ ids.ID,
	_ bool,
) (bool, uint64, []byte, *warp.UnsignedMessage, error) {
	exists, nodeIDStaked, _, _, owner, err := storage.GetStake(ctx, mu, u.Stake)
	if err != nil {
		return false, UnstakeValidatorComputeUnits, utils.ErrBytes(err), nil, nil
	}
	if !exists {
		return false, UnstakeValidatorComputeUnits, OutputStakeMissing, nil, nil
	}
	if owner != auth.Actor() {
		return false, UnstakeValidatorComputeUnits, OutputUnauthorized, nil, nil
	}
	if !bytes.Equal(nodeIDStaked.Bytes(), u.NodeID) {
		return false, UnstakeValidatorComputeUnits, OutputDifferentNodeIDThanStaked, nil, nil
	}
	return true, UnstakeValidatorComputeUnits, nil, nil, nil
}

func (*UnstakeValidator) MaxComputeUnits(chain.Rules) uint64 {
	return UnstakeValidatorComputeUnits
}

func (*UnstakeValidator) Size() int {
	return consts.IDLen
}

func (u *UnstakeValidator) Marshal(p *codec.Packer) {
	p.PackID(u.Stake)
	p.PackBytes(u.NodeID)
}

func UnmarshalUnstakeValidator(p *codec.Packer, _ *warp.Message) (chain.Action, error) {
	var unstake UnstakeValidator
	p.UnpackID(true, &unstake.Stake)
	p.UnpackBytes(consts.NodeIDLen, false, &unstake.NodeID)
	return &unstake, p.Err()
}

func (*UnstakeValidator) ValidRange(chain.Rules) (int64, int64) {
	// Returning -1, -1 means that the action is always valid.
	return -1, -1
}