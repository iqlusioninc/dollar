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

package keeper_test

import (
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	hyperlaneutil "github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

func TestCircuitBreakerActivation(t *testing.T) {
	k, _, _, baseCtx, _ := setupV2Test(t)

	// Set NAV change limit to 10% (1000 bps)
	params, err := k.GetVaultsV2Params(baseCtx)
	require.NoError(t, err)
	params.MaxNavChangeBps = 1000
	require.NoError(t, k.SetVaultsV2Params(baseCtx, params))

	// Set initial NAV
	navInfo := vaultsv2.NAVInfo{
		CurrentNav:  sdkmath.NewInt(1000 * ONE_V2),
		PreviousNav: sdkmath.ZeroInt(),
		LastUpdate:  time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(baseCtx, navInfo))

	// Set local funds to 1000
	require.NoError(t, k.AddVaultsV2LocalFunds(baseCtx, sdkmath.NewInt(1000*ONE_V2)))

	// Create a remote position with initial value
	positionID := uint64(1)
	vaultAddress := hyperlaneutil.CreateMockHexAddress("vault", 1)
	position := vaultsv2.RemotePosition{
		VaultAddress: vaultAddress,
		SharePrice:   sdkmath.LegacyOneDec(),
		SharesHeld:   sdkmath.NewInt(1000 * ONE_V2),
		Principal:    sdkmath.NewInt(1000 * ONE_V2),
		TotalValue:   sdkmath.NewInt(1000 * ONE_V2),
		Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
		LastUpdate:   time.Now(),
	}
	require.NoError(t, k.SetVaultsV2RemotePosition(baseCtx, positionID, position))

	// Try to recalculate NAV with a 20% increase (should trigger circuit breaker)
	// Current NAV = 1000 local + 1000 remote = 2000
	// Previous NAV = 1000
	// Change = 100% = 10000 bps > 1000 bps limit
	_, err = k.RecalculateVaultsV2NAV(baseCtx, time.Now(), positionID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "circuit breaker activated")

	// Check circuit breaker is now active
	active, err := k.IsCircuitBreakerActive(baseCtx)
	require.NoError(t, err)
	assert.True(t, active, "circuit breaker should be active")

	// Check latest trip was recorded
	trip, found, err := k.GetLatestCircuitBreakerTrip(baseCtx)
	require.NoError(t, err)
	assert.True(t, found, "should have recorded a trip")
	assert.Equal(t, positionID, trip.RemotePositionId)
	assert.Equal(t, sdkmath.NewInt(1000*ONE_V2), trip.PreviousNav)
	assert.Equal(t, sdkmath.NewInt(2000*ONE_V2), trip.AttemptedNav)
	assert.Equal(t, int32(10000), trip.ChangeBps) // 100% increase = 10000 bps
}

func TestCircuitBreakerNotActivatedWithinLimit(t *testing.T) {
	k, _, _, baseCtx, _ := setupV2Test(t)

	// Set NAV change limit to 20% (2000 bps)
	params, err := k.GetVaultsV2Params(baseCtx)
	require.NoError(t, err)
	params.MaxNavChangeBps = 2000
	require.NoError(t, k.SetVaultsV2Params(baseCtx, params))

	// Set initial NAV
	navInfo := vaultsv2.NAVInfo{
		CurrentNav:  sdkmath.NewInt(1000 * ONE_V2),
		PreviousNav: sdkmath.ZeroInt(),
		LastUpdate:  time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(baseCtx, navInfo))

	// Set local funds
	require.NoError(t, k.AddVaultsV2LocalFunds(baseCtx, sdkmath.NewInt(1100*ONE_V2)))

	// Recalculate NAV with 10% increase (should NOT trigger circuit breaker)
	positionID := uint64(1)
	newNav, err := k.RecalculateVaultsV2NAV(baseCtx, time.Now(), positionID)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1100*ONE_V2), newNav)

	// Check circuit breaker is NOT active
	active, err := k.IsCircuitBreakerActive(baseCtx)
	require.NoError(t, err)
	assert.False(t, active, "circuit breaker should not be active")

	// Check no trip was recorded
	_, found, err := k.GetLatestCircuitBreakerTrip(baseCtx)
	require.NoError(t, err)
	assert.False(t, found, "should not have recorded a trip")
}

func TestCircuitBreakerMultipleTrips(t *testing.T) {
	k, _, _, baseCtx, _ := setupV2Test(t)

	// Set NAV change limit to 5% (500 bps)
	params, err := k.GetVaultsV2Params(baseCtx)
	require.NoError(t, err)
	params.MaxNavChangeBps = 500
	require.NoError(t, k.SetVaultsV2Params(baseCtx, params))

	// First trip
	navInfo1 := vaultsv2.NAVInfo{
		CurrentNav:  sdkmath.NewInt(1000 * ONE_V2),
		PreviousNav: sdkmath.ZeroInt(),
		LastUpdate:  time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(baseCtx, navInfo1))
	require.NoError(t, k.AddVaultsV2LocalFunds(baseCtx, sdkmath.NewInt(1200*ONE_V2)))

	positionID1 := uint64(1)
	_, err = k.RecalculateVaultsV2NAV(baseCtx, time.Now(), positionID1)
	require.Error(t, err)

	trip1, found, err := k.GetLatestCircuitBreakerTrip(baseCtx)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, positionID1, trip1.RemotePositionId)

	// Clear circuit breaker and set up for second trip
	require.NoError(t, k.ClearCircuitBreaker(baseCtx))
	active, err := k.IsCircuitBreakerActive(baseCtx)
	require.NoError(t, err)
	assert.False(t, active)

	// Second trip
	navInfo2 := vaultsv2.NAVInfo{
		CurrentNav:  sdkmath.NewInt(1200 * ONE_V2),
		PreviousNav: sdkmath.ZeroInt(),
		LastUpdate:  time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(baseCtx, navInfo2))
	require.NoError(t, k.SubtractVaultsV2LocalFunds(baseCtx, sdkmath.NewInt(1200*ONE_V2)))
	require.NoError(t, k.AddVaultsV2LocalFunds(baseCtx, sdkmath.NewInt(1500*ONE_V2)))

	positionID2 := uint64(2)
	_, err = k.RecalculateVaultsV2NAV(baseCtx, time.Now(), positionID2)
	require.Error(t, err)

	// Get latest trip (should be trip 2)
	trip2, found, err := k.GetLatestCircuitBreakerTrip(baseCtx)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, positionID2, trip2.RemotePositionId)
	assert.NotEqual(t, trip1.TriggeredAt, trip2.TriggeredAt)

	// Verify both trips exist by iterating
	trips := []vaultsv2.CircuitBreakerTrip{}
	err = k.IterateCircuitBreakerTrips(baseCtx, func(id uint64, trip vaultsv2.CircuitBreakerTrip) (bool, error) {
		trips = append(trips, trip)
		return false, nil
	})
	require.NoError(t, err)
	assert.Len(t, trips, 2, "should have 2 trips recorded")
	assert.Equal(t, positionID1, trips[0].RemotePositionId)
	assert.Equal(t, positionID2, trips[1].RemotePositionId)
}

func TestCircuitBreakerClear(t *testing.T) {
	k, _, _, baseCtx, _ := setupV2Test(t)

	// Trigger circuit breaker
	params, err := k.GetVaultsV2Params(baseCtx)
	require.NoError(t, err)
	params.MaxNavChangeBps = 100
	require.NoError(t, k.SetVaultsV2Params(baseCtx, params))

	navInfo := vaultsv2.NAVInfo{
		CurrentNav:  sdkmath.NewInt(1000 * ONE_V2),
		PreviousNav: sdkmath.ZeroInt(),
		LastUpdate:  time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(baseCtx, navInfo))
	require.NoError(t, k.AddVaultsV2LocalFunds(baseCtx, sdkmath.NewInt(2000*ONE_V2)))

	_, err = k.RecalculateVaultsV2NAV(baseCtx, time.Now(), 1)
	require.Error(t, err)

	active, err := k.IsCircuitBreakerActive(baseCtx)
	require.NoError(t, err)
	require.True(t, active)

	// Clear circuit breaker
	err = k.ClearCircuitBreaker(baseCtx)
	require.NoError(t, err)

	// Verify it's cleared
	active, err = k.IsCircuitBreakerActive(baseCtx)
	require.NoError(t, err)
	assert.False(t, active, "circuit breaker should be inactive after clear")

	// Verify trip history is preserved
	trip, found, err := k.GetLatestCircuitBreakerTrip(baseCtx)
	require.NoError(t, err)
	assert.True(t, found, "trip history should be preserved")
	assert.Equal(t, uint64(1), trip.RemotePositionId)
}

func TestCircuitBreakerWithNegativeChange(t *testing.T) {
	k, _, _, baseCtx, _ := setupV2Test(t)

	// Set NAV change limit to 10% (1000 bps)
	params, err := k.GetVaultsV2Params(baseCtx)
	require.NoError(t, err)
	params.MaxNavChangeBps = 1000
	require.NoError(t, k.SetVaultsV2Params(baseCtx, params))

	// Set initial NAV at 2000
	navInfo := vaultsv2.NAVInfo{
		CurrentNav:  sdkmath.NewInt(2000 * ONE_V2),
		PreviousNav: sdkmath.ZeroInt(),
		LastUpdate:  time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(baseCtx, navInfo))

	// Set local funds to 1500 (25% decrease)
	require.NoError(t, k.AddVaultsV2LocalFunds(baseCtx, sdkmath.NewInt(1500*ONE_V2)))

	// Try to recalculate NAV with 25% decrease (should trigger circuit breaker)
	positionID := uint64(1)
	_, err = k.RecalculateVaultsV2NAV(baseCtx, time.Now(), positionID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "circuit breaker activated")

	// Check circuit breaker is active
	active, err := k.IsCircuitBreakerActive(baseCtx)
	require.NoError(t, err)
	assert.True(t, active)

	// Check trip recorded with negative change
	trip, found, err := k.GetLatestCircuitBreakerTrip(baseCtx)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int32(-2500), trip.ChangeBps) // -25% = -2500 bps
	assert.Equal(t, sdkmath.NewInt(2000*ONE_V2), trip.PreviousNav)
	assert.Equal(t, sdkmath.NewInt(1500*ONE_V2), trip.AttemptedNav)
}

func TestCircuitBreakerEmptyState(t *testing.T) {
	k, _, _, baseCtx, _ := setupV2Test(t)

	// Check circuit breaker is not active in fresh state
	active, err := k.IsCircuitBreakerActive(baseCtx)
	require.NoError(t, err)
	assert.False(t, active)

	// Check no trips exist
	trip, found, err := k.GetLatestCircuitBreakerTrip(baseCtx)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, vaultsv2.CircuitBreakerTrip{}, trip)

	// Check iteration returns nothing
	count := 0
	err = k.IterateCircuitBreakerTrips(baseCtx, func(id uint64, trip vaultsv2.CircuitBreakerTrip) (bool, error) {
		count++
		return false, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCircuitBreakerWithRealOracleFlow(t *testing.T) {
	k, vaultsV2Server, _, baseCtx, bob := setupV2Test(t)

	// Set stricter circuit breaker limit
	params, err := k.GetVaultsV2Params(baseCtx)
	require.NoError(t, err)
	params.MaxNavChangeBps = 1500 // 15%
	require.NoError(t, k.SetVaultsV2Params(baseCtx, params))

	// Setup: Deposit and create remote position
	require.NoError(t, k.Mint(baseCtx, bob.Bytes, sdkmath.NewInt(1000*ONE_V2), nil))
	_, err = vaultsV2Server.Deposit(baseCtx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	vaultAddress := hyperlaneutil.CreateMockHexAddress("vault", 1)
	createResp, err := vaultsV2Server.CreateRemotePosition(baseCtx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: vaultAddress.String(),
		ChainId:      8453,
		Amount:       sdkmath.NewInt(800 * ONE_V2),
		MinSharesOut: sdkmath.ZeroInt(),
	})
	require.NoError(t, err)
	positionID := createResp.PositionId

	// Setup oracle with initial value
	oracleAddress := hyperlaneutil.CreateMockHexAddress("oracle", 1)
	oracle := vaultsv2.RemotePositionOracle{
		PositionId:    positionID,
		ChainId:       8453,
		OracleAddress: oracleAddress,
		SharePrice:    sdkmath.LegacyOneDec(),
		SharesHeld:    sdkmath.NewInt(800 * ONE_V2),
		LastUpdate:    time.Time{},
	}
	require.NoError(t, k.SetVaultsV2RemotePositionOracle(baseCtx, positionID, oracle))

	// Initial NAV should be 1000 (200 local + 800 remote)
	initialNAV, err := k.RecalculateVaultsV2NAV(baseCtx, time.Now(), positionID)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1000*ONE_V2), initialNAV)

	// Simulate oracle reporting 20% gain (should trigger circuit breaker at 15% limit)
	// Create NAV message with 20% increase: 800 * 1.2 = 960
	mailboxID := hyperlaneutil.CreateMockHexAddress("mailbox", 1)
	message := hyperlaneutil.HyperlaneMessage{
		Origin: 8453,
		Sender: oracleAddress,
		Body:   createMockNAVPayload(positionID, sdkmath.LegacyMustNewDecFromStr("1.2"), sdkmath.NewInt(800*ONE_V2)),
	}

	_, err = k.HandleHyperlaneNAVMessage(baseCtx, mailboxID, message)
	require.Error(t, err)
	require.Contains(t, err.Error(), "circuit breaker activated")

	// Verify circuit breaker was triggered
	active, err := k.IsCircuitBreakerActive(baseCtx)
	require.NoError(t, err)
	assert.True(t, active)

	trip, found, err := k.GetLatestCircuitBreakerTrip(baseCtx)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, positionID, trip.RemotePositionId)
	// New NAV would be: 200 local + 960 remote = 1160
	// Previous NAV: 1000
	// Change: 16% = 1600 bps > 1500 bps limit
	assert.Equal(t, int32(1600), trip.ChangeBps)
}

// Helper function to create mock NAV payload
func createMockNAVPayload(positionID uint64, sharePrice sdkmath.LegacyDec, sharesHeld sdkmath.Int) []byte {
	payload := make([]byte, 105)

	// Byte 0: Message type (0x01 for NAV update)
	payload[0] = 0x01

	// Bytes 1-32: Position ID (32 bytes, big-endian)
	positionIDBytes := make([]byte, 32)
	positionIDBig := big.NewInt(int64(positionID))
	positionIDBig.FillBytes(positionIDBytes)
	copy(payload[1:33], positionIDBytes)

	// Bytes 33-64: Share price (32 bytes)
	// Convert share price to 1e18 precision integer
	sharePriceInt := sharePrice.MulInt64(1e18).TruncateInt().BigInt()
	sharePriceBytes := make([]byte, 32)
	sharePriceInt.FillBytes(sharePriceBytes)
	copy(payload[33:65], sharePriceBytes)

	// Bytes 65-96: Shares held (32 bytes)
	sharesHeldBytes := make([]byte, 32)
	sharesHeld.BigInt().FillBytes(sharesHeldBytes)
	copy(payload[65:97], sharesHeldBytes)

	// Bytes 97-104: Timestamp (8 bytes, big-endian)
	timestamp := time.Now().Unix()
	binary.BigEndian.PutUint64(payload[97:105], uint64(timestamp))

	return payload
}
