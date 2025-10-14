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
	"fmt"
	"strconv"
	"time"

	"cosmossdk.io/errors"
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
		return errors.Wrap(types.ErrInvalidRequest, "hyptoken identifier cannot be zero")
	}
	if route.ReceiverChainHook.IsZeroAddress() {
		return errors.Wrap(types.ErrInvalidRequest, "receiver chain hook cannot be zero")
	}
	if route.RemotePositionAddress.IsZeroAddress() {
		return errors.Wrap(types.ErrInvalidRequest, "remote position address cannot be zero")
	}
	if route.MaxInflightValue.IsNegative() || route.MaxInflightValue.IsZero() {
		return errors.Wrap(vaultsv2.ErrInvalidAmount, "max inflight value must be positive")
	}

	return nil
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

func oracleIdentifier(positionID uint64, sourceChain string) string {
	if sourceChain == "" {
		return strconv.FormatUint(positionID, 10)
	}
	return fmt.Sprintf("%d:%s", positionID, sourceChain)
}

func NewMsgServerV2(keeper *Keeper) vaultsv2.MsgServer {
	return &msgServerV2{Keeper: keeper}
}

func (m msgServerV2) Deposit(ctx context.Context, msg *vaultsv2.MsgDeposit) (*vaultsv2.MsgDepositResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "deposit amount must be positive")
	}

	addrBz, err := m.address.StringToBytes(msg.Depositor)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid depositor address: %s", msg.Depositor)
	}
	depositor := sdk.AccAddress(addrBz)

	params, err := m.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault parameters")
	}
	if !params.VaultEnabled {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "vault deposits are disabled")
	}
	if params.MinDepositAmount.IsPositive() && msg.Amount.LT(params.MinDepositAmount) {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAmount, "deposit below minimum of %s", params.MinDepositAmount.String())
	}

	config, err := m.GetVaultsV2Config(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault configuration")
	}
	if !config.Enabled {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "vault is disabled")
	}

	balance := m.bank.GetBalance(ctx, depositor, m.denom).Amount
	if balance.LT(msg.Amount) {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "insufficient balance for deposit")
	}

	coin := sdk.NewCoin(m.denom, msg.Amount)
	if err := m.bank.SendCoins(ctx, depositor, types.ModuleAddress, sdk.NewCoins(coin)); err != nil {
		return nil, errors.Wrap(err, "unable to transfer deposit into module account")
	}

	if err := m.AddVaultsV2PendingDeploymentFunds(ctx, msg.Amount); err != nil {
		return nil, errors.Wrap(err, "unable to record pending deployment funds")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	position, found, err := m.GetVaultsV2UserPosition(ctx, depositor)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		position.FirstDepositTime = headerInfo.Time
		position.DepositAmount = sdkmath.ZeroInt()
		position.AccruedYield = sdkmath.ZeroInt()
		position.AmountPendingWithdrawal = sdkmath.ZeroInt()
	}
	position.LastActivityTime = headerInfo.Time
	position.DepositAmount, err = position.DepositAmount.SafeAdd(msg.Amount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update position deposit amount")
	}
	if !found || msg.ReceiveYieldOverride {
		position.ReceiveYield = msg.ReceiveYield
	}
	if err := m.SetVaultsV2UserPosition(ctx, depositor, position); err != nil {
		return nil, errors.Wrap(err, "unable to store user position")
	}

	currentShares, err := m.GetVaultsV2UserShares(ctx, depositor)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user shares")
	}
	updatedShares, err := currentShares.SafeAdd(msg.Amount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update user shares")
	}
	if err := m.SetVaultsV2UserShares(ctx, depositor, updatedShares); err != nil {
		return nil, errors.Wrap(err, "unable to store user shares")
	}
	if currentShares.IsZero() && updatedShares.IsPositive() {
		if err := m.IncrementVaultsV2TotalUsers(ctx); err != nil {
			return nil, errors.Wrap(err, "unable to increment total users")
		}
	}

	totalShares, err := m.GetVaultsV2TotalShares(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch total share supply")
	}
	totalShares, err = totalShares.SafeAdd(msg.Amount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update total share supply")
	}
	if err := m.SetVaultsV2TotalShares(ctx, totalShares); err != nil {
		return nil, errors.Wrap(err, "unable to persist total share supply")
	}

	if err := m.AddAmountToVaultsV2Totals(ctx, msg.Amount, sdkmath.ZeroInt()); err != nil {
		return nil, errors.Wrap(err, "unable to update aggregate vault totals")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}
	state.DepositsEnabled = config.Enabled
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventDeposit{
		Depositor:       msg.Depositor,
		AmountDeposited: msg.Amount,
		BlockHeight:     sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:       headerInfo.Time,
	}); err != nil {
		return nil, errors.Wrap(err, "unable to emit deposit event")
	}

	return &vaultsv2.MsgDepositResponse{AmountDeposited: msg.Amount}, nil
}

func (m msgServerV2) RequestWithdrawal(ctx context.Context, msg *vaultsv2.MsgRequestWithdrawal) (*vaultsv2.MsgRequestWithdrawalResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "withdrawal amount must be positive")
	}

	addrBz, err := m.address.StringToBytes(msg.Requester)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid requester address: %s", msg.Requester)
	}
	requester := sdk.AccAddress(addrBz)

	position, found, err := m.GetVaultsV2UserPosition(ctx, requester)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		return nil, errors.Wrap(vaultsv2.ErrInvalidVaultState, "no position found for requester")
	}

	available, err := position.DepositAmount.SafeSub(position.AmountPendingWithdrawal)
	if err != nil {
		return nil, errors.Wrap(err, "unable to determine available balance")
	}
	if available.LT(msg.Amount) {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "insufficient unlocked balance for withdrawal")
	}

	userShares, err := m.GetVaultsV2UserShares(ctx, requester)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user shares")
	}
	if userShares.LT(msg.Amount) {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "insufficient shares to withdraw")
	}
	updatedShares, err := userShares.SafeSub(msg.Amount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update user shares")
	}
	if err := m.SetVaultsV2UserShares(ctx, requester, updatedShares); err != nil {
		return nil, errors.Wrap(err, "unable to persist user shares")
	}
	if userShares.IsPositive() && updatedShares.IsZero() {
		if err := m.DecrementVaultsV2TotalUsers(ctx); err != nil {
			return nil, errors.Wrap(err, "unable to decrement total users")
		}
	}

	totalShares, err := m.GetVaultsV2TotalShares(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch total share supply")
	}
	totalShares, err = totalShares.SafeSub(msg.Amount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update total share supply")
	}
	if err := m.SetVaultsV2TotalShares(ctx, totalShares); err != nil {
		return nil, errors.Wrap(err, "unable to persist total share supply")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	position.AmountPendingWithdrawal, err = position.AmountPendingWithdrawal.SafeAdd(msg.Amount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update pending withdrawal amount")
	}
	position.ActiveWithdrawalRequests++
	position.LastActivityTime = headerInfo.Time
	if err := m.SetVaultsV2UserPosition(ctx, requester, position); err != nil {
		return nil, errors.Wrap(err, "unable to persist user position")
	}

	if err := m.AddVaultsV2PendingWithdrawalAmount(ctx, msg.Amount); err != nil {
		return nil, errors.Wrap(err, "unable to record pending withdrawal amount")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}
	state.PendingWithdrawalRequests++
	state.TotalAmountPendingWithdrawal, err = state.TotalAmountPendingWithdrawal.SafeAdd(msg.Amount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update pending withdrawal totals")
	}
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	params, err := m.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault parameters")
	}

	id, err := m.NextVaultsV2WithdrawalID(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to allocate withdrawal id")
	}

	unlockTime := headerInfo.Time
	if params.WithdrawalRequestTimeout > 0 {
		unlockTime = unlockTime.Add(time.Duration(params.WithdrawalRequestTimeout) * time.Second)
	}

	request := vaultsv2.WithdrawalRequest{
		Requester:          msg.Requester,
		WithdrawAmount:     msg.Amount,
		RequestTime:        headerInfo.Time,
		UnlockTime:         unlockTime,
		Status:             vaultsv2.WITHDRAWAL_REQUEST_STATUS_PENDING,
		EstimatedAmount:    msg.Amount,
		RequestBlockHeight: sdk.UnwrapSDKContext(ctx).BlockHeight(),
	}
	if err := m.SetVaultsV2Withdrawal(ctx, id, request); err != nil {
		return nil, errors.Wrap(err, "unable to persist withdrawal request")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventWithdrawlRequested{
		Requester:           msg.Requester,
		AmountToWithdraw:    msg.Amount,
		WithdrawalRequestId: strconv.FormatUint(id, 10),
		ExpectedUnlockTime:  unlockTime,
		BlockHeight:         request.RequestBlockHeight,
		Timestamp:           headerInfo.Time,
	}); err != nil {
		return nil, errors.Wrap(err, "unable to emit withdrawal requested event")
	}

	return &vaultsv2.MsgRequestWithdrawalResponse{
		RequestId:           strconv.FormatUint(id, 10),
		AmountLocked:        msg.Amount,
		YieldPortion:        sdkmath.ZeroInt(),
		ExpectedClaimableAt: unlockTime,
	}, nil
}

func (m msgServerV2) SetYieldPreference(ctx context.Context, msg *vaultsv2.MsgSetYieldPreference) (*vaultsv2.MsgSetYieldPreferenceResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	addrBz, err := m.address.StringToBytes(msg.User)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid user address: %s", msg.User)
	}
	user := sdk.AccAddress(addrBz)

	position, found, err := m.GetVaultsV2UserPosition(ctx, user)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		return nil, errors.Wrap(vaultsv2.ErrInvalidVaultState, "user position not found")
	}

	previousPreference := position.ReceiveYield
	if previousPreference == msg.ReceiveYield {
		headerInfo := m.header.GetHeaderInfo(ctx)
		position.LastActivityTime = headerInfo.Time
		if err := m.SetVaultsV2UserPosition(ctx, user, position); err != nil {
			return nil, errors.Wrap(err, "unable to persist user position")
		}
		return &vaultsv2.MsgSetYieldPreferenceResponse{
			PreviousPreference: previousPreference,
			NewPreference:      msg.ReceiveYield,
		}, nil
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	position.ReceiveYield = msg.ReceiveYield
	position.LastActivityTime = headerInfo.Time

	if err := m.SetVaultsV2UserPosition(ctx, user, position); err != nil {
		return nil, errors.Wrap(err, "unable to persist user position")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventYieldPreferenceUpdated{
		User:                    msg.User,
		PreviousYieldPreference: previousPreference,
		NewYieldPreference:      msg.ReceiveYield,
		BlockHeight:             sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:               headerInfo.Time,
	}); err != nil {
		return nil, errors.Wrap(err, "unable to emit yield preference event")
	}

	return &vaultsv2.MsgSetYieldPreferenceResponse{
		PreviousPreference: previousPreference,
		NewPreference:      msg.ReceiveYield,
	}, nil
}

func (m msgServerV2) ProcessWithdrawalQueue(ctx context.Context, msg *vaultsv2.MsgProcessWithdrawalQueue) (*vaultsv2.MsgProcessWithdrawalQueueResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	limit := msg.MaxRequests
	if limit <= 0 {
		limit = int32(^uint32(0) >> 1)
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
		return nil, errors.Wrap(err, "unable to iterate withdrawal queue")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	if processed > 0 {
		if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventWithdrawalProcessed{
			RequestsProcessed:      processed,
			TotalAmountProcessed:   totalProcessed,
			TotalAmountDistributed: sdkmath.ZeroInt(),
			BlockHeight:            sdk.UnwrapSDKContext(ctx).BlockHeight(),
			Timestamp:              headerInfo.Time,
		}); err != nil {
			return nil, errors.Wrap(err, "unable to emit withdrawal processed event")
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
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	if msg.Config.MaxTotalDeposits.IsNegative() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "maximum total deposits cannot be negative")
	}
	if msg.Config.TargetYieldRate.IsNegative() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "target yield rate cannot be negative")
	}

	existingConfig, err := m.GetVaultsV2Config(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch current vault config")
	}

	if err := m.SetVaultsV2Config(ctx, msg.Config); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault config")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}
	state.DepositsEnabled = msg.Config.Enabled
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	prevJSON, err := m.cdc.MarshalJSON(&existingConfig)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal previous config")
	}
	newJSON, err := m.cdc.MarshalJSON(&msg.Config)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal new config")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventVaultConfigUpdated{
		PreviousConfig: string(prevJSON),
		NewConfig:      string(newJSON),
		Authority:      msg.Authority,
		Reason:         msg.Reason,
		BlockHeight:    sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:      headerInfo.Time,
	}); err != nil {
		return nil, errors.Wrap(err, "unable to emit vault config updated event")
	}

	return &vaultsv2.MsgUpdateVaultConfigResponse{
		PreviousConfig: string(prevJSON),
		NewConfig:      string(newJSON),
	}, nil
}

func (m msgServerV2) UpdateParams(ctx context.Context, msg *vaultsv2.MsgUpdateParams) (*vaultsv2.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	if msg.Params.MinDepositAmount.IsNegative() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "minimum deposit amount cannot be negative")
	}
	if msg.Params.MinWithdrawalAmount.IsNegative() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "minimum withdrawal amount cannot be negative")
	}
	if msg.Params.MaxNavChangeBps < 0 {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "maximum NAV change must be non-negative")
	}
	if msg.Params.WithdrawalRequestTimeout < 0 {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "withdrawal request timeout must be non-negative")
	}
	if msg.Params.MaxWithdrawalRequestsPerBlock < 0 {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "max withdrawal requests per block must be non-negative")
	}

	params := msg.Params
	params.Authority = m.authority

	if err := m.SetVaultsV2Params(ctx, params); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault params")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	config, err := m.GetVaultsV2Config(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault config")
	}

	state.DepositsEnabled = params.VaultEnabled && config.Enabled
	state.WithdrawalsEnabled = params.VaultEnabled

	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	return &vaultsv2.MsgUpdateParamsResponse{}, nil
}

func (m msgServerV2) CreateCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgCreateCrossChainRoute) (*vaultsv2.MsgCreateCrossChainRouteResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	if err := validateCrossChainRoute(msg.Route); err != nil {
		return nil, err
	}

	var duplicateErr error
	if err := m.IterateVaultsV2CrossChainRoutes(ctx, func(_ uint32, existing vaultsv2.CrossChainRoute) (bool, error) {
		if existing.RemotePositionAddress == msg.Route.RemotePositionAddress {
			duplicateErr = errors.Wrap(types.ErrInvalidRequest, "cross-chain route already exists for remote position address")
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, errors.Wrap(err, "unable to verify existing routes")
	}
	if duplicateErr != nil {
		return nil, duplicateErr
	}

	routeID, err := m.NextVaultsV2CrossChainRouteID(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to allocate cross-chain route id")
	}

	if err := m.SetVaultsV2CrossChainRoute(ctx, routeID, msg.Route); err != nil {
		return nil, errors.Wrap(err, "unable to persist cross-chain route")
	}

	return &vaultsv2.MsgCreateCrossChainRouteResponse{RouteId: routeID}, nil
}

func (m msgServerV2) UpdateCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgUpdateCrossChainRoute) (*vaultsv2.MsgUpdateCrossChainRouteResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	if err := validateCrossChainRoute(msg.Route); err != nil {
		return nil, err
	}

	existing, found, err := m.GetVaultsV2CrossChainRoute(ctx, msg.RouteId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch cross-chain route")
	}
	if !found {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "cross-chain route %d not found", msg.RouteId)
	}

	var duplicateErr error
	if err := m.IterateVaultsV2CrossChainRoutes(ctx, func(id uint32, route vaultsv2.CrossChainRoute) (bool, error) {
		if id == msg.RouteId {
			return false, nil
		}
		if route.RemotePositionAddress == msg.Route.RemotePositionAddress {
			duplicateErr = errors.Wrap(types.ErrInvalidRequest, "cross-chain route already exists for remote position address")
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, errors.Wrap(err, "unable to verify existing routes")
	}
	if duplicateErr != nil {
		return nil, duplicateErr
	}

	prevJSON, err := m.cdc.MarshalJSON(&existing)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal previous route config")
	}

	newJSON, err := m.cdc.MarshalJSON(&msg.Route)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal new route config")
	}

	if err := m.SetVaultsV2CrossChainRoute(ctx, msg.RouteId, msg.Route); err != nil {
		return nil, errors.Wrap(err, "unable to persist cross-chain route")
	}

	return &vaultsv2.MsgUpdateCrossChainRouteResponse{
		RouteId:        msg.RouteId,
		PreviousConfig: string(prevJSON),
		NewConfig:      string(newJSON),
	}, nil
}

func (m msgServerV2) DisableCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgDisableCrossChainRoute) (*vaultsv2.MsgDisableCrossChainRouteResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	route, found, err := m.GetVaultsV2CrossChainRoute(ctx, msg.RouteId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch cross-chain route")
	}
	if !found {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "cross-chain route %d not found", msg.RouteId)
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	var affected int64

	positions, err := m.GetAllVaultsV2RemotePositions(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote positions")
	}

	for _, entry := range positions {
		if entry.Position.VaultAddress == route.RemotePositionAddress {
			affected++
			entry.Position.Status = vaultsv2.REMOTE_POSITION_ERROR
			entry.Position.LastUpdate = headerInfo.Time
			if err := m.SetVaultsV2RemotePosition(ctx, entry.ID, entry.Position); err != nil {
				return nil, errors.Wrapf(err, "unable to update remote position %d", entry.ID)
			}
		}
	}

	if err := m.DeleteVaultsV2CrossChainRoute(ctx, msg.RouteId); err != nil {
		return nil, errors.Wrap(err, "unable to remove cross-chain route")
	}

	return &vaultsv2.MsgDisableCrossChainRouteResponse{
		RouteId:           msg.RouteId,
		AffectedPositions: affected,
	}, nil
}

func (m msgServerV2) CreateRemotePosition(ctx context.Context, msg *vaultsv2.MsgCreateRemotePosition) (*vaultsv2.MsgCreateRemotePositionResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Manager != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Manager)
	}
	if msg.ChainId == 0 {
		return nil, errors.Wrap(types.ErrInvalidRequest, "chain id must be provided")
	}
	if msg.VaultAddress == "" {
		return nil, errors.Wrap(types.ErrInvalidRequest, "vault address must be provided")
	}
	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "amount must be positive")
	}
	if msg.MinSharesOut.IsPositive() && msg.Amount.LT(msg.MinSharesOut) {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "amount less than minimum shares out")
	}

	pendingDeployment, err := m.GetVaultsV2PendingDeploymentFunds(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch pending deployment funds")
	}
	if pendingDeployment.LT(msg.Amount) {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "insufficient pending deployment funds")
	}

	vaultAddress, err := hyperlaneutil.DecodeHexAddress(msg.VaultAddress)
	if err != nil {
		return nil, errors.Wrap(err, "unable to decode vault address")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	positionID, err := m.NextVaultsV2RemotePositionID(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to allocate remote position id")
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
		return nil, errors.Wrap(err, "unable to store remote position")
	}

	if err := m.SetVaultsV2RemotePositionChainID(ctx, positionID, msg.ChainId); err != nil {
		return nil, errors.Wrap(err, "unable to store remote position chain id")
	}

	if err := m.SubtractVaultsV2PendingDeploymentFunds(ctx, msg.Amount); err != nil {
		return nil, errors.Wrap(err, "unable to update pending deployment funds")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	return &vaultsv2.MsgCreateRemotePositionResponse{
		PositionId:         positionID,
		RouteId:            msg.ChainId,
		ExpectedCompletion: headerInfo.Time,
	}, nil
}

func (m msgServerV2) CloseRemotePosition(ctx context.Context, msg *vaultsv2.MsgCloseRemotePosition) (*vaultsv2.MsgCloseRemotePositionResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Manager != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Manager)
	}
	if msg.PositionId == 0 {
		return nil, errors.Wrap(types.ErrInvalidRequest, "position id must be provided")
	}

	position, found, err := m.GetVaultsV2RemotePosition(ctx, msg.PositionId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote position")
	}
	if !found {
		return nil, errors.Wrap(vaultsv2.ErrRemotePositionNotFound, "remote position not found")
	}
	if position.Status == vaultsv2.REMOTE_POSITION_CLOSED {
		return nil, errors.Wrap(vaultsv2.ErrInvalidVaultState, "remote position already closed")
	}

	withdrawAmount := position.TotalValue
	if msg.PartialAmount.IsPositive() {
		if msg.PartialAmount.GT(position.TotalValue) {
			return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "partial amount exceeds position value")
		}
		withdrawAmount = msg.PartialAmount
	}
	if !withdrawAmount.IsPositive() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "withdraw amount must be positive")
	}

	totalValue, err := position.TotalValue.SafeSub(withdrawAmount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update position value")
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
		return nil, errors.Wrap(err, "unable to persist remote position")
	}

	if err := m.AddVaultsV2PendingWithdrawalDistribution(ctx, withdrawAmount); err != nil {
		return nil, errors.Wrap(err, "unable to update pending withdrawal distribution")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	return &vaultsv2.MsgCloseRemotePositionResponse{
		PositionId:         msg.PositionId,
		Initiated:          true,
		ExpectedCompletion: headerInfo.Time,
	}, nil
}

func (m msgServerV2) Rebalance(ctx context.Context, msg *vaultsv2.MsgRebalance) (*vaultsv2.MsgRebalanceResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}

	if msg.Manager != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Manager)
	}

	if len(msg.TargetAllocations) == 0 {
		return nil, errors.Wrap(types.ErrInvalidRequest, "at least one target allocation is required")
	}

	seen := make(map[uint64]struct{}, len(msg.TargetAllocations))
	var total uint32

	for _, allocation := range msg.TargetAllocations {
		if allocation == nil {
			return nil, errors.Wrap(types.ErrInvalidRequest, "target allocation cannot be nil")
		}
		if allocation.PositionId == 0 {
			return nil, errors.Wrap(types.ErrInvalidRequest, "target allocation position id must be greater than zero")
		}
		if allocation.TargetPercentage == 0 {
			return nil, errors.Wrap(types.ErrInvalidRequest, "target allocation percentage must be greater than zero")
		}
		if allocation.TargetPercentage > 100 {
			return nil, errors.Wrap(types.ErrInvalidRequest, "target allocation percentage cannot exceed 100")
		}
		if _, exists := seen[allocation.PositionId]; exists {
			return nil, errors.Wrapf(types.ErrInvalidRequest, "duplicate target allocation for position %d", allocation.PositionId)
		}

		seen[allocation.PositionId] = struct{}{}
		total += allocation.TargetPercentage
		if total > 100 {
			return nil, errors.Wrap(types.ErrInvalidRequest, "target allocations exceed 100 percent")
		}
	}

	positions, err := m.GetAllVaultsV2RemotePositions(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote positions")
	}
	if len(positions) == 0 {
		return nil, errors.Wrap(vaultsv2.ErrRemotePositionNotFound, "no remote positions configured")
	}

	indexByID := make(map[uint64]int, len(positions))
	for i := range positions {
		indexByID[positions[i].ID] = i
	}

	targetDesired := make(map[uint64]sdkmath.Int, len(msg.TargetAllocations))
	totalTracked := sdkmath.ZeroInt()

	pendingDeployment, err := m.GetVaultsV2PendingDeploymentFunds(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch pending deployment funds")
	}

	totalTracked = pendingDeployment
	for _, entry := range positions {
		totalTracked, err = totalTracked.SafeAdd(entry.Position.TotalValue)
		if err != nil {
			return nil, errors.Wrap(err, "unable to aggregate remote position value")
		}
	}

	for _, allocation := range msg.TargetAllocations {
		idx, ok := indexByID[allocation.PositionId]
		if !ok {
			return nil, errors.Wrapf(vaultsv2.ErrRemotePositionNotFound, "target position %d not found", allocation.PositionId)
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

	available := pendingDeployment
	operations := 0

	for _, adj := range decreases {
		if adj.amount.IsZero() {
			continue
		}
		position := positions[adj.index].Position

		if adj.amount.GT(position.TotalValue) {
			return nil, errors.Wrap(vaultsv2.ErrInvalidVaultState, "rebalance reduction exceeds position value")
		}

		totalValue, err := position.TotalValue.SafeSub(adj.amount)
		if err != nil {
			return nil, errors.Wrap(err, "unable to reduce position value")
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
			return nil, errors.Wrap(err, "unable to accumulate freed funds")
		}

		positions[adj.index].Position = position
		operations++
	}

	for _, adj := range increases {
		if adj.amount.IsZero() {
			continue
		}
		if available.LT(adj.amount) {
			return nil, errors.Wrap(vaultsv2.ErrInvalidVaultState, "insufficient liquidity for rebalance")
		}

		entry := positions[adj.index]

		inflightID, err := m.NextVaultsV2InflightID(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "unable to allocate inflight identifier")
		}

		destination := &vaultsv2.RemotePosition{
			HyptokenId:   entry.Position.HyptokenId,
			VaultAddress: entry.Position.VaultAddress,
			SharePrice:   entry.Position.SharePrice,
			Status:       entry.Position.Status,
		}

		fund := vaultsv2.InflightFund{
			Id:                strconv.FormatUint(inflightID, 10),
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
			return nil, errors.Wrap(err, "unable to persist inflight fund")
		}

		available, err = available.SafeSub(adj.amount)
		if err != nil {
			return nil, errors.Wrap(err, "unable to deduct deployed funds")
		}

		operations++
	}

	for _, entry := range positions {
		if err := m.SetVaultsV2RemotePosition(ctx, entry.ID, entry.Position); err != nil {
			return nil, errors.Wrap(err, "unable to persist remote position")
		}
	}

	if err := m.VaultsV2PendingDeploymentFunds.Set(ctx, available); err != nil {
		return nil, errors.Wrap(err, "unable to persist pending deployment funds")
	}

	headerInfo = m.header.GetHeaderInfo(ctx)
	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	summary := fmt.Sprintf("rebalanced %d positions; pending deployment %s", operations, available.String())

	return &vaultsv2.MsgRebalanceResponse{
		OperationsInitiated: int32(operations),
		Summary:             summary,
	}, nil
}

func (m msgServerV2) RemoteDeposit(ctx context.Context, msg *vaultsv2.MsgRemoteDeposit) (*vaultsv2.MsgRemoteDepositResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "deposit amount must be positive")
	}
	if msg.MinShares.IsNegative() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "minimum shares cannot be negative")
	}

	route, found, err := m.GetVaultsV2CrossChainRoute(ctx, msg.RouteId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch cross-chain route")
	}
	if !found {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "cross-chain route %d not found", msg.RouteId)
	}

	currentInflight, err := m.GetVaultsV2InflightValueByRoute(ctx, msg.RouteId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch route inflight value")
	}
	projected, err := currentInflight.SafeAdd(msg.Amount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to calculate projected inflight value")
	}
	if projected.GT(route.MaxInflightValue) {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "route inflight cap exceeded")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	inflightID, err := m.NextVaultsV2InflightID(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to allocate inflight identifier")
	}

	sharesAllocated := msg.MinShares
	if !sharesAllocated.IsPositive() {
		sharesAllocated = msg.Amount
	}

	fund := vaultsv2.InflightFund{
		Id:                strconv.FormatUint(inflightID, 10),
		TransactionId:     msg.RemoteAddress,
		Amount:            msg.Amount,
		ValueAtInitiation: sharesAllocated,
		InitiatedAt:       headerInfo.Time,
		ExpectedAt:        headerInfo.Time,
		Status:            vaultsv2.INFLIGHT_PENDING,
		Origin: &vaultsv2.InflightFund_NobleOrigin{
			NobleOrigin: &vaultsv2.NobleEndpoint{OperationType: vaultsv2.OPERATION_TYPE_DEPOSIT},
		},
		Destination: &vaultsv2.InflightFund_RemoteDestination{
			RemoteDestination: &vaultsv2.RemotePosition{
				HyptokenId:    route.HyptokenId,
				VaultAddress:  route.RemotePositionAddress,
				SharesHeld:    sdkmath.ZeroInt(),
				Principal:     sdkmath.ZeroInt(),
				SharePrice:    sdkmath.LegacyOneDec(),
				TotalValue:    sdkmath.ZeroInt(),
				Status:        vaultsv2.REMOTE_POSITION_ACTIVE,
				OracleAddress: "",
				MaxStaleness:  0,
				LastUpdate:    headerInfo.Time,
			},
		},
		ProviderTracking: &vaultsv2.ProviderTrackingInfo{
			TrackingInfo: &vaultsv2.ProviderTrackingInfo_HyperlaneTracking{
				HyperlaneTracking: &vaultsv2.HyperlaneTrackingInfo{
					DestinationDomain: msg.RouteId,
					Nonce:             inflightID,
					Processed:         false,
				},
			},
		},
	}

	if err := m.AddVaultsV2InflightValueByRoute(ctx, msg.RouteId, msg.Amount); err != nil {
		return nil, errors.Wrap(err, "unable to record inflight value for route")
	}

	if err := m.SetVaultsV2InflightFund(ctx, fund); err != nil {
		return nil, errors.Wrap(err, "unable to persist inflight fund")
	}

	return &vaultsv2.MsgRemoteDepositResponse{
		Nonce:              inflightID,
		RouteId:            msg.RouteId,
		SharesAllocated:    sharesAllocated,
		AmountSent:         msg.Amount,
		ExpectedCompletion: headerInfo.Time,
		ProviderTracking:   fund.ProviderTracking,
	}, nil
}

func (m msgServerV2) RemoteWithdraw(ctx context.Context, msg *vaultsv2.MsgRemoteWithdraw) (*vaultsv2.MsgRemoteWithdrawResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Shares.IsNil() || !msg.Shares.IsPositive() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "withdraw shares must be positive")
	}
	if msg.MinAmount.IsNegative() {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "minimum amount cannot be negative")
	}

	route, found, err := m.GetVaultsV2CrossChainRoute(ctx, msg.RouteId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch cross-chain route")
	}
	if !found {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "cross-chain route %d not found", msg.RouteId)
	}

	positionID, position, foundPosition, err := m.findRemotePositionByAddress(ctx, route.RemotePositionAddress)
	if err != nil {
		return nil, errors.Wrap(err, "unable to lookup remote position")
	}
	if !foundPosition {
		return nil, errors.Wrap(vaultsv2.ErrRemotePositionNotFound, "remote position not found for route")
	}
	if position.SharesHeld.LT(msg.Shares) {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "insufficient remote shares for withdrawal")
	}

	currentInflight, err := m.GetVaultsV2InflightValueByRoute(ctx, msg.RouteId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch route inflight value")
	}
	inflightAmount := msg.MinAmount
	if !inflightAmount.IsPositive() {
		inflightAmount = msg.Shares
	}
	projected, err := currentInflight.SafeAdd(inflightAmount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to calculate projected inflight value")
	}
	if projected.GT(route.MaxInflightValue) {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "route inflight cap exceeded")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	position.Status = vaultsv2.REMOTE_POSITION_WITHDRAWING
	position.LastUpdate = headerInfo.Time
	if err := m.SetVaultsV2RemotePosition(ctx, positionID, position); err != nil {
		return nil, errors.Wrap(err, "unable to update remote position status")
	}

	inflightID, err := m.NextVaultsV2InflightID(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to allocate inflight identifier")
	}

	fund := vaultsv2.InflightFund{
		Id:                strconv.FormatUint(inflightID, 10),
		TransactionId:     msg.Withdrawer,
		Amount:            inflightAmount,
		ValueAtInitiation: msg.Shares,
		InitiatedAt:       headerInfo.Time,
		ExpectedAt:        headerInfo.Time,
		Status:            vaultsv2.INFLIGHT_PENDING,
		Origin: &vaultsv2.InflightFund_RemoteOrigin{
			RemoteOrigin: &vaultsv2.RemotePosition{
				HyptokenId:    route.HyptokenId,
				VaultAddress:  route.RemotePositionAddress,
				SharesHeld:    position.SharesHeld,
				Principal:     position.Principal,
				SharePrice:    position.SharePrice,
				TotalValue:    position.TotalValue,
				LastUpdate:    position.LastUpdate,
				Status:        position.Status,
				OracleAddress: position.OracleAddress,
				MaxStaleness:  position.MaxStaleness,
			},
		},
		Destination: &vaultsv2.InflightFund_NobleDestination{
			NobleDestination: &vaultsv2.NobleEndpoint{OperationType: vaultsv2.OPERATION_TYPE_WITHDRAWAL},
		},
		ProviderTracking: &vaultsv2.ProviderTrackingInfo{
			TrackingInfo: &vaultsv2.ProviderTrackingInfo_HyperlaneTracking{
				HyperlaneTracking: &vaultsv2.HyperlaneTrackingInfo{
					OriginDomain: msg.RouteId,
					Nonce:        inflightID,
					Processed:    false,
				},
			},
		},
	}

	if err := m.AddVaultsV2InflightValueByRoute(ctx, msg.RouteId, inflightAmount); err != nil {
		return nil, errors.Wrap(err, "unable to record inflight value for route")
	}

	if err := m.SetVaultsV2InflightFund(ctx, fund); err != nil {
		return nil, errors.Wrap(err, "unable to persist inflight fund")
	}

	return &vaultsv2.MsgRemoteWithdrawResponse{
		Nonce:              inflightID,
		RouteId:            msg.RouteId,
		SharesWithdrawn:    msg.Shares,
		ExpectedAmount:     inflightAmount,
		ExpectedCompletion: headerInfo.Time,
		ProviderTracking:   fund.ProviderTracking,
	}, nil
}

func (m msgServerV2) ProcessInFlightPosition(ctx context.Context, msg *vaultsv2.MsgProcessInFlightPosition) (*vaultsv2.MsgProcessInFlightPositionResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	inflightID := strconv.FormatUint(msg.Nonce, 10)
	fund, found, err := m.GetVaultsV2InflightFund(ctx, inflightID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch inflight fund")
	}
	if !found {
		return nil, errors.Wrapf(vaultsv2.ErrInflightNotFound, "inflight fund %d not found", msg.Nonce)
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	if msg.ProviderTracking != nil {
		fund.ProviderTracking = msg.ProviderTracking
	}

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
			_ = m.AddVaultsV2PendingDeploymentFunds(ctx, fund.Amount)
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
		return nil, errors.Wrap(err, "unable to persist inflight fund update")
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
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}
	if msg.PositionId == 0 {
		return nil, errors.Wrap(types.ErrInvalidRequest, "position id must be provided")
	}

	routePosition, foundPosition, err := m.GetVaultsV2RemotePosition(ctx, msg.PositionId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote position")
	}
	if !foundPosition {
		return nil, errors.Wrap(vaultsv2.ErrRemotePositionNotFound, "remote position not found")
	}

	oracleID := oracleIdentifier(msg.PositionId, msg.SourceChain)
	if _, exists, err := m.GetVaultsV2EnrolledOracle(ctx, oracleID); err != nil {
		return nil, errors.Wrap(err, "unable to check existing oracle")
	} else if exists {
		return nil, errors.Wrap(types.ErrInvalidRequest, "oracle already registered")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	remoteChainID, hasChain, err := m.GetVaultsV2RemotePositionChainID(ctx, msg.PositionId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote position chain id")
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
		return nil, errors.Wrap(err, "unable to persist enrolled oracle")
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
		return nil, errors.Wrap(err, "unable to persist remote position oracle")
	}

	return &vaultsv2.MsgRegisterOracleResponse{
		OracleId:     oracleID,
		PositionId:   msg.PositionId,
		RegisteredAt: headerInfo.Time,
	}, nil
}

func (m msgServerV2) UpdateOracleConfig(ctx context.Context, msg *vaultsv2.MsgUpdateOracleConfig) (*vaultsv2.MsgUpdateOracleConfigResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}
	if msg.OracleId == "" {
		return nil, errors.Wrap(types.ErrInvalidRequest, "oracle id must be provided")
	}

	metadata, found, err := m.GetVaultsV2EnrolledOracle(ctx, msg.OracleId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch enrolled oracle")
	}
	if !found {
		return nil, errors.Wrap(types.ErrInvalidRequest, "oracle not found")
	}

	if msg.MaxStaleness > 0 {
		metadata.MaxStaleness = msg.MaxStaleness
	}
	metadata.Active = msg.Active

	if err := m.SetVaultsV2EnrolledOracle(ctx, msg.OracleId, metadata); err != nil {
		return nil, errors.Wrap(err, "unable to persist oracle config")
	}

	return &vaultsv2.MsgUpdateOracleConfigResponse{
		OracleId:      msg.OracleId,
		UpdatedConfig: &metadata,
	}, nil
}

func (m msgServerV2) RemoveOracle(ctx context.Context, msg *vaultsv2.MsgRemoveOracle) (*vaultsv2.MsgRemoveOracleResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}
	if msg.OracleId == "" {
		return nil, errors.Wrap(types.ErrInvalidRequest, "oracle id must be provided")
	}

	metadata, found, err := m.GetVaultsV2EnrolledOracle(ctx, msg.OracleId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch enrolled oracle")
	}
	if !found {
		return nil, errors.Wrap(types.ErrInvalidRequest, "oracle not found")
	}

	if err := m.DeleteVaultsV2EnrolledOracle(ctx, msg.OracleId); err != nil {
		return nil, errors.Wrap(err, "unable to delete enrolled oracle")
	}

	if err := m.DeleteVaultsV2RemotePositionOracle(ctx, metadata.PositionId); err != nil {
		return nil, errors.Wrap(err, "unable to remove remote position oracle")
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
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	previous, err := m.GetVaultsV2OracleParams(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch oracle params")
	}

	if err := m.SetVaultsV2OracleParams(ctx, msg.Params); err != nil {
		return nil, errors.Wrap(err, "unable to persist oracle params")
	}

	return &vaultsv2.MsgUpdateOracleParamsResponse{
		PreviousParams: previous,
		NewParams:      msg.Params,
	}, nil
}

func (m msgServerV2) ClaimWithdrawal(ctx context.Context, msg *vaultsv2.MsgClaimWithdrawal) (*vaultsv2.MsgClaimWithdrawalResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.RequestId == "" {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request id must be provided")
	}

	id, err := strconv.ParseUint(msg.RequestId, 10, 64)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid request id: %s", msg.RequestId)
	}

	addrBz, err := m.address.StringToBytes(msg.Claimer)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid claimer address: %s", msg.Claimer)
	}
	claimer := sdk.AccAddress(addrBz)

	request, found, err := m.GetVaultsV2Withdrawal(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch withdrawal request")
	}
	if !found {
		return nil, errors.Wrap(vaultsv2.ErrWithdrawalNotFound, "withdrawal request not found")
	}
	if request.Requester != msg.Claimer {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "withdrawal does not belong to claimer")
	}
	if request.Status != vaultsv2.WITHDRAWAL_REQUEST_STATUS_READY {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "withdrawal is not ready for claiming")
	}

	headerInfo := m.header.GetHeaderInfo(ctx)
	if headerInfo.Time.Before(request.UnlockTime) {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "withdrawal is still locked")
	}

	withdrawAmount := request.WithdrawAmount
	if err := m.DeleteVaultsV2Withdrawal(ctx, id); err != nil {
		return nil, errors.Wrap(err, "unable to remove withdrawal request")
	}

	if err := m.SubtractVaultsV2PendingWithdrawalAmount(ctx, withdrawAmount); err != nil {
		return nil, errors.Wrap(err, "unable to update pending withdrawal amount")
	}

	if err := m.SubtractAmountFromVaultsV2Totals(ctx, withdrawAmount, sdkmath.ZeroInt()); err != nil {
		return nil, errors.Wrap(err, "unable to update vault totals")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}
	if state.PendingWithdrawalRequests > 0 {
		state.PendingWithdrawalRequests--
	}
	state.TotalAmountPendingWithdrawal, err = state.TotalAmountPendingWithdrawal.SafeSub(withdrawAmount)
	if err != nil {
		return nil, errors.Wrap(err, "unable to update pending withdrawal totals")
	}
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	position, found, err := m.GetVaultsV2UserPosition(ctx, claimer)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user position")
	}
	if found {
		if position.AmountPendingWithdrawal.IsPositive() {
			if position.AmountPendingWithdrawal, err = position.AmountPendingWithdrawal.SafeSub(withdrawAmount); err != nil {
				position.AmountPendingWithdrawal = sdkmath.ZeroInt()
			}
		}
		if position.DepositAmount.IsPositive() {
			if position.DepositAmount, err = position.DepositAmount.SafeSub(withdrawAmount); err != nil {
				position.DepositAmount = sdkmath.ZeroInt()
			}
		}
		if position.ActiveWithdrawalRequests > 0 {
			position.ActiveWithdrawalRequests--
		}
		position.LastActivityTime = headerInfo.Time
		if position.DepositAmount.IsZero() && !position.AmountPendingWithdrawal.IsPositive() && position.ActiveWithdrawalRequests == 0 {
			if err := m.DeleteVaultsV2UserPosition(ctx, claimer); err != nil {
				return nil, errors.Wrap(err, "unable to delete empty user position")
			}
		} else {
			if err := m.SetVaultsV2UserPosition(ctx, claimer, position); err != nil {
				return nil, errors.Wrap(err, "unable to persist user position")
			}
		}
	}

	if err := m.bank.SendCoins(ctx, types.ModuleAddress, claimer, sdk.NewCoins(sdk.NewCoin(m.denom, withdrawAmount))); err != nil {
		return nil, errors.Wrap(err, "unable to transfer withdrawal proceeds")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventWithdraw{
		Withdrawer:          msg.Claimer,
		PrincipalWithdrawn:  withdrawAmount,
		YieldWithdrawn:      sdkmath.ZeroInt(),
		TotalAmountReceived: withdrawAmount,
		FeesPaid:            sdkmath.ZeroInt(),
		BlockHeight:         sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:           headerInfo.Time,
	}); err != nil {
		return nil, errors.Wrap(err, "unable to emit withdrawal event")
	}

	return &vaultsv2.MsgClaimWithdrawalResponse{
		AmountClaimed:   withdrawAmount,
		PrincipalAmount: withdrawAmount,
		YieldAmount:     sdkmath.ZeroInt(),
		FeesDeducted:    sdkmath.ZeroInt(),
	}, nil
}

func (m msgServerV2) UpdateNAV(ctx context.Context, msg *vaultsv2.MsgUpdateNAV) (*vaultsv2.MsgUpdateNAVResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	navInfo, err := m.GetVaultsV2NAVInfo(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch nav info")
	}

	previousNav := navInfo.CurrentNav

	if msg.PreviousNav.IsPositive() && !previousNav.Equal(msg.PreviousNav) {
		return nil, errors.Wrap(vaultsv2.ErrInvalidAmount, "previous nav mismatch")
	}

	changeBps := msg.ChangeBps
	if changeBps == 0 && previousNav.IsPositive() {
		previousDec := previousNav.ToLegacyDec()
		if !previousDec.IsZero() {
			delta := msg.NewNav.ToLegacyDec().Sub(previousDec)
			changeDec := delta.MulInt(sdkmath.NewInt(navBasisPointsMultiplier)).Quo(previousDec)
			changeBps = int32(changeDec.TruncateInt64())
		}
	}

	navInfo.PreviousNav = previousNav
	navInfo.CurrentNav = msg.NewNav
	navInfo.LastUpdate = headerInfo.Time
	navInfo.ChangeBps = changeBps
	navInfo.CircuitBreakerActive = msg.CircuitBreakerActive

	if err := m.SetVaultsV2NAVInfo(ctx, navInfo); err != nil {
		return nil, errors.Wrap(err, "unable to persist nav info")
	}

	state, err := m.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	state.TotalNav = msg.NewNav
	state.LastNavUpdate = headerInfo.Time
	if err := m.SetVaultsV2VaultState(ctx, state); err != nil {
		return nil, errors.Wrap(err, "unable to persist vault state")
	}

	if err := m.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventNAVUpdated{
		PreviousNav:       previousNav,
		NewNav:            msg.NewNav,
		ChangeBps:         changeBps,
		TotalDeposits:     state.TotalDeposits,
		TotalAccruedYield: state.TotalAccruedYield,
		Authority:         msg.Authority,
		Reason:            "",
		BlockHeight:       sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Timestamp:         headerInfo.Time,
	}); err != nil {
		return nil, errors.Wrap(err, "unable to emit nav updated event")
	}

	return &vaultsv2.MsgUpdateNAVResponse{
		AppliedNav:           msg.NewNav,
		ChangeBps:            changeBps,
		Timestamp:            headerInfo.Time,
		CircuitBreakerActive: msg.CircuitBreakerActive,
	}, nil
}

func (m msgServerV2) HandleStaleInflight(ctx context.Context, msg *vaultsv2.MsgHandleStaleInflight) (*vaultsv2.MsgHandleStaleInflightResponse, error) {
	if msg == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "message cannot be nil")
	}
	if msg.Authority != m.authority {
		return nil, errors.Wrapf(vaultsv2.ErrInvalidAuthority, "expected %s, got %s", m.authority, msg.Authority)
	}
	if msg.InflightId == "" {
		return nil, errors.Wrap(types.ErrInvalidRequest, "inflight id must be provided")
	}

	nonce, err := strconv.ParseUint(msg.InflightId, 10, 64)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid inflight id: %s", msg.InflightId)
	}

	fund, found, err := m.GetVaultsV2InflightFund(ctx, msg.InflightId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch inflight fund")
	}
	if !found {
		return nil, errors.Wrapf(vaultsv2.ErrInflightNotFound, "inflight fund %s not found", msg.InflightId)
	}

	headerInfo := m.header.GetHeaderInfo(ctx)

	returned, err := m.ProcessInFlightPosition(ctx, &vaultsv2.MsgProcessInFlightPosition{
		Authority:        msg.Authority,
		Nonce:            nonce,
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
