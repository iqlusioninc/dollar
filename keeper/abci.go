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
	"math/big"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

// updateVaultsV2Accounting synchronises the aggregate vault accounting figures
// with the latest NAV information and distributes accrued yield across user
// positions proportionally to their share holdings.
//
// DEPRECATED: This function processes all positions in a single call and is no
// longer used in BeginBlock. Use updateVaultsV2AccountingWithCursor instead,
// which supports cursor-based pagination for better scalability.
// This function is kept for reference and backwards compatibility.
func (k *Keeper) updateVaultsV2Accounting(ctx context.Context) error {
	navInfo, err := k.GetVaultsV2NAVInfo(ctx)
	if err != nil {
		return err
	}

	if navInfo.CurrentNav.IsNil() {
		return nil
	}

	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return err
	}

	totalShares, err := k.GetVaultsV2TotalShares(ctx)
	if err != nil {
		return err
	}

	// When there are no active shares, reset accrued yield information and
	// simply mirror the NAV in the aggregate vault state.
	if totalShares.IsZero() {
		if err := k.IterateVaultsV2UserPositions(ctx, func(address types.AccAddress, position vaultsv2.UserPosition) (bool, error) {
			if position.AccruedYield.IsZero() {
				return false, nil
			}

			position.AccruedYield = sdkmath.ZeroInt()
			if err := k.SetVaultsV2UserPosition(ctx, address, position); err != nil {
				return true, err
			}

			return false, nil
		}); err != nil {
			return err
		}

		state.TotalNav = navInfo.CurrentNav

		if !state.TotalDeposits.IsNil() {
			if state.TotalAccruedYield, err = navInfo.CurrentNav.SafeSub(state.TotalDeposits); err != nil {
				return err
			}
		} else {
			state.TotalAccruedYield = sdkmath.ZeroInt()
		}

		if !navInfo.LastUpdate.IsZero() {
			state.LastNavUpdate = navInfo.LastUpdate
		}

		return k.SetVaultsV2VaultState(ctx, state)
	}

	if state.TotalNav.Equal(navInfo.CurrentNav) && !navInfo.LastUpdate.After(state.LastNavUpdate) {
		return nil
	}

	navBig := navInfo.CurrentNav.BigInt()
	totalSharesBig := totalShares.BigInt()

	residual := new(big.Int)
	aggregatedYield := sdkmath.ZeroInt()
	aggregatedDeposits := sdkmath.ZeroInt()

	err = k.IterateVaultsV2UserPositions(ctx, func(address types.AccAddress, position vaultsv2.UserPosition) (bool, error) {
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

			return false, nil
		}

		numerator := new(big.Int).Mul(navBig, shares.BigInt())
		quotient := new(big.Int)
		remainder := new(big.Int)
		quotient.QuoRem(numerator, totalSharesBig, remainder)

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

		aggregatedYield, err = aggregatedYield.SafeAdd(accruedYield)
		if err != nil {
			return true, err
		}

		aggregatedDeposits, err = aggregatedDeposits.SafeAdd(depositAmount)
		if err != nil {
			return true, err
		}

		return false, nil
	})
	if err != nil {
		return err
	}

	state.TotalAccruedYield = aggregatedYield
	state.TotalDeposits = aggregatedDeposits
	state.TotalNav = navInfo.CurrentNav
	if !navInfo.LastUpdate.IsZero() {
		state.LastNavUpdate = navInfo.LastUpdate
	}

	return k.SetVaultsV2VaultState(ctx, state)
}

// BeginBlocker is called at the beginning of each block.
func (k *Keeper) BeginBlocker(ctx context.Context) error {
	// Note: Vault accounting is now triggered via MsgUpdateVaultAccounting
	// instead of being automatically called in BeginBlock. This allows
	// the vault manager to control when accounting is performed and enables
	// cursor-based pagination for large numbers of positions.

	defer func() {
		if r := recover(); r != nil {
			k.logger.Error("recovered panic while ending vaults season one", "err", r)
			return
		}
	}()

	// If we haven't ended Vaults Season One, and the current time exceeds the
	// configured end time, we end it.
	if k.header.GetHeaderInfo(ctx).Time.Unix() > k.vaultsSeasonOneEndTimestamp && !k.IsVaultsSeasonOneEnded(ctx) {
		defer func() {
			// We mark Vaults Season One as ended even if there is an error.
			if err := k.VaultsSeasonOneEnded.Set(ctx, true); err != nil {
				k.logger.Error("failed to set vaults season one as ended", "err", err)
			}
		}()

		// Create a cached context for the execution.
		cachedCtx, commit := types.UnwrapSDKContext(ctx).CacheContext()

		if err := k.endVaultsSeasonOne(cachedCtx); err != nil {
			return err
		}

		// Commit the results.
		commit()
	}

	return nil
}
