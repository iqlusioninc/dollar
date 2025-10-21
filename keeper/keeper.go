// SPDX-License-Identifier: BUSL-1.1
//
// Copyright (C) 2025, NASD Inc. All rights reserved.
// Use of this software is governed by the Business Source License included
// in the LICENSE file of this repository and at www.mariadb.com/bsl11.
//
// ANY USE OF THE LICENSED WORK IN VIOLATION OF THIS LICENSE WILL AUTOMATICALLY
// TERMINATE YOUR RIGHTS UNDER THIS LICENSE FOR THE CURRENT AND ALL OTHER
// VERSIONS OF THE LICENSED WORK.
//
// THIS LICENSE DOES NOT GRANT YOU ANY RIGHT IN ANY TRADEMARK OR LOGO OF
// LICENSOR OR ITS AFFILIATES (PROVIDED THAT YOU MAY USE A TRADEMARK OR LOGO OF
// LICENSOR AS EXPRESSLY REQUIRED BY THIS LICENSE).
//
// TO THE EXTENT PERMITTED BY APPLICABLE LAW, THE LICENSED WORK IS PROVIDED ON
// AN "AS IS" BASIS. LICENSOR HEREBY DISCLAIMS ALL WARRANTIES AND CONDITIONS,
// EXPRESS OR IMPLIED, INCLUDING (WITHOUT LIMITATION) WARRANTIES OF
// MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, NON-INFRINGEMENT, AND
// TITLE.

package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/event"
	"cosmossdk.io/core/header"
	"cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"dollar.noble.xyz/v3/types"
	"dollar.noble.xyz/v3/types/portal"
	"dollar.noble.xyz/v3/types/portal/ntt"
	modulev2 "dollar.noble.xyz/v3/types/v2"
	"dollar.noble.xyz/v3/types/vaults"
	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

type Keeper struct {
	denom                         string
	authority                     string
	vaultsMinimumLock             int64
	vaultsMinimumUnlock           int64
	vaultsSeasonOneEndTimestamp   int64
	vaultsSeasonTwoYieldCollector sdk.AccAddress

	cdc   codec.Codec
	store store.KVStoreService

	logger   log.Logger
	header   header.Service
	event    event.Service
	address  address.Codec
	account  types.AccountKeeper
	bank     types.BankKeeper
	channel  types.ChannelKeeper
	transfer types.TransferKeeper
	warp     *warpkeeper.Keeper
	wormhole portal.WormholeKeeper

	Paused             collections.Item[bool]
	Index              collections.Item[int64]
	Principal          collections.Map[[]byte, math.Int]
	Stats              collections.Item[modulev2.Stats]
	TotalExternalYield collections.Map[collections.Pair[int32, string], math.Int]
	YieldRecipients    collections.Map[collections.Pair[int32, string], string]
	RetryAmounts       collections.Map[collections.Pair[int32, string], math.Int]

	VaultsV2Params                        collections.Item[vaultsv2.Params]
	VaultsV2Config                        collections.Item[vaultsv2.VaultConfig]
	VaultsV2UserPositionSequence          collections.Map[[]byte, uint64]
	VaultsV2UserPositions                 collections.Map[collections.Pair[[]byte, uint64], vaultsv2.UserPosition]
	VaultsV2DepositLimits                 collections.Item[vaultsv2.DepositLimit]
	VaultsV2UserDepositHistory            collections.Map[collections.Pair[[]byte, int64], math.Int]
	VaultsV2DepositVelocity               collections.Map[[]byte, vaultsv2.DepositVelocity]
	VaultsV2BlockDepositVolume            collections.Map[int64, math.Int]
	VaultsV2PendingDeploymentFunds        collections.Item[math.Int]
	VaultsV2PendingWithdrawalsAmount      collections.Item[math.Int]
	VaultsV2WithdrawalQueue               collections.Map[uint64, vaultsv2.WithdrawalRequest]
	VaultsV2WithdrawalNextID              collections.Item[uint64]
	VaultsV2NAVInfo                       collections.Item[vaultsv2.NAVInfo]
	VaultsV2VaultState                    collections.Item[vaultsv2.VaultState]
	VaultsV2AccountingCursor              collections.Item[vaultsv2.AccountingCursor]
	VaultsV2AccountingSnapshots           collections.Map[collections.Pair[[]byte, uint64], vaultsv2.AccountingSnapshot]
	VaultsV2RemotePositions               collections.Map[uint64, vaultsv2.RemotePosition]
	VaultsV2RemotePositionNextID          collections.Item[uint64]
	VaultsV2RemotePositionChains          collections.Map[uint64, uint32]
	VaultsV2RemotePositionOracles         collections.Map[uint64, vaultsv2.RemotePositionOracle]
	VaultsV2CrossChainRoutes              collections.Map[uint32, vaultsv2.CrossChainRoute]
	VaultsV2CrossChainRouteNextID         collections.Item[uint32]
	VaultsV2InflightFunds                 collections.Map[string, vaultsv2.InflightFund]
	VaultsV2InflightNextID                collections.Item[uint64]
	VaultsV2EnrolledOracles               collections.Map[string, vaultsv2.EnrolledOracle]
	VaultsV2OracleParams                  collections.Item[vaultsv2.OracleGovernanceParams]
	VaultsV2InflightValueByRoute          collections.Map[uint32, math.Int]
	VaultsV2PendingWithdrawalDistribution collections.Item[math.Int]
	VaultsV2NAVSnapshots                  collections.Map[int64, vaultsv2.NAVSnapshot]
	VaultsV2NAVSnapshotNextID             collections.Item[int64]

	PortalOwner         collections.Item[string]
	PortalPaused        collections.Item[bool]
	PortalPeers         collections.Map[uint16, portal.Peer]
	PortalBridgingPaths collections.Map[collections.Pair[uint16, []byte], bool]
	PortalNonce         collections.Item[uint32]

	VaultsPaused                 collections.Item[int32]
	VaultsSeasonOneEnded         collections.Item[bool]
	VaultsPositions              *collections.IndexedMap[collections.Triple[[]byte, int32, int64], vaults.Position, VaultsPositionsIndexes]
	VaultsTotalFlexiblePrincipal collections.Item[math.Int]
	VaultsRewards                collections.Map[int64, vaults.Reward]
	VaultsStats                  collections.Item[vaults.Stats]
}

func NewKeeper(
	denom string,
	authority string,
	vaultsMinimumLock int64,
	vaultsMinimumUnlock int64,
	vaultsSeasonOneEndTimestamp int64,
	vaultsSeasonTwoYieldCollector sdk.AccAddress,
	cdc codec.Codec,
	store store.KVStoreService,
	logger log.Logger,
	header header.Service,
	event event.Service,
	address address.Codec,
	account types.AccountKeeper,
	bank types.BankKeeper,
	channel types.ChannelKeeper,
	transfer types.TransferKeeper,
	warp *warpkeeper.Keeper,
	wormhole portal.WormholeKeeper,
) *Keeper {
	transceiverAddress := authtypes.NewModuleAddress(fmt.Sprintf("%s/transceiver", portal.SubmoduleName))
	copy(portal.PaddedTransceiverAddress[12:], transceiverAddress)
	portal.TransceiverAddress, _ = address.BytesToString(transceiverAddress)

	managerAddress := authtypes.NewModuleAddress(fmt.Sprintf("%s/manager", portal.SubmoduleName))
	copy(portal.PaddedManagerAddress[12:], managerAddress)
	portal.ManagerAddress, _ = address.BytesToString(managerAddress)

	bz := []byte(denom)
	copy(portal.RawToken[32-len(bz):], bz)

	builder := collections.NewSchemaBuilder(store)

	keeper := &Keeper{
		denom:                         denom,
		authority:                     authority,
		vaultsMinimumLock:             vaultsMinimumLock,
		vaultsMinimumUnlock:           vaultsMinimumUnlock,
		vaultsSeasonOneEndTimestamp:   vaultsSeasonOneEndTimestamp,
		vaultsSeasonTwoYieldCollector: vaultsSeasonTwoYieldCollector,

		cdc:   cdc,
		store: store,

		logger:   logger.With("module", types.ModuleName),
		header:   header,
		event:    event,
		address:  address,
		account:  account,
		bank:     bank,
		channel:  channel,
		transfer: transfer,
		warp:     warp,
		wormhole: wormhole,

		Paused:             collections.NewItem(builder, types.PausedKey, "paused", collections.BoolValue),
		Index:              collections.NewItem(builder, types.IndexKey, "index", collections.Int64Value),
		Principal:          collections.NewMap(builder, types.PrincipalPrefix, "principal", collections.BytesKey, sdk.IntValue),
		Stats:              collections.NewItem(builder, types.StatsKey, "stats", codec.CollValue[modulev2.Stats](cdc)),
		TotalExternalYield: collections.NewMap(builder, types.TotalExternalYieldPrefix, "total_external_yield", collections.PairKeyCodec(collections.Int32Key, collections.StringKey), sdk.IntValue),
		YieldRecipients:    collections.NewMap(builder, types.YieldRecipientPrefix, "yield_recipients", collections.PairKeyCodec(collections.Int32Key, collections.StringKey), collections.StringValue),
		RetryAmounts:       collections.NewMap(builder, types.RetryAmountPrefix, "retry_amounts", collections.PairKeyCodec(collections.Int32Key, collections.StringKey), sdk.IntValue),

		VaultsV2Params:                        collections.NewItem(builder, vaultsv2.ParamsKey, "vaults_v2_params", codec.CollValue[vaultsv2.Params](cdc)),
		VaultsV2Config:                        collections.NewItem(builder, vaultsv2.VaultConfigurationKey, "vaults_v2_config", codec.CollValue[vaultsv2.VaultConfig](cdc)),
		VaultsV2UserPositionSequence:          collections.NewMap(builder, vaultsv2.UserPositionSequencePrefix, "vaults_v2_user_position_sequence", collections.BytesKey, collections.Uint64Value),
		VaultsV2UserPositions:                 collections.NewMap(builder, vaultsv2.UserPositionPrefix, "vaults_v2_user_positions", collections.PairKeyCodec(collections.BytesKey, collections.Uint64Key), codec.CollValue[vaultsv2.UserPosition](cdc)),
		VaultsV2DepositLimits:                 collections.NewItem(builder, vaultsv2.DepositLimitsKey, "vaults_v2_deposit_limits", codec.CollValue[vaultsv2.DepositLimit](cdc)),
		VaultsV2UserDepositHistory:            collections.NewMap(builder, vaultsv2.UserDepositHistoryPrefix, "vaults_v2_user_deposit_history", collections.PairKeyCodec(collections.BytesKey, collections.Int64Key), sdk.IntValue),
		VaultsV2DepositVelocity:               collections.NewMap(builder, vaultsv2.DepositVelocityPrefix, "vaults_v2_deposit_velocity", collections.BytesKey, codec.CollValue[vaultsv2.DepositVelocity](cdc)),
		VaultsV2BlockDepositVolume:            collections.NewMap(builder, vaultsv2.BlockDepositVolumePrefix, "vaults_v2_block_deposit_volume", collections.Int64Key, sdk.IntValue),
		VaultsV2PendingDeploymentFunds:        collections.NewItem(builder, vaultsv2.PendingDeploymentFundsKey, "vaults_v2_pending_deployment", sdk.IntValue),
		VaultsV2PendingWithdrawalsAmount:      collections.NewItem(builder, vaultsv2.PendingWithdrawalsKey, "vaults_v2_pending_withdrawals", sdk.IntValue),
		VaultsV2WithdrawalQueue:               collections.NewMap(builder, vaultsv2.WithdrawalQueuePrefix, "vaults_v2_withdrawal_queue", collections.Uint64Key, codec.CollValue[vaultsv2.WithdrawalRequest](cdc)),
		VaultsV2WithdrawalNextID:              collections.NewItem(builder, vaultsv2.WithdrawalQueueNextIDKey, "vaults_v2_withdrawal_next_id", collections.Uint64Value),
		VaultsV2NAVInfo:                       collections.NewItem(builder, vaultsv2.NAVInfoKey, "vaults_v2_nav_info", codec.CollValue[vaultsv2.NAVInfo](cdc)),
		VaultsV2VaultState:                    collections.NewItem(builder, vaultsv2.VaultStateKey, "vaults_v2_vault_state", codec.CollValue[vaultsv2.VaultState](cdc)),
		VaultsV2AccountingCursor:              collections.NewItem(builder, vaultsv2.AccountingCursorKey, "vaults_v2_accounting_cursor", codec.CollValue[vaultsv2.AccountingCursor](cdc)),
		VaultsV2AccountingSnapshots:           collections.NewMap(builder, vaultsv2.AccountingSnapshotPrefix, "vaults_v2_accounting_snapshots", collections.PairKeyCodec(collections.BytesKey, collections.Uint64Key), codec.CollValue[vaultsv2.AccountingSnapshot](cdc)),
		VaultsV2RemotePositions:               collections.NewMap(builder, vaultsv2.RemotePositionPrefix, "vaults_v2_remote_positions", collections.Uint64Key, codec.CollValue[vaultsv2.RemotePosition](cdc)),
		VaultsV2RemotePositionNextID:          collections.NewItem(builder, vaultsv2.RemotePositionNextIDKey, "vaults_v2_remote_position_next_id", collections.Uint64Value),
		VaultsV2RemotePositionChains:          collections.NewMap(builder, vaultsv2.RemotePositionChainPrefix, "vaults_v2_remote_position_chains", collections.Uint64Key, collections.Uint32Value),
		VaultsV2RemotePositionOracles:         collections.NewMap(builder, vaultsv2.RemotePositionOraclesPrefix, "vaults_v2_remote_position_oracles", collections.Uint64Key, codec.CollValue[vaultsv2.RemotePositionOracle](cdc)),
		VaultsV2CrossChainRoutes:              collections.NewMap(builder, vaultsv2.CrossChainRoutePrefix, "vaults_v2_cross_chain_routes", collections.Uint32Key, codec.CollValue[vaultsv2.CrossChainRoute](cdc)),
		VaultsV2CrossChainRouteNextID:         collections.NewItem(builder, vaultsv2.CrossChainRouteNextIDKey, "vaults_v2_cross_chain_route_next_id", collections.Uint32Value),
		VaultsV2InflightFunds:                 collections.NewMap(builder, vaultsv2.InflightFundsPrefix, "vaults_v2_inflight_funds", collections.StringKey, codec.CollValue[vaultsv2.InflightFund](cdc)),
		VaultsV2InflightNextID:                collections.NewItem(builder, vaultsv2.InflightNextIDKey, "vaults_v2_inflight_next_id", collections.Uint64Value),
		VaultsV2EnrolledOracles:               collections.NewMap(builder, vaultsv2.EnrolledOraclePrefix, "vaults_v2_enrolled_oracles", collections.StringKey, codec.CollValue[vaultsv2.EnrolledOracle](cdc)),
		VaultsV2OracleParams:                  collections.NewItem(builder, vaultsv2.OracleParamsKey, "vaults_v2_oracle_params", codec.CollValue[vaultsv2.OracleGovernanceParams](cdc)),
		VaultsV2InflightValueByRoute:          collections.NewMap(builder, vaultsv2.InflightValueByRoutePrefix, "vaults_v2_inflight_value_by_route", collections.Uint32Key, sdk.IntValue),
		VaultsV2PendingWithdrawalDistribution: collections.NewItem(builder, vaultsv2.PendingWithdrawalDistributionKey, "vaults_v2_pending_withdrawal_distribution", sdk.IntValue),
		VaultsV2NAVSnapshots:                  collections.NewMap(builder, vaultsv2.NAVSnapshotsPrefix, "vaults_v2_nav_snapshots", collections.Int64Key, codec.CollValue[vaultsv2.NAVSnapshot](cdc)),
		VaultsV2NAVSnapshotNextID:             collections.NewItem(builder, vaultsv2.NAVSnapshotNextIDKey, "vaults_v2_nav_snapshot_next_id", collections.Int64Value),

		PortalOwner:         collections.NewItem(builder, portal.OwnerKey, "portal_owner", collections.StringValue),
		PortalPaused:        collections.NewItem(builder, portal.PausedKey, "portal_paused", collections.BoolValue),
		PortalPeers:         collections.NewMap(builder, portal.PeerPrefix, "portal_peers", collections.Uint16Key, codec.CollValue[portal.Peer](cdc)),
		PortalBridgingPaths: collections.NewMap(builder, portal.BridgingPathPrefix, "portal_bridging_paths", collections.PairKeyCodec(collections.Uint16Key, collections.BytesKey), collections.BoolValue),
		PortalNonce:         collections.NewItem(builder, portal.NonceKey, "portal_nonce", collections.Uint32Value),

		VaultsPaused:                 collections.NewItem(builder, vaults.PausedKey, "vaults_paused", collections.Int32Value),
		VaultsSeasonOneEnded:         collections.NewItem(builder, vaults.SeasonOneEndedKey, "vaults_season_one_ended", collections.BoolValue),
		VaultsPositions:              collections.NewIndexedMap(builder, vaults.PositionPrefix, "vaults_positions", collections.TripleKeyCodec(collections.BytesKey, collections.Int32Key, collections.Int64Key), codec.CollValue[vaults.Position](cdc), NewVaultsPositionsIndexes(builder)),
		VaultsTotalFlexiblePrincipal: collections.NewItem(builder, vaults.TotalFlexiblePrincipalKey, "vaults_total_flexible_principal", sdk.IntValue),
		VaultsRewards:                collections.NewMap(builder, vaults.RewardPrefix, "vaults_rewards", collections.Int64Key, codec.CollValue[vaults.Reward](cdc)),
		VaultsStats:                  collections.NewItem(builder, vaults.StatsKey, "vaults_stats", codec.CollValue[vaults.Stats](cdc)),
	}

	_, err := builder.Build()
	if err != nil {
		panic(err)
	}

	return keeper
}

// SetBankKeeper overwrites the bank keeper used in this module.
func (k *Keeper) SetBankKeeper(bankKeeper types.BankKeeper) {
	k.bank = bankKeeper
}

// SetIBCKeepers overrides the provided IBC specific keepers for this module.
// This exists because IBC doesn't support dependency injection.
func (k *Keeper) SetIBCKeepers(channel types.ChannelKeeper, transfer types.TransferKeeper) {
	k.channel = channel
	k.transfer = transfer
}

// SendRestrictionFn performs an underlying transfer of principal when executing a $USDN transfer.
func (k *Keeper) SendRestrictionFn(ctx context.Context, sender, recipient sdk.AccAddress, coins sdk.Coins) (newRecipient sdk.AccAddress, err error) {
	coin := coins.AmountOf(k.denom)
	if amount := coin; !amount.IsZero() {
		// We don't want to perform any principal updates in the case of yield payout.
		// -> Transfer from Yield to User account.
		if sender.Equals(types.YieldAddress) {
			return recipient, nil
		}
		// Handle transfers where the recipient is the yield account.
		if recipient.Equals(types.YieldAddress) {
			if sender.Equals(types.ModuleAddress) {
				// We don't want to perform any principal updates in the case of yield accrual.
				// -> Transfer from Module to Yield account.
				return recipient, nil
			} else {
				// We don't want to allow any other transfers to the yield account.
				// -> Transfer from User to Yield account.
				return recipient, fmt.Errorf("transfers of %s to %s are not allowed", k.denom, recipient.String())
			}
		}

		// When burning and transferring, the $M token executes the
		// `_getPrincipalAmountRoundedUp` function. When minting, the $M token
		// executes the `_getPrincipalAmountRoundedDown` function. As $USDN
		// inherits the yielding properties of $M, we mimic that functionality
		// here.
		index, err := k.Index.Get(ctx)
		if err != nil {
			return recipient, sdkerrors.Wrap(err, "unable to get index from state")
		}
		principal := k.GetPrincipalAmountRoundedUp(amount, index)

		// We don't want to update the sender's principal in the case of issuance.
		// -> Transfer from Module to User account.
		if !sender.Equals(types.ModuleAddress) {
			senderPrincipal, err := k.Principal.Get(ctx, sender)
			if err != nil {
				if sdkerrors.IsOf(err, collections.ErrNotFound) {
					senderPrincipal = math.ZeroInt()
				} else {
					return recipient, sdkerrors.Wrap(err, "unable to get sender principal from state")
				}
			}
			err = k.Principal.Set(ctx, sender, senderPrincipal.Sub(principal))
			if err != nil {
				return recipient, sdkerrors.Wrap(err, "unable to set sender principal to state")
			}

			balance := k.bank.GetBalance(ctx, sender, k.denom)
			if balance.IsZero() {
				// If the sender's $USDN balance is zero, this indicates that
				// they are no longer a holder, and we should decrement the
				// statistic.
				err = k.DecrementTotalHolders(ctx)
				if err != nil {
					return recipient, sdkerrors.Wrap(err, "unable to decrement total holders")
				}
			}
		} else {
			err = k.IncrementTotalPrincipal(ctx, principal)
			if err != nil {
				return recipient, sdkerrors.Wrap(err, "unable to increment total principal")
			}
		}

		// We don't want to update the recipient's principal in the case of withdrawal.
		// -> Transfer from User to Module account.
		if !recipient.Equals(types.ModuleAddress) {
			if sender.Equals(types.ModuleAddress) {
				principal = k.GetPrincipalAmountRoundedDown(amount, index)
			}

			recipientPrincipal, err := k.Principal.Get(ctx, recipient)
			if err != nil {
				if sdkerrors.IsOf(err, collections.ErrNotFound) {
					recipientPrincipal = math.ZeroInt()
				} else {
					return recipient, sdkerrors.Wrap(err, "unable to get recipient principal from state")
				}
			}
			err = k.Principal.Set(ctx, recipient, recipientPrincipal.Add(principal))
			if err != nil {
				return recipient, sdkerrors.Wrap(err, "unable to set recipient principal to state")
			}

			balance := k.bank.GetBalance(ctx, recipient, k.denom)
			if balance.IsZero() {
				// If the recipient's $USDN balance is zero, this indicates
				// that they are a new holder, and we should increment the
				// statistic.
				err = k.IncrementTotalHolders(ctx)
				if err != nil {
					return recipient, sdkerrors.Wrap(err, "unable to increment total holders")
				}
			}
		} else {
			err = k.DecrementTotalPrincipal(ctx, principal)
			if err != nil {
				return recipient, sdkerrors.Wrap(err, "unable to decrement total principal")
			}
		}
	}

	return recipient, nil
}

// GetDenom is a utility that returns the configured denomination of $USDN.
func (k *Keeper) GetDenom() string {
	return k.denom
}

// GetVaultsSeasonTwoYieldCollector is a utility that returns the
// configured yield collector address for Vaults Season Two.
func (k *Keeper) GetVaultsSeasonTwoYieldCollector() sdk.AccAddress {
	return k.vaultsSeasonTwoYieldCollector
}

// GetYield is a utility that returns the user's current amount of claimable $USDN yield.
func (k *Keeper) GetYield(ctx context.Context, account string) (math.Int, []byte, error) {
	bz, err := k.address.StringToBytes(account)
	if err != nil {
		return math.ZeroInt(), nil, sdkerrors.Wrapf(err, "unable to decode account %s", account)
	}

	principal, err := k.Principal.Get(ctx, bz)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil, sdkerrors.Wrapf(err, "unable to get principal for account %s from state", account)
		}

		principal = math.ZeroInt()
	}

	index, err := k.Index.Get(ctx)
	if err != nil {
		return math.ZeroInt(), nil, sdkerrors.Wrap(err, "unable to get index from state")
	}

	currentBalance := k.bank.GetBalance(ctx, bz, k.denom).Amount
	expectedBalance := k.GetPresentAmount(principal, index)

	// We need to make sure that the yield value is valid and > 1.
	yield, err := expectedBalance.SafeSub(currentBalance)
	if err != nil || yield.Equal(math.OneInt()) || yield.IsNegative() {
		yield = math.ZeroInt()
	}

	return yield, bz, nil
}

// Deliver is internal logic executed when delivering portal messages.
func (k *Keeper) Deliver(ctx context.Context, bz []byte) error {
	if k.GetPortalPaused(ctx) {
		return portal.ErrPaused
	}

	vaa, err := k.wormhole.ParseAndVerifyVAA(ctx, bz)
	if err != nil {
		return err
	}

	peer, err := k.PortalPeers.Get(ctx, uint16(vaa.EmitterChain))
	if err != nil {
		return sdkerrors.Wrapf(portal.ErrInvalidPeer, "chain %d not configured", vaa.EmitterChain)
	}

	if !bytes.Equal(peer.Transceiver, vaa.EmitterAddress.Bytes()) {
		return sdkerrors.Wrapf(
			portal.ErrInvalidPeer,
			"expected transceiver %s for chain %d, got %s",
			hex.EncodeToString(peer.Transceiver), vaa.EmitterChain,
			vaa.EmitterAddress.String(),
		)
	}

	transceiverMessage, err := ntt.ParseTransceiverMessage(vaa.Payload)
	if err != nil {
		return err
	}

	if !bytes.Equal(peer.Manager, transceiverMessage.SourceManagerAddress) {
		return sdkerrors.Wrapf(
			portal.ErrInvalidPeer,
			"expected manager %s for chain %d, got %s",
			hex.EncodeToString(peer.Manager), vaa.EmitterChain,
			hex.EncodeToString(transceiverMessage.SourceManagerAddress),
		)
	}

	if !bytes.Equal(portal.PaddedManagerAddress, transceiverMessage.RecipientManagerAddress) {
		return sdkerrors.Wrapf(
			portal.ErrInvalidMessage,
			"expected recipient manager %s, got %s",
			hex.EncodeToString(portal.PaddedManagerAddress),
			hex.EncodeToString(transceiverMessage.RecipientManagerAddress),
		)
	}

	managerMessage, err := ntt.ParseManagerMessage(transceiverMessage.ManagerPayload)
	if err != nil {
		return err
	}

	messageId := ntt.ManagerMessageDigest(uint16(vaa.EmitterChain), managerMessage)
	eventPayload := portal.EventsPayload{
		SourceChainId: uint32(vaa.EmitterChain),
		Sender:        managerMessage.Sender,
		MessageId:     messageId,
	}

	if err := k.HandlePayload(ctx, managerMessage.Payload, eventPayload); err != nil {
		return err
	}

	return nil
}

// HandlePayload is a utility that handles custom payloads when delivering portal messages.
func (k *Keeper) HandlePayload(ctx context.Context, payload []byte, eventsPayload portal.EventsPayload) error {
	chain, err := k.wormhole.GetChain(ctx)
	if err != nil {
		return sdkerrors.Wrap(err, "unable to get wormhole chain id")
	}

	switch portal.GetPayloadType(payload) {
	case portal.Unknown:
		return nil
	case portal.Token:
		tokenPayload := portal.DecodeTokenPayload(payload)
		if chain != tokenPayload.DestinationChainId {
			return fmt.Errorf("wrong destination chain: expected %d, got %d", chain, tokenPayload.DestinationChainId)
		}
		if !bytes.Equal(portal.RawToken, tokenPayload.DestinationToken) {
			return fmt.Errorf("wrong destination token: expected %d, got %d", portal.RawToken, tokenPayload.DestinationToken)
		}

		if err := k.Mint(ctx, tokenPayload.Recipient, tokenPayload.Amount, &tokenPayload.Index); err != nil {
			return err
		}

		recipient, err := k.address.BytesToString(tokenPayload.Recipient)
		if err != nil {
			return fmt.Errorf("error encoding the recipient address: %w", err)
		}

		if err := k.event.EventManager(ctx).Emit(ctx, &portal.MTokenReceived{
			SourceChainId:    eventsPayload.SourceChainId,
			DestinationToken: tokenPayload.DestinationToken,
			Sender:           eventsPayload.Sender,
			Recipient:        recipient,
			Amount:           tokenPayload.Amount,
			Index:            tokenPayload.Index,
			MessageId:        eventsPayload.MessageId,
		}); err != nil {
			return err
		}

		return k.event.EventManager(ctx).Emit(ctx, &portal.TransferRedeemed{
			Digest: eventsPayload.MessageId,
		})
	case portal.Index:
		index, destination := portal.DecodeIndexPayload(payload)
		if chain != destination {
			return fmt.Errorf("not destination chain: expected %d, got %d", chain, destination)
		}

		return k.UpdateIndex(ctx, index)
	}

	return nil
}
