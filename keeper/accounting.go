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
func (k *Keeper) updateVaultsV2AccountingWithCursor(ctx context.Context, maxPositions uint32, forceRestart bool) (*AccountingResult, error) {
	navInfo, err := k.GetVaultsV2NAVInfo(ctx)
	if err != nil {
		return nil, err
	}

	if navInfo.CurrentNav.IsNil() {
		return nil, fmt.Errorf("current NAV is not set")
	}

	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, err
	}

	totalShares, err := k.GetVaultsV2TotalShares(ctx)
	if err != nil {
		return nil, err
	}

	cursor, err := k.GetVaultsV2AccountingCursor(ctx)
	if err != nil {
		return nil, err
	}

	// Check if we need to initialize or restart the cursor
	needsInit := !cursor.InProgress ||
		forceRestart ||
		cursor.AccountingNav.IsNil() ||
		!cursor.AccountingNav.Equal(navInfo.CurrentNav) ||
		!cursor.AccountingNavTimestamp.Equal(navInfo.LastUpdate)

	if needsInit {
		if cursor.InProgress && !forceRestart {
			return nil, fmt.Errorf("accounting already in progress, cannot start new session without force_restart")
		}

		// Count total positions
		totalPositions := uint64(0)
		if err := k.IterateVaultsV2UserPositions(ctx, func(address types.AccAddress, position vaultsv2.UserPosition) (bool, error) {
			totalPositions++
			return false, nil
		}); err != nil {
			return nil, err
		}

		// Initialize cursor
		cursor = vaultsv2.AccountingCursor{
			LastProcessedUser:        "",
			AccountingNav:            navInfo.CurrentNav,
			AccountingNavTimestamp:   navInfo.LastUpdate,
			PositionsProcessed:       0,
			TotalPositions:           totalPositions,
			InProgress:               true,
			StartedAt:                k.header.GetHeaderInfo(ctx).Time,
			AccumulatedResidual:      sdkmath.ZeroInt(),
		}
	}

	// When there are no active shares, reset accrued yield and mirror NAV
	if totalShares.IsZero() {
		return k.accountingWithZeroShares(ctx, navInfo, state, cursor)
	}

	// Perform cursor-based accounting
	return k.accountingWithCursor(ctx, navInfo, state, totalShares, cursor, maxPositions)
}

// accountingWithZeroShares handles the case where there are no active shares.
func (k *Keeper) accountingWithZeroShares(
	ctx context.Context,
	navInfo vaultsv2.NAVInfo,
	state vaultsv2.VaultState,
	cursor vaultsv2.AccountingCursor,
) (*AccountingResult, error) {
	positionsProcessed := uint32(0)
	maxBatch := uint32(100) // Process in batches when resetting yield

	startAfter := cursor.LastProcessedUser
	complete := true

	if err := k.IterateVaultsV2UserPositions(ctx, func(address types.AccAddress, position vaultsv2.UserPosition) (bool, error) {
		addrStr := address.String()

		// Skip until we reach the last processed user
		if startAfter != "" {
			if addrStr <= startAfter {
				return false, nil
			}
		}

		if position.AccruedYield.IsZero() {
			return false, nil
		}

		position.AccruedYield = sdkmath.ZeroInt()
		if err := k.SetVaultsV2UserPosition(ctx, address, position); err != nil {
			return true, err
		}

		cursor.LastProcessedUser = addrStr
		cursor.PositionsProcessed++
		positionsProcessed++

		// Stop if we've hit the batch limit
		if positionsProcessed >= maxBatch {
			complete = false
			return true, nil
		}

		return false, nil
	}); err != nil {
		return nil, err
	}

	// Update vault state
	if complete {
		state.TotalNav = navInfo.CurrentNav
		if !state.TotalDeposits.IsNil() {
			var err error
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

	nextUser := ""
	if !complete {
		nextUser = cursor.LastProcessedUser
	}

	return &AccountingResult{
		PositionsProcessed:      uint64(positionsProcessed),
		TotalPositionsProcessed: cursor.PositionsProcessed,
		TotalPositions:          cursor.TotalPositions,
		Complete:                complete,
		NextUser:                nextUser,
		AppliedNav:              navInfo.CurrentNav,
		YieldDistributed:        sdkmath.ZeroInt(),
	}, nil
}

// accountingWithCursor performs the main accounting logic with cursor pagination.
func (k *Keeper) accountingWithCursor(
	ctx context.Context,
	navInfo vaultsv2.NAVInfo,
	state vaultsv2.VaultState,
	totalShares sdkmath.Int,
	cursor vaultsv2.AccountingCursor,
	maxPositions uint32,
) (*AccountingResult, error) {
	navBig := navInfo.CurrentNav.BigInt()
	totalSharesBig := totalShares.BigInt()

	// Restore residual from cursor
	residual := cursor.AccumulatedResidual.BigInt()

	aggregatedYield := sdkmath.ZeroInt()
	aggregatedDeposits := sdkmath.ZeroInt()
	positionsProcessed := uint32(0)
	yieldThisBatch := sdkmath.ZeroInt()

	startAfter := cursor.LastProcessedUser
	var lastUser string
	complete := true

	err := k.IterateVaultsV2UserPositions(ctx, func(address types.AccAddress, position vaultsv2.UserPosition) (bool, error) {
		addrStr := address.String()

		// Skip until we reach the last processed user
		if startAfter != "" {
			if addrStr <= startAfter {
				return false, nil
			}
		}

		shares, err := k.GetVaultsV2UserShares(ctx, address)
		if err != nil {
			return true, err
		}

		if !shares.IsPositive() {
			pending := position.AmountPendingWithdrawal

			if !position.AccruedYield.IsZero() || position.DepositAmount.IsNil() || !position.DepositAmount.Equal(pending) {
				position.AccruedYield = sdkmath.ZeroInt()
				position.DepositAmount = pending
				if err := k.SetVaultsV2UserPosition(ctx, address, position); err != nil {
					return true, err
				}
			}

			aggregatedDeposits, err = aggregatedDeposits.SafeAdd(pending)
			if err != nil {
				return true, err
			}

			lastUser = addrStr
			positionsProcessed++

			// Check if we should stop for this batch
			if maxPositions > 0 && positionsProcessed >= maxPositions {
				complete = false
				return true, nil
			}

			return false, nil
		}

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
		accruedYield, err := value.SafeSub(shares)
		if err != nil {
			return true, err
		}

		// Update position
		position.AccruedYield = accruedYield
		depositAmount := shares
		if position.AmountPendingWithdrawal.IsPositive() {
			if depositAmount, err = depositAmount.SafeAdd(position.AmountPendingWithdrawal); err != nil {
				return true, err
			}
		}
		position.DepositAmount = depositAmount
		if err := k.SetVaultsV2UserPosition(ctx, address, position); err != nil {
			return true, err
		}

		// Aggregate totals
		aggregatedYield, err = aggregatedYield.SafeAdd(accruedYield)
		if err != nil {
			return true, err
		}

		aggregatedDeposits, err = aggregatedDeposits.SafeAdd(depositAmount)
		if err != nil {
			return true, err
		}

		yieldThisBatch, err = yieldThisBatch.SafeAdd(accruedYield)
		if err != nil {
			return true, err
		}

		lastUser = addrStr
		positionsProcessed++

		// Check if we should stop for this batch
		if maxPositions > 0 && positionsProcessed >= maxPositions {
			complete = false
			return true, nil
		}

		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Update cursor
	cursor.PositionsProcessed += uint64(positionsProcessed)
	cursor.LastProcessedUser = lastUser
	cursor.AccumulatedResidual = sdkmath.NewIntFromBigInt(residual)

	// If complete, update state and clear cursor
	if complete {
		state.TotalAccruedYield = aggregatedYield
		state.TotalDeposits = aggregatedDeposits
		state.TotalNav = navInfo.CurrentNav
		if !navInfo.LastUpdate.IsZero() {
			state.LastNavUpdate = navInfo.LastUpdate
		}

		if err := k.SetVaultsV2VaultState(ctx, state); err != nil {
			return nil, err
		}

		if err := k.ClearVaultsV2AccountingCursor(ctx); err != nil {
			return nil, err
		}
	} else {
		// Save cursor for next iteration
		if err := k.SetVaultsV2AccountingCursor(ctx, cursor); err != nil {
			return nil, err
		}
	}

	nextUser := ""
	if !complete {
		nextUser = lastUser
	}

	return &AccountingResult{
		PositionsProcessed:      uint64(positionsProcessed),
		TotalPositionsProcessed: cursor.PositionsProcessed,
		TotalPositions:          cursor.TotalPositions,
		Complete:                complete,
		NextUser:                nextUser,
		AppliedNav:              navInfo.CurrentNav,
		YieldDistributed:        yieldThisBatch,
	}, nil
}
