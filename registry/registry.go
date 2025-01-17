// Copyright (C) 2024, AllianceBlock. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"

	"github.com/nuklai/nuklaivm/actions"
	"github.com/nuklai/nuklaivm/auth"
	nconsts "github.com/nuklai/nuklaivm/consts"
)

// Setup types
func init() {
	nconsts.ActionRegistry = codec.NewTypeParser[chain.Action, *warp.Message]()
	nconsts.AuthRegistry = codec.NewTypeParser[chain.Auth, *warp.Message]()

	errs := &wrappers.Errs{}
	errs.Add(
		// When registering new actions, ALWAYS make sure to append at the end.
		nconsts.ActionRegistry.Register((&actions.Transfer{}).GetTypeID(), actions.UnmarshalTransfer, false),

		nconsts.ActionRegistry.Register((&actions.CreateAsset{}).GetTypeID(), actions.UnmarshalCreateAsset, false),
		nconsts.ActionRegistry.Register((&actions.MintAsset{}).GetTypeID(), actions.UnmarshalMintAsset, false),
		nconsts.ActionRegistry.Register((&actions.BurnAsset{}).GetTypeID(), actions.UnmarshalBurnAsset, false),
		nconsts.ActionRegistry.Register((&actions.ImportAsset{}).GetTypeID(), actions.UnmarshalImportAsset, true),
		nconsts.ActionRegistry.Register((&actions.ExportAsset{}).GetTypeID(), actions.UnmarshalExportAsset, false),

		nconsts.ActionRegistry.Register((&actions.RegisterValidatorStake{}).GetTypeID(), actions.UnmarshalRegisterValidatorStake, false),
		nconsts.ActionRegistry.Register((&actions.ClaimValidatorStakeRewards{}).GetTypeID(), actions.UnmarshalClaimValidatorStakeRewards, false),
		nconsts.ActionRegistry.Register((&actions.WithdrawValidatorStake{}).GetTypeID(), actions.UnmarshalWithdrawValidatorStake, false),
		nconsts.ActionRegistry.Register((&actions.DelegateUserStake{}).GetTypeID(), actions.UnmarshalDelegateUserStake, false),
		nconsts.ActionRegistry.Register((&actions.ClaimDelegationStakeRewards{}).GetTypeID(), actions.UnmarshalClaimDelegationStakeRewards, false),
		nconsts.ActionRegistry.Register((&actions.UndelegateUserStake{}).GetTypeID(), actions.UnmarshalUndelegateUserStake, false),

		// When registering new auth, ALWAYS make sure to append at the end.
		nconsts.AuthRegistry.Register((&auth.ED25519{}).GetTypeID(), auth.UnmarshalED25519, false),
		nconsts.AuthRegistry.Register((&auth.SECP256R1{}).GetTypeID(), auth.UnmarshalSECP256R1, false),
		nconsts.AuthRegistry.Register((&auth.BLS{}).GetTypeID(), auth.UnmarshalBLS, false),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
