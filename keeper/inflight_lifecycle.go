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
	"time"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

// DetectStaleInflightFunds identifies inflight funds that have exceeded their expected completion time.
// Returns a list of stale fund IDs and their details.
func (k *Keeper) DetectStaleInflightFunds(ctx context.Context, staleThresholdHours int64) ([]StaleInflightFund, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime()

	var staleFunds []StaleInflightFund

	err := k.IterateVaultsV2InflightFunds(ctx, func(txID string, fund vaultsv2.InflightFund) (bool, error) {
		// Calculate how long past the expected time
		if currentTime.After(fund.ExpectedAt) {
			hoursOverdue := int64(currentTime.Sub(fund.ExpectedAt).Hours())

			// Only include if past the threshold
			if hoursOverdue >= staleThresholdHours {
				// Extract route ID
				routeID := uint32(0)
				if tracking := fund.GetProviderTracking(); tracking != nil {
					if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
						routeID = hyperlane.DestinationDomain
					}
				}

				staleFunds = append(staleFunds, StaleInflightFund{
					TransactionID: txID,
					RouteID:       routeID,
					Fund:          fund,
					HoursOverdue:  hoursOverdue,
				})
			}
		}

		return false, nil
	})

	return staleFunds, err
}

// CleanupStaleInflightFund removes a stale inflight fund and returns its value to the vault.
// This should be called by governance/authority after manual verification.
func (k *Keeper) CleanupStaleInflightFund(ctx context.Context, txID string, reason string, authority string) error {
	// Get the fund
	fund, found, err := k.GetVaultsV2InflightFund(ctx, txID)
	if err != nil {
		return errors.Wrap(err, "unable to fetch inflight fund")
	}
	if !found {
		return errors.Wrapf(vaultsv2.ErrInflightNotFound, "inflight fund %s not found", txID)
	}

	// Extract route ID for tracking
	routeID := uint32(0)
	if tracking := fund.GetProviderTracking(); tracking != nil {
		if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
			routeID = hyperlane.DestinationDomain
		}
	}

	// Subtract from route's inflight value
	if routeID != 0 {
		currentInflight, err := k.GetVaultsV2InflightValueByRoute(ctx, routeID)
		if err == nil {
			newInflight := currentInflight.Sub(fund.Amount)
			if newInflight.IsNegative() {
				newInflight = sdkmath.ZeroInt()
			}
			if err := k.SubtractVaultsV2InflightValueByRoute(ctx, routeID, fund.Amount); err != nil {
				return errors.Wrap(err, "unable to update route inflight value")
			}
		}
	}

	// Return funds to vault - update pending deployment if it was an outbound operation
	if origin := fund.GetNobleOrigin(); origin != nil {
		if origin.OperationType == vaultsv2.OPERATION_TYPE_DEPOSIT ||
			origin.OperationType == vaultsv2.OPERATION_TYPE_REBALANCE {
			// Subtract from pending deployment
			if err := k.SubtractVaultsV2PendingDeploymentFunds(ctx, fund.Amount); err != nil {
				return errors.Wrap(err, "unable to update pending deployment")
			}
		} else if origin.OperationType == vaultsv2.OPERATION_TYPE_WITHDRAWAL {
			// Subtract from pending withdrawal distribution
			if err := k.SubtractVaultsV2PendingWithdrawalDistribution(ctx, fund.Amount); err != nil {
				return errors.Wrap(err, "unable to update pending withdrawal distribution")
			}
		}
	}

	// Delete the inflight fund
	if err := k.DeleteVaultsV2InflightFund(ctx, txID); err != nil {
		return errors.Wrap(err, "unable to delete inflight fund")
	}

	// Emit cleanup event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventInflightFundCleaned{
		TransactionId:  txID,
		RouteId:        routeID,
		AmountReturned: fund.Amount,
		Reason:         reason,
		Authority:      authority,
		BlockHeight:    sdkCtx.BlockHeight(),
		Timestamp:      sdkCtx.BlockTime(),
	}); err != nil {
		return errors.Wrap(err, "unable to emit cleanup event")
	}

	return nil
}

// EmitInflightStatusChangeEvent emits an event when an inflight fund's status changes.
func (k *Keeper) EmitInflightStatusChangeEvent(ctx context.Context, txID string, routeID uint32, prevStatus, newStatus vaultsv2.InflightStatus, amount sdkmath.Int, reason string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	return sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventInflightFundStatusChanged{
		TransactionId:  txID,
		RouteId:        routeID,
		PreviousStatus: prevStatus.String(),
		NewStatus:      newStatus.String(),
		Amount:         amount,
		Reason:         reason,
		BlockHeight:    sdkCtx.BlockHeight(),
		Timestamp:      sdkCtx.BlockTime(),
	})
}

// EmitInflightCreatedEvent emits an event when a new inflight fund is created.
func (k *Keeper) EmitInflightCreatedEvent(ctx context.Context, txID string, routeID uint32, opType vaultsv2.OperationType, amount sdkmath.Int, initiator, sourceChain, destChain string, expectedAt time.Time) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	return sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventInflightFundCreated{
		TransactionId:     txID,
		RouteId:           routeID,
		OperationType:     opType.String(),
		Amount:            amount,
		ValueAtInitiation: amount,
		Initiator:         initiator,
		SourceChain:       sourceChain,
		DestinationChain:  destChain,
		ExpectedAt:        expectedAt,
		BlockHeight:       sdkCtx.BlockHeight(),
		Timestamp:         sdkCtx.BlockTime(),
	})
}

// EmitInflightCompletedEvent emits an event when an inflight fund completes.
func (k *Keeper) EmitInflightCompletedEvent(ctx context.Context, txID string, routeID uint32, opType vaultsv2.OperationType, initialAmount, finalAmount sdkmath.Int, initiatedAt time.Time) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime()

	durationSeconds := int64(currentTime.Sub(initiatedAt).Seconds())

	return sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventInflightFundCompleted{
		TransactionId:   txID,
		RouteId:         routeID,
		OperationType:   opType.String(),
		FinalAmount:     finalAmount,
		InitialAmount:   initialAmount,
		DurationSeconds: durationSeconds,
		BlockHeight:     sdkCtx.BlockHeight(),
		Timestamp:       currentTime,
	})
}

// EmitStaleInflightDetectedEvent emits an event when a stale inflight fund is detected.
func (k *Keeper) EmitStaleInflightDetectedEvent(ctx context.Context, txID string, routeID uint32, amount sdkmath.Int, hoursOverdue int64, expectedAt time.Time, status vaultsv2.InflightStatus) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	return sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventInflightFundStale{
		TransactionId:     txID,
		RouteId:           routeID,
		Amount:            amount,
		HoursOverdue:      hoursOverdue,
		ExpectedAt:        expectedAt,
		CurrentStatus:     status.String(),
		RecommendedAction: "Manual intervention required - verify cross-chain state",
		BlockHeight:       sdkCtx.BlockHeight(),
		Timestamp:         sdkCtx.BlockTime(),
	})
}

// EnforceRouteCapacity checks if a route has capacity for an operation.
// Returns error if the operation would exceed the route's max_inflight_value.
func (k *Keeper) EnforceRouteCapacity(ctx context.Context, routeID uint32, additionalAmount sdkmath.Int) error {
	// Get route configuration
	route, found, err := k.GetVaultsV2CrossChainRoute(ctx, routeID)
	if err != nil {
		return errors.Wrap(err, "unable to fetch route")
	}
	if !found {
		return errors.Wrapf(vaultsv2.ErrRouteNotFound, "route %d not found", routeID)
	}

	// If no limit set, allow
	if !route.MaxInflightValue.IsPositive() {
		return nil
	}

	// Get current inflight value for this route
	currentInflight, err := k.GetVaultsV2InflightValueByRoute(ctx, routeID)
	if err != nil {
		currentInflight = sdkmath.ZeroInt()
	}

	// Check if adding this amount would exceed capacity
	newInflight := currentInflight.Add(additionalAmount)
	if newInflight.GT(route.MaxInflightValue) {
		// Emit capacity exceeded event
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		_ = sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventRouteCapacityExceeded{
			RouteId:              routeID,
			CurrentInflightValue: currentInflight,
			CapacityLimit:        route.MaxInflightValue,
			AttemptedAmount:      additionalAmount,
			BlockHeight:          sdkCtx.BlockHeight(),
			Timestamp:            sdkCtx.BlockTime(),
		})

		return errors.Wrapf(vaultsv2.ErrRouteCapacityExceeded,
			"route %d capacity exceeded: current=%s, limit=%s, attempted=%s",
			routeID, currentInflight.String(), route.MaxInflightValue.String(), additionalAmount.String())
	}

	return nil
}

// StaleInflightFund represents a detected stale inflight fund.
type StaleInflightFund struct {
	TransactionID string
	RouteID       uint32
	Fund          vaultsv2.InflightFund
	HoursOverdue  int64
}

// AutoDetectAndEmitStaleAlerts checks for stale inflight funds and emits alert events.
// This can be called periodically (e.g., in BeginBlock or EndBlock).
// staleThresholdHours: minimum hours overdue before considering stale
func (k *Keeper) AutoDetectAndEmitStaleAlerts(ctx context.Context, staleThresholdHours int64) (int, error) {
	staleFunds, err := k.DetectStaleInflightFunds(ctx, staleThresholdHours)
	if err != nil {
		return 0, err
	}

	// Emit alert for each stale fund
	for _, stale := range staleFunds {
		if err := k.EmitStaleInflightDetectedEvent(
			ctx,
			stale.TransactionID,
			stale.RouteID,
			stale.Fund.Amount,
			stale.HoursOverdue,
			stale.Fund.ExpectedAt,
			stale.Fund.Status,
		); err != nil {
			// Log but don't fail - continue processing other alerts
			continue
		}
	}

	return len(staleFunds), nil
}
