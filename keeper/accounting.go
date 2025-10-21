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
	"math/big"

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

	totalShares, err := k.GetVaultsV2TotalShares(ctx)
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

		// Count total positions
		totalPositions := uint64(0)
		if err := k.IterateVaultsV2UserPositions(ctx, func(address types.AccAddress, position vaultsv2.UserPosition) (bool, error) {
			totalPositions++
			return false, nil
		}); err != nil {
			return nil, err
		}

		// Initialize new accounting session
		cursor = vaultsv2.AccountingCursor{
			LastProcessedUser:      "",
			AccountingNav:          navInfo.CurrentNav,
			AccountingNavTimestamp: navInfo.LastUpdate,
			PositionsProcessed:     0,
			TotalPositions:         totalPositions,
			InProgress:             true,
			StartedAt:              k.header.GetHeaderInfo(ctx).Time,
			AccumulatedResidual:    sdkmath.ZeroInt(),
		}

		// Save initial cursor
		if err := k.SetVaultsV2AccountingCursor(ctx, cursor); err != nil {
			return nil, err
		}
	}

	// When there are no active shares, handle specially
	if totalShares.IsZero() {
		return k.accountingWithZeroShares(ctx, navInfo, cursor, maxPositions)
	}

	// Perform cursor-based accounting
	return k.accountingWithCursor(ctx, navInfo, totalShares, cursor, maxPositions)
}

// accountingWithZeroShares handles the case where there are no active shares.
// Writes zero yield to snapshots for all users.
func (k *Keeper) accountingWithZeroShares(
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
	complete := cursor.PositionsProcessed >= cursor.TotalPositions

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
	totalShares sdkmath.Int,
	cursor vaultsv2.AccountingCursor,
	maxPositions uint32,
) (*AccountingResult, error) {
	navBig := navInfo.CurrentNav.BigInt()
	totalSharesBig := totalShares.BigInt()

	// Restore residual from cursor
	residual := cursor.AccumulatedResidual.BigInt()

	yieldThisBatch := sdkmath.ZeroInt()
	headerInfo := k.header.GetHeaderInfo(ctx)

	// Use paginated iterator
	lastProcessed, count, err := k.IterateVaultsV2UserPositionsPaginated(
		ctx,
		cursor.LastProcessedUser,
		maxPositions,
		func(address types.AccAddress, position vaultsv2.UserPosition) error {
			shares, err := k.GetVaultsV2UserShares(ctx, address)
			if err != nil {
				return err
			}

			var depositAmount, accruedYield sdkmath.Int

			if !shares.IsPositive() {
				// User has no shares, set to pending withdrawal
				depositAmount = position.AmountPendingWithdrawal
				accruedYield = sdkmath.ZeroInt()
			} else {
				// Calculate proportional value
				numerator := new(big.Int).Mul(navBig, shares.BigInt())
				quotient := new(big.Int)
				remainder := new(big.Int)
				quotient.QuoRem(numerator, totalSharesBig, remainder)

				// Accumulate remainder for fair distribution
				residual.Add(residual, remainder)
				if residual.Cmp(totalSharesBig) >= 0 {
					extra := new(big.Int).Div(new(big.Int).Set(residual), totalSharesBig)
					if extra.Sign() > 0 {
						quotient.Add(quotient, extra)
						residual.Sub(residual, new(big.Int).Mul(totalSharesBig, extra))
					}
				}

				value := sdkmath.NewIntFromBigInt(quotient)
				accruedYield, err = value.SafeSub(shares)
				if err != nil {
					return err
				}

				depositAmount = shares
				if position.AmountPendingWithdrawal.IsPositive() {
					if depositAmount, err = depositAmount.SafeAdd(position.AmountPendingWithdrawal); err != nil {
						return err
					}
				}

				// Track yield for this batch
				yieldThisBatch, err = yieldThisBatch.SafeAdd(accruedYield)
				if err != nil {
					return err
				}
			}

			// Write to snapshot instead of directly to position
			snapshot := vaultsv2.AccountingSnapshot{
				User:          address.String(),
				DepositAmount: depositAmount,
				AccruedYield:  accruedYield,
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
	cursor.AccumulatedResidual = sdkmath.NewIntFromBigInt(residual)
	complete := cursor.PositionsProcessed >= cursor.TotalPositions

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
