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
	"fmt"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
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

// GetVaultsV2AUMInfo returns the cached AUM information or a zero-value copy
// when unset.
func (k *Keeper) GetVaultsV2AUMInfo(ctx context.Context) (vaultsv2.AUMInfo, error) {
	aum, err := k.VaultsV2AUMInfo.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.AUMInfo{}, nil
		}
		return vaultsv2.AUMInfo{}, err
	}

	return aum, nil
}

// SetVaultsV2AUMInfo updates the cached AUM information in storage.
func (k *Keeper) SetVaultsV2AUMInfo(ctx context.Context, aum vaultsv2.AUMInfo) error {
	return k.VaultsV2AUMInfo.Set(ctx, aum)
}

// GetVaultsV2UserPosition returns a specific position for the supplied account and position ID.
// The boolean flag indicates whether the position existed in state.
func (k *Keeper) GetVaultsV2UserPosition(ctx context.Context, address sdk.AccAddress, positionID uint64) (vaultsv2.UserPosition, bool, error) {
	store := k.store.OpenKVStore(ctx)
	key := vaultsv2.GetUserPositionKey(address, positionID)

	bz, _ := store.Get(key)
	if bz == nil {
		return vaultsv2.UserPosition{}, false, nil
	}

	var position vaultsv2.UserPosition
	if err := position.Unmarshal(bz); err != nil {
		return vaultsv2.UserPosition{}, false, err
	}

	return position, true, nil
}

// SetVaultsV2UserPosition writes the provided user position to state.
func (k *Keeper) SetVaultsV2UserPosition(ctx context.Context, address sdk.AccAddress, positionID uint64, position vaultsv2.UserPosition) error {
	store := k.store.OpenKVStore(ctx)
	key := vaultsv2.GetUserPositionKey(address, positionID)

	// Ensure position ID is set correctly
	position.PositionId = positionID

	bz, err := position.Marshal()
	if err != nil {
		return err
	}

	store.Set(key, bz)
	return nil
}

// DeleteVaultsV2UserPosition removes an existing position entry from state.
func (k *Keeper) DeleteVaultsV2UserPosition(ctx context.Context, address sdk.AccAddress, positionID uint64) error {
	store := k.store.OpenKVStore(ctx)
	key := vaultsv2.GetUserPositionKey(address, positionID)
	store.Delete(key)
	return nil
}

// GetVaultsV2UserPositionLegacy returns the first position for backward compatibility
// TODO: Remove this once all callers are updated to use position IDs
func (k *Keeper) GetVaultsV2UserPositionLegacy(ctx context.Context, address sdk.AccAddress) (vaultsv2.UserPosition, bool, error) {
	return k.GetVaultsV2UserPosition(ctx, address, 1)
}

// GetUserPositionCount returns the number of positions a user has
func (k *Keeper) GetUserPositionCount(ctx context.Context, address sdk.AccAddress) (uint64, error) {
	store := k.store.OpenKVStore(ctx)
	key := vaultsv2.GetUserPositionCountKey(address)

	bz, _ := store.Get(key)
	if bz == nil {
		return 0, nil
	}

	return sdk.BigEndianToUint64(bz), nil
}

// SetUserPositionCount sets the number of positions a user has
func (k *Keeper) SetUserPositionCount(ctx context.Context, address sdk.AccAddress, count uint64) error {
	store := k.store.OpenKVStore(ctx)
	key := vaultsv2.GetUserPositionCountKey(address)
	store.Set(key, sdk.Uint64ToBigEndian(count))
	return nil
}

// GetNextUserPositionID gets the next position ID for a user and increments it
func (k *Keeper) GetNextUserPositionID(ctx context.Context, address sdk.AccAddress) (uint64, error) {
	store := k.store.OpenKVStore(ctx)
	key := vaultsv2.GetUserNextPositionIDKey(address)

	bz, _ := store.Get(key)
	var nextID uint64 = 1
	if bz != nil {
		nextID = sdk.BigEndianToUint64(bz)
	}

	// Increment and save for next time
	store.Set(key, sdk.Uint64ToBigEndian(nextID+1))

	// Also increment position count
	count, err := k.GetUserPositionCount(ctx, address)
	if err != nil {
		return 0, err
	}
	if err := k.SetUserPositionCount(ctx, address, count+1); err != nil {
		return 0, err
	}

	return nextID, nil
}

// IterateUserPositions walks all positions for a specific user
func (k *Keeper) IterateUserPositions(ctx context.Context, address sdk.AccAddress, fn func(positionID uint64, position vaultsv2.UserPosition) (bool, error)) error {
	store := k.store.OpenKVStore(ctx)
	prefix := append(vaultsv2.UserPositionPrefix, address...)

	iterator, _ := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var position vaultsv2.UserPosition
		if err := position.Unmarshal(iterator.Value()); err != nil {
			return err
		}

		// Extract position ID from the key
		fullKey := iterator.Key()
		_, positionID := vaultsv2.ParseUserPositionKey(fullKey)

		stop, err := fn(positionID, position)
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}

	return nil
}

// IterateVaultsV2UserPositions walks every stored user position and invokes
// the supplied callback. Returning true from the callback stops the iteration
// early.
func (k *Keeper) IterateVaultsV2UserPositions(ctx context.Context, fn func(address sdk.AccAddress, position vaultsv2.UserPosition) (bool, error)) error {
	store := k.store.OpenKVStore(ctx)
	iterator, _ := store.Iterator(vaultsv2.UserPositionPrefix, storetypes.PrefixEndBytes(vaultsv2.UserPositionPrefix))
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var position vaultsv2.UserPosition
		if err := position.Unmarshal(iterator.Value()); err != nil {
			return err
		}

		// Extract address from the key
		fullKey := iterator.Key()
		address, _ := vaultsv2.ParseUserPositionKey(fullKey)

		stop, err := fn(sdk.AccAddress(address), position)
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}

	return nil
}

// IterateVaultsV2UserPositionsPaginated iterates over user positions with pagination support.
// It starts from startAfter (exclusive) and processes up to maxPositions.
// Returns the last processed address (to use as next startAfter), the count of positions processed, and any error.
func (k *Keeper) IterateVaultsV2UserPositionsPaginated(
	ctx context.Context,
	startAfter string,
	maxPositions uint32,
	fn func(address sdk.AccAddress, positionID uint64, position vaultsv2.UserPosition) error,
) (lastProcessed string, count uint32, err error) {
	store := k.store.OpenKVStore(ctx)

	var startKey []byte
	if startAfter != "" {
		// Parse the startAfter as "address:positionID" format
		parts := strings.Split(startAfter, ":")
		if len(parts) != 2 {
			return "", 0, fmt.Errorf("invalid startAfter format, expected 'address:positionID'")
		}

		addr, err := sdk.AccAddressFromBech32(parts[0])
		if err != nil {
			return "", 0, fmt.Errorf("invalid startAfter address: %w", err)
		}

		posID, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return "", 0, fmt.Errorf("invalid startAfter position ID: %w", err)
		}

		// Create the composite key to start after
		startKey = vaultsv2.GetUserPositionKey(addr, posID)
	} else {
		startKey = vaultsv2.UserPositionPrefix
	}

	// Create an iterator for user positions
	iter, err := store.Iterator(startKey, storetypes.PrefixEndBytes(vaultsv2.UserPositionPrefix))
	if err != nil {
		return "", 0, err
	}
	defer iter.Close()

	// Skip the first item if we have a startAfter key (to make it exclusive)
	if startAfter != "" && iter.Valid() {
		iter.Next()
	}

	processed := uint32(0)
	lastProcessedKey := ""

	for ; iter.Valid(); iter.Next() {
		// Parse the composite key to get address and position ID
		addr, positionID := vaultsv2.ParseUserPositionKey(iter.Key())

		var position vaultsv2.UserPosition
		if err := k.cdc.Unmarshal(iter.Value(), &position); err != nil {
			return "", 0, fmt.Errorf("failed to unmarshal position: %w", err)
		}

		if err := fn(sdk.AccAddress(addr), positionID, position); err != nil {
			return "", 0, err
		}

		// Format as "address:positionID" for cursor
		lastProcessedKey = fmt.Sprintf("%s:%d", sdk.AccAddress(addr).String(), positionID)
		processed++

		// Stop if we've hit the limit
		if maxPositions > 0 && processed >= maxPositions {
			break
		}
	}

	return lastProcessedKey, processed, nil
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

// ResetVaultsV2UserPosition is deprecated - use DeleteVaultsV2UserPosition with position ID instead

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
	}

	if accruedYield.IsPositive() {
		if state.TotalAccruedYield, err = state.TotalAccruedYield.SafeAdd(accruedYield); err != nil {
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
	}

	if accruedYield.IsPositive() {
		if state.TotalAccruedYield, err = state.TotalAccruedYield.SafeSub(accruedYield); err != nil {
			return err
		}
	}

	return k.SetVaultsV2VaultState(ctx, state)
}

// GetVaultsV2LocalFunds returns the amount of funds held locally in the vault,
// available for deployment to remote positions or for paying withdrawals.
func (k *Keeper) GetVaultsV2LocalFunds(ctx context.Context) (math.Int, error) {
	amount, err := k.VaultsV2LocalFunds.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	return amount, nil
}

// AddVaultsV2LocalFunds increases the tracked local funds balance by the supplied amount.
func (k *Keeper) AddVaultsV2LocalFunds(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return err
	}

	current, err = current.SafeAdd(amount)
	if err != nil {
		return err
	}

	return k.VaultsV2LocalFunds.Set(ctx, current)
}

// SubtractVaultsV2LocalFunds decreases the local funds balance, removing the entry when it reaches zero.
func (k *Keeper) SubtractVaultsV2LocalFunds(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}

	current, err := k.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return err
	}

	current, err = current.SafeSub(amount)
	if err != nil {
		return err
	}

	if !current.IsPositive() {
		if err := k.VaultsV2LocalFunds.Remove(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		return nil
	}

	return k.VaultsV2LocalFunds.Set(ctx, current)
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

// IncrementVaultsV2TotalPositions increases the tracked position count.
func (k *Keeper) IncrementVaultsV2TotalPositions(ctx context.Context) error {
	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return err
	}

	state.TotalPositions++

	return k.SetVaultsV2VaultState(ctx, state)
}

// DecrementVaultsV2TotalPositions reduces the tracked position count, guarding against
// underflow.
func (k *Keeper) DecrementVaultsV2TotalPositions(ctx context.Context) error {
	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return err
	}

	if state.TotalPositions > 0 {
		state.TotalPositions--
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

func (k *Keeper) setDepositVelocity(ctx context.Context, addr sdk.AccAddress, velocity vaultsv2.DepositVelocity) error {
	return k.VaultsV2DepositVelocity.Set(ctx, addr, velocity)
}

func (k *Keeper) recordUserDeposit(ctx context.Context, addr sdk.AccAddress, block int64, amount math.Int) error {
	key := collections.Join(addr.Bytes(), block)
	return k.VaultsV2UserDepositHistory.Set(ctx, key, amount)
}

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
	if fund.Id == 0 {
		return errors.New("inflight fund identifier cannot be zero")
	}

	return k.VaultsV2InflightFunds.Set(ctx, fund.Id, fund)
}

// GetVaultsV2InflightFund fetches an inflight fund by its identifier.
func (k *Keeper) GetVaultsV2InflightFund(ctx context.Context, id uint64) (vaultsv2.InflightFund, bool, error) {
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
func (k *Keeper) DeleteVaultsV2InflightFund(ctx context.Context, id uint64) error {
	return k.VaultsV2InflightFunds.Remove(ctx, id)
}

// IterateVaultsV2InflightFunds walks all inflight fund entries invoking the supplied callback.
func (k *Keeper) IterateVaultsV2InflightFunds(ctx context.Context, fn func(uint64, vaultsv2.InflightFund) (bool, error)) error {
	return k.VaultsV2InflightFunds.Walk(ctx, nil, fn)
}

// TWAP (Time-Weighted Average Price) Snapshot Management

// NextVaultsV2AUMSnapshotID increments and returns the next AUM snapshot identifier.
func (k *Keeper) NextVaultsV2AUMSnapshotID(ctx context.Context) (int64, error) {
	next, err := k.VaultsV2AUMSnapshotNextID.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return 0, err
		}
		next = 1
	} else {
		next++
	}

	if err := k.VaultsV2AUMSnapshotNextID.Set(ctx, next); err != nil {
		return 0, err
	}

	return next, nil
}

// AddVaultsV2AUMSnapshot records a new AUM snapshot for TWAP calculations.
func (k *Keeper) AddVaultsV2AUMSnapshot(ctx context.Context, snapshot vaultsv2.AUMSnapshot) error {
	id, err := k.NextVaultsV2AUMSnapshotID(ctx)
	if err != nil {
		return err
	}

	return k.VaultsV2AUMSnapshots.Set(ctx, id, snapshot)
}

// GetVaultsV2AUMSnapshot retrieves a specific AUM snapshot by ID.
func (k *Keeper) GetVaultsV2AUMSnapshot(ctx context.Context, id int64) (vaultsv2.AUMSnapshot, bool, error) {
	snapshot, err := k.VaultsV2AUMSnapshots.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.AUMSnapshot{}, false, nil
		}
		return vaultsv2.AUMSnapshot{}, false, err
	}

	return snapshot, true, nil
}

// GetRecentVaultsV2AUMSnapshots retrieves the N most recent AUM snapshots.
// Returns snapshots in reverse chronological order (newest first).
func (k *Keeper) GetRecentVaultsV2AUMSnapshots(ctx context.Context, limit int) ([]vaultsv2.AUMSnapshot, error) {
	if limit <= 0 {
		return nil, nil
	}

	currentID, err := k.VaultsV2AUMSnapshotNextID.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	snapshots := make([]vaultsv2.AUMSnapshot, 0, limit)

	// Iterate backwards from most recent
	for i := currentID - 1; i > 0 && len(snapshots) < limit; i-- {
		snapshot, found, err := k.GetVaultsV2AUMSnapshot(ctx, i)
		if err != nil {
			return nil, err
		}
		if found {
			snapshots = append(snapshots, snapshot)
		}
	}

	return snapshots, nil
}

// PruneOldVaultsV2AUMSnapshots removes snapshots older than the specified age.
// This helps keep storage bounded for TWAP calculations.
// maxAgeInBlocks specifies how many blocks back to keep snapshots
func (k *Keeper) PruneOldVaultsV2AUMSnapshots(ctx context.Context, maxAgeInBlocks int64, currentHeight int64) (int, error) {
	pruned := 0
	cutoffHeight := currentHeight - maxAgeInBlocks

	// Get current max ID
	currentID, err := k.VaultsV2AUMSnapshotNextID.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}

	// Iterate through all snapshots and remove old ones
	for i := int64(1); i < currentID; i++ {
		snapshot, found, err := k.GetVaultsV2AUMSnapshot(ctx, i)
		if err != nil {
			return pruned, err
		}
		if !found {
			continue
		}

		if snapshot.BlockHeight < cutoffHeight {
			if err := k.VaultsV2AUMSnapshots.Remove(ctx, i); err != nil {
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
func (k *Keeper) GetVaultsV2DepositVelocity(ctx context.Context, user []byte) (vaultsv2.DepositVelocity, error) {
	velocity, err := k.VaultsV2DepositVelocity.Get(ctx, user)
	if err != nil {
		return vaultsv2.DepositVelocity{}, err
	}
	return velocity, nil
}

// SetVaultsV2DepositVelocity sets the deposit velocity for a user.
func (k *Keeper) SetVaultsV2DepositVelocity(ctx context.Context, user []byte, velocity vaultsv2.DepositVelocity) error {
	return k.VaultsV2DepositVelocity.Set(ctx, user, velocity)
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

// GetVaultsV2AccountingCursor retrieves the current accounting cursor state.
// Returns a zero-value cursor if not found.
func (k *Keeper) GetVaultsV2AccountingCursor(ctx context.Context) (vaultsv2.AccountingCursor, error) {
	cursor, err := k.VaultsV2AccountingCursor.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.AccountingCursor{}, nil
		}
		return vaultsv2.AccountingCursor{}, err
	}
	return cursor, nil
}

// SetVaultsV2AccountingCursor sets the accounting cursor state.
func (k *Keeper) SetVaultsV2AccountingCursor(ctx context.Context, cursor vaultsv2.AccountingCursor) error {
	return k.VaultsV2AccountingCursor.Set(ctx, cursor)
}

// ClearVaultsV2AccountingCursor removes the accounting cursor from state.
func (k *Keeper) ClearVaultsV2AccountingCursor(ctx context.Context) error {
	return k.VaultsV2AccountingCursor.Remove(ctx)
}

// SetVaultsV2AccountingSnapshot stores a pending accounting update for a user position.
func (k *Keeper) SetVaultsV2AccountingSnapshot(ctx context.Context, snapshot vaultsv2.AccountingSnapshot) error {
	addr, err := sdk.AccAddressFromBech32(snapshot.User)
	if err != nil {
		return fmt.Errorf("invalid user address in snapshot: %w", err)
	}
	// Use composite key with address and position ID
	key := vaultsv2.GetAccountingSnapshotKey(addr.Bytes(), snapshot.PositionId)
	return k.VaultsV2AccountingSnapshots.Set(ctx, key, snapshot)
}

// GetVaultsV2AccountingSnapshot retrieves a pending accounting snapshot for a user position.
func (k *Keeper) GetVaultsV2AccountingSnapshot(ctx context.Context, user sdk.AccAddress, positionID uint64) (vaultsv2.AccountingSnapshot, bool, error) {
	key := vaultsv2.GetAccountingSnapshotKey(user.Bytes(), positionID)
	snapshot, err := k.VaultsV2AccountingSnapshots.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.AccountingSnapshot{}, false, nil
		}
		return vaultsv2.AccountingSnapshot{}, false, err
	}
	return snapshot, true, nil
}

// DeleteVaultsV2AccountingSnapshot removes a pending accounting snapshot for a user position.
func (k *Keeper) DeleteVaultsV2AccountingSnapshot(ctx context.Context, user sdk.AccAddress, positionID uint64) error {
	key := vaultsv2.GetAccountingSnapshotKey(user.Bytes(), positionID)
	return k.VaultsV2AccountingSnapshots.Remove(ctx, key)
}

// IterateVaultsV2AccountingSnapshots walks all pending accounting snapshots.
func (k *Keeper) IterateVaultsV2AccountingSnapshots(ctx context.Context, fn func(snapshot vaultsv2.AccountingSnapshot) (bool, error)) error {
	return k.VaultsV2AccountingSnapshots.Walk(ctx, nil, func(key []byte, snapshot vaultsv2.AccountingSnapshot) (bool, error) {
		return fn(snapshot)
	})
}

// ClearAllVaultsV2AccountingSnapshots removes all pending accounting snapshots.
func (k *Keeper) ClearAllVaultsV2AccountingSnapshots(ctx context.Context) error {
	var keys [][]byte
	err := k.VaultsV2AccountingSnapshots.Walk(ctx, nil, func(key []byte, _ vaultsv2.AccountingSnapshot) (bool, error) {
		keys = append(keys, key)
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, key := range keys {
		if err := k.VaultsV2AccountingSnapshots.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// CommitVaultsV2AccountingSnapshots atomically applies all pending snapshots to actual user positions.
func (k *Keeper) CommitVaultsV2AccountingSnapshots(ctx context.Context) error {
	return k.IterateVaultsV2AccountingSnapshots(ctx, func(snapshot vaultsv2.AccountingSnapshot) (bool, error) {
		addr, err := sdk.AccAddressFromBech32(snapshot.User)
		if err != nil {
			return true, fmt.Errorf("invalid user address in snapshot: %w", err)
		}

		// Get current position using position ID from snapshot
		position, found, err := k.GetVaultsV2UserPosition(ctx, addr, snapshot.PositionId)
		if err != nil {
			return true, err
		}
		if !found {
			return true, fmt.Errorf("position not found for user %s with ID %d during snapshot commit", snapshot.User, snapshot.PositionId)
		}

		// Update only the accounting fields
		position.DepositAmount = snapshot.DepositAmount
		position.AccruedYield = snapshot.AccruedYield

		// Save updated position
		if err := k.SetVaultsV2UserPosition(ctx, addr, snapshot.PositionId, position); err != nil {
			return true, err
		}

		// Delete the snapshot after successful commit
		if err := k.DeleteVaultsV2AccountingSnapshot(ctx, addr, snapshot.PositionId); err != nil {
			return true, err
		}

		return false, nil
	})
}

// IsCircuitBreakerActive returns whether the circuit breaker is currently active
func (k *Keeper) IsCircuitBreakerActive(ctx context.Context) (bool, error) {
	active, err := k.VaultsV2CircuitBreakerActive.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return active, nil
}

// GetLatestCircuitBreakerTrip returns the most recent circuit breaker trip
func (k *Keeper) GetLatestCircuitBreakerTrip(ctx context.Context) (vaultsv2.CircuitBreakerTrip, bool, error) {
	// Get the next ID to be used
	nextID, err := k.VaultsV2CircuitBreakerNextID.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.CircuitBreakerTrip{}, false, nil
		}
		return vaultsv2.CircuitBreakerTrip{}, false, err
	}

	if nextID == 0 {
		return vaultsv2.CircuitBreakerTrip{}, false, nil
	}

	// Get the last trip (nextID - 1)
	trip, err := k.VaultsV2CircuitBreakerTrips.Get(ctx, nextID-1)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return vaultsv2.CircuitBreakerTrip{}, false, nil
		}
		return vaultsv2.CircuitBreakerTrip{}, false, err
	}

	return trip, true, nil
}

// ClearCircuitBreaker deactivates the circuit breaker (requires governance)
func (k *Keeper) ClearCircuitBreaker(ctx context.Context) error {
	return k.VaultsV2CircuitBreakerActive.Remove(ctx)
}

// IterateCircuitBreakerTrips iterates over all circuit breaker trips in chronological order
func (k *Keeper) IterateCircuitBreakerTrips(ctx context.Context, fn func(id uint64, trip vaultsv2.CircuitBreakerTrip) (bool, error)) error {
	return k.VaultsV2CircuitBreakerTrips.Walk(ctx, nil, fn)
}
