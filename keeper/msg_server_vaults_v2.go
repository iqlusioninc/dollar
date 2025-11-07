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
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"cosmossdk.io/collections"
	sdkerrors "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	hyperlaneutil "github.com/bcp-innovations/hyperlane-cosmos/util"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"dollar.noble.xyz/v3/types"
	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

var _ vaultsv2.MsgServer = &msgServerV2{}

type msgServerV2 struct {
	vaultsv2.UnimplementedMsgServer
	*Keeper
}

const navBasisPointsMultiplier int64 = 10_000

func validateCrossChainRoute(route vaultsv2.CrossChainRoute) error {
	if route.HyptokenId.IsZeroAddress() {
		return sdkerrors.Wrap(types.ErrInvalidRequest, "hyptoken identifier cannot be zero")
	}
	if route.ReceiverChainHook.IsZeroAddress() {
		return sdkerrors.Wrap(types.ErrInvalidRequest, "receiver chain hook cannot be zero")
	}
	if route.RemotePositionAddress.IsZeroAddress() {
		return sdkerrors.Wrap(types.ErrInvalidRequest, "remote position address cannot be zero")
	}
	if route.MaxInflightValue.IsNegative() || route.MaxInflightValue.IsZero() {
		return sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "max inflight value must be positive")
	}

	return nil
}

// calculateTWAPNav computes the Time-Weighted Average Price of NAV using recent snapshots.
// Returns the TWAP value or the current NAV if TWAP is disabled or insufficient data.
func (m msgServerV2) calculateTWAPNav(ctx context.Context, currentNav sdkmath.Int) (sdkmath.Int, error) {
	params, err := m.GetVaultsV2Params(ctx)
	if err != nil {
		return currentNav, sdkerrors.Wrap(err, "unable to fetch params")
	}

	// If TWAP is disabled, use current NAV
	if !params.TwapConfig.Enabled || params.TwapConfig.WindowSize == 0 {
		return currentNav, nil
	}

	// Get recent snapshots
	snapshots, err := m.GetRecentVaultsV2NAVSnapshots(ctx, int(params.TwapConfig.WindowSize))
	if err != nil {
		return currentNav, sdkerrors.Wrap(err, "unable to fetch NAV snapshots")
	}

	// If insufficient snapshots, use current NAV
	if len(snapshots) == 0 {
		return currentNav, nil
	}

	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()

	// Filter snapshots by age (in blocks)
	validSnapshots := make([]vaultsv2.NAVSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		ageInBlocks := currentHeight - snapshot.BlockHeight
		// Convert MaxSnapshotAge from seconds to blocks (assume ~6 seconds per block)
		maxAgeInBlocks := params.TwapConfig.MaxSnapshotAge / 6
		if params.TwapConfig.MaxSnapshotAge > 0 && ageInBlocks > maxAgeInBlocks {
			continue
		}
		validSnapshots = append(validSnapshots, snapshot)
	}

	// If no valid snapshots, use current NAV
	if len(validSnapshots) == 0 {
		return currentNav, nil
	}

	// Calculate Time-Weighted Average
	// We use simple average for now, but could be enhanced to true time-weighting
	sum := sdkmath.ZeroInt()
	for _, snapshot := range validSnapshots {
		sum, err = sum.SafeAdd(snapshot.Nav)
		if err != nil {
			return currentNav, sdkerrors.Wrap(err, "overflow in TWAP calculation")
		}
	}

	// Include current NAV in the average
	sum, err = sum.SafeAdd(currentNav)
	if err != nil {
		return currentNav, sdkerrors.Wrap(err, "overflow adding current NAV to TWAP")
	}

	count := int64(len(validSnapshots) + 1)
	twapNav := sum.QuoRaw(count)

	return twapNav, nil
}

// shouldRecordNAVSnapshot determines if a new NAV snapshot should be recorded.
func (m msgServerV2) shouldRecordNAVSnapshot(ctx context.Context) (bool, error) {
	params, err := m.GetVaultsV2Params(ctx)
	if err != nil {
		return false, sdkerrors.Wrap(err, "unable to fetch params")
	}

	// If TWAP is disabled, don't record snapshots
	if !params.TwapConfig.Enabled {
		return false, nil
	}

	// Check minimum interval
	if params.TwapConfig.MinSnapshotInterval <= 0 {
		return true, nil // No minimum interval
	}

	// Get most recent snapshot
	snapshots, err := m.GetRecentVaultsV2NAVSnapshots(ctx, 1)
	if err != nil {
		return false, sdkerrors.Wrap(err, "unable to fetch recent snapshots")
	}

	if len(snapshots) == 0 {
		return true, nil // No previous snapshot, record the first one
	}

	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	blocksSinceLastSnapshot := currentHeight - snapshots[0].BlockHeight
	// Convert MinSnapshotInterval from seconds to blocks (assume ~6 seconds per block)
	minIntervalInBlocks := params.TwapConfig.MinSnapshotInterval / 6

	return blocksSinceLastSnapshot >= minIntervalInBlocks, nil
}

// UpdateDepositLimits updates the deposit limits and risk control configuration.
func (m msgServerV2) UpdateDepositLimits(ctx context.Context, msg *vaultsv2.MsgUpdateDepositLimits) (*vaultsv2.MsgUpdateDepositLimitsResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	// Validate the new limits
	if msg.Limits.MaxUserDepositPerWindow.IsNegative() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "max user deposit per window cannot be negative")
	}
	if msg.Limits.MaxBlockDepositVolume.IsNegative() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "max block deposit volume cannot be negative")
	}
	if msg.Limits.GlobalDepositCap.IsNegative() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "global deposit cap cannot be negative")
	}
	if msg.Limits.DepositCooldownBlocks < 0 {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "deposit cooldown blocks cannot be negative")
	}
	if msg.Limits.VelocityWindowBlocks < 0 {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "velocity window blocks cannot be negative")
	}

	// Get existing limits for comparison
	existingLimits, hasExisting, err := m.getDepositLimits(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch existing deposit limits")
	}

	var previousJSON string
	if hasExisting {
		prevBytes, err := m.cdc.MarshalJSON(&existingLimits)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to marshal previous limits")
		}
		previousJSON = string(prevBytes)
	} else {
		previousJSON = "{}"
	}

	// Persist the new limits
	if err := m.setDepositLimits(ctx, msg.Limits); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist deposit limits")
	}

	newBytes, err := m.cdc.MarshalJSON(&msg.Limits)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to marshal new limits")
	}
	newJSON := string(newBytes)

	// Emit event for audit trail
	headerInfo := m.header.GetHeaderInfo(ctx)
	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventVaultConfigUpdated{
		PreviousConfig: previousJSON,
		NewConfig:      newJSON,
		Authority:      msg.Authority,
		Reason:         msg.Reason,
		BlockHeight:    sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:      headerInfo.Time,
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to emit deposit limits updated event")
	}

	return &vaultsv2.MsgUpdateDepositLimitsResponse{
		PreviousLimits: previousJSON,
		NewLimits:      newJSON,
	}, nil
}

func (m msgServerV2) findRemotePositionByAddress(ctx context.Context, address hyperlaneutil.HexAddress) (uint64, vaultsv2.RemotePosition, bool, error) {
	positions, err := m.GetAllVaultsV2RemotePositions(ctx)
	if err != nil {
		return 0, vaultsv2.RemotePosition{}, false, err
	}

	for _, entry := range positions {
		if entry.Position.VaultAddress == address {
			return entry.ID, entry.Position, true, nil
		}
	}

	return 0, vaultsv2.RemotePosition{}, false, nil
}

func (m msgServerV2) getDepositLimits(ctx context.Context) (vaultsv2.DepositLimit, bool, error) {
	limits, err := m.GetVaultsV2DepositLimits(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.DepositLimit{}, false, nil
		}
		return vaultsv2.DepositLimit{}, false, err
	}
	return limits, true, nil
}

func (m msgServerV2) updateDepositVelocity(ctx context.Context, addr sdk.AccAddress, amount sdkmath.Int, block int64, params vaultsv2.DepositLimit) (vaultsv2.DepositVelocity, error) {
	velocity, found, err := m.getDepositVelocity(ctx, addr)
	if err != nil {
		return vaultsv2.DepositVelocity{}, err
	}
	if !found {
		velocity.TimeWindowBlocks = params.DepositCooldownBlocks
	}
	velocity.LastDepositBlock = block
	velocity.RecentDepositCount++
	if velocity.RecentDepositVolume.IsZero() {
		velocity.RecentDepositVolume = amount
	} else {
		volume, err := velocity.RecentDepositVolume.SafeAdd(amount)
		if err != nil {
			return vaultsv2.DepositVelocity{}, err
		}
		velocity.RecentDepositVolume = volume
	}
	return velocity, nil
}

func oracleIdentifier(positionID uint64, sourceChain string) string {
	if sourceChain == "" {
		return strconv.FormatUint(positionID, 10)
	}
	return fmt.Sprintf("%d:%s", positionID, sourceChain)
}

func NewMsgServerV2(keeper *Keeper) vaultsv2.MsgServer {
	return &msgServerV2{Keeper: keeper}
}

// Deposit creates a position for the user with the given amount that may or may not receive yield
func (m msgServerV2) Deposit(ctx context.Context, msg *vaultsv2.MsgDeposit) (*vaultsv2.MsgDepositResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "deposit amount must be positive")
	}

	addrBz, err := m.address.StringToBytes(msg.Depositor)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidRequest, "invalid depositor address: %s", msg.Depositor)
	}
	depositor := sdk.AccAddress(addrBz)

	params, err := m.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault parameters")
	}
	if !params.VaultEnabled {
		return nil, sdkerrors.Wrap(vaultsv2.ErrOperationNotPermitted, "vault deposits are disabled")
	}
	if params.MinDepositAmount.IsPositive() && msg.Amount.LT(params.MinDepositAmount) {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAmount, "deposit below minimum of %s", params.MinDepositAmount.String())
	}

	config, err := m.GetVaultsV2Config(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault configuration")
	}
	if !config.Enabled {
		return nil, sdkerrors.Wrap(vaultsv2.ErrOperationNotPermitted, "vault is disabled")
	}

	if err := m.checkAccountingNotInProgress(ctx); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()
	if err := m.enforceDepositLimits(ctx, depositor, msg.Amount, currentBlock); err != nil {
		return nil, err
	}

	balance := m.bank.GetBalance(ctx, depositor, m.denom).Amount
	if balance.LT(msg.Amount) {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "insufficient balance for deposit")
	}

	coin := sdk.NewCoin(m.denom, msg.Amount)
	if err := m.bank.SendCoins(ctx, depositor, types.ModuleAddress, sdk.NewCoins(coin)); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to transfer deposit into module account")
	}

	if err := m.AddVaultsV2LocalFunds(ctx, msg.Amount); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to record local funds")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	positionID, err := m.GetNextUserPositionID(ctx, depositor)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to generate position ID")
	}

	userPositionCount, err := m.GetUserPositionCount(ctx, depositor)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to get user position count")
	}
	isFirstPosition := userPositionCount == 1

	position := vaultsv2.UserPosition{
		PositionId:               positionID,
		DepositAmount:            msg.Amount,
		AccruedYield:             sdkmath.ZeroInt(),
		FirstDepositTime:         headerInfo.Time,
		LastActivityTime:         headerInfo.Time,
		ReceiveYield:             msg.ReceiveYield,
		DepositPendingWithdrawal: sdkmath.ZeroInt(),
		YieldPendingWithdrawal:   sdkmath.ZeroInt(),
		TotalPendingWithdrawal:   sdkmath.ZeroInt(),
		ActiveWithdrawalRequests: 0,
	}

	if err := m.SetVaultsV2UserPosition(ctx, depositor, positionID, position); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to store user position")
	}

	if isFirstPosition {
		if err := m.IncrementVaultsV2TotalUsers(ctx); err != nil {
			return nil, sdkerrors.Wrap(err, "unable to increment total users")
		}
	}

	if err := m.AddAmountToVaultsV2Totals(ctx, msg.Amount, sdkmath.ZeroInt()); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update aggregate vault totals")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}
	state.TotalPositions++

	if msg.ReceiveYield {
		state.TotalEligibleDeposits, err = state.TotalEligibleDeposits.SafeAdd(msg.Amount)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to update total eligible deposits")
		}
	}

	if err := m.updateDepositTracking(ctx, depositor, msg.Amount, currentBlock); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update deposit tracking")
	}

	// TODO(Collin): We don't need this line setting DepositsEnabled
	state.DepositsEnabled = config.Enabled
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventDeposit{
		Depositor:       msg.Depositor,
		AmountDeposited: msg.Amount,
		BlockHeight:     sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:       headerInfo.Time,
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to emit deposit event")
	}

	return &vaultsv2.MsgDepositResponse{
		AmountDeposited: msg.Amount,
		PositionId:      positionID,
	}, nil
}

// RequestWithdrawal creates a pending withdrawal request containing the principle + yield needed to service it
func (m msgServerV2) RequestWithdrawal(ctx context.Context, msg *vaultsv2.MsgRequestWithdrawal) (*vaultsv2.MsgRequestWithdrawalResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "withdrawal amount must be positive")
	}

	addrBz, err := m.address.StringToBytes(msg.Requester)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidRequest, "invalid requester address: %s", msg.Requester)
	}
	requester := sdk.AccAddress(addrBz)

	if err := m.checkAccountingNotInProgress(ctx); err != nil {
		return nil, err
	}

	position, found, err := m.GetVaultsV2UserPosition(ctx, requester, msg.PositionId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidVaultState, "position %d not found for requester", msg.PositionId)
	}

	totalValue, err := position.DepositAmount.SafeAdd(position.AccruedYield)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to calculate total position value")
	}
	available, err := totalValue.SafeSub(position.TotalPendingWithdrawal)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to determine available balance")
	}
	if available.LT(msg.Amount) {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAmount, "insufficient balance in position %d: available %s, requested %s",
			msg.PositionId, available.String(), msg.Amount.String())
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	yieldAmount, principalAmount := calculateWithdrawalSplit(
		msg.Amount,
		position.AccruedYield,
		position.DepositAmount,
	)

	position.DepositPendingWithdrawal, err = position.DepositPendingWithdrawal.SafeAdd(principalAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update deposit pending withdrawal amount")
	}
	position.YieldPendingWithdrawal, err = position.YieldPendingWithdrawal.SafeAdd(yieldAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update yield pending withdrawal amount")
	}
	position.TotalPendingWithdrawal, err = position.TotalPendingWithdrawal.SafeAdd(msg.Amount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total pending withdrawal amount")
	}
	position.ActiveWithdrawalRequests++
	position.LastActivityTime = headerInfo.Time
	if err := m.SetVaultsV2UserPosition(ctx, requester, msg.PositionId, position); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist user position")
	}

	// TODO (Collin): Why do we record PendingWithdrawAmount in VaultState and in an Int store?
	if err := m.AddVaultsV2PendingWithdrawalAmount(ctx, msg.Amount); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to record pending withdrawal amount")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}
	state.PendingWithdrawalRequests++
	state.TotalDepositPendingWithdrawal, err = state.TotalDepositPendingWithdrawal.SafeAdd(principalAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total deposit pending withdrawal")
	}
	state.TotalYieldPendingWithdrawal, err = state.TotalYieldPendingWithdrawal.SafeAdd(yieldAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total yield pending withdrawal")
	}
	state.TotalPendingWithdrawal, err = state.TotalPendingWithdrawal.SafeAdd(msg.Amount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total pending withdrawal")
	}

	if position.ReceiveYield && principalAmount.IsPositive() {
		state.TotalEligibleDeposits, _ = state.TotalEligibleDeposits.SafeSub(principalAmount)
	}

	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	params, err := m.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault parameters")
	}

	id, err := m.NextVaultsV2WithdrawalID(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to allocate withdrawal id")
	}

	unlockTime := headerInfo.Time
	if params.WithdrawalRequestTimeout > 0 {
		unlockTime = unlockTime.Add(time.Duration(params.WithdrawalRequestTimeout) * time.Second)
	}

	request := vaultsv2.WithdrawalRequest{
		Requester:          msg.Requester,
		PositionId:         msg.PositionId,
		WithdrawAmount:     msg.Amount,
		RequestTime:        headerInfo.Time,
		UnlockTime:         unlockTime,
		Status:             vaultsv2.WITHDRAWAL_REQUEST_STATUS_PENDING,
		EstimatedAmount:    msg.Amount,
		RequestBlockHeight: sdk.UnwrapSDKContext(ctx).BlockHeight(),
		YieldAmount:        yieldAmount,
		PrincipalAmount:    principalAmount,
	}
	if err := m.SetVaultsV2Withdrawal(ctx, id, request); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist withdrawal request")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventWithdrawlRequested{
		Requester:           msg.Requester,
		AmountToWithdraw:    msg.Amount,
		WithdrawalRequestId: id,
		ExpectedUnlockTime:  unlockTime,
		BlockHeight:         request.RequestBlockHeight,
		Timestamp:           headerInfo.Time,
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to emit withdrawal requested event")
	}

	return &vaultsv2.MsgRequestWithdrawalResponse{
		RequestId:           id,
		AmountLocked:        msg.Amount,
		YieldPortion:        yieldAmount,
		ExpectedClaimableAt: unlockTime,
	}, nil
}

func (m msgServerV2) SetYieldPreference(ctx context.Context, msg *vaultsv2.MsgSetYieldPreference) (*vaultsv2.MsgSetYieldPreferenceResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	addrBz, err := m.address.StringToBytes(msg.User)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidRequest, "invalid user address: %s", msg.User)
	}
	user := sdk.AccAddress(addrBz)

	// Get the specific position
	position, found, err := m.GetVaultsV2UserPosition(ctx, user, msg.PositionId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidVaultState, "position %d not found for user", msg.PositionId)
	}

	previousPreference := position.ReceiveYield
	headerInfo := m.header.GetHeaderInfo(ctx)

	// Check if accounting is in progress
	if err := m.checkAccountingNotInProgress(ctx); err != nil {
		return nil, err
	}

	// Calculate active deposits (excluding pending withdrawals) for eligible deposits tracking
	activeDeposit := position.DepositAmount
	if position.DepositPendingWithdrawal.IsPositive() {
		activeDeposit, _ = activeDeposit.SafeSub(position.DepositPendingWithdrawal)
	}

	// Update position
	position.ReceiveYield = msg.ReceiveYield
	position.LastActivityTime = headerInfo.Time

	if err := m.SetVaultsV2UserPosition(ctx, user, msg.PositionId, position); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist user position")
	}

	// Update TotalEligibleDeposits based on preference change
	if activeDeposit.IsPositive() && previousPreference != msg.ReceiveYield {
		state, err := m.GetVaultsV2VaultState(ctx)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
		}

		if msg.ReceiveYield {
			state.TotalEligibleDeposits, _ = state.TotalEligibleDeposits.SafeAdd(activeDeposit)
		} else {
			state.TotalEligibleDeposits, _ = state.TotalEligibleDeposits.SafeSub(activeDeposit)
		}

		if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
			return nil, sdkerrors.Wrap(err, "unable to persist vault state")
		}
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventYieldPreferenceUpdated{
		User:                    msg.User,
		PositionId:              msg.PositionId,
		PreviousYieldPreference: previousPreference,
		NewYieldPreference:      msg.ReceiveYield,
		BlockHeight:             sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:               headerInfo.Time,
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to emit yield preference event")
	}

	return &vaultsv2.MsgSetYieldPreferenceResponse{
		PreviousPreference: previousPreference,
		NewPreference:      msg.ReceiveYield,
		PositionId:         msg.PositionId,
	}, nil
}

// ProcessWithdrawalQueue marks eligible withdrawal requests as ready to be claimed
func (m msgServerV2) ProcessWithdrawalQueue(ctx context.Context, msg *vaultsv2.MsgProcessWithdrawalQueue) (*vaultsv2.MsgProcessWithdrawalQueueResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	limit := msg.MaxRequests
	if limit <= 0 {
		limit = math.MaxInt32
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	processed := int32(0)
	totalProcessed := sdkmath.ZeroInt()

	err := m.IterateVaultsV2Withdrawals(ctx, func(id uint64, request vaultsv2.WithdrawalRequest) (bool, error) {
		if processed >= limit {
			return true, nil
		}
		if request.Status != vaultsv2.WITHDRAWAL_REQUEST_STATUS_PENDING {
			return false, nil
		}
		if headerInfo.Time.Before(request.UnlockTime) {
			return false, nil
		}

		request.Status = vaultsv2.WITHDRAWAL_REQUEST_STATUS_READY
		if err := m.SetVaultsV2Withdrawal(ctx, id, request); err != nil {
			return true, err
		}

		processed++
		var err error
		totalProcessed, err = totalProcessed.SafeAdd(request.WithdrawAmount)
		if err != nil {
			return true, err
		}

		return processed >= limit, nil
	})
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to iterate withdrawal queue")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}

	if processed > 0 {
		if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventWithdrawalProcessed{
			RequestsProcessed:      processed,
			TotalAmountProcessed:   totalProcessed,
			TotalAmountDistributed: sdkmath.ZeroInt(),
			BlockHeight:            sdk.UnwrapSDKContext(ctx).BlockHeight(),
			Timestamp:              headerInfo.Time,
		}); err != nil {
			return nil, sdkerrors.Wrap(err, "unable to emit withdrawal processed event")
		}
	}

	return &vaultsv2.MsgProcessWithdrawalQueueResponse{
		RequestsProcessed:      processed,
		TotalAmountProcessed:   totalProcessed,
		TotalAmountDistributed: sdkmath.ZeroInt(),
		RemainingRequests:      state.PendingWithdrawalRequests,
	}, nil
}

func (m msgServerV2) UpdateVaultConfig(ctx context.Context, msg *vaultsv2.MsgUpdateVaultConfig) (*vaultsv2.MsgUpdateVaultConfigResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	if msg.Config.MaxTotalDeposits.IsNegative() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "maximum total deposits cannot be negative")
	}
	if msg.Config.TargetYieldRate.IsNegative() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "target yield rate cannot be negative")
	}

	existingConfig, err := m.GetVaultsV2Config(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch current vault config")
	}

	if err := m.SetVaultsV2Config(ctx, msg.Config); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault config")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}
	state.DepositsEnabled = msg.Config.Enabled
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	prevJSON, err := m.cdc.MarshalJSON(&existingConfig)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to marshal previous config")
	}
	newJSON, err := m.cdc.MarshalJSON(&msg.Config)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to marshal new config")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventVaultConfigUpdated{
		PreviousConfig: string(prevJSON),
		NewConfig:      string(newJSON),
		Authority:      msg.Authority,
		Reason:         msg.Reason,
		BlockHeight:    sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:      headerInfo.Time,
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to emit vault config updated event")
	}

	return &vaultsv2.MsgUpdateVaultConfigResponse{
		PreviousConfig: string(prevJSON),
		NewConfig:      string(newJSON),
	}, nil
}

func (m msgServerV2) UpdateParams(ctx context.Context, msg *vaultsv2.MsgUpdateParams) (*vaultsv2.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	if msg.Params.MinDepositAmount.IsNegative() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "minimum deposit amount cannot be negative")
	}
	if msg.Params.MinWithdrawalAmount.IsNegative() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "minimum withdrawal amount cannot be negative")
	}
	if msg.Params.MaxNavChangeBps < 0 {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "maximum NAV change must be non-negative")
	}
	if msg.Params.WithdrawalRequestTimeout < 0 {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "withdrawal request timeout must be non-negative")
	}
	if msg.Params.MaxWithdrawalRequestsPerBlock < 0 {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "max withdrawal requests per block must be non-negative")
	}

	params := msg.Params
	params.Authority = m.authority

	if err := m.SetVaultsV2Params(ctx, params); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault params")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}

	config, err := m.GetVaultsV2Config(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault config")
	}

	state.DepositsEnabled = params.VaultEnabled && config.Enabled
	// TODO (Collin): This isn't used anywhere
	state.WithdrawalsEnabled = params.VaultEnabled

	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	return &vaultsv2.MsgUpdateParamsResponse{}, nil
}

func (m msgServerV2) CreateCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgCreateCrossChainRoute) (*vaultsv2.MsgCreateCrossChainRouteResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	if err := validateCrossChainRoute(msg.Route); err != nil {
		return nil, err
	}

	var duplicateErr error
	if err := m.IterateVaultsV2CrossChainRoutes(ctx, func(_ uint32, existing vaultsv2.CrossChainRoute) (bool, error) {
		if existing.RemotePositionAddress == msg.Route.RemotePositionAddress {
			duplicateErr = sdkerrors.Wrap(types.ErrInvalidRequest, "cross-chain route already exists for remote position address")
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to verify existing routes")
	}
	if duplicateErr != nil {
		return nil, duplicateErr
	}

	routeID, err := m.NextVaultsV2CrossChainRouteID(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to allocate cross-chain route id")
	}

	if err := m.SetVaultsV2CrossChainRoute(ctx, routeID, msg.Route); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist cross-chain route")
	}

	return &vaultsv2.MsgCreateCrossChainRouteResponse{RouteId: routeID}, nil
}

func (m msgServerV2) UpdateCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgUpdateCrossChainRoute) (*vaultsv2.MsgUpdateCrossChainRouteResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	if err := validateCrossChainRoute(msg.Route); err != nil {
		return nil, err
	}

	existing, found, err := m.GetVaultsV2CrossChainRoute(ctx, msg.RouteId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch cross-chain route")
	}
	if !found {
		return nil, sdkerrors.Wrapf(types.ErrInvalidRequest, "cross-chain route %d not found", msg.RouteId)
	}

	var duplicateErr error
	if err := m.IterateVaultsV2CrossChainRoutes(ctx, func(id uint32, route vaultsv2.CrossChainRoute) (bool, error) {
		if id == msg.RouteId {
			return false, nil
		}
		if route.RemotePositionAddress == msg.Route.RemotePositionAddress {
			duplicateErr = sdkerrors.Wrap(types.ErrInvalidRequest, "cross-chain route already exists for remote position address")
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to verify existing routes")
	}
	if duplicateErr != nil {
		return nil, duplicateErr
	}

	prevJSON, err := m.cdc.MarshalJSON(&existing)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to marshal previous route config")
	}

	newJSON, err := m.cdc.MarshalJSON(&msg.Route)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to marshal new route config")
	}

	if err := m.SetVaultsV2CrossChainRoute(ctx, msg.RouteId, msg.Route); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist cross-chain route")
	}

	return &vaultsv2.MsgUpdateCrossChainRouteResponse{
		RouteId:        msg.RouteId,
		PreviousConfig: string(prevJSON),
		NewConfig:      string(newJSON),
	}, nil
}

func (m msgServerV2) DisableCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgDisableCrossChainRoute) (*vaultsv2.MsgDisableCrossChainRouteResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	route, found, err := m.GetVaultsV2CrossChainRoute(ctx, msg.RouteId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch cross-chain route")
	}
	if !found {
		return nil, sdkerrors.Wrapf(types.ErrInvalidRequest, "cross-chain route %d not found", msg.RouteId)
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	var affected int64

	positions, err := m.GetAllVaultsV2RemotePositions(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch remote positions")
	}

	for _, entry := range positions {
		if entry.Position.VaultAddress == route.RemotePositionAddress {
			affected++
			entry.Position.Status = vaultsv2.REMOTE_POSITION_ERROR
			entry.Position.LastUpdate = headerInfo.Time
			if err := m.SetVaultsV2RemotePosition(ctx, entry.ID, entry.Position); err != nil {
				return nil, sdkerrors.Wrapf(err, "unable to update remote position %d", entry.ID)
			}
		}
	}

	if err := m.DeleteVaultsV2CrossChainRoute(ctx, msg.RouteId); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to remove cross-chain route")
	}

	return &vaultsv2.MsgDisableCrossChainRouteResponse{
		RouteId:           msg.RouteId,
		AffectedPositions: affected,
	}, nil
}

func (m msgServerV2) CreateRemotePosition(ctx context.Context, msg *vaultsv2.MsgCreateRemotePosition) (*vaultsv2.MsgCreateRemotePositionResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Manager != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Manager)
	}
	if msg.ChainId == 0 {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "chain id must be provided")
	}
	if msg.VaultAddress == "" {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "vault address must be provided")
	}
	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "amount must be positive")
	}
	if msg.MinSharesOut.IsPositive() && msg.Amount.LT(msg.MinSharesOut) {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "amount less than minimum shares out")
	}

	localFunds, err := m.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch local funds")
	}
	if localFunds.LT(msg.Amount) {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "insufficient local funds")
	}

	vaultAddress, err := hyperlaneutil.DecodeHexAddress(msg.VaultAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to decode vault address")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	positionID, err := m.NextVaultsV2RemotePositionID(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to allocate remote position id")
	}

	position := vaultsv2.RemotePosition{
		VaultAddress: vaultAddress,
		SharesHeld:   msg.Amount,
		Principal:    msg.Amount,
		SharePrice:   sdkmath.LegacyOneDec(),
		TotalValue:   msg.Amount,
		LastUpdate:   headerInfo.Time,
		Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
	}

	if err := m.SetVaultsV2RemotePosition(ctx, positionID, position); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to store remote position")
	}

	if err := m.SetVaultsV2RemotePositionChainID(ctx, positionID, msg.ChainId); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to store remote position chain id")
	}

	if err := m.SubtractVaultsV2LocalFunds(ctx, msg.Amount); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update local funds")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	return &vaultsv2.MsgCreateRemotePositionResponse{
		PositionId:         positionID,
		RouteId:            msg.ChainId,
		ExpectedCompletion: headerInfo.Time,
	}, nil
}

func (m msgServerV2) CloseRemotePosition(ctx context.Context, msg *vaultsv2.MsgCloseRemotePosition) (*vaultsv2.MsgCloseRemotePositionResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Manager != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Manager)
	}
	if msg.PositionId == 0 {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "position id must be provided")
	}

	position, found, err := m.GetVaultsV2RemotePosition(ctx, msg.PositionId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch remote position")
	}
	if !found {
		return nil, sdkerrors.Wrap(vaultsv2.ErrRemotePositionNotFound, "remote position not found")
	}
	if position.Status == vaultsv2.REMOTE_POSITION_CLOSED {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidVaultState, "remote position already closed")
	}

	withdrawAmount := position.TotalValue
	if msg.PartialAmount.IsPositive() {
		if msg.PartialAmount.GT(position.TotalValue) {
			return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "partial amount exceeds position value")
		}
		withdrawAmount = msg.PartialAmount
	}
	if !withdrawAmount.IsPositive() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "withdraw amount must be positive")
	}

	totalValue, err := position.TotalValue.SafeSub(withdrawAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update position value")
	}
	position.TotalValue = totalValue

	shares, err := position.SharesHeld.SafeSub(withdrawAmount)
	if err != nil {
		shares = sdkmath.ZeroInt()
	}
	position.SharesHeld = shares

	principal := position.Principal
	if principal.IsPositive() {
		principal, err = principal.SafeSub(withdrawAmount)
		if err != nil || principal.IsNegative() {
			principal = sdkmath.ZeroInt()
		}
	}
	position.Principal = principal

	if position.TotalValue.IsPositive() {
		position.Status = vaultsv2.REMOTE_POSITION_ACTIVE
	} else {
		position.Status = vaultsv2.REMOTE_POSITION_CLOSED
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	position.LastUpdate = headerInfo.Time

	if err := m.SetVaultsV2RemotePosition(ctx, msg.PositionId, position); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist remote position")
	}

	if err := m.AddVaultsV2PendingWithdrawalDistribution(ctx, withdrawAmount); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update pending withdrawal distribution")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	return &vaultsv2.MsgCloseRemotePositionResponse{
		PositionId:         msg.PositionId,
		Initiated:          true,
		ExpectedCompletion: headerInfo.Time,
	}, nil
}

func (m msgServerV2) Rebalance(ctx context.Context, msg *vaultsv2.MsgRebalance) (*vaultsv2.MsgRebalanceResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	if msg.Manager != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Manager)
	}

	if len(msg.TargetAllocations) == 0 {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "at least one target allocation is required")
	}

	seen := make(map[uint64]struct{}, len(msg.TargetAllocations))
	var total uint32

	for _, allocation := range msg.TargetAllocations {
		if allocation == nil {
			return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "target allocation cannot be nil")
		}
		if allocation.PositionId == 0 {
			return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "target allocation position id must be greater than zero")
		}
		if allocation.TargetPercentage == 0 {
			return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "target allocation percentage must be greater than zero")
		}
		if allocation.TargetPercentage > 100 {
			return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "target allocation percentage cannot exceed 100")
		}
		if _, exists := seen[allocation.PositionId]; exists {
			return nil, sdkerrors.Wrapf(types.ErrInvalidRequest, "duplicate target allocation for position %d", allocation.PositionId)
		}

		seen[allocation.PositionId] = struct{}{}
		total += allocation.TargetPercentage
		if total > 100 {
			return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "target allocations exceed 100 percent")
		}
	}

	positions, err := m.GetAllVaultsV2RemotePositions(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch remote positions")
	}
	if len(positions) == 0 {
		return nil, sdkerrors.Wrap(vaultsv2.ErrRemotePositionNotFound, "no remote positions configured")
	}

	indexByID := make(map[uint64]int, len(positions))
	for i := range positions {
		indexByID[positions[i].ID] = i
	}

	targetDesired := make(map[uint64]sdkmath.Int, len(msg.TargetAllocations))
	totalTracked := sdkmath.ZeroInt()

	localFunds, err := m.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch local funds")
	}

	totalTracked = localFunds
	for _, entry := range positions {
		totalTracked, err = totalTracked.SafeAdd(entry.Position.TotalValue)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to aggregate remote position value")
		}
	}

	for _, allocation := range msg.TargetAllocations {
		idx, ok := indexByID[allocation.PositionId]
		if !ok {
			return nil, sdkerrors.Wrapf(vaultsv2.ErrRemotePositionNotFound, "target position %d not found", allocation.PositionId)
		}
		if totalTracked.IsZero() || allocation.TargetPercentage == 0 {
			targetDesired[allocation.PositionId] = sdkmath.ZeroInt()
			continue
		}
		desired := totalTracked.MulRaw(int64(allocation.TargetPercentage)).QuoRaw(100)

		// ensure desired value is not greater than the total tracked amount to avoid rounding overflow
		if desired.GT(totalTracked) {
			desired = totalTracked
		}

		targetDesired[allocation.PositionId] = desired
		positions[idx].Position.Status = vaultsv2.REMOTE_POSITION_ACTIVE
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	type adjustment struct {
		index  int
		amount sdkmath.Int
	}

	var decreases []adjustment
	var increases []adjustment

	for i := range positions {
		id := positions[i].ID
		current := positions[i].Position.TotalValue
		desired, ok := targetDesired[id]
		if !ok {
			desired = current
		}

		delta := desired.Sub(current)
		if delta.IsZero() {
			continue
		}

		if delta.IsNegative() {
			decreases = append(decreases, adjustment{index: i, amount: delta.Neg()})
		} else {
			increases = append(increases, adjustment{index: i, amount: delta})
		}
	}

	available := localFunds
	operations := 0

	for _, adj := range decreases {
		if adj.amount.IsZero() {
			continue
		}
		position := positions[adj.index].Position

		if adj.amount.GT(position.TotalValue) {
			return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidVaultState, "rebalance reduction exceeds position value")
		}

		totalValue, err := position.TotalValue.SafeSub(adj.amount)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to reduce position value")
		}
		position.TotalValue = totalValue

		shares, err := position.SharesHeld.SafeSub(adj.amount)
		if err != nil {
			shares = sdkmath.ZeroInt()
		}
		position.SharesHeld = shares

		principal := position.Principal
		if principal.IsPositive() {
			principal, err = principal.SafeSub(adj.amount)
			if err != nil || principal.IsNegative() {
				principal = sdkmath.ZeroInt()
			}
		}
		position.Principal = principal

		if position.TotalValue.IsPositive() {
			position.Status = vaultsv2.REMOTE_POSITION_ACTIVE
		} else {
			position.Status = vaultsv2.REMOTE_POSITION_CLOSED
		}

		position.LastUpdate = headerInfo.Time

		available, err = available.SafeAdd(adj.amount)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to accumulate freed funds")
		}

		positions[adj.index].Position = position
		operations++
	}

	for _, adj := range increases {
		if adj.amount.IsZero() {
			continue
		}
		if available.LT(adj.amount) {
			return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidVaultState, "insufficient liquidity for rebalance")
		}

		entry := positions[adj.index]

		inflightID, err := m.NextVaultsV2InflightID(ctx)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to allocate inflight identifier")
		}

		destination := &vaultsv2.RemotePosition{
			HyptokenId:   entry.Position.HyptokenId,
			VaultAddress: entry.Position.VaultAddress,
			SharePrice:   entry.Position.SharePrice,
			Status:       entry.Position.Status,
		}

		fund := vaultsv2.InflightFund{
			Id:                inflightID,
			TransactionId:     fmt.Sprintf("rebalance:%d", entry.ID),
			Amount:            adj.amount,
			ValueAtInitiation: adj.amount,
			InitiatedAt:       headerInfo.Time,
			ExpectedAt:        headerInfo.Time,
			Status:            vaultsv2.INFLIGHT_PENDING,
			Origin:            &vaultsv2.InflightFund_NobleOrigin{NobleOrigin: &vaultsv2.NobleEndpoint{OperationType: vaultsv2.OPERATION_TYPE_REBALANCE}},
			Destination:       &vaultsv2.InflightFund_RemoteDestination{RemoteDestination: destination},
			ProviderTracking: &vaultsv2.ProviderTrackingInfo{
				TrackingInfo: &vaultsv2.ProviderTrackingInfo_HyperlaneTracking{
					HyperlaneTracking: &vaultsv2.HyperlaneTrackingInfo{
						DestinationDomain: entry.ChainID,
					},
				},
			},
		}

		if err := m.SetVaultsV2InflightFund(ctx, fund); err != nil {
			return nil, sdkerrors.Wrap(err, "unable to persist inflight fund")
		}

		available, err = available.SafeSub(adj.amount)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to deduct deployed funds")
		}

		operations++
	}

	for _, entry := range positions {
		if err := m.SetVaultsV2RemotePosition(ctx, entry.ID, entry.Position); err != nil {
			return nil, sdkerrors.Wrap(err, "unable to persist remote position")
		}
	}

	if err := m.VaultsV2LocalFunds.Set(ctx, available); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist local funds")
	}

	headerInfo = m.header.GetHeaderInfo(ctx)
	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	summary := fmt.Sprintf("rebalanced %d positions; pending deployment %s", operations, available.String())

	return &vaultsv2.MsgRebalanceResponse{
		OperationsInitiated: int32(operations),
		Summary:             summary,
	}, nil
}

func (m msgServerV2) ProcessInFlightPosition(ctx context.Context, msg *vaultsv2.MsgProcessInFlightPosition) (*vaultsv2.MsgProcessInFlightPositionResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	fund, found, err := m.GetVaultsV2InflightFund(ctx, msg.Nonce)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch inflight fund")
	}
	if !found {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInflightNotFound, "inflight fund %d not found", msg.Nonce)
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	if msg.ProviderTracking != nil {
		fund.ProviderTracking = msg.ProviderTracking
	}

	// Capture previous status for event
	previousStatus := fund.Status

	fund.Status = msg.ResultStatus
	fund.ExpectedAt = headerInfo.Time

	processedAmount := msg.ResultAmount
	if !processedAmount.IsPositive() {
		processedAmount = fund.Amount
	}

	sharesAffected := fund.ValueAtInitiation
	if !sharesAffected.IsPositive() {
		sharesAffected = processedAmount
	}

	var routeID uint32
	if tracking := fund.GetProviderTracking(); tracking != nil {
		if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
			if hyperlane.DestinationDomain != 0 {
				routeID = hyperlane.DestinationDomain
			} else if hyperlane.OriginDomain != 0 {
				routeID = hyperlane.OriginDomain
			}
			hyperlane.Processed = msg.ResultStatus == vaultsv2.INFLIGHT_COMPLETED
		}
	}

	removeRouteValue := func(amount sdkmath.Int) {
		if routeID != 0 && amount.IsPositive() {
			_ = m.SubtractVaultsV2InflightValueByRoute(ctx, routeID, amount)
		}
	}

	switch msg.ResultStatus {
	case vaultsv2.INFLIGHT_COMPLETED:
		removeRouteValue(processedAmount)

		if fund.GetOrigin() != nil {
			if origin := fund.GetRemoteOrigin(); origin != nil {
				positionID, position, foundPosition, err := m.findRemotePositionByAddress(ctx, origin.VaultAddress)
				if err == nil && foundPosition {
					if sharesAffected.IsPositive() {
						newShares, err := position.SharesHeld.SafeSub(sharesAffected)
						if err == nil {
							position.SharesHeld = newShares
						} else {
							position.SharesHeld = sdkmath.ZeroInt()
						}
					}
					if processedAmount.IsPositive() {
						total, err := position.TotalValue.SafeSub(processedAmount)
						if err == nil {
							position.TotalValue = total
						} else {
							position.TotalValue = sdkmath.ZeroInt()
						}
						principal, err := position.Principal.SafeSub(processedAmount)
						if err == nil && !principal.IsNegative() {
							position.Principal = principal
						} else {
							position.Principal = sdkmath.ZeroInt()
						}
					}
					if position.SharesHeld.IsPositive() {
						position.Status = vaultsv2.REMOTE_POSITION_ACTIVE
					} else {
						position.Status = vaultsv2.REMOTE_POSITION_CLOSED
					}
					position.LastUpdate = headerInfo.Time
					_ = m.SetVaultsV2RemotePosition(ctx, positionID, position)
				}
			}
			if processedAmount.IsPositive() {
				_ = m.AddVaultsV2PendingWithdrawalDistribution(ctx, processedAmount)
			}
		} else if fund.GetDestination() != nil {
			// Deposit completion – nothing additional for now beyond clearing inflight totals.
		}
	case vaultsv2.INFLIGHT_FAILED, vaultsv2.INFLIGHT_TIMEOUT:
		removeRouteValue(fund.Amount)

		if fund.GetOrigin() == nil && fund.Amount.IsPositive() {
			_ = m.AddVaultsV2LocalFunds(ctx, fund.Amount)
		}

		if origin := fund.GetRemoteOrigin(); origin != nil {
			positionID, position, foundPosition, err := m.findRemotePositionByAddress(ctx, origin.VaultAddress)
			if err == nil && foundPosition {
				position.Status = vaultsv2.REMOTE_POSITION_ACTIVE
				position.LastUpdate = headerInfo.Time
				_ = m.SetVaultsV2RemotePosition(ctx, positionID, position)
			}
		}
	default:
		// No-op for pending/confirmed transitions.
	}

	if err := m.SetVaultsV2InflightFund(ctx, fund); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist inflight fund update")
	}

	// Emit status change event
	if previousStatus != msg.ResultStatus {
		_ = m.EmitInflightStatusChangeEvent(
			ctx,
			msg.Nonce,
			fund.TransactionId,
			routeID,
			previousStatus,
			msg.ResultStatus,
			processedAmount,
			msg.ErrorMessage,
		)
	}

	// Emit completion event if completed
	if msg.ResultStatus == vaultsv2.INFLIGHT_COMPLETED {
		opType := vaultsv2.OPERATION_TYPE_DEPOSIT
		if origin := fund.GetNobleOrigin(); origin != nil {
			opType = origin.OperationType
		}
		_ = m.EmitInflightCompletedEvent(
			ctx,
			msg.Nonce,
			fund.TransactionId,
			routeID,
			opType,
			fund.Amount,
			processedAmount,
			fund.InitiatedAt,
		)
	}

	return &vaultsv2.MsgProcessInFlightPositionResponse{
		Nonce:           msg.Nonce,
		FinalStatus:     msg.ResultStatus,
		AmountProcessed: processedAmount,
		SharesAffected:  sharesAffected,
	}, nil
}

func (m msgServerV2) RegisterOracle(ctx context.Context, msg *vaultsv2.MsgRegisterOracle) (*vaultsv2.MsgRegisterOracleResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}
	if msg.PositionId == 0 {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "position id must be provided")
	}

	routePosition, foundPosition, err := m.GetVaultsV2RemotePosition(ctx, msg.PositionId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch remote position")
	}
	if !foundPosition {
		return nil, sdkerrors.Wrap(vaultsv2.ErrRemotePositionNotFound, "remote position not found")
	}

	oracleID := oracleIdentifier(msg.PositionId, msg.SourceChain)
	if _, exists, err := m.GetVaultsV2EnrolledOracle(ctx, oracleID); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to check existing oracle")
	} else if exists {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "oracle already registered")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	remoteChainID, hasChain, err := m.GetVaultsV2RemotePositionChainID(ctx, msg.PositionId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch remote position chain id")
	}
	if !hasChain {
		remoteChainID = 0
	}

	metadata := vaultsv2.EnrolledOracle{
		PositionId:    msg.PositionId,
		SourceChain:   msg.SourceChain,
		OracleAddress: msg.OracleAddress,
		MaxStaleness:  msg.MaxStaleness,
		RegisteredAt:  headerInfo.Time,
		Active:        true,
	}

	if err := m.SetVaultsV2EnrolledOracle(ctx, oracleID, metadata); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist enrolled oracle")
	}

	remoteOracle := vaultsv2.RemotePositionOracle{
		PositionId:    msg.PositionId,
		ChainId:       remoteChainID,
		OracleAddress: msg.OracleAddress,
		VaultAddress:  routePosition.VaultAddress.String(),
		SharesHeld:    sdkmath.ZeroInt(),
		SharePrice:    sdkmath.LegacyZeroDec(),
		LastUpdate:    headerInfo.Time,
	}

	if err := m.SetVaultsV2RemotePositionOracle(ctx, msg.PositionId, remoteOracle); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist remote position oracle")
	}

	return &vaultsv2.MsgRegisterOracleResponse{
		OracleId:     oracleID,
		PositionId:   msg.PositionId,
		RegisteredAt: headerInfo.Time,
	}, nil
}

func (m msgServerV2) UpdateOracleConfig(ctx context.Context, msg *vaultsv2.MsgUpdateOracleConfig) (*vaultsv2.MsgUpdateOracleConfigResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}
	if msg.OracleId == "" {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "oracle id must be provided")
	}

	metadata, found, err := m.GetVaultsV2EnrolledOracle(ctx, msg.OracleId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch enrolled oracle")
	}
	if !found {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "oracle not found")
	}

	if msg.MaxStaleness > 0 {
		metadata.MaxStaleness = msg.MaxStaleness
	}
	metadata.Active = msg.Active

	if err := m.SetVaultsV2EnrolledOracle(ctx, msg.OracleId, metadata); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist oracle config")
	}

	return &vaultsv2.MsgUpdateOracleConfigResponse{
		OracleId:      msg.OracleId,
		UpdatedConfig: &metadata,
	}, nil
}

func (m msgServerV2) RemoveOracle(ctx context.Context, msg *vaultsv2.MsgRemoveOracle) (*vaultsv2.MsgRemoveOracleResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}
	if msg.OracleId == "" {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "oracle id must be provided")
	}

	metadata, found, err := m.GetVaultsV2EnrolledOracle(ctx, msg.OracleId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch enrolled oracle")
	}
	if !found {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "oracle not found")
	}

	if err := m.DeleteVaultsV2EnrolledOracle(ctx, msg.OracleId); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to delete enrolled oracle")
	}

	if err := m.DeleteVaultsV2RemotePositionOracle(ctx, metadata.PositionId); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to remove remote position oracle")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	return &vaultsv2.MsgRemoveOracleResponse{
		OracleId:   msg.OracleId,
		PositionId: metadata.PositionId,
		RemovedAt:  headerInfo.Time,
	}, nil
}

func (m msgServerV2) UpdateOracleParams(ctx context.Context, msg *vaultsv2.MsgUpdateOracleParams) (*vaultsv2.MsgUpdateOracleParamsResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	previous, err := m.GetVaultsV2OracleParams(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch oracle params")
	}

	if err := m.SetVaultsV2OracleParams(ctx, msg.Params); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist oracle params")
	}

	return &vaultsv2.MsgUpdateOracleParamsResponse{
		PreviousParams: previous,
		NewParams:      msg.Params,
	}, nil
}

// calculateWithdrawalSplit splits a withdrawal amount between yield and principal.
// Withdrawals are taken from yield first, then principal.
func calculateWithdrawalSplit(withdrawAmount, accruedYield, depositAmount sdkmath.Int) (yieldWithdrawn, principalWithdrawn sdkmath.Int) {
	totalValue := depositAmount.Add(accruedYield)

	if !totalValue.IsPositive() || !withdrawAmount.IsPositive() {
		// No value or no withdrawal, treat as all principal
		return sdkmath.ZeroInt(), withdrawAmount
	}

	if withdrawAmount.GTE(totalValue) {
		// Withdrawing entire position
		return accruedYield, depositAmount
	}

	// Withdraw from yield first, then principal
	if withdrawAmount.LTE(accruedYield) {
		// Entire withdrawal is from yield
		return withdrawAmount, sdkmath.ZeroInt()
	}

	// Withdraw all yield, remainder from principal
	principalWithdrawn, _ = withdrawAmount.SafeSub(accruedYield)
	return accruedYield, principalWithdrawn
}

// ClaimWithdrawal transfers the amount in the specified withdrawal request from the module account to the user
// and updates the accounting state
func (m msgServerV2) ClaimWithdrawal(ctx context.Context, msg *vaultsv2.MsgClaimWithdrawal) (*vaultsv2.MsgClaimWithdrawalResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	addrBz, err := m.address.StringToBytes(msg.Claimer)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidRequest, "invalid claimer address: %s", msg.Claimer)
	}
	claimer := sdk.AccAddress(addrBz)

	request, found, err := m.GetVaultsV2Withdrawal(ctx, msg.RequestId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch withdrawal request")
	}
	if !found {
		return nil, sdkerrors.Wrap(vaultsv2.ErrWithdrawalNotFound, "withdrawal request not found")
	}
	if request.Requester != msg.Claimer {
		return nil, sdkerrors.Wrap(vaultsv2.ErrOperationNotPermitted, "withdrawal does not belong to claimer")
	}
	if request.Status != vaultsv2.WITHDRAWAL_REQUEST_STATUS_READY {
		return nil, sdkerrors.Wrap(vaultsv2.ErrOperationNotPermitted, "withdrawal is not ready for claiming")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	if headerInfo.Time.Before(request.UnlockTime) {
		return nil, sdkerrors.Wrap(vaultsv2.ErrOperationNotPermitted, "withdrawal is still locked")
	}

	// Check if accounting is in progress
	if err := m.checkAccountingNotInProgress(ctx); err != nil {
		return nil, err
	}

	// Rather than enforcing that the sum of funds ready for withdrawal is less than LocalFunds
	// in ProcessWithdrawalQueue, we assume the manager has done their due diligence. However,
	// It's possible for LocalFunds to be reduced between the time ProcessWithdrawalQueue marks
	// a request as Ready and when it is claimed, thus this check.
	localFunds, err := m.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch local funds")
	}
	if localFunds.LT(request.WithdrawAmount) {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInsufficientLocalFunds, "unable to service withdrawal")
	}

	withdrawAmount := request.WithdrawAmount

	// Get position to calculate yield vs principal split
	position, found, err := m.GetVaultsV2UserPosition(ctx, claimer, request.PositionId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidVaultState, "position %d not found for withdrawal", request.PositionId)
	}

	yieldWithdrawn := request.YieldAmount
	principalWithdrawn := request.PrincipalAmount

	if err := m.DeleteVaultsV2Withdrawal(ctx, msg.RequestId); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to remove withdrawal request")
	}

	if err := m.SubtractVaultsV2PendingWithdrawalAmount(ctx, withdrawAmount); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update pending withdrawal amount")
	}

	// Deduct from local funds since coins are leaving the module account
	if err := m.SubtractVaultsV2LocalFunds(ctx, withdrawAmount); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update local funds")
	}

	// Update vault totals with correct split
	if err := m.SubtractAmountFromVaultsV2Totals(ctx, principalWithdrawn, yieldWithdrawn); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update vault totals")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}
	if state.PendingWithdrawalRequests > 0 {
		state.PendingWithdrawalRequests--
	}
	state.TotalDepositPendingWithdrawal, err = state.TotalDepositPendingWithdrawal.SafeSub(principalWithdrawn)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total deposit pending withdrawal")
	}
	state.TotalYieldPendingWithdrawal, err = state.TotalYieldPendingWithdrawal.SafeSub(yieldWithdrawn)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total yield pending withdrawal")
	}
	state.TotalPendingWithdrawal, err = state.TotalPendingWithdrawal.SafeSub(withdrawAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total pending withdrawal")
	}

	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	// Update the position (we already fetched it above)
	{
		if position.DepositPendingWithdrawal.IsPositive() {
			if position.DepositPendingWithdrawal, err = position.DepositPendingWithdrawal.SafeSub(principalWithdrawn); err != nil {
				position.DepositPendingWithdrawal = sdkmath.ZeroInt()
			}
		}
		if position.YieldPendingWithdrawal.IsPositive() {
			if position.YieldPendingWithdrawal, err = position.YieldPendingWithdrawal.SafeSub(yieldWithdrawn); err != nil {
				position.YieldPendingWithdrawal = sdkmath.ZeroInt()
			}
		}
		if position.TotalPendingWithdrawal.IsPositive() {
			if position.TotalPendingWithdrawal, err = position.TotalPendingWithdrawal.SafeSub(withdrawAmount); err != nil {
				position.TotalPendingWithdrawal = sdkmath.ZeroInt()
			}
		}

		position.AccruedYield, _ = position.AccruedYield.SafeSub(yieldWithdrawn)
		position.DepositAmount, _ = position.DepositAmount.SafeSub(principalWithdrawn)

		if position.ActiveWithdrawalRequests > 0 {
			position.ActiveWithdrawalRequests--
		}
		position.LastActivityTime = headerInfo.Time

		// Delete position if it's completely empty
		if position.DepositAmount.IsZero() && position.AccruedYield.IsZero() &&
			!position.TotalPendingWithdrawal.IsPositive() && position.ActiveWithdrawalRequests == 0 {
			if err := m.DeleteVaultsV2UserPosition(ctx, claimer, request.PositionId); err != nil {
				return nil, sdkerrors.Wrap(err, "unable to delete empty user position")
			}

			// Decrement total positions count
			if err := m.DecrementVaultsV2TotalPositions(ctx); err != nil {
				return nil, sdkerrors.Wrap(err, "unable to decrement total positions")
			}

			// Decrement user position count
			count, _ := m.GetUserPositionCount(ctx, claimer)
			if count > 0 {
				isLast := count == 1
				if err := m.SetUserPositionCount(ctx, claimer, count-1); err != nil {
					return nil, sdkerrors.Wrap(err, "unable to update user position count")
				}

				if isLast {
					if err := m.DecrementVaultsV2TotalUsers(ctx); err != nil {
						return nil, sdkerrors.Wrap(err, "unable to decrement total users")
					}
				}
			}
		} else {
			if err := m.SetVaultsV2UserPosition(ctx, claimer, request.PositionId, position); err != nil {
				return nil, sdkerrors.Wrap(err, "unable to persist user position")
			}
		}
	}

	if err := m.bank.SendCoins(ctx, types.ModuleAddress, claimer, sdk.NewCoins(sdk.NewCoin(m.denom, withdrawAmount))); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to transfer withdrawal proceeds")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventWithdraw{
		Withdrawer:          msg.Claimer,
		PrincipalWithdrawn:  principalWithdrawn,
		YieldWithdrawn:      yieldWithdrawn,
		TotalAmountReceived: withdrawAmount,
		FeesPaid:            sdkmath.ZeroInt(),
		BlockHeight:         sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:           headerInfo.Time,
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to emit withdrawal event")
	}

	return &vaultsv2.MsgClaimWithdrawalResponse{
		AmountClaimed:   withdrawAmount,
		PrincipalAmount: principalWithdrawn,
		YieldAmount:     yieldWithdrawn,
		FeesDeducted:    sdkmath.ZeroInt(),
	}, nil
}

// CancelWithdrawal marks the withdrawal request as cancelled and resets the UserPosition and ErrInvalidVaultState accounting
func (m msgServerV2) CancelWithdrawal(ctx context.Context, msg *vaultsv2.MsgCancelWithdrawal) (*vaultsv2.MsgCancelWithdrawalResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	addrBz, err := m.address.StringToBytes(msg.Requester)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidRequest, "invalid requester address: %s", msg.Requester)
	}
	requester := sdk.AccAddress(addrBz)

	// Check if accounting is in progress
	if err := m.checkAccountingNotInProgress(ctx); err != nil {
		return nil, err
	}

	// Fetch the withdrawal request
	request, found, err := m.GetVaultsV2Withdrawal(ctx, msg.RequestId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch withdrawal request")
	}
	if !found {
		return nil, sdkerrors.Wrap(vaultsv2.ErrWithdrawalNotFound, "withdrawal request not found")
	}
	if request.Requester != msg.Requester {
		return nil, sdkerrors.Wrap(vaultsv2.ErrOperationNotPermitted, "withdrawal does not belong to requester")
	}

	// Can only cancel PENDING or READY requests (not already PROCESSED or CANCELLED)
	if request.Status != vaultsv2.WITHDRAWAL_REQUEST_STATUS_PENDING && request.Status != vaultsv2.WITHDRAWAL_REQUEST_STATUS_READY {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrOperationNotPermitted, "cannot cancel withdrawal with status %s", request.Status.String())
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	withdrawAmount := request.WithdrawAmount

	yieldAmount := request.YieldAmount
	principalAmount := request.PrincipalAmount

	position, found, err := m.GetVaultsV2UserPosition(ctx, requester, request.PositionId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidVaultState, "position %d not found for requester", request.PositionId)
	}

	request.Status = vaultsv2.WITHDRAWAL_REQUEST_STATUS_CANCELLED
	if err := m.SetVaultsV2Withdrawal(ctx, msg.RequestId, request); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update withdrawal request status")
	}

	position.DepositPendingWithdrawal, err = position.DepositPendingWithdrawal.SafeSub(principalAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update deposit pending withdrawal amount")
	}
	position.YieldPendingWithdrawal, err = position.YieldPendingWithdrawal.SafeSub(yieldAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update yield pending withdrawal amount")
	}
	position.TotalPendingWithdrawal, err = position.TotalPendingWithdrawal.SafeSub(withdrawAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total pending withdrawal amount")
	}
	if position.ActiveWithdrawalRequests > 0 {
		position.ActiveWithdrawalRequests--
	}
	position.LastActivityTime = headerInfo.Time
	if err := m.SetVaultsV2UserPosition(ctx, requester, request.PositionId, position); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist user position")
	}

	// Reverse the separate pending withdrawal tracking
	if err := m.SubtractVaultsV2PendingWithdrawalAmount(ctx, withdrawAmount); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update pending withdrawal amount")
	}

	// Reverse VaultState changes
	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch vault state")
	}
	if state.PendingWithdrawalRequests > 0 {
		state.PendingWithdrawalRequests--
	}
	state.TotalDepositPendingWithdrawal, err = state.TotalDepositPendingWithdrawal.SafeSub(principalAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total deposit pending withdrawal")
	}
	state.TotalYieldPendingWithdrawal, err = state.TotalYieldPendingWithdrawal.SafeSub(yieldAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total yield pending withdrawal")
	}
	state.TotalPendingWithdrawal, err = state.TotalPendingWithdrawal.SafeSub(withdrawAmount)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update total pending withdrawal")
	}

	if position.ReceiveYield && principalAmount.IsPositive() {
		state.TotalEligibleDeposits, _ = state.TotalEligibleDeposits.SafeAdd(principalAmount)
	}

	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to persist vault state")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventWithdrawalCancelled{
		Requester:           msg.Requester,
		WithdrawalRequestId: msg.RequestId,
		AmountReturned:      withdrawAmount,
		PositionId:          request.PositionId,
		BlockHeight:         sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:           headerInfo.Time,
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to emit withdrawal cancelled event")
	}

	return &vaultsv2.MsgCancelWithdrawalResponse{
		AmountUnlocked: withdrawAmount,
		PositionId:     request.PositionId,
	}, nil
}

func (m msgServerV2) UpdateVaultAccounting(ctx context.Context, msg *vaultsv2.MsgUpdateVaultAccounting) (*vaultsv2.MsgUpdateVaultAccountingResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	// Verify that the manager is the authority
	if msg.Manager != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Manager)
	}

	// Execute cursor-based accounting
	result, err := m.updateVaultsV2AccountingWithCursor(ctx, msg.MaxPositions)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to update vault accounting")
	}

	// Emit event for accounting progress
	headerInfo := m.header.GetHeaderInfo(ctx)
	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventAccountingUpdated{
		PositionsProcessed:      result.PositionsProcessed,
		TotalPositionsProcessed: result.TotalPositionsProcessed,
		TotalPositions:          result.TotalPositions,
		Complete:                result.Complete,
		AppliedNav:              result.AppliedNav,
		YieldDistributed:        result.YieldDistributed,
		Manager:                 msg.Manager,
		BlockHeight:             sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:               headerInfo.Time,
	}); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to emit accounting updated event")
	}

	return &vaultsv2.MsgUpdateVaultAccountingResponse{
		PositionsProcessed:      result.PositionsProcessed,
		TotalPositionsProcessed: result.TotalPositionsProcessed,
		TotalPositions:          result.TotalPositions,
		AccountingComplete:      result.Complete,
		AppliedNav:              result.AppliedNav,
		YieldDistributed:        result.YieldDistributed,
		NextUser:                result.NextUser,
		NegativeYieldWarning:    result.NegativeYieldWarning,
	}, nil
}

func (m msgServerV2) HandleStaleInflight(ctx context.Context, msg *vaultsv2.MsgHandleStaleInflight) (*vaultsv2.MsgHandleStaleInflightResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	fund, found, err := m.GetVaultsV2InflightFund(ctx, msg.InflightId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch inflight fund")
	}
	if !found {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInflightNotFound, "inflight fund %d not found", msg.InflightId)
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	returned, err := m.ProcessInFlightPosition(ctx, &vaultsv2.MsgProcessInFlightPosition{
		Authority:        msg.Authority,
		Nonce:            msg.InflightId,
		ResultStatus:     msg.NewStatus,
		ResultAmount:     fund.Amount,
		ErrorMessage:     msg.Reason,
		ProviderTracking: fund.ProviderTracking,
	})
	if err != nil {
		return nil, err
	}

	return &vaultsv2.MsgHandleStaleInflightResponse{
		InflightId:  msg.InflightId,
		FinalStatus: returned.FinalStatus,
		HandledAt:   headerInfo.Time,
	}, nil
}

func (m msgServerV2) CleanupStaleInflight(ctx context.Context, msg *vaultsv2.MsgCleanupStaleInflight) (*vaultsv2.MsgCleanupStaleInflightResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}
	if msg.InflightId == 0 {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "inflight id must be provided")
	}
	if msg.Reason == "" {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "reason must be provided for audit trail")
	}

	// Get the fund before cleanup to return details
	fund, found, err := m.GetVaultsV2InflightFund(ctx, msg.InflightId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch inflight fund")
	}
	if !found {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInflightNotFound, "inflight fund %d not found", msg.InflightId)
	}

	// Extract route ID
	routeID := uint32(0)
	if tracking := fund.GetProviderTracking(); tracking != nil {
		if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
			routeID = hyperlane.DestinationDomain
		}
	}

	// Perform cleanup
	if err := m.CleanupStaleInflightFund(ctx, fund.Id, msg.Reason, msg.Authority); err != nil {
		return nil, err
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	return &vaultsv2.MsgCleanupStaleInflightResponse{
		InflightId:     fund.Id,
		TransactionId:  fund.TransactionId,
		AmountReturned: fund.Amount,
		RouteId:        routeID,
		CleanedAt:      headerInfo.Time,
	}, nil
}

// enforceDepositLimits validates deposit against all risk control limits.
// This function enforces:
// 1. Global deposit cap (vault capacity)
// 2. Per-block deposit volume limits
// 3. Per-user deposit limits per time window
// 4. Cooldown period between deposits
// 5. Maximum number of deposits per user in time window
func (m msgServerV2) enforceDepositLimits(ctx context.Context, depositor sdk.AccAddress, amount sdkmath.Int, currentBlock int64) error {
	// Get deposit limits configuration
	limits, hasLimits, err := m.getDepositLimits(ctx)
	if err != nil {
		return sdkerrors.Wrap(err, "unable to fetch deposit limits")
	}

	// If no limits configured, allow deposit
	if !hasLimits {
		return nil
	}

	// 1. Check global deposit cap
	if limits.GlobalDepositCap.IsPositive() {
		state, err := m.GetVaultsV2VaultState(ctx)
		if err != nil {
			return sdkerrors.Wrap(err, "unable to fetch vault state")
		}

		projectedTotal, err := state.TotalDeposits.SafeAdd(amount)
		if err != nil {
			return sdkerrors.Wrap(err, "deposit amount causes overflow")
		}

		if projectedTotal.GT(limits.GlobalDepositCap) {
			return sdkerrors.Wrapf(vaultsv2.ErrOperationNotPermitted,
				"deposit would exceed global cap of %s (current: %s, deposit: %s)",
				limits.GlobalDepositCap.String(), state.TotalDeposits.String(), amount.String())
		}
	}

	// 2. Check per-block deposit volume
	if limits.MaxBlockDepositVolume.IsPositive() {
		blockVolume, err := m.getBlockDepositVolume(ctx, currentBlock)
		if err != nil {
			return sdkerrors.Wrap(err, "unable to fetch block deposit volume")
		}

		projectedBlockVolume, err := blockVolume.SafeAdd(amount)
		if err != nil {
			return sdkerrors.Wrap(err, "block volume calculation overflow")
		}

		if projectedBlockVolume.GT(limits.MaxBlockDepositVolume) {
			return sdkerrors.Wrapf(vaultsv2.ErrOperationNotPermitted,
				"deposit would exceed per-block limit of %s (current block volume: %s)",
				limits.MaxBlockDepositVolume.String(), blockVolume.String())
		}
	}

	// 3. Check cooldown period
	if limits.DepositCooldownBlocks > 0 {
		velocity, hasVelocity, err := m.getDepositVelocity(ctx, depositor)
		if err != nil {
			return sdkerrors.Wrap(err, "unable to fetch deposit velocity")
		}

		if hasVelocity && velocity.LastDepositBlock > 0 {
			blocksSinceLastDeposit := currentBlock - velocity.LastDepositBlock
			if blocksSinceLastDeposit < limits.DepositCooldownBlocks {
				remainingBlocks := limits.DepositCooldownBlocks - blocksSinceLastDeposit
				return sdkerrors.Wrapf(vaultsv2.ErrOperationNotPermitted,
					"deposit cooldown active: %d blocks remaining (last deposit at block %d)",
					remainingBlocks, velocity.LastDepositBlock)
			}
		}
	}

	// 4. Check per-user deposit limits and velocity
	if limits.MaxUserDepositPerWindow.IsPositive() || limits.MaxDepositsPerWindow > 0 {
		velocity, hasVelocity, err := m.getDepositVelocity(ctx, depositor)
		if err != nil {
			return sdkerrors.Wrap(err, "unable to fetch deposit velocity")
		}

		// Initialize velocity if not exists
		if !hasVelocity {
			velocity.TimeWindowBlocks = limits.VelocityWindowBlocks
			velocity.RecentDepositVolume = sdkmath.ZeroInt()
		}

		// Check if we need to reset the window
		if limits.VelocityWindowBlocks > 0 && velocity.LastDepositBlock > 0 {
			blocksSinceLastDeposit := currentBlock - velocity.LastDepositBlock
			if blocksSinceLastDeposit >= limits.VelocityWindowBlocks {
				// Window expired, reset counters
				velocity.RecentDepositCount = 0
				velocity.RecentDepositVolume = sdkmath.ZeroInt()
			}
		}

		// Check deposit count limit
		if limits.MaxDepositsPerWindow > 0 && velocity.RecentDepositCount >= limits.MaxDepositsPerWindow {
			return sdkerrors.Wrapf(vaultsv2.ErrOperationNotPermitted,
				"user has reached maximum of %d deposits per %d-block window",
				limits.MaxDepositsPerWindow, limits.VelocityWindowBlocks)
		}

		// Check deposit volume limit
		if limits.MaxUserDepositPerWindow.IsPositive() {
			projectedVolume, err := velocity.RecentDepositVolume.SafeAdd(amount)
			if err != nil {
				return sdkerrors.Wrap(err, "velocity volume calculation overflow")
			}

			if projectedVolume.GT(limits.MaxUserDepositPerWindow) {
				return sdkerrors.Wrapf(vaultsv2.ErrOperationNotPermitted,
					"deposit would exceed user limit of %s per %d-block window (current: %s)",
					limits.MaxUserDepositPerWindow.String(), limits.VelocityWindowBlocks,
					velocity.RecentDepositVolume.String())
			}
		}
	}

	return nil
}

// updateDepositTracking updates all deposit tracking metrics after a successful deposit.
// This should be called after the deposit has been processed and coins transferred.
func (m msgServerV2) updateDepositTracking(ctx context.Context, depositor sdk.AccAddress, amount sdkmath.Int, currentBlock int64) error {
	limits, hasLimits, err := m.getDepositLimits(ctx)
	if err != nil {
		return sdkerrors.Wrap(err, "unable to fetch deposit limits")
	}

	if !hasLimits {
		return nil
	}

	// Update block volume
	if err := m.incrementBlockDeposit(ctx, currentBlock, amount); err != nil {
		return sdkerrors.Wrap(err, "unable to update block deposit volume")
	}

	// Update user deposit history
	if err := m.recordUserDeposit(ctx, depositor, currentBlock, amount); err != nil {
		return sdkerrors.Wrap(err, "unable to record user deposit")
	}

	// Update user velocity
	velocity, hasVelocity, err := m.getDepositVelocity(ctx, depositor)
	if err != nil {
		return sdkerrors.Wrap(err, "unable to fetch deposit velocity")
	}

	if !hasVelocity {
		velocity.TimeWindowBlocks = limits.VelocityWindowBlocks
		velocity.RecentDepositVolume = sdkmath.ZeroInt()
		velocity.RecentDepositCount = 0
	}

	// Check if we need to reset the window
	if limits.VelocityWindowBlocks > 0 && velocity.LastDepositBlock > 0 {
		blocksSinceLastDeposit := currentBlock - velocity.LastDepositBlock
		if blocksSinceLastDeposit >= limits.VelocityWindowBlocks {
			// Window expired, reset counters
			velocity.RecentDepositCount = 0
			velocity.RecentDepositVolume = sdkmath.ZeroInt()
		}
	}

	velocity.LastDepositBlock = currentBlock
	velocity.RecentDepositCount++
	velocity.RecentDepositVolume, err = velocity.RecentDepositVolume.SafeAdd(amount)
	if err != nil {
		return sdkerrors.Wrap(err, "unable to update velocity volume")
	}

	if err := m.setDepositVelocity(ctx, depositor, velocity); err != nil {
		return sdkerrors.Wrap(err, "unable to persist deposit velocity")
	}

	return nil
}

func (m msgServerV2) ProcessIncomingWarpFunds(ctx context.Context, msg *vaultsv2.MsgProcessIncomingWarpFunds) (*vaultsv2.MsgProcessIncomingWarpFundsResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	// Validate vault manager permission
	if msg.Processor != m.authority {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidAuthority, "only vault manager can process incoming warp funds, expected %s, got %s", m.authority, msg.Processor)
	}

	// Validate amount received
	if msg.AmountReceived.IsNil() || !msg.AmountReceived.IsPositive() {
		return nil, sdkerrors.Wrap(vaultsv2.ErrInvalidAmount, "amount received must be positive")
	}

	// Get the inflight fund record
	fund, found, err := m.GetVaultsV2InflightFund(ctx, msg.InflightId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to fetch inflight fund")
	}
	if !found {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInflightNotFound, "inflight fund %d not found", msg.InflightId)
	}

	// Verify the fund is in a state that can be completed
	if fund.Status == vaultsv2.INFLIGHT_COMPLETED {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInflightAlreadyCompleted, "inflight fund %d already completed", msg.InflightId)
	}
	if fund.Status == vaultsv2.INFLIGHT_FAILED || fund.Status == vaultsv2.INFLIGHT_TIMEOUT {
		return nil, sdkerrors.Wrapf(vaultsv2.ErrInflightAlreadyProcessed, "inflight fund %d already failed/timed out", msg.InflightId)
	}

	// Verify route ID matches
	if tracking := fund.GetProviderTracking(); tracking != nil {
		if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
			if hyperlane.OriginDomain != 0 && hyperlane.OriginDomain != msg.OriginDomain {
				return nil, sdkerrors.Wrapf(vaultsv2.ErrInvalidRoute,
					"origin domain mismatch: expected %d, got %d", hyperlane.OriginDomain, msg.OriginDomain)
			}
		}
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	originalAmount := fund.Amount
	amountMatched := msg.AmountReceived.Equal(originalAmount)

	// Update fund status to completed
	fund.Status = vaultsv2.INFLIGHT_COMPLETED
	fund.ExpectedAt = msg.ReceivedAt

	// Update provider tracking with message ID
	if fund.ProviderTracking != nil {
		if hyperlane := fund.ProviderTracking.GetHyperlaneTracking(); hyperlane != nil {
			hyperlane.Processed = true
			hyperlane.DestinationTxHash = msg.HyperlaneMessageId
		}
	}

	// Store updated fund
	if err := m.SetVaultsV2InflightFund(ctx, fund); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update inflight fund")
	}

	// Remove from route inflight value tracking
	if err := m.SubtractVaultsV2InflightValueByRoute(ctx, msg.RouteId, originalAmount); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update route inflight value")
	}

	// Update pending withdrawal distribution with received amount
	if err := m.AddVaultsV2PendingWithdrawalDistribution(ctx, msg.AmountReceived); err != nil {
		return nil, sdkerrors.Wrap(err, "unable to update pending withdrawal distribution")
	}

	// Get updated pending distribution for response
	updatedPending, err := m.GetVaultsV2PendingWithdrawalDistribution(ctx)
	if err != nil {
		updatedPending = sdkmath.ZeroInt()
	}

	// Update remote position if this was a withdrawal from a remote position
	if fund.GetRemoteOrigin() != nil {
		remoteOrigin := fund.GetRemoteOrigin()
		// Find the position by vault address
		var positionID uint64
		var position vaultsv2.RemotePosition
		var foundPosition bool

		err := m.IterateVaultsV2RemotePositions(ctx, func(id uint64, pos vaultsv2.RemotePosition) (bool, error) {
			if pos.VaultAddress.Equal(remoteOrigin.VaultAddress) {
				positionID = id
				position = pos
				foundPosition = true
				return true, nil // stop iteration
			}
			return false, nil
		})
		if err != nil {
			return nil, sdkerrors.Wrap(err, "unable to find remote position")
		}

		if foundPosition {
			// Subtract the amount from position principal and total value
			newPrincipal, err := position.Principal.SafeSub(originalAmount)
			if err == nil {
				position.Principal = newPrincipal
			} else {
				position.Principal = sdkmath.ZeroInt()
			}

			newTotalValue, err := position.TotalValue.SafeSub(originalAmount)
			if err == nil {
				position.TotalValue = newTotalValue
			} else {
				position.TotalValue = sdkmath.ZeroInt()
			}

			// Update position status if no value remaining
			if position.TotalValue.IsZero() || position.SharesHeld.IsZero() {
				position.Status = vaultsv2.REMOTE_POSITION_CLOSED
			}

			position.LastUpdate = headerInfo.Time

			if err := m.SetVaultsV2RemotePosition(ctx, positionID, position); err != nil {
				return nil, sdkerrors.Wrap(err, "unable to update remote position")
			}
		}
	}

	// Emit completion event
	_ = m.EmitInflightCompletedEvent(
		ctx,
		fund.Id,
		fund.TransactionId,
		msg.RouteId,
		vaultsv2.OPERATION_TYPE_WITHDRAWAL,
		originalAmount,
		msg.AmountReceived,
		fund.InitiatedAt,
	)

	// If amounts don't match, emit a warning event
	if !amountMatched {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		_ = sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventInflightAmountMismatch{
			TransactionId:  fund.TransactionId,
			RouteId:        msg.RouteId,
			ExpectedAmount: originalAmount,
			ReceivedAmount: msg.AmountReceived,
			Difference:     originalAmount.Sub(msg.AmountReceived).Abs(),
			BlockHeight:    sdkCtx.BlockHeight(),
			Timestamp:      headerInfo.Time,
			InflightId:     msg.InflightId,
		})
	}

	return &vaultsv2.MsgProcessIncomingWarpFundsResponse{
		TransactionId:              msg.TransactionId,
		RouteId:                    msg.RouteId,
		AmountCompleted:            msg.AmountReceived,
		OriginalAmount:             originalAmount,
		AmountMatched:              amountMatched,
		UpdatedPendingDistribution: updatedPending,
		InflightId:                 msg.InflightId,
	}, nil
}
