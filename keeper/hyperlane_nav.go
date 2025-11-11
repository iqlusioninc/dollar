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
	stderrors "errors"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	hyperlaneutil "github.com/bcp-innovations/hyperlane-cosmos/util"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

// HandleHyperlaneAUMMessage processes a Hyperlane message containing AUM data
// for a remote position oracle. The function performs basic authentication by
// checking the message origin and sender against the enrolled oracle record
// before applying the AUM update and recalculating the aggregate vault AUM.
func (k *Keeper) HandleHyperlaneAUMMessage(ctx context.Context, mailboxID hyperlaneutil.HexAddress, message hyperlaneutil.HyperlaneMessage) (*vaultsv2.OracleUpdateResult, error) {
	if mailboxID.IsZeroAddress() {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "mailbox identifier must be provided")
	}

	payload, err := vaultsv2.ParseAUMPayload(message.Body)
	if err != nil {
		return nil, errors.Wrap(err, "unable to parse AUM payload")
	}

	oracle, found, err := k.GetVaultsV2RemotePositionOracle(ctx, payload.PositionID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote position oracle")
	}
	if !found {
		return nil, errors.Wrapf(vaultsv2.ErrRemotePositionNotFound, "position %d", payload.PositionID)
	}

	if message.Origin != oracle.ChainId {
		return nil, errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "unexpected message origin %d (expected %d)", message.Origin, oracle.ChainId)
	}

	if !message.Sender.Equal(oracle.OracleAddress) {
		return nil, errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "unexpected oracle sender %s (expected %s)", message.Sender.String(), oracle.OracleAddress.String())
	}

	if !oracle.LastUpdate.IsZero() && payload.Timestamp.Before(oracle.LastUpdate) {
		return nil, errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "stale update for position %d", payload.PositionID)
	}

	oracle.SharePrice = payload.SharePrice
	oracle.SharesHeld = payload.SharesHeld
	oracle.LastUpdate = payload.Timestamp

	if err := k.SetVaultsV2RemotePositionOracle(ctx, payload.PositionID, oracle); err != nil {
		return nil, errors.Wrap(err, "unable to persist remote position oracle")
	}

	position, found, err := k.GetVaultsV2RemotePosition(ctx, payload.PositionID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote position")
	}
	if found {
		position.SharePrice = payload.SharePrice
		position.SharesHeld = payload.SharesHeld
		position.TotalValue = payload.SharePrice.MulInt(payload.SharesHeld).TruncateInt()
		position.LastUpdate = payload.Timestamp
		if position.TotalValue.IsPositive() {
			position.Status = vaultsv2.REMOTE_POSITION_ACTIVE
		} else {
			position.Status = vaultsv2.REMOTE_POSITION_CLOSED
		}

		if err := k.SetVaultsV2RemotePosition(ctx, payload.PositionID, position); err != nil {
			return nil, errors.Wrap(err, "unable to persist remote position")
		}
	}

	updatedAum, err := k.RecalculateVaultsV2AUM(ctx, payload.Timestamp, payload.PositionID)
	if err != nil {
		return nil, err
	}

	positionValue := payload.SharePrice.MulInt(payload.SharesHeld).TruncateInt()

	return &vaultsv2.OracleUpdateResult{
		PositionId:    payload.PositionID,
		NewSharePrice: payload.SharePrice,
		NewShares:     payload.SharesHeld,
		PositionValue: positionValue,
		UpdatedAum:    updatedAum,
	}, nil
}

// RecalculateVaultsV2AUM recalculates the vault AUM from all components:
// local (pending deployment or withdrawal) funds, remote positions, and inflight funds.
// This is called when oracle updates remote position values.
func (k *Keeper) RecalculateVaultsV2AUM(ctx context.Context, timestamp time.Time, triggeringPositionID uint64) (math.Int, error) {
	// Check if accounting is currently in progress
	k.checkAccountingNotInProgress(ctx)

	// Calculate AUM according to spec: Local Assets + Remote Positions + Inflight Funds
	total := math.ZeroInt()

	// 1. Get local vault assets (liquidity available for deployment or withdrawals)
	localFunds, err := k.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to get local funds")
	}
	total, err = total.SafeAdd(localFunds)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to add local funds to AUM")
	}

	// 2. Add remote position values
	err = k.IterateVaultsV2RemotePositions(ctx, func(_ uint64, position vaultsv2.RemotePosition) (bool, error) {
		var err error
		total, err = total.SafeAdd(position.TotalValue)
		if err != nil {
			return true, errors.Wrap(err, "unable to accumulate remote position value")
		}

		return false, nil
	})
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to iterate remote position oracles")
	}

	// 3. Add inflight funds
	err = k.IterateVaultsV2InflightFunds(ctx, func(_ uint64, fund vaultsv2.InflightFund) (bool, error) {
		if fund.Status == vaultsv2.INFLIGHT_PENDING {
			var err error
			total, err = total.SafeAdd(fund.Amount)
			if err != nil {
				return true, errors.Wrap(err, "unable to accumulate inflight funds")
			}
		}
		return false, nil
	})
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to iterate inflight funds")
	}

	aumInfo, err := k.GetVaultsV2AUMInfo(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch AUM info")
	}

	// Initialize AUM values if they are nil (first time setup)
	if aumInfo.CurrentAum.IsNil() {
		aumInfo.CurrentAum = math.ZeroInt()
	}
	if aumInfo.PreviousAum.IsNil() {
		aumInfo.PreviousAum = math.ZeroInt()
	}

	previousAum := aumInfo.CurrentAum

	// Calculate change basis points
	changeBps := int32(0)
	if previousAum.IsPositive() {
		previousDec := previousAum.ToLegacyDec()
		if !previousDec.IsZero() {
			delta := total.ToLegacyDec().Sub(previousDec)
			changeDec := delta.MulInt(math.NewInt(basisPointsMultiplier)).QuoInt(previousAum)
			changeBps = int32(changeDec.TruncateInt64())
		}
	}

	// Circuit breaker: Check if AUM change exceeds maximum allowed threshold
	// TODO: Revisit circuit breaker behavior for oracle updates
	params, err := k.GetVaultsV2Params(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch vault parameters")
	}

	if params.MaxAumChangeBps > 0 && previousAum.IsPositive() {
		// Calculate absolute value of change
		absChangeBps := changeBps
		if absChangeBps < 0 {
			absChangeBps = -absChangeBps
		}

		if absChangeBps > params.MaxAumChangeBps {
			// Record the circuit breaker trip
			trip := vaultsv2.CircuitBreakerTrip{
				ChangeBps:        changeBps,
				RemotePositionId: triggeringPositionID,
				TriggeredAt:      timestamp,
				PreviousAum:      previousAum,
				AttemptedAum:     total,
			}

			// Get next trip ID
			nextID, err := k.VaultsV2CircuitBreakerNextID.Get(ctx)
			if err != nil {
				if !stderrors.Is(err, collections.ErrNotFound) {
					return math.ZeroInt(), errors.Wrap(err, "unable to get circuit breaker next ID")
				}
				// First trip, start at 0
				nextID = 0
			}

			// Store the trip
			if err := k.VaultsV2CircuitBreakerTrips.Set(ctx, nextID, trip); err != nil {
				return math.ZeroInt(), errors.Wrap(err, "unable to record circuit breaker trip")
			}

			// Increment next ID
			if err := k.VaultsV2CircuitBreakerNextID.Set(ctx, nextID+1); err != nil {
				return math.ZeroInt(), errors.Wrap(err, "unable to update circuit breaker next ID")
			}

			// Activate circuit breaker
			if err := k.VaultsV2CircuitBreakerActive.Set(ctx, true); err != nil {
				return math.ZeroInt(), errors.Wrap(err, "unable to activate circuit breaker")
			}

			return math.ZeroInt(), errors.Wrapf(vaultsv2.ErrOperationNotPermitted,
				"AUM change of %d bps exceeds maximum allowed %d bps - circuit breaker activated",
				absChangeBps, params.MaxAumChangeBps)
		}
	}

	aumInfo.PreviousAum = previousAum
	aumInfo.CurrentAum = total
	aumInfo.LastUpdate = timestamp

	if err := k.SetVaultsV2AUMInfo(ctx, aumInfo); err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to persist AUM info")
	}

	// Record AUM snapshot for TWAP if conditions are met
	shouldRecord, err := k.shouldRecordAUMSnapshot(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to check snapshot recording conditions")
	}

	if shouldRecord {
		snapshot := vaultsv2.AUMSnapshot{
			Aum:         total,
			BlockHeight: k.header.GetHeaderInfo(ctx).Height,
			TotalShares: math.ZeroInt(), // Shares no longer used - kept for backwards compatibility
		}

		if err := k.AddVaultsV2AUMSnapshot(ctx, snapshot); err != nil {
			return math.ZeroInt(), errors.Wrap(err, "unable to record AUM snapshot")
		}

		// Optionally prune old snapshots if max age is configured
		if params.TwapConfig.MaxSnapshotAge > 0 {
			// Convert MaxSnapshotAge from seconds to blocks (assume ~6 seconds per block)
			maxAgeInBlocks := params.TwapConfig.MaxSnapshotAge / 6
			currentHeight := k.header.GetHeaderInfo(ctx).Height
			_, _ = k.PruneOldVaultsV2AUMSnapshots(ctx, maxAgeInBlocks, currentHeight)
		}
	}

	// Emit AUM updated event
	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch vault state")
	}

	// TODO: Add reason field for oracle updates (e.g., position ID)
	if err := k.event.EventManager(ctx).Emit(ctx, &vaultsv2.EventAUMUpdated{
		PreviousAum:       previousAum,
		NewAum:            total,
		ChangeBps:         changeBps,
		TotalDeposits:     state.TotalDeposits,
		TotalAccruedYield: state.TotalAccruedYield,
		Reason:            "",
		BlockHeight:       k.header.GetHeaderInfo(ctx).Height,
		Timestamp:         timestamp,
	}); err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to emit aum updated event")
	}

	return total, nil
}

// shouldRecordAUMSnapshot determines if a new AUM snapshot should be recorded for TWAP calculations.
func (k *Keeper) shouldRecordAUMSnapshot(ctx context.Context) (bool, error) {
	params, err := k.GetVaultsV2Params(ctx)
	if err != nil {
		return false, errors.Wrap(err, "unable to fetch params")
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
	snapshots, err := k.GetRecentVaultsV2AUMSnapshots(ctx, 1)
	if err != nil {
		return false, errors.Wrap(err, "unable to fetch recent snapshots")
	}

	if len(snapshots) == 0 {
		return true, nil // No previous snapshot, record the first one
	}

	currentHeight := k.header.GetHeaderInfo(ctx).Height
	blocksSinceLastSnapshot := currentHeight - snapshots[0].BlockHeight
	// Convert MinSnapshotInterval from seconds to blocks (assume ~6 seconds per block)
	minIntervalInBlocks := params.TwapConfig.MinSnapshotInterval / 6

	return blocksSinceLastSnapshot >= minIntervalInBlocks, nil
}
