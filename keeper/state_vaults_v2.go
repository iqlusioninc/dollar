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
	"errors"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

// RemotePositionEntry represents a remote position entry with its identifier and chain metadata.
type RemotePositionEntry struct {
	ID       uint64
	Position vaultsv2.RemotePosition
	ChainID  uint32
}

// GetVaultsV2Params returns the currently configured vaults v2 parameters.
// When no parameters have been stored yet the zero-value configuration is
// returned without error.
func (k *Keeper) GetVaultsV2Params(ctx context.Context) (vaultsv2.Params, error) {
	params, err := k.VaultsV2Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.Params{}, nil
		}
		return vaultsv2.Params{}, err
	}

	return params, nil
}

// SetVaultsV2Params persists the supplied params to state.
func (k *Keeper) SetVaultsV2Params(ctx context.Context, params vaultsv2.Params) error {
	return k.VaultsV2Params.Set(ctx, params)
}

// GetVaultsV2Config returns the stored vault configuration or a zero-value
// config when it has not been set yet.
func (k *Keeper) GetVaultsV2Config(ctx context.Context) (vaultsv2.VaultConfig, error) {
	config, err := k.VaultsV2Config.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.VaultConfig{}, nil
		}
		return vaultsv2.VaultConfig{}, err
	}

	return config, nil
}

// SetVaultsV2Config persists the provided vault configuration in state.
func (k *Keeper) SetVaultsV2Config(ctx context.Context, config vaultsv2.VaultConfig) error {
	return k.VaultsV2Config.Set(ctx, config)
}

// GetVaultsV2VaultState fetches the aggregate vault state from storage. If the
// state has not been initialised yet a zero-value instance is returned so
// callers can update it safely.
func (k *Keeper) GetVaultsV2VaultState(ctx context.Context) (vaultsv2.VaultState, error) {
	state, err := k.VaultsV2VaultState.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.VaultState{}, nil
		}
		return vaultsv2.VaultState{}, err
	}

	return state, nil
}

// SetVaultsV2VaultState stores the provided aggregate vault state.
func (k *Keeper) SetVaultsV2VaultState(ctx context.Context, state vaultsv2.VaultState) error {
	return k.VaultsV2VaultState.Set(ctx, state)
}

// GetVaultsV2NAVInfo returns the cached NAV information or a zero-value copy
// when unset.
func (k *Keeper) GetVaultsV2NAVInfo(ctx context.Context) (vaultsv2.NAVInfo, error) {
	nav, err := k.VaultsV2NAVInfo.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.NAVInfo{}, nil
		}
		return vaultsv2.NAVInfo{}, err
	}

	return nav, nil
}

// SetVaultsV2NAVInfo updates the cached NAV information in storage.
func (k *Keeper) SetVaultsV2NAVInfo(ctx context.Context, nav vaultsv2.NAVInfo) error {
	return k.VaultsV2NAVInfo.Set(ctx, nav)
}

// GetVaultsV2UserPosition returns the position for the supplied account. The
// boolean flag indicates whether the position existed in state.
func (k *Keeper) GetVaultsV2UserPosition(ctx context.Context, address sdk.AccAddress) (vaultsv2.UserPosition, bool, error) {
	position, err := k.VaultsV2UserPositions.Get(ctx, address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.UserPosition{}, false, nil
		}
		return vaultsv2.UserPosition{}, false, err
	}

	return position, true, nil
}

// SetVaultsV2UserPosition writes the provided user position to state.
func (k *Keeper) SetVaultsV2UserPosition(ctx context.Context, address sdk.AccAddress, position vaultsv2.UserPosition) error {
	return k.VaultsV2UserPositions.Set(ctx, address, position)
}

// DeleteVaultsV2UserPosition removes an existing position entry from state.
func (k *Keeper) DeleteVaultsV2UserPosition(ctx context.Context, address sdk.AccAddress) error {
	return k.VaultsV2UserPositions.Remove(ctx, address)
}

// IterateVaultsV2UserPositions walks every stored user position and invokes
// the supplied callback. Returning true from the callback stops the iteration
// early.
func (k *Keeper) IterateVaultsV2UserPositions(ctx context.Context, fn func(address sdk.AccAddress, position vaultsv2.UserPosition) (bool, error)) error {
	return k.VaultsV2UserPositions.Walk(ctx, nil, func(key []byte, position vaultsv2.UserPosition) (bool, error) {
		return fn(sdk.AccAddress(key), position)
	})
}

// NextVaultsV2WithdrawalID increments and returns the next withdrawal queue
// identifier. Identifiers start at one for readability when exposed to users.
func (k *Keeper) NextVaultsV2WithdrawalID(ctx context.Context) (uint64, error) {
	next, err := k.VaultsV2WithdrawalNextID.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return 0, err
		}

		next = 1
	} else {
		next++
	}

	if err := k.VaultsV2WithdrawalNextID.Set(ctx, next); err != nil {
		return 0, err
	}

	return next, nil
}

// PeekVaultsV2WithdrawalID returns the currently stored next withdrawal ID
// without mutating state. The zero value indicates that no withdrawals have
// been created yet.
func (k *Keeper) PeekVaultsV2WithdrawalID(ctx context.Context) (uint64, error) {
	id, err := k.VaultsV2WithdrawalNextID.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}

	return id, nil
}

// SetVaultsV2Withdrawal stores a withdrawal request under the provided id. The
// request's RequestId field is normalised to the decimal representation of the
// identifier to keep string-based lookups consistent.
func (k *Keeper) SetVaultsV2Withdrawal(ctx context.Context, id uint64, request vaultsv2.WithdrawalRequest) error {
	request.RequestId = strconv.FormatUint(id, 10)
	return k.VaultsV2WithdrawalQueue.Set(ctx, id, request)
}

// NextVaultsV2RemotePositionID increments and returns the next remote position identifier.
func (k *Keeper) NextVaultsV2RemotePositionID(ctx context.Context) (uint64, error) {
	next, err := k.VaultsV2RemotePositionNextID.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return 0, err
		}
		next = 1
	} else {
		next++
	}

	if err := k.VaultsV2RemotePositionNextID.Set(ctx, next); err != nil {
		return 0, err
	}

	return next, nil
}

// GetVaultsV2RemotePosition fetches a remote position entry by id.
func (k *Keeper) GetVaultsV2RemotePosition(ctx context.Context, id uint64) (vaultsv2.RemotePosition, bool, error) {
	position, err := k.VaultsV2RemotePositions.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.RemotePosition{}, false, nil
		}
		return vaultsv2.RemotePosition{}, false, err
	}

	return position, true, nil
}

// SetVaultsV2RemotePosition stores a remote position entry.
func (k *Keeper) SetVaultsV2RemotePosition(ctx context.Context, id uint64, position vaultsv2.RemotePosition) error {
	return k.VaultsV2RemotePositions.Set(ctx, id, position)
}

// DeleteVaultsV2RemotePosition removes a remote position entry and associated metadata.
func (k *Keeper) DeleteVaultsV2RemotePosition(ctx context.Context, id uint64) error {
	if err := k.VaultsV2RemotePositions.Remove(ctx, id); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if err := k.VaultsV2RemotePositionChains.Remove(ctx, id); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	return nil
}

// GetVaultsV2RemotePositionChainID returns the chain identifier associated with a remote position.
func (k *Keeper) GetVaultsV2RemotePositionChainID(ctx context.Context, id uint64) (uint32, bool, error) {
	chainID, err := k.VaultsV2RemotePositionChains.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}

	return chainID, true, nil
}

// SetVaultsV2RemotePositionChainID associates a chain identifier with a remote position.
func (k *Keeper) SetVaultsV2RemotePositionChainID(ctx context.Context, id uint64, chainID uint32) error {
	return k.VaultsV2RemotePositionChains.Set(ctx, id, chainID)
}

// NextVaultsV2CrossChainRouteID increments and returns the next cross-chain route identifier.
func (k *Keeper) NextVaultsV2CrossChainRouteID(ctx context.Context) (uint32, error) {
	next, err := k.VaultsV2CrossChainRouteNextID.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return 0, err
		}

		next = 1
	} else {
		next++
	}

	if err := k.VaultsV2CrossChainRouteNextID.Set(ctx, next); err != nil {
		return 0, err
	}

	return next, nil
}

// SetVaultsV2CrossChainRoute stores a cross-chain route configuration under the provided identifier.
func (k *Keeper) SetVaultsV2CrossChainRoute(ctx context.Context, id uint32, route vaultsv2.CrossChainRoute) error {
	return k.VaultsV2CrossChainRoutes.Set(ctx, id, route)
}

// GetVaultsV2CrossChainRoute fetches a cross-chain route configuration by identifier.
func (k *Keeper) GetVaultsV2CrossChainRoute(ctx context.Context, id uint32) (vaultsv2.CrossChainRoute, bool, error) {
	route, err := k.VaultsV2CrossChainRoutes.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.CrossChainRoute{}, false, nil
		}

		return vaultsv2.CrossChainRoute{}, false, err
	}

	return route, true, nil
}

// DeleteVaultsV2CrossChainRoute removes a cross-chain route configuration.
func (k *Keeper) DeleteVaultsV2CrossChainRoute(ctx context.Context, id uint32) error {
	return k.VaultsV2CrossChainRoutes.Remove(ctx, id)
}

// IterateVaultsV2CrossChainRoutes walks each stored cross-chain route and executes the supplied callback.
func (k *Keeper) IterateVaultsV2CrossChainRoutes(ctx context.Context, fn func(uint32, vaultsv2.CrossChainRoute) (bool, error)) error {
	return k.VaultsV2CrossChainRoutes.Walk(ctx, nil, fn)
}

// GetVaultsV2InflightValueByRoute returns the currently tracked inflight value for a route.
func (k *Keeper) GetVaultsV2InflightValueByRoute(ctx context.Context, routeID uint32) (math.Int, error) {
	value, err := k.VaultsV2InflightValueByRoute.Get(ctx, routeID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	return value, nil
}

// AddVaultsV2InflightValueByRoute increments the inflight value tracked for the given route.
func (k *Keeper) AddVaultsV2InflightValueByRoute(ctx context.Context, routeID uint32, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2InflightValueByRoute(ctx, routeID)
	if err != nil {
		return err
	}

	current, err = current.SafeAdd(amount)
	if err != nil {
		return err
	}

	return k.VaultsV2InflightValueByRoute.Set(ctx, routeID, current)
}

// SubtractVaultsV2InflightValueByRoute decrements the inflight value tracked for the given route.
func (k *Keeper) SubtractVaultsV2InflightValueByRoute(ctx context.Context, routeID uint32, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2InflightValueByRoute(ctx, routeID)
	if err != nil {
		return err
	}

	current, err = current.SafeSub(amount)
	if err != nil {
		return err
	}

	if !current.IsPositive() {
		if err := k.VaultsV2InflightValueByRoute.Remove(ctx, routeID); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		return nil
	}

	return k.VaultsV2InflightValueByRoute.Set(ctx, routeID, current)
}

// GetAllVaultsV2RemotePositions returns all remote positions stored in state.
func (k *Keeper) GetAllVaultsV2RemotePositions(ctx context.Context) ([]RemotePositionEntry, error) {
	var positions []RemotePositionEntry

	err := k.VaultsV2RemotePositions.Walk(ctx, nil, func(id uint64, position vaultsv2.RemotePosition) (bool, error) {
		chainID, err := k.VaultsV2RemotePositionChains.Get(ctx, id)
		if err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				return true, err
			}
			chainID = 0
		}

		positions = append(positions, RemotePositionEntry{
			ID:       id,
			Position: position,
			ChainID:  chainID,
		})

		return false, nil
	})

	return positions, err
}

// GetVaultsV2Withdrawal fetches a withdrawal request by id.
func (k *Keeper) GetVaultsV2Withdrawal(ctx context.Context, id uint64) (vaultsv2.WithdrawalRequest, bool, error) {
	req, err := k.VaultsV2WithdrawalQueue.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.WithdrawalRequest{}, false, nil
		}
		return vaultsv2.WithdrawalRequest{}, false, err
	}

	return req, true, nil
}

// DeleteVaultsV2Withdrawal removes a withdrawal entry by id.
func (k *Keeper) DeleteVaultsV2Withdrawal(ctx context.Context, id uint64) error {
	return k.VaultsV2WithdrawalQueue.Remove(ctx, id)
}

// IterateVaultsV2Withdrawals walks all withdrawal requests invoking the
// callback for each stored entry.
func (k *Keeper) IterateVaultsV2Withdrawals(ctx context.Context, fn func(id uint64, request vaultsv2.WithdrawalRequest) (bool, error)) error {
	return k.VaultsV2WithdrawalQueue.Walk(ctx, nil, fn)
}

// GetAllVaultsV2Withdrawals returns a slice containing every withdrawal request
// currently stored.
func (k *Keeper) GetAllVaultsV2Withdrawals(ctx context.Context) ([]vaultsv2.WithdrawalRequest, error) {
	var requests []vaultsv2.WithdrawalRequest

	err := k.IterateVaultsV2Withdrawals(ctx, func(_ uint64, request vaultsv2.WithdrawalRequest) (bool, error) {
		requests = append(requests, request)
		return false, nil
	})

	return requests, err
}

// ResetVaultsV2UserPosition clears a user position but returns the prior
// value to the caller so it can be used for downstream processing.
func (k *Keeper) ResetVaultsV2UserPosition(ctx context.Context, address sdk.AccAddress) (vaultsv2.UserPosition, error) {
	position, found, err := k.GetVaultsV2UserPosition(ctx, address)
	if err != nil {
		return vaultsv2.UserPosition{}, err
	}
	if !found {
		return vaultsv2.UserPosition{}, nil
	}

	if err := k.DeleteVaultsV2UserPosition(ctx, address); err != nil {
		return vaultsv2.UserPosition{}, err
	}

	return position, nil
}

// AddAmountToVaultsV2Totals updates the aggregate vault totals by the supplied
// delta values. Positive amounts increment totals while negative values are
// ignored to avoid panics – callers should use a dedicated decrement helper if
// that behaviour is required.
func (k *Keeper) AddAmountToVaultsV2Totals(ctx context.Context, deposits, accruedYield math.Int) error {
	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return err
	}

	if deposits.IsPositive() {
		if state.TotalDeposits, err = state.TotalDeposits.SafeAdd(deposits); err != nil {
			return err
		}
		if state.TotalNav, err = state.TotalNav.SafeAdd(deposits); err != nil {
			return err
		}
	}

	if accruedYield.IsPositive() {
		if state.TotalAccruedYield, err = state.TotalAccruedYield.SafeAdd(accruedYield); err != nil {
			return err
		}
		if state.TotalNav, err = state.TotalNav.SafeAdd(accruedYield); err != nil {
			return err
		}
	}

	return k.SetVaultsV2VaultState(ctx, state)
}

// SubtractAmountFromVaultsV2Totals decrements the aggregate vault totals by the
// supplied deltas.
func (k *Keeper) SubtractAmountFromVaultsV2Totals(ctx context.Context, deposits, accruedYield math.Int) error {
	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return err
	}

	if deposits.IsPositive() {
		if state.TotalDeposits, err = state.TotalDeposits.SafeSub(deposits); err != nil {
			return err
		}
		if state.TotalNav, err = state.TotalNav.SafeSub(deposits); err != nil {
			return err
		}
	}

	if accruedYield.IsPositive() {
		if state.TotalAccruedYield, err = state.TotalAccruedYield.SafeSub(accruedYield); err != nil {
			return err
		}
		if state.TotalNav, err = state.TotalNav.SafeSub(accruedYield); err != nil {
			return err
		}
	}

	return k.SetVaultsV2VaultState(ctx, state)
}

// GetVaultsV2UserShares returns the share balance for a user. Missing entries
// are treated as zero without error.
func (k *Keeper) GetVaultsV2UserShares(ctx context.Context, address sdk.AccAddress) (math.Int, error) {
	shares, err := k.VaultsV2UserShares.Get(ctx, address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	return shares, nil
}

// SetVaultsV2UserShares updates the share balance for a user, deleting the
// entry when the balance reaches zero to keep the store compact.
func (k *Keeper) SetVaultsV2UserShares(ctx context.Context, address sdk.AccAddress, shares math.Int) error {
	if !shares.IsPositive() {
		if err := k.VaultsV2UserShares.Remove(ctx, address); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		return nil
	}

	return k.VaultsV2UserShares.Set(ctx, address, shares)
}

// GetVaultsV2TotalShares returns the aggregate share supply recorded on-chain.
func (k *Keeper) GetVaultsV2TotalShares(ctx context.Context) (math.Int, error) {
	total, err := k.VaultsV2TotalShares.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	return total, nil
}

// SetVaultsV2TotalShares overwrites the aggregate share supply in storage.
func (k *Keeper) SetVaultsV2TotalShares(ctx context.Context, shares math.Int) error {
	return k.VaultsV2TotalShares.Set(ctx, shares)
}

// GetVaultsV2PendingDeploymentFunds returns the amount of deposits awaiting
// deployment to remote positions.
func (k *Keeper) GetVaultsV2PendingDeploymentFunds(ctx context.Context) (math.Int, error) {
	amount, err := k.VaultsV2PendingDeploymentFunds.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	return amount, nil
}

// AddVaultsV2PendingDeploymentFunds increases the tracked pending deployment
// balance by the supplied amount.
func (k *Keeper) AddVaultsV2PendingDeploymentFunds(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2PendingDeploymentFunds(ctx)
	if err != nil {
		return err
	}

	current, err = current.SafeAdd(amount)
	if err != nil {
		return err
	}

	return k.VaultsV2PendingDeploymentFunds.Set(ctx, current)
}

// SubtractVaultsV2PendingDeploymentFunds decreases the pending deployment
// balance, removing the entry when it reaches zero.
func (k *Keeper) SubtractVaultsV2PendingDeploymentFunds(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2PendingDeploymentFunds(ctx)
	if err != nil {
		return err
	}

	current, err = current.SafeSub(amount)
	if err != nil {
		return err
	}

	if !current.IsPositive() {
		if err := k.VaultsV2PendingDeploymentFunds.Remove(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		return nil
	}

	return k.VaultsV2PendingDeploymentFunds.Set(ctx, current)
}

// GetVaultsV2PendingWithdrawalAmount returns the aggregate amount linked to
// outstanding withdrawal requests.
func (k *Keeper) GetVaultsV2PendingWithdrawalAmount(ctx context.Context) (math.Int, error) {
	amount, err := k.VaultsV2PendingWithdrawalsAmount.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	return amount, nil
}

// AddVaultsV2PendingWithdrawalAmount increments the tracked pending withdrawal
// amount by the supplied value.
func (k *Keeper) AddVaultsV2PendingWithdrawalAmount(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2PendingWithdrawalAmount(ctx)
	if err != nil {
		return err
	}

	current, err = current.SafeAdd(amount)
	if err != nil {
		return err
	}

	return k.VaultsV2PendingWithdrawalsAmount.Set(ctx, current)
}

// SubtractVaultsV2PendingWithdrawalAmount decrements the pending withdrawal
// amount, removing the entry when it reaches zero.
func (k *Keeper) SubtractVaultsV2PendingWithdrawalAmount(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2PendingWithdrawalAmount(ctx)
	if err != nil {
		return err
	}

	current, err = current.SafeSub(amount)
	if err != nil {
		return err
	}

	if !current.IsPositive() {
		if err := k.VaultsV2PendingWithdrawalsAmount.Remove(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		return nil
	}

	return k.VaultsV2PendingWithdrawalsAmount.Set(ctx, current)
}

// GetVaultsV2PendingWithdrawalDistribution returns the amount awaiting distribution post remote withdrawals.
func (k *Keeper) GetVaultsV2PendingWithdrawalDistribution(ctx context.Context) (math.Int, error) {
	amount, err := k.VaultsV2PendingWithdrawalDistribution.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	return amount, nil
}

// AddVaultsV2PendingWithdrawalDistribution increments the distribution balance.
func (k *Keeper) AddVaultsV2PendingWithdrawalDistribution(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2PendingWithdrawalDistribution(ctx)
	if err != nil {
		return err
	}

	current, err = current.SafeAdd(amount)
	if err != nil {
		return err
	}

	return k.VaultsV2PendingWithdrawalDistribution.Set(ctx, current)
}

// SubtractVaultsV2PendingWithdrawalDistribution decrements the distribution balance.
func (k *Keeper) SubtractVaultsV2PendingWithdrawalDistribution(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2PendingWithdrawalDistribution(ctx)
	if err != nil {
		return err
	}

	current, err = current.SafeSub(amount)
	if err != nil {
		return err
	}

	if !current.IsPositive() {
		if err := k.VaultsV2PendingWithdrawalDistribution.Remove(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		return nil
	}

	return k.VaultsV2PendingWithdrawalDistribution.Set(ctx, current)
}

// IncrementVaultsV2TotalUsers increases the total user count tracked in the
// aggregate vault state.
func (k *Keeper) IncrementVaultsV2TotalUsers(ctx context.Context) error {
	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return err
	}

	state.TotalUsers++

	return k.SetVaultsV2VaultState(ctx, state)
}

// DecrementVaultsV2TotalUsers reduces the tracked user count, guarding against
// underflow.
func (k *Keeper) DecrementVaultsV2TotalUsers(ctx context.Context) error {
	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return err
	}

	if state.TotalUsers > 0 {
		state.TotalUsers--
	}

	return k.SetVaultsV2VaultState(ctx, state)
}

// GetVaultsV2RemotePositionOracle fetches the oracle tracking information for
// a remote position. The boolean return value indicates whether the oracle was
// found in state.
func (k *Keeper) GetVaultsV2RemotePositionOracle(ctx context.Context, positionID uint64) (vaultsv2.RemotePositionOracle, bool, error) {
	oracle, err := k.VaultsV2RemotePositionOracles.Get(ctx, positionID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.RemotePositionOracle{}, false, nil
		}

		return vaultsv2.RemotePositionOracle{}, false, err
	}

	return oracle, true, nil
}

// SetVaultsV2RemotePositionOracle persists the provided oracle configuration
// for the given position identifier.
func (k *Keeper) SetVaultsV2RemotePositionOracle(ctx context.Context, positionID uint64, oracle vaultsv2.RemotePositionOracle) error {
	oracle.PositionId = positionID
	return k.VaultsV2RemotePositionOracles.Set(ctx, positionID, oracle)
}

// IterateVaultsV2RemotePositionOracles walks all stored remote position
// oracles invoking the supplied callback for each entry.
func (k *Keeper) IterateVaultsV2RemotePositionOracles(ctx context.Context, fn func(uint64, vaultsv2.RemotePositionOracle) (bool, error)) error {
	return k.VaultsV2RemotePositionOracles.Walk(ctx, nil, fn)
}

// NextVaultsV2InflightID increments and returns the next inflight fund identifier.
func (k *Keeper) NextVaultsV2InflightID(ctx context.Context) (uint64, error) {
	next, err := k.VaultsV2InflightNextID.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return 0, err
		}
		next = 1
	} else {
		next++
	}

	if err := k.VaultsV2InflightNextID.Set(ctx, next); err != nil {
		return 0, err
	}

	return next, nil
}

// SetVaultsV2InflightFund stores the provided inflight fund under its identifier.
func (k *Keeper) SetVaultsV2InflightFund(ctx context.Context, fund vaultsv2.InflightFund) error {
	if fund.Id == "" {
		return errors.New("inflight fund identifier cannot be empty")
	}

	return k.VaultsV2InflightFunds.Set(ctx, fund.Id, fund)
}

// GetVaultsV2InflightFund fetches an inflight fund by its identifier.
func (k *Keeper) GetVaultsV2InflightFund(ctx context.Context, id string) (vaultsv2.InflightFund, bool, error) {
	fund, err := k.VaultsV2InflightFunds.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.InflightFund{}, false, nil
		}

		return vaultsv2.InflightFund{}, false, err
	}

	return fund, true, nil
}

// DeleteVaultsV2InflightFund removes an inflight fund entry from state.
func (k *Keeper) DeleteVaultsV2InflightFund(ctx context.Context, id string) error {
	return k.VaultsV2InflightFunds.Remove(ctx, id)
}

// IterateVaultsV2InflightFunds walks all inflight fund entries invoking the supplied callback.
func (k *Keeper) IterateVaultsV2InflightFunds(ctx context.Context, fn func(string, vaultsv2.InflightFund) (bool, error)) error {
	return k.VaultsV2InflightFunds.Walk(ctx, nil, fn)
}
