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

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

// AccountingResult contains the results of a cursor-based accounting operation.
type AccountingResult struct {
	PositionsProcessed      uint64
	TotalPositionsProcessed uint64
	TotalPositions          uint64
	Complete                bool
	NextUser                string
	AppliedNav              sdkmath.Int
	YieldDistributed        sdkmath.Int
}

// updateVaultsV2AccountingWithCursor performs yield accounting with cursor-based pagination.
// This allows the accounting to be split across multiple message invocations.
// All updates are written to snapshots and only committed atomically when accounting completes.
func (k *Keeper) updateVaultsV2AccountingWithCursor(ctx context.Context, maxPositions uint32) (*AccountingResult, error) {
	navInfo, err := k.GetVaultsV2NAVInfo(ctx)
	if err != nil {
		return nil, err
	}

	if navInfo.CurrentNav.IsNil() {
		return nil, fmt.Errorf("current NAV is not set")
	}

	// Get vault state to access total positions
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, err
	}

	cursor, err := k.GetVaultsV2AccountingCursor(ctx)
	if err != nil {
		return nil, err
	}

	// Determine if we need to start a new accounting session
	needsInit := !cursor.InProgress ||
		cursor.AccountingNav.IsNil() ||
		!cursor.AccountingNav.Equal(navInfo.CurrentNav) ||
		!cursor.AccountingNavTimestamp.Equal(navInfo.LastUpdate)

	if needsInit {
		// If accounting is in progress with a different NAV, we have a problem
		if cursor.InProgress {
			return nil, fmt.Errorf(
				"accounting already in progress for NAV %s (started at %s, %d/%d positions processed). "+
					"Cannot start new accounting for NAV %s until current session completes. "+
					"Continue calling this message to complete the current session",
				cursor.AccountingNav.String(),
				cursor.StartedAt.String(),
				cursor.PositionsProcessed,
				cursor.TotalPositions,
				navInfo.CurrentNav.String(),
			)
		}

		// Clear any old snapshots from previous incomplete sessions
		if err := k.ClearAllVaultsV2AccountingSnapshots(ctx); err != nil {
			return nil, fmt.Errorf("failed to clear old snapshots: %w", err)
		}

		// Initialize new accounting session
		// Note: We don't count total positions upfront to avoid expensive iteration.
		// Instead, we detect completion when the iterator returns 0 positions.
		cursor = vaultsv2.AccountingCursor{
			LastProcessedUser:      "",
			AccountingNav:          navInfo.CurrentNav,
			AccountingNavTimestamp: navInfo.LastUpdate,
			PositionsProcessed:     0,
			TotalPositions:         0, // Not known upfront, updated as we go
			InProgress:             true,
			StartedAt:              k.header.GetHeaderInfo(ctx).Time,
			AccumulatedResidual:    sdkmath.ZeroInt(),
		}

		// Save initial cursor
		if err := k.SetVaultsV2AccountingCursor(ctx, cursor); err != nil {
			return nil, err
		}
	}

	// When there are no active positions, handle specially
	if vaultState.TotalPositions == 0 {
		return k.accountingWithZeroPositions(ctx, navInfo, cursor, maxPositions)
	}

	// Perform cursor-based accounting
	return k.accountingWithCursor(ctx, navInfo, vaultState, cursor, maxPositions)
}

// accountingWithZeroPositions handles the case where there are no active positions.
// Writes zero yield to snapshots for all users.
func (k *Keeper) accountingWithZeroPositions(
	ctx context.Context,
	navInfo vaultsv2.NAVInfo,
	cursor vaultsv2.AccountingCursor,
	maxPositions uint32,
) (*AccountingResult, error) {
	// Use paginated iterator
	lastProcessed, count, err := k.IterateVaultsV2UserPositionsPaginated(
		ctx,
		cursor.LastProcessedUser,
		maxPositions,
		func(address types.AccAddress, position vaultsv2.UserPosition) error {
			// Create snapshot with zero yield
			snapshot := vaultsv2.AccountingSnapshot{
				User:           address.String(),
				DepositAmount:  position.AmountPendingWithdrawal,
				AccruedYield:   sdkmath.ZeroInt(),
				AccountingNav:  navInfo.CurrentNav,
				CreatedAt:      k.header.GetHeaderInfo(ctx).Time,
			}

			return k.SetVaultsV2AccountingSnapshot(ctx, snapshot)
		},
	)
	if err != nil {
		return nil, err
	}

	// Update cursor
	cursor.PositionsProcessed += uint64(count)
	cursor.LastProcessedUser = lastProcessed

	// Accounting is complete when we process a batch and get 0 positions back
	// (meaning there are no more positions to process)
	complete := count == 0

	// Update total positions to reflect actual count
	if complete {
		cursor.TotalPositions = cursor.PositionsProcessed
	}

	if complete {
		// Commit all snapshots atomically
		if err := k.CommitVaultsV2AccountingSnapshots(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit snapshots: %w", err)
		}

		// Update vault state
		state, err := k.GetVaultsV2VaultState(ctx)
		if err != nil {
			return nil, err
		}

		state.TotalNav = navInfo.CurrentNav
		if !state.TotalDeposits.IsNil() {
			if state.TotalAccruedYield, err = navInfo.CurrentNav.SafeSub(state.TotalDeposits); err != nil {
				return nil, err
			}
		} else {
			state.TotalAccruedYield = sdkmath.ZeroInt()
		}

		if !navInfo.LastUpdate.IsZero() {
			state.LastNavUpdate = navInfo.LastUpdate
		}

		if err := k.SetVaultsV2VaultState(ctx, state); err != nil {
			return nil, err
		}

		// Clear cursor
		if err := k.ClearVaultsV2AccountingCursor(ctx); err != nil {
			return nil, err
		}
	} else {
		// Save cursor for next iteration
		if err := k.SetVaultsV2AccountingCursor(ctx, cursor); err != nil {
			return nil, err
		}
	}

	return &AccountingResult{
		PositionsProcessed:      uint64(count),
		TotalPositionsProcessed: cursor.PositionsProcessed,
		TotalPositions:          cursor.TotalPositions,
		Complete:                complete,
		NextUser:                lastProcessed,
		AppliedNav:              navInfo.CurrentNav,
		YieldDistributed:        sdkmath.ZeroInt(),
	}, nil
}

// accountingWithCursor performs the main accounting logic with cursor pagination.
// Writes all updates to snapshots for atomic commit when complete.
func (k *Keeper) accountingWithCursor(
	ctx context.Context,
	navInfo vaultsv2.NAVInfo,
	vaultState vaultsv2.VaultState,
	cursor vaultsv2.AccountingCursor,
	maxPositions uint32,
) (*AccountingResult, error) {
	// For position-based accounting, we don't need complex NAV calculations
	// Each position tracks its own yield directly
	
	yieldThisBatch := sdkmath.ZeroInt()
	headerInfo := k.header.GetHeaderInfo(ctx)

	// Use paginated iterator to process positions directly
	lastProcessed, count, err := k.IterateVaultsV2UserPositionsPaginated(
		ctx,
		cursor.LastProcessedUser,
		maxPositions,
		func(address types.AccAddress, position vaultsv2.UserPosition) error {
			// In the position-based system, yield calculation is simpler
			// Each position accumulates yield based on its receive_yield preference
			
			// Calculate new yield for this position (simplified logic)
			newYield := sdkmath.ZeroInt()
			if position.ReceiveYield && navInfo.CurrentNav.IsPositive() {
				// Simple yield calculation: could be enhanced with time-based logic
				// For now, use a basic proportional approach
				if vaultState.TotalDeposits.IsPositive() {
					yieldRate := sdkmath.LegacyNewDecFromInt(navInfo.CurrentNav).
						Quo(sdkmath.LegacyNewDecFromInt(vaultState.TotalDeposits)).
						Sub(sdkmath.LegacyOneDec())
					
					if yieldRate.IsPositive() {
						newYield = yieldRate.MulInt(position.DepositAmount).TruncateInt()
						var addErr error
						yieldThisBatch, addErr = yieldThisBatch.SafeAdd(newYield)
						if addErr != nil {
							return addErr
						}
					}
				}
			}

			// Write to snapshot instead of directly to position
			snapshot := vaultsv2.AccountingSnapshot{
				User:          address.String(),
				PositionId:    position.PositionId,
				DepositAmount: position.DepositAmount,
				AccruedYield:  position.AccruedYield.Add(newYield),
				AccountingNav: navInfo.CurrentNav,
				CreatedAt:     headerInfo.Time,
			}

			return k.SetVaultsV2AccountingSnapshot(ctx, snapshot)
		},
	)
	if err != nil {
		return nil, err
	}

	// Update cursor
	cursor.PositionsProcessed += uint64(count)
	cursor.LastProcessedUser = lastProcessed
	cursor.AccumulatedResidual = sdkmath.ZeroInt() // No longer need residual tracking

	// Accounting is complete when we process a batch and get 0 positions back
	// (meaning there are no more positions to process)
	complete := count == 0

	// Update total positions to reflect actual count
	if complete {
		cursor.TotalPositions = cursor.PositionsProcessed
	}

	if complete {
		// Calculate aggregated totals from all snapshots
		aggregatedYield := sdkmath.ZeroInt()
		aggregatedDeposits := sdkmath.ZeroInt()

		err := k.IterateVaultsV2AccountingSnapshots(ctx, func(snapshot vaultsv2.AccountingSnapshot) (bool, error) {
			var err error
			aggregatedYield, err = aggregatedYield.SafeAdd(snapshot.AccruedYield)
			if err != nil {
				return true, err
			}

			aggregatedDeposits, err = aggregatedDeposits.SafeAdd(snapshot.DepositAmount)
			if err != nil {
				return true, err
			}

			return false, nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to aggregate snapshot totals: %w", err)
		}

		// Commit all snapshots atomically
		if err := k.CommitVaultsV2AccountingSnapshots(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit snapshots: %w", err)
		}

		// Update vault state
		state, err := k.GetVaultsV2VaultState(ctx)
		if err != nil {
			return nil, err
		}

		state.TotalAccruedYield = aggregatedYield
		state.TotalDeposits = aggregatedDeposits
		state.TotalNav = navInfo.CurrentNav
		if !navInfo.LastUpdate.IsZero() {
			state.LastNavUpdate = navInfo.LastUpdate
		}

		if err := k.SetVaultsV2VaultState(ctx, state); err != nil {
			return nil, err
		}

		// Clear cursor
		if err := k.ClearVaultsV2AccountingCursor(ctx); err != nil {
			return nil, err
		}
	} else {
		// Save cursor for next iteration
		if err := k.SetVaultsV2AccountingCursor(ctx, cursor); err != nil {
			return nil, err
		}
	}

	return &AccountingResult{
		PositionsProcessed:      uint64(count),
		TotalPositionsProcessed: cursor.PositionsProcessed,
		TotalPositions:          cursor.TotalPositions,
		Complete:                complete,
		NextUser:                lastProcessed,
		AppliedNav:              navInfo.CurrentNav,
		YieldDistributed:        yieldThisBatch,
	}, nil
}
