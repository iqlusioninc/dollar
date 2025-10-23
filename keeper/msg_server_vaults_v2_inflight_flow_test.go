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
	"testing"
	"time"

	"cosmossdk.io/math"
	hyperlaneutil "github.com/bcp-innovations/hyperlane-cosmos/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)


// TestInflightFundsFlow tests the complete flow:
// 1. Strategist initiates remote position redemption
// 2. Funds are marked as inflight
// 3. Warp route receives funds and marks them as completed
func TestInflightFundsFlow(t *testing.T) {
	keeper, server, _, ctx, _ := setupV2Test(t)

	// Setup test addresses
	strategistAddr := sdk.AccAddress("strategist_address_1")
	processorAddr := sdk.AccAddress("processor_address_1")

	// Setup a cross-chain route
	routeID := uint32(1)
	route := vaultsv2.CrossChainRoute{
		HyptokenId:             hyperlaneutil.HexAddress{0x01},
		ReceiverChainHook:      hyperlaneutil.HexAddress{0x02},
		RemotePositionAddress:  hyperlaneutil.HexAddress{0x03},
		MaxInflightValue:       math.NewInt(1000000 * ONE_V2), // 1M USDN
	}
	err := keeper.SetVaultsV2CrossChainRoute(ctx, routeID, route)
	require.NoError(t, err)

	// Setup a remote position
	positionID := uint64(1)
	position := vaultsv2.RemotePosition{
		HyptokenId:   hyperlaneutil.HexAddress{0x01},
		VaultAddress: hyperlaneutil.HexAddress{0x03},
		SharesHeld:   math.NewInt(100),
		Principal:    math.NewInt(500000 * ONE_V2), // 500K USDN
		SharePrice:   math.LegacyNewDec(5000),     // $5000 per share
		TotalValue:   math.NewInt(500000 * ONE_V2), // 500K USDN total value
		LastUpdate:   time.Now(),
		Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
	}
	err = keeper.SetVaultsV2RemotePosition(ctx, positionID, position)
	require.NoError(t, err)

	// Test amounts
	redemptionAmount := math.NewInt(100000 * ONE_V2) // 100K USDN
	receivedAmount := math.NewInt(99500 * ONE_V2)    // 99.5K USDN (after fees)

	// Step 1: Strategist initiates remote position redemption
	expectedCompletion := time.Now().Add(1 * time.Hour)
	initiateMsg := &vaultsv2.MsgInitiateRemotePositionRedemption{
		Strategist:         strategistAddr.String(),
		PositionId:         positionID,
		Amount:             redemptionAmount,
		RouteId:            routeID,
		ExpectedCompletion: expectedCompletion,
		TransactionId:      "test_redemption_123",
	}

	initiateResp, err := server.InitiateRemotePositionRedemption(ctx, initiateMsg)
	require.NoError(t, err)
	assert.Equal(t, "test_redemption_123", initiateResp.TransactionId)
	assert.Equal(t, routeID, initiateResp.RouteId)
	assert.Equal(t, redemptionAmount, initiateResp.AmountInflight)
	assert.Equal(t, positionID, initiateResp.PositionId)

	// Verify inflight fund was created
	fund, found, err := keeper.GetVaultsV2InflightFund(ctx, "test_redemption_123")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, vaultsv2.INFLIGHT_PENDING, fund.Status)
	assert.Equal(t, redemptionAmount, fund.Amount)
	assert.NotNil(t, fund.GetRemoteOrigin())

	// Verify route inflight value was updated
	routeInflight, err := keeper.GetVaultsV2InflightValueByRoute(ctx, routeID)
	require.NoError(t, err)
	assert.Equal(t, redemptionAmount, routeInflight)

	// Step 2: Process incoming warp funds (simulating strategist sending funds back)
	processMsg := &vaultsv2.MsgProcessIncomingWarpFunds{
		Processor:           processorAddr.String(),
		TransactionId:       "test_redemption_123",
		AmountReceived:      receivedAmount,
		RouteId:             routeID,
		HyperlaneMessageId:  "hyperlane_msg_456",
		OriginDomain:        routeID,
		ReceivedAt:          time.Now(),
	}

	processResp, err := server.ProcessIncomingWarpFunds(ctx, processMsg)
	require.NoError(t, err)
	assert.Equal(t, "test_redemption_123", processResp.TransactionId)
	assert.Equal(t, routeID, processResp.RouteId)
	assert.Equal(t, receivedAmount, processResp.AmountCompleted)
	assert.Equal(t, redemptionAmount, processResp.OriginalAmount)
	assert.False(t, processResp.AmountMatched) // Should be false due to fees

	// Verify inflight fund was marked completed
	fund, found, err = keeper.GetVaultsV2InflightFund(ctx, "test_redemption_123")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, vaultsv2.INFLIGHT_COMPLETED, fund.Status)

	// Verify route inflight value was decremented
	routeInflight, err = keeper.GetVaultsV2InflightValueByRoute(ctx, routeID)
	require.NoError(t, err)
	assert.Equal(t, math.ZeroInt(), routeInflight)

	// Verify pending withdrawal distribution was updated
	pendingDistribution, err := keeper.GetVaultsV2PendingWithdrawalDistribution(ctx)
	require.NoError(t, err)
	assert.Equal(t, receivedAmount, pendingDistribution)

	// Verify remote position was updated
	updatedPosition, found, err := keeper.GetVaultsV2RemotePosition(ctx, positionID)
	require.NoError(t, err)
	assert.True(t, found)
	expectedPrincipal := position.Principal.Sub(redemptionAmount)
	expectedTotalValue := position.TotalValue.Sub(redemptionAmount)
	assert.Equal(t, expectedPrincipal, updatedPosition.Principal)
	assert.Equal(t, expectedTotalValue, updatedPosition.TotalValue)
	assert.Equal(t, vaultsv2.REMOTE_POSITION_ACTIVE, updatedPosition.Status) // Still active
}

// TestInflightFundsFlowValidation tests various validation scenarios
func TestInflightFundsFlowValidation(t *testing.T) {
	keeper, server, _, ctx, _ := setupV2Test(t)

	strategistAddr := sdk.AccAddress("strategist_address_1")
	processorAddr := sdk.AccAddress("processor_address_1")

	// Setup route and position
	routeID := uint32(1)
	route := vaultsv2.CrossChainRoute{
		HyptokenId:             hyperlaneutil.HexAddress{0x01},
		ReceiverChainHook:      hyperlaneutil.HexAddress{0x02},
		RemotePositionAddress:  hyperlaneutil.HexAddress{0x03},
		MaxInflightValue:       math.NewInt(1000000 * ONE_V2),
	}
	err := keeper.SetVaultsV2CrossChainRoute(ctx, routeID, route)
	require.NoError(t, err)

	positionID := uint64(1)
	position := vaultsv2.RemotePosition{
		HyptokenId:   hyperlaneutil.HexAddress{0x01},
		VaultAddress: hyperlaneutil.HexAddress{0x03},
		SharesHeld:   math.NewInt(100),
		Principal:    math.NewInt(500000 * ONE_V2),
		SharePrice:   math.LegacyNewDec(5000),
		TotalValue:   math.NewInt(500000 * ONE_V2),
		LastUpdate:   time.Now(),
		Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
	}
	err = keeper.SetVaultsV2RemotePosition(ctx, positionID, position)
	require.NoError(t, err)

	// Test invalid amounts
	t.Run("InvalidRedemptionAmount", func(t *testing.T) {
		msg := &vaultsv2.MsgInitiateRemotePositionRedemption{
			Strategist:         strategistAddr.String(),
			PositionId:         positionID,
			Amount:             math.ZeroInt(), // Invalid: zero amount
			RouteId:            routeID,
			ExpectedCompletion: time.Now().Add(1 * time.Hour),
		}
		_, err := server.InitiateRemotePositionRedemption(ctx, msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "redemption amount must be positive")
	})

	// Test insufficient position value
	t.Run("InsufficientPositionValue", func(t *testing.T) {
		msg := &vaultsv2.MsgInitiateRemotePositionRedemption{
			Strategist:         strategistAddr.String(),
			PositionId:         positionID,
			Amount:             math.NewInt(1000000 * ONE_V2), // More than position value
			RouteId:            routeID,
			ExpectedCompletion: time.Now().Add(1 * time.Hour),
		}
		_, err := server.InitiateRemotePositionRedemption(ctx, msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient for redemption amount")
	})

	// Test processing non-existent inflight fund
	t.Run("ProcessNonExistentInflightFund", func(t *testing.T) {
		msg := &vaultsv2.MsgProcessIncomingWarpFunds{
			Processor:          processorAddr.String(),
			TransactionId:      "non_existent_123",
			AmountReceived:     math.NewInt(100000 * ONE_V2),
			RouteId:            routeID,
			HyperlaneMessageId: "hyperlane_msg_456",
			OriginDomain:       routeID,
			ReceivedAt:         time.Now(),
		}
		_, err := server.ProcessIncomingWarpFunds(ctx, msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "inflight fund non_existent_123 not found")
	})

	// Test successful redemption and processing completed fund again
	t.Run("ProcessAlreadyCompletedFund", func(t *testing.T) {
		// First, create a successful redemption
		initiateMsg := &vaultsv2.MsgInitiateRemotePositionRedemption{
			Strategist:         strategistAddr.String(),
			PositionId:         positionID,
			Amount:             math.NewInt(100000 * ONE_V2),
			RouteId:            routeID,
			ExpectedCompletion: time.Now().Add(1 * time.Hour),
			TransactionId:      "test_redemption_double",
		}
		_, err := server.InitiateRemotePositionRedemption(ctx, initiateMsg)
		require.NoError(t, err)

		// Process it successfully
		processMsg := &vaultsv2.MsgProcessIncomingWarpFunds{
			Processor:          processorAddr.String(),
			TransactionId:      "test_redemption_double",
			AmountReceived:     math.NewInt(100000 * ONE_V2),
			RouteId:            routeID,
			HyperlaneMessageId: "hyperlane_msg_789",
			OriginDomain:       routeID,
			ReceivedAt:         time.Now(),
		}
		_, err = server.ProcessIncomingWarpFunds(ctx, processMsg)
		require.NoError(t, err)

		// Try to process again - should fail
		_, err = server.ProcessIncomingWarpFunds(ctx, processMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already completed")
	})
}