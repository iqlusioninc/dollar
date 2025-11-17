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

// EmitInflightStatusChangeEvent emits an event when an inflight fund's status changes.
func (k *Keeper) EmitInflightStatusChangeEvent(ctx context.Context, inflightID uint64, txID string, routeID uint32, prevStatus, newStatus vaultsv2.InflightStatus, amount sdkmath.Int, reason string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	return sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventInflightFundStatusChanged{
		InflightId:     inflightID,
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
func (k *Keeper) EmitInflightCompletedEvent(ctx context.Context, inflightID uint64, txID string, routeID uint32, opType vaultsv2.OperationType, initialAmount, finalAmount sdkmath.Int, initiatedAt time.Time) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime()

	durationSeconds := int64(currentTime.Sub(initiatedAt).Seconds())

	return sdkCtx.EventManager().EmitTypedEvent(&vaultsv2.EventInflightFundCompleted{
		InflightId:      inflightID,
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
