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

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

// RecalculateVaultsV2AUM recalculates the vault AUM from all components:
// local (pending deployment or withdrawal) funds, remote positions, and inflight funds.
func (k *Keeper) RecalculateVaultsV2AUM(ctx context.Context, timestamp time.Time) (math.Int, error) {
	// TODO (Collin): This isn't stopping if accounting is in progress
	k.checkAccountingNotInProgress(ctx)
	total := math.ZeroInt()

	localFunds, err := k.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to get local funds")
	}
	total, err = total.SafeAdd(localFunds)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to add local funds to AUM")
	}
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

	if aumInfo.CurrentAum.IsNil() {
		aumInfo.CurrentAum = math.ZeroInt()
	}
	if aumInfo.PreviousAum.IsNil() {
		aumInfo.PreviousAum = math.ZeroInt()
	}

	previousAum := aumInfo.CurrentAum

	changeBps := int32(0)
	if previousAum.IsPositive() {
		previousDec := previousAum.ToLegacyDec()
		if !previousDec.IsZero() {
			delta := total.ToLegacyDec().Sub(previousDec)
			changeDec := delta.MulInt(math.NewInt(basisPointsMultiplier)).QuoInt(previousAum)
			changeBps = int32(changeDec.TruncateInt64())
		}
	}

	aumInfo.PreviousAum = previousAum
	aumInfo.CurrentAum = total
	aumInfo.LastUpdate = timestamp

	if err := k.SetVaultsV2AUMInfo(ctx, aumInfo); err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to persist AUM info")
	}

	params, err := k.GetVaultsV2Params(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch vault parameters")
	}

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

		if params.TwapConfig.MaxSnapshotAge > 0 {
			maxAgeInBlocks := params.TwapConfig.MaxSnapshotAge / 6
			currentHeight := k.header.GetHeaderInfo(ctx).Height
			_, _ = k.PruneOldVaultsV2AUMSnapshots(ctx, maxAgeInBlocks, currentHeight)
		}
	}

	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch vault state")
	}

	state.LastAumUpdate = timestamp
	if err := k.SetVaultsV2VaultState(ctx, state); err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to persist vault state")
	}

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

// RecalculateVaultsV2AUMFromOracle recalculates AUM after an oracle update and applies
// circuit breaker logic to detect abnormal AUM changes that could indicate manipulation.
func (k *Keeper) RecalculateVaultsV2AUMFromOracle(ctx context.Context, timestamp time.Time, triggeringPositionID uint64) (math.Int, error) {
	aumInfo, err := k.GetVaultsV2AUMInfo(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch AUM info")
	}

	if aumInfo.CurrentAum.IsNil() {
		aumInfo.CurrentAum = math.ZeroInt()
	}
	if aumInfo.PreviousAum.IsNil() {
		aumInfo.PreviousAum = math.ZeroInt()
	}

	previousAum := aumInfo.CurrentAum

	newAum, err := k.RecalculateVaultsV2AUM(ctx, timestamp)
	if err != nil {
		return math.ZeroInt(), err
	}

	params, err := k.GetVaultsV2Params(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch vault parameters")
	}

	if params.MaxAumChangeBps > 0 && previousAum.IsPositive() {
		changeBps := int32(0)
		previousDec := previousAum.ToLegacyDec()
		if !previousDec.IsZero() {
			delta := newAum.ToLegacyDec().Sub(previousDec)
			changeDec := delta.MulInt(math.NewInt(basisPointsMultiplier)).QuoInt(previousAum)
			changeBps = int32(changeDec.TruncateInt64())
		}

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
				AttemptedAum:     newAum,
			}

			nextID, err := k.VaultsV2CircuitBreakerNextID.Get(ctx)
			if err != nil {
				if !stderrors.Is(err, collections.ErrNotFound) {
					return math.ZeroInt(), errors.Wrap(err, "unable to get circuit breaker next ID")
				}
				nextID = 0
			}

			if err := k.VaultsV2CircuitBreakerTrips.Set(ctx, nextID, trip); err != nil {
				return math.ZeroInt(), errors.Wrap(err, "unable to record circuit breaker trip")
			}

			if err := k.VaultsV2CircuitBreakerNextID.Set(ctx, nextID+1); err != nil {
				return math.ZeroInt(), errors.Wrap(err, "unable to update circuit breaker next ID")
			}

			if err := k.VaultsV2CircuitBreakerActive.Set(ctx, true); err != nil {
				return math.ZeroInt(), errors.Wrap(err, "unable to activate circuit breaker")
			}

			return math.ZeroInt(), errors.Wrapf(vaultsv2.ErrOperationNotPermitted,
				"AUM change of %d bps exceeds maximum allowed %d bps - circuit breaker activated",
				absChangeBps, params.MaxAumChangeBps)
		}
	}

	return newAum, nil
}

// shouldRecordAUMSnapshot determines if a new AUM snapshot should be recorded for TWAP calculations.
func (k *Keeper) shouldRecordAUMSnapshot(ctx context.Context) (bool, error) {
	params, err := k.GetVaultsV2Params(ctx)
	if err != nil {
		return false, errors.Wrap(err, "unable to fetch params")
	}

	if !params.TwapConfig.Enabled {
		return false, nil
	}

	if params.TwapConfig.MinSnapshotInterval <= 0 {
		return true, nil
	}

	snapshots, err := k.GetRecentVaultsV2AUMSnapshots(ctx, 1)
	if err != nil {
		return false, errors.Wrap(err, "unable to fetch recent snapshots")
	}

	if len(snapshots) == 0 {
		return true, nil // No previous snapshot, record the first one
	}

	currentHeight := k.header.GetHeaderInfo(ctx).Height
	blocksSinceLastSnapshot := currentHeight - snapshots[0].BlockHeight
	minIntervalInBlocks := params.TwapConfig.MinSnapshotInterval / 6

	return blocksSinceLastSnapshot >= minIntervalInBlocks, nil
}
