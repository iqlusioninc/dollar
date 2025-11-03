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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
	"dollar.noble.xyz/v3/utils/mocks"
)

// TestProcessIncomingWarpFunds tests processing incoming funds from remote chains
// Note: InitiateRemotePositionRedemption has been removed as it's now handled by the manager on the remote chain
func TestProcessIncomingWarpFunds(t *testing.T) {
	keeper, server, _, ctx, _ := setupV2Test(t)

	inflightID := uint64(1)

	// Setup a cross-chain route
	routeID := uint32(1)
	route := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.HexAddress{0x01},
		ReceiverChainHook:     hyperlaneutil.HexAddress{0x02},
		RemotePositionAddress: hyperlaneutil.HexAddress{0x03},
		MaxInflightValue:      math.NewInt(1000000 * ONE_V2), // 1M USDN
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
		SharePrice:   math.LegacyNewDec(5000),      // $5000 per share
		TotalValue:   math.NewInt(500000 * ONE_V2), // 500K USDN total value
		LastUpdate:   time.Now(),
		Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
	}
	err = keeper.SetVaultsV2RemotePosition(ctx, positionID, position)
	require.NoError(t, err)

	// Test amounts
	redemptionAmount := math.NewInt(100000 * ONE_V2) // 100K USDN
	receivedAmount := math.NewInt(99500 * ONE_V2)    // 99.5K USDN (after fees)

	// Create an inflight fund entry manually (simulating what would have been done by the remote chain manager)
	fund := vaultsv2.InflightFund{
		Id:                inflightID,
		TransactionId:     "",
		Amount:            redemptionAmount,
		Status:            vaultsv2.INFLIGHT_PENDING,
		InitiatedAt:       time.Now(),
		ExpectedAt:        time.Now().Add(1 * time.Hour),
		ValueAtInitiation: redemptionAmount,
		Origin: &vaultsv2.InflightFund_RemoteOrigin{
			RemoteOrigin: &position,
		},
		ProviderTracking: &vaultsv2.ProviderTrackingInfo{
			TrackingInfo: &vaultsv2.ProviderTrackingInfo_HyperlaneTracking{
				HyperlaneTracking: &vaultsv2.HyperlaneTrackingInfo{
					OriginDomain:      routeID,
					DestinationDomain: routeID,
					Nonce:             1,
				},
			},
		},
	}
	err = keeper.SetVaultsV2InflightFund(ctx, fund)
	require.NoError(t, err)

	// Track the inflight value for the route
	err = keeper.AddVaultsV2InflightValueByRoute(ctx, routeID, redemptionAmount)
	require.NoError(t, err)

	// Process incoming warp funds (simulating funds coming back from remote chain)
	processMsg := &vaultsv2.MsgProcessIncomingWarpFunds{
		Processor:          mocks.Authority,
		InflightId:         inflightID,
		TransactionId:      "hyperlane_msg_456",
		AmountReceived:     receivedAmount,
		RouteId:            routeID,
		HyperlaneMessageId: "hyperlane_msg_456",
		OriginDomain:       routeID,
		ReceivedAt:         time.Now(),
	}

	processResp, err := server.ProcessIncomingWarpFunds(ctx, processMsg)
	require.NoError(t, err)
	assert.Equal(t, "hyperlane_msg_456", processResp.TransactionId)
	assert.Equal(t, routeID, processResp.RouteId)
	assert.Equal(t, receivedAmount, processResp.AmountCompleted)
	assert.Equal(t, redemptionAmount, processResp.OriginalAmount)
	assert.False(t, processResp.AmountMatched) // Should be false due to fees

	// Verify inflight fund was marked completed
	fund, found, err := keeper.GetVaultsV2InflightFund(ctx, inflightID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, vaultsv2.INFLIGHT_COMPLETED, fund.Status)

	// Verify route inflight value was decremented
	routeInflight, err := keeper.GetVaultsV2InflightValueByRoute(ctx, routeID)
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

// TestProcessIncomingWarpFundsValidation tests validation scenarios
func TestProcessIncomingWarpFundsValidation(t *testing.T) {
	keeper, server, _, ctx, _ := setupV2Test(t)

	// Setup route and position
	routeID := uint32(1)
	route := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.HexAddress{0x01},
		ReceiverChainHook:     hyperlaneutil.HexAddress{0x02},
		RemotePositionAddress: hyperlaneutil.HexAddress{0x03},
		MaxInflightValue:      math.NewInt(1000000 * ONE_V2),
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

	// Test processing non-existent inflight fund
	t.Run("ProcessNonExistentInflightFund", func(t *testing.T) {
		nonExistentID := uint64(999)
		msg := &vaultsv2.MsgProcessIncomingWarpFunds{
			Processor:          mocks.Authority,
			InflightId:         nonExistentID,
			TransactionId:      "nonexistent_tx",
			AmountReceived:     math.NewInt(100000 * ONE_V2),
			RouteId:            routeID,
			HyperlaneMessageId: "hyperlane_msg_456",
			OriginDomain:       routeID,
			ReceivedAt:         time.Now(),
		}
		_, err := server.ProcessIncomingWarpFunds(ctx, msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "inflight fund 999 not found")
	})

	// Test processing already completed fund
	t.Run("ProcessAlreadyCompletedFund", func(t *testing.T) {
		doubleProcessID := uint64(2)
		// First, create a pending fund
		fund := vaultsv2.InflightFund{
			Id:                doubleProcessID,
			TransactionId:     "",
			Amount:            math.NewInt(100000 * ONE_V2),
			Status:            vaultsv2.INFLIGHT_PENDING,
			InitiatedAt:       time.Now(),
			ExpectedAt:        time.Now().Add(1 * time.Hour),
			ValueAtInitiation: math.NewInt(100000 * ONE_V2),
			Origin: &vaultsv2.InflightFund_RemoteOrigin{
				RemoteOrigin: &position,
			},
			ProviderTracking: &vaultsv2.ProviderTrackingInfo{
				TrackingInfo: &vaultsv2.ProviderTrackingInfo_HyperlaneTracking{
					HyperlaneTracking: &vaultsv2.HyperlaneTrackingInfo{
						OriginDomain:      routeID,
						DestinationDomain: routeID,
						Nonce:             2,
					},
				},
			},
		}
		err := keeper.SetVaultsV2InflightFund(ctx, fund)
		require.NoError(t, err)
		err = keeper.AddVaultsV2InflightValueByRoute(ctx, routeID, fund.Amount)
		require.NoError(t, err)

		// Process it successfully
		processMsg := &vaultsv2.MsgProcessIncomingWarpFunds{
			Processor:          mocks.Authority,
			InflightId:         doubleProcessID,
			TransactionId:      "hyperlane_double_process",
			AmountReceived:     math.NewInt(100000 * ONE_V2),
			RouteId:            routeID,
			HyperlaneMessageId: "hyperlane_msg_456",
			OriginDomain:       routeID,
			ReceivedAt:         time.Now(),
		}
		_, err = server.ProcessIncomingWarpFunds(ctx, processMsg)
		require.NoError(t, err)

		// Try to process it again - should fail
		_, err = server.ProcessIncomingWarpFunds(ctx, processMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already completed")
	})
}

// TestHandleStaleInflight tests handling of stale inflight funds
func TestHandleStaleInflight(t *testing.T) {
	keeper, server, _, ctx, _ := setupV2Test(t)

	staleInflightID := uint64(3)

	// Setup route
	routeID := uint32(1)
	route := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.HexAddress{0x01},
		ReceiverChainHook:     hyperlaneutil.HexAddress{0x02},
		RemotePositionAddress: hyperlaneutil.HexAddress{0x03},
		MaxInflightValue:      math.NewInt(1000000 * ONE_V2),
	}
	err := keeper.SetVaultsV2CrossChainRoute(ctx, routeID, route)
	require.NoError(t, err)

	// Create a stale inflight fund
	staleFund := vaultsv2.InflightFund{
		Id:                staleInflightID,
		TransactionId:     "",
		Amount:            math.NewInt(100000 * ONE_V2),
		Status:            vaultsv2.INFLIGHT_PENDING,
		InitiatedAt:       time.Now().Add(-2 * time.Hour), // 2 hours ago
		ExpectedAt:        time.Now().Add(-1 * time.Hour), // Should have completed 1 hour ago
		ValueAtInitiation: math.NewInt(100000 * ONE_V2),
		Origin: &vaultsv2.InflightFund_RemoteOrigin{
			RemoteOrigin: &vaultsv2.RemotePosition{
				HyptokenId:   route.HyptokenId,
				VaultAddress: route.RemotePositionAddress,
				SharesHeld:   math.NewInt(100),
				Principal:    math.NewInt(100000 * ONE_V2),
				SharePrice:   math.LegacyNewDec(1000),
				TotalValue:   math.NewInt(100000 * ONE_V2),
				LastUpdate:   time.Now(),
				Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
			},
		},
		ProviderTracking: &vaultsv2.ProviderTrackingInfo{
			TrackingInfo: &vaultsv2.ProviderTrackingInfo_HyperlaneTracking{
				HyperlaneTracking: &vaultsv2.HyperlaneTrackingInfo{
					OriginDomain:      routeID,
					DestinationDomain: routeID,
					Nonce:             3,
				},
			},
		},
	}
	err = keeper.SetVaultsV2InflightFund(ctx, staleFund)
	require.NoError(t, err)
	err = keeper.AddVaultsV2InflightValueByRoute(ctx, routeID, staleFund.Amount)
	require.NoError(t, err)

	// Handle stale inflight fund
	msg := &vaultsv2.MsgHandleStaleInflight{
		Authority:  mocks.Authority,
		InflightId: staleInflightID,
		NewStatus:  vaultsv2.INFLIGHT_TIMEOUT,
		Reason:     "Transaction exceeded expected completion time",
	}

	resp, err := server.HandleStaleInflight(ctx, msg)
	require.NoError(t, err)
	assert.Equal(t, staleInflightID, resp.InflightId)
	assert.Equal(t, vaultsv2.INFLIGHT_TIMEOUT, resp.FinalStatus)

	// Verify fund was marked as expired
	fund, found, err := keeper.GetVaultsV2InflightFund(ctx, staleInflightID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, vaultsv2.INFLIGHT_TIMEOUT, fund.Status)

	// Verify route inflight value was cleared
	routeInflight, err := keeper.GetVaultsV2InflightValueByRoute(ctx, routeID)
	require.NoError(t, err)
	assert.Equal(t, math.ZeroInt(), routeInflight)
}
