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
	"strconv"
	"time"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"dollar.noble.xyz/v3/types"
	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

var _ vaultsv2.MsgServer = &msgServerV2{}

type msgServerV2 struct {
	vaultsv2.UnimplementedMsgServer
	*Keeper
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
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "set yield preference")
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
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "update vault config")
}

func (m msgServerV2) UpdateParams(ctx context.Context, msg *vaultsv2.MsgUpdateParams) (*vaultsv2.MsgUpdateParamsResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "update params")
}

func (m msgServerV2) CreateCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgCreateCrossChainRoute) (*vaultsv2.MsgCreateCrossChainRouteResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "create cross-chain route")
}

func (m msgServerV2) UpdateCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgUpdateCrossChainRoute) (*vaultsv2.MsgUpdateCrossChainRouteResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "update cross-chain route")
}

func (m msgServerV2) DisableCrossChainRoute(ctx context.Context, msg *vaultsv2.MsgDisableCrossChainRoute) (*vaultsv2.MsgDisableCrossChainRouteResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "disable cross-chain route")
}

func (m msgServerV2) CreateRemotePosition(ctx context.Context, msg *vaultsv2.MsgCreateRemotePosition) (*vaultsv2.MsgCreateRemotePositionResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "create remote position")
}

func (m msgServerV2) CloseRemotePosition(ctx context.Context, msg *vaultsv2.MsgCloseRemotePosition) (*vaultsv2.MsgCloseRemotePositionResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "close remote position")
}

func (m msgServerV2) Rebalance(ctx context.Context, msg *vaultsv2.MsgRebalance) (*vaultsv2.MsgRebalanceResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "rebalance")
}

func (m msgServerV2) RemoteDeposit(ctx context.Context, msg *vaultsv2.MsgRemoteDeposit) (*vaultsv2.MsgRemoteDepositResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "remote deposit")
}

func (m msgServerV2) RemoteWithdraw(ctx context.Context, msg *vaultsv2.MsgRemoteWithdraw) (*vaultsv2.MsgRemoteWithdrawResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "remote withdraw")
}

func (m msgServerV2) ProcessInFlightPosition(ctx context.Context, msg *vaultsv2.MsgProcessInFlightPosition) (*vaultsv2.MsgProcessInFlightPositionResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "process inflight position")
}

func (m msgServerV2) RegisterOracle(ctx context.Context, msg *vaultsv2.MsgRegisterOracle) (*vaultsv2.MsgRegisterOracleResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "register oracle")
}

func (m msgServerV2) UpdateOracleConfig(ctx context.Context, msg *vaultsv2.MsgUpdateOracleConfig) (*vaultsv2.MsgUpdateOracleConfigResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "update oracle config")
}

func (m msgServerV2) RemoveOracle(ctx context.Context, msg *vaultsv2.MsgRemoveOracle) (*vaultsv2.MsgRemoveOracleResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "remove oracle")
}

func (m msgServerV2) UpdateOracleParams(ctx context.Context, msg *vaultsv2.MsgUpdateOracleParams) (*vaultsv2.MsgUpdateOracleParamsResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "update oracle params")
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
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "update NAV")
}

func (m msgServerV2) HandleStaleInflight(ctx context.Context, msg *vaultsv2.MsgHandleStaleInflight) (*vaultsv2.MsgHandleStaleInflightResponse, error) {
	return nil, errors.Wrap(vaultsv2.ErrUnimplemented, "handle stale inflight")
}
