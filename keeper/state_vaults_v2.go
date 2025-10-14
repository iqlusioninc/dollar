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

// SetVaultsV2EnrolledOracle stores an enrolled oracle configuration.
func (k *Keeper) SetVaultsV2EnrolledOracle(ctx context.Context, oracleID string, oracle vaultsv2.EnrolledOracle) error {
	return k.VaultsV2EnrolledOracles.Set(ctx, oracleID, oracle)
}

// GetVaultsV2EnrolledOracle retrieves an enrolled oracle configuration.
func (k *Keeper) GetVaultsV2EnrolledOracle(ctx context.Context, oracleID string) (vaultsv2.EnrolledOracle, bool, error) {
	oracle, err := k.VaultsV2EnrolledOracles.Get(ctx, oracleID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.EnrolledOracle{}, false, nil
		}
		return vaultsv2.EnrolledOracle{}, false, err
	}

	return oracle, true, nil
}

// DeleteVaultsV2EnrolledOracle removes an enrolled oracle configuration.
func (k *Keeper) DeleteVaultsV2EnrolledOracle(ctx context.Context, oracleID string) error {
	return k.VaultsV2EnrolledOracles.Remove(ctx, oracleID)
}

// IterateVaultsV2EnrolledOracles iterates over all enrolled oracles.
func (k *Keeper) IterateVaultsV2EnrolledOracles(ctx context.Context, fn func(string, vaultsv2.EnrolledOracle) (bool, error)) error {
	return k.VaultsV2EnrolledOracles.Walk(ctx, nil, fn)
}

// GetVaultsV2OracleParams returns the stored oracle governance parameters.
func (k *Keeper) GetVaultsV2OracleParams(ctx context.Context) (vaultsv2.OracleGovernanceParams, error) {
	params, err := k.VaultsV2OracleParams.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.OracleGovernanceParams{}, nil
		}
		return vaultsv2.OracleGovernanceParams{}, err
	}

	return params, nil
}

// SetVaultsV2OracleParams stores oracle governance parameters.
func (k *Keeper) SetVaultsV2OracleParams(ctx context.Context, params vaultsv2.OracleGovernanceParams) error {
	return k.VaultsV2OracleParams.Set(ctx, params)
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

// IterateVaultsV2RemotePositions iterates over all remote positions and calls the provided function for each.
func (k *Keeper) IterateVaultsV2RemotePositions(ctx context.Context, fn func(id uint64, position vaultsv2.RemotePosition) (bool, error)) error {
	return k.VaultsV2RemotePositions.Walk(ctx, nil, fn)
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

func (k *Keeper) getDepositLimits(ctx context.Context) (vaultsv2.DepositLimit, bool, error) {
	limits, err := k.VaultsV2DepositLimits.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.DepositLimit{}, false, nil
		}
		return vaultsv2.DepositLimit{}, false, err
	}
	return limits, true, nil
}

func (k *Keeper) setDepositLimits(ctx context.Context, limits vaultsv2.DepositLimit) error {
	return k.VaultsV2DepositLimits.Set(ctx, limits)
}

// getDepositVelocity is an internal helper that retrieves deposit velocity with a found flag.
// This is used by message handlers that need to distinguish between "not found" and "storage error".
// The second return value (bool) indicates whether the velocity entry exists.
// For query operations, use GetVaultsV2DepositVelocity instead which has simpler error semantics.
func (k *Keeper) getDepositVelocity(ctx context.Context, addr sdk.AccAddress) (vaultsv2.DepositVelocity, bool, error) {
	velocity, err := k.VaultsV2DepositVelocity.Get(ctx, addr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.DepositVelocity{}, false, nil
		}
		return vaultsv2.DepositVelocity{}, false, err
	}
	return velocity, true, nil
}

// setDepositVelocity is an internal helper that sets deposit velocity for a user.
// This is used by message handlers. Takes a typed sdk.AccAddress for convenience.
// For external use, see SetVaultsV2DepositVelocity which takes raw bytes.
func (k *Keeper) setDepositVelocity(ctx context.Context, addr sdk.AccAddress, velocity vaultsv2.DepositVelocity) error {
	return k.VaultsV2DepositVelocity.Set(ctx, addr, velocity)
}

// recordUserDeposit is an internal helper that records a user's deposit at a specific block height.
// This creates an entry in the deposit history used for velocity tracking and malicious deposit detection.
// The history is pruned when users fully exit the vault (see PruneUserDepositHistory).
func (k *Keeper) recordUserDeposit(ctx context.Context, addr sdk.AccAddress, block int64, amount math.Int) error {
	key := collections.Join(addr.Bytes(), block)
	return k.VaultsV2UserDepositHistory.Set(ctx, key, amount)
}

// incrementBlockDeposit is an internal helper that adds to the total deposit volume for a specific block.
// This is used to enforce per-block deposit limits and detect suspicious activity.
// Uses safe addition to prevent overflow.
func (k *Keeper) incrementBlockDeposit(ctx context.Context, block int64, amount math.Int) error {
	current, err := k.VaultsV2BlockDepositVolume.Get(ctx, block)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return k.VaultsV2BlockDepositVolume.Set(ctx, block, amount)
		}
		return err
	}
	updated, err := current.SafeAdd(amount)
	if err != nil {
		return err
	}
	return k.VaultsV2BlockDepositVolume.Set(ctx, block, updated)
}

// getBlockDepositVolume is an internal helper that retrieves the total deposit volume for a specific block.
// Returns zero if no deposits occurred in that block (not an error).
func (k *Keeper) getBlockDepositVolume(ctx context.Context, block int64) (math.Int, error) {
	volume, err := k.VaultsV2BlockDepositVolume.Get(ctx, block)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}
	return volume, nil
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

// DeleteVaultsV2RemotePositionOracle removes the oracle entry for a position.
func (k *Keeper) DeleteVaultsV2RemotePositionOracle(ctx context.Context, positionID uint64) error {
	return k.VaultsV2RemotePositionOracles.Remove(ctx, positionID)
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

// TWAP (Time-Weighted Average Price) Snapshot Management

// NextVaultsV2NAVSnapshotID increments and returns the next NAV snapshot identifier.
func (k *Keeper) NextVaultsV2NAVSnapshotID(ctx context.Context) (int64, error) {
	next, err := k.VaultsV2NAVSnapshotNextID.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return 0, err
		}
		next = 1
	} else {
		next++
	}

	if err := k.VaultsV2NAVSnapshotNextID.Set(ctx, next); err != nil {
		return 0, err
	}

	return next, nil
}

// AddVaultsV2NAVSnapshot records a new NAV snapshot for TWAP calculations.
func (k *Keeper) AddVaultsV2NAVSnapshot(ctx context.Context, snapshot vaultsv2.NAVSnapshot) error {
	id, err := k.NextVaultsV2NAVSnapshotID(ctx)
	if err != nil {
		return err
	}

	return k.VaultsV2NAVSnapshots.Set(ctx, id, snapshot)
}

// GetVaultsV2NAVSnapshot retrieves a specific NAV snapshot by ID.
func (k *Keeper) GetVaultsV2NAVSnapshot(ctx context.Context, id int64) (vaultsv2.NAVSnapshot, bool, error) {
	snapshot, err := k.VaultsV2NAVSnapshots.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.NAVSnapshot{}, false, nil
		}
		return vaultsv2.NAVSnapshot{}, false, err
	}

	return snapshot, true, nil
}

// GetRecentVaultsV2NAVSnapshots retrieves the N most recent NAV snapshots.
// Returns snapshots in reverse chronological order (newest first).
func (k *Keeper) GetRecentVaultsV2NAVSnapshots(ctx context.Context, limit int) ([]vaultsv2.NAVSnapshot, error) {
	if limit <= 0 {
		return nil, nil
	}

	currentID, err := k.VaultsV2NAVSnapshotNextID.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	snapshots := make([]vaultsv2.NAVSnapshot, 0, limit)

	// Iterate backwards from most recent
	for i := currentID - 1; i > 0 && len(snapshots) < limit; i-- {
		snapshot, found, err := k.GetVaultsV2NAVSnapshot(ctx, i)
		if err != nil {
			return nil, err
		}
		if found {
			snapshots = append(snapshots, snapshot)
		}
	}

	return snapshots, nil
}

// PruneOldVaultsV2NAVSnapshots removes snapshots older than the specified age.
// This helps keep storage bounded for TWAP calculations.
func (k *Keeper) PruneOldVaultsV2NAVSnapshots(ctx context.Context, maxAge int64, currentTime int64) (int, error) {
	pruned := 0
	// Early return if chain is too young to prune
	if maxAge >= currentTime {
		return 0, nil // Nothing to prune yet
	}

	cutoffTime := currentTime - maxAge

	// Get current max ID
	currentID, err := k.VaultsV2NAVSnapshotNextID.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}

	// Iterate through all snapshots and remove old ones
	for i := int64(1); i < currentID; i++ {
		snapshot, found, err := k.GetVaultsV2NAVSnapshot(ctx, i)
		if err != nil {
			return pruned, err
		}
		if !found {
			continue
		}

		if snapshot.Timestamp.Unix() < cutoffTime {
			if err := k.VaultsV2NAVSnapshots.Remove(ctx, i); err != nil {
				return pruned, err
			}
			pruned++
		}
	}

	return pruned, nil
}

// GetVaultsV2DepositLimits retrieves the deposit limits configuration.
func (k *Keeper) GetVaultsV2DepositLimits(ctx context.Context) (vaultsv2.DepositLimit, error) {
	limits, err := k.VaultsV2DepositLimits.Get(ctx)
	if err != nil {
		return vaultsv2.DepositLimit{}, err
	}
	return limits, nil
}

// SetVaultsV2DepositLimits sets the deposit limits configuration.
func (k *Keeper) SetVaultsV2DepositLimits(ctx context.Context, limits vaultsv2.DepositLimit) error {
	return k.VaultsV2DepositLimits.Set(ctx, limits)
}

// GetVaultsV2DepositVelocity retrieves the deposit velocity for a user.
// This is the public API used by query handlers and external callers.
// Returns an error if the entry is not found or if there's a storage error.
// For message handlers that need to distinguish "not found" from "error", use the internal getDepositVelocity helper.
func (k *Keeper) GetVaultsV2DepositVelocity(ctx context.Context, user []byte) (vaultsv2.DepositVelocity, error) {
	velocity, err := k.VaultsV2DepositVelocity.Get(ctx, user)
	if err != nil {
		return vaultsv2.DepositVelocity{}, err
	}
	return velocity, nil
}

// SetVaultsV2DepositVelocity sets the deposit velocity for a user.
// This is the public API used by external callers. Takes raw bytes for the user address.
// For message handlers that work with typed addresses, use the internal setDepositVelocity helper.
func (k *Keeper) SetVaultsV2DepositVelocity(ctx context.Context, user []byte, velocity vaultsv2.DepositVelocity) error {
	return k.VaultsV2DepositVelocity.Set(ctx, user, velocity)
}

// isUserPositionEmpty checks if a user has completely exited the vault.
// A position is considered empty when all deposits have been withdrawn and no pending operations exist.
func isUserPositionEmpty(position vaultsv2.UserPosition) bool {
	return position.DepositAmount.IsZero() &&
		position.AccruedYield.IsZero() &&
		position.AmountPendingWithdrawal.IsZero() &&
		position.ActiveWithdrawalRequests == 0
}

// PruneUserDepositHistory removes all deposit history entries for a user.
// This should only be called when the user has fully withdrawn all funds.
// Returns the number of entries pruned and any error encountered.
func (k *Keeper) PruneUserDepositHistory(ctx context.Context, userAddr sdk.AccAddress) (int, error) {
	pruned := 0

	// First, verify the user has no active position
	position, err := k.VaultsV2UserPositions.Get(ctx, userAddr.Bytes())
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// No position exists, safe to prune
		} else {
			return 0, err
		}
	} else if !isUserPositionEmpty(position) {
		// User still has an active position, don't prune
		return 0, nil
	}

	// Collect all deposit history entries for this user
	var toDelete []collections.Pair[[]byte, int64]

	// Create a range that matches all entries for this user
	// The key is Pair[userAddr, blockHeight], so we want all entries where K1() == userAddr
	err = k.VaultsV2UserDepositHistory.Walk(ctx, nil, func(key collections.Pair[[]byte, int64], amount math.Int) (bool, error) {
		// Check if this entry belongs to our user
		if sdk.AccAddress(key.K1()).Equals(userAddr) {
			toDelete = append(toDelete, key)
		}
		return false, nil
	})

	if err != nil {
		return pruned, err
	}

	// Delete all marked entries
	for _, key := range toDelete {
		if err := k.VaultsV2UserDepositHistory.Remove(ctx, key); err != nil {
			return pruned, err
		}
		pruned++
	}

	// Also clean up deposit velocity tracking if position is empty
	if pruned > 0 {
		if err := k.VaultsV2DepositVelocity.Remove(ctx, userAddr.Bytes()); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return pruned, err
		}
	}

	return pruned, nil
}

// GetVaultsV2BlockDepositVolume retrieves the total deposit volume for a specific block.
func (k *Keeper) GetVaultsV2BlockDepositVolume(ctx context.Context, blockHeight int64) (math.Int, error) {
	volume, err := k.VaultsV2BlockDepositVolume.Get(ctx, blockHeight)
	if err != nil {
		return math.ZeroInt(), err
	}
	return volume, nil
}

// SetVaultsV2BlockDepositVolume sets the total deposit volume for a specific block.
func (k *Keeper) SetVaultsV2BlockDepositVolume(ctx context.Context, blockHeight int64, volume math.Int) error {
	return k.VaultsV2BlockDepositVolume.Set(ctx, blockHeight, volume)
}
