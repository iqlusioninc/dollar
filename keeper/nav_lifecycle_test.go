// SPDX-License-Identifier: BUSL-1.1
//
// Copyright (C) 2025, NASD Inc. All rights reserved.

package keeper_test

import (
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/core/header"
	sdkmath "cosmossdk.io/math"
	hyperlaneutil "github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

// TestNAVLifecycle tests the complete NAV calculation lifecycle:
// 1. Deposit → LocalFunds increases, NAV increases
// 2. CreateRemotePosition → LocalFunds decreases, RemotePosition created, NAV stays same
// 3. Funds go inflight → InflightFunds increases, NAV stays same
// 4. Oracle updates remote position value → RemotePosition value changes, NAV changes
// 5. User requests withdrawal → Funds locked in position, NAV stays same
// 6. Process withdrawal queue → Withdrawal marked ready, NAV stays same
// 7. User claims withdrawal → Funds leave vault, NAV decreases
//
// This test ensures NAV = LocalFunds + RemotePositions + InflightFunds is correct at each step
func TestNAVLifecycle(t *testing.T) {
	k, vaultsV2Server, _, baseCtx, bob := setupV2Test(t)

	// Increase MaxNavChangeBps for this test since we have large NAV swings
	params, err := k.GetVaultsV2Params(baseCtx)
	require.NoError(t, err)
	params.MaxNavChangeBps = 5000 // 50% to allow large test changes
	require.NoError(t, k.SetVaultsV2Params(baseCtx, params))

	inflightID := uint64(1)
	transactionID := "123"

	// Helper to check NAV invariant
	checkNAV := func(step string, expectedPending, expectedRemote, expectedInflight, expectedTotal sdkmath.Int) {
		t.Helper()
		t.Logf("checkNAV: Starting %s", step)

		// Get local funds funds
		t.Logf("checkNAV: Getting local funds funds for %s", step)
		pending, err := k.GetVaultsV2LocalFunds(baseCtx)
		require.NoError(t, err, "step: %s", step)
		t.Logf("checkNAV: Got pending=%s for %s", pending, step)
		assert.Equal(t, expectedPending.String(), pending.String(),
			"step %s: LocalFunds mismatch", step)

		// Get remote positions total
		t.Logf("checkNAV: Iterating remote positions for %s", step)
		remoteTotal := sdkmath.ZeroInt()
		err = k.IterateVaultsV2RemotePositions(baseCtx, func(_ uint64, pos vaultsv2.RemotePosition) (bool, error) {
			var err error
			remoteTotal, err = remoteTotal.SafeAdd(pos.TotalValue)
			return false, err
		})
		require.NoError(t, err, "step: %s", step)
		t.Logf("checkNAV: Got remote=%s for %s", remoteTotal, step)
		assert.Equal(t, expectedRemote.String(), remoteTotal.String(),
			"step %s: RemotePositions total mismatch", step)

		// Get inflight funds total
		t.Logf("checkNAV: Iterating inflight funds for %s", step)
		inflightTotal := sdkmath.ZeroInt()
		err = k.IterateVaultsV2InflightFunds(baseCtx, func(_ uint64, fund vaultsv2.InflightFund) (bool, error) {
			var err error
			inflightTotal, err = inflightTotal.SafeAdd(fund.Amount)
			return false, err
		})
		require.NoError(t, err, "step: %s", step)
		t.Logf("checkNAV: Got inflight=%s for %s", inflightTotal, step)
		assert.Equal(t, expectedInflight.String(), inflightTotal.String(),
			"step %s: InflightFunds total mismatch", step)

		// Calculate expected NAV
		calculatedNAV := pending.Add(remoteTotal).Add(inflightTotal)
		assert.Equal(t, expectedTotal.String(), calculatedNAV.String(),
			"step %s: NAV = Pending(%s) + Remote(%s) + Inflight(%s) = %s, expected %s",
			step, pending, remoteTotal, inflightTotal, calculatedNAV, expectedTotal)

		// If we call recalculateVaultsV2NAV, it should match
		t.Logf("checkNAV: Calling RecalculateVaultsV2NAV for %s", step)
		timestamp := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		calculatedByFunc, err := k.RecalculateVaultsV2NAV(baseCtx, timestamp, 0)
		require.NoError(t, err, "step: %s", step)
		t.Logf("checkNAV: RecalculateVaultsV2NAV returned %s for %s", calculatedByFunc, step)
		assert.Equal(t, calculatedNAV.String(), calculatedByFunc.String(),
			"step %s: recalculateVaultsV2NAV() returned %s, expected %s",
			step, calculatedByFunc, calculatedNAV)
	}

	// === STEP 1: Initial state - everything is zero ===
	t.Log("Starting step 1: Initial state check")
	checkNAV("1-initial",
		sdkmath.NewInt(0), // pending
		sdkmath.NewInt(0), // remote
		sdkmath.NewInt(0), // inflight
		sdkmath.NewInt(0)) // total NAV
	t.Log("Completed step 1")

	// === STEP 2: User deposits 1000 ===
	require.NoError(t, k.Mint(baseCtx, bob.Bytes, sdkmath.NewInt(1000*ONE_V2), nil))
	_, err = vaultsV2Server.Deposit(baseCtx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// NAV should increase by 1000 (all in LocalFunds)
	checkNAV("2-after-deposit",
		sdkmath.NewInt(1000*ONE_V2), // pending +1000
		sdkmath.NewInt(0),           // remote
		sdkmath.NewInt(0),           // inflight
		sdkmath.NewInt(1000*ONE_V2)) // total NAV = 1000

	// === STEP 3: Deploy 600 to remote position ===
	vaultAddress := hyperlaneutil.CreateMockHexAddress("vault", 1).String()
	createResp, err := vaultsV2Server.CreateRemotePosition(baseCtx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: vaultAddress,
		ChainId:      8453,
		Amount:       sdkmath.NewInt(600 * ONE_V2),
		MinSharesOut: sdkmath.ZeroInt(),
	})
	require.NoError(t, err)
	positionID := createResp.PositionId

	// NAV should stay same: pending decreased, remote increased
	checkNAV("3-after-create-position",
		sdkmath.NewInt(400*ONE_V2),  // pending -600 = 400
		sdkmath.NewInt(600*ONE_V2),  // remote +600 = 600
		sdkmath.NewInt(0),           // inflight
		sdkmath.NewInt(1000*ONE_V2)) // total NAV still 1000

	// === STEP 4: Simulate oracle update - remote position value increases to 660 ===
	position, found, err := k.GetVaultsV2RemotePosition(baseCtx, positionID)
	require.NoError(t, err)
	require.True(t, found)

	// Update position value (simulating oracle NAV update)
	position.TotalValue = sdkmath.NewInt(660 * ONE_V2)
	position.SharePrice = sdkmath.LegacyNewDec(11).QuoInt64(10) // 1.1
	require.NoError(t, k.SetVaultsV2RemotePosition(baseCtx, positionID, position))

	// Update oracle too (simplified - just the fields that exist)
	oracle := vaultsv2.RemotePositionOracle{
		PositionId:    positionID,
		ChainId:       8453,
		OracleAddress: hyperlaneutil.CreateMockHexAddress("oracle", 1),
		SharePrice:    sdkmath.LegacyNewDec(11).QuoInt64(10), // 1.1
		SharesHeld:    sdkmath.NewInt(600 * ONE_V2),
		LastUpdate:    time.Now(),
	}
	require.NoError(t, k.SetVaultsV2RemotePositionOracle(baseCtx, positionID, oracle))

	// NAV should increase by 60 (remote position gained value)
	checkNAV("4-after-oracle-update",
		sdkmath.NewInt(400*ONE_V2),  // pending unchanged
		sdkmath.NewInt(660*ONE_V2),  // remote +60 = 660
		sdkmath.NewInt(0),           // inflight
		sdkmath.NewInt(1060*ONE_V2)) // total NAV = 1060

	// === STEP 5: Create inflight fund (simulating cross-chain transfer) ===
	inflightFund := vaultsv2.InflightFund{
		Id:                inflightID,
		TransactionId:     transactionID,
		Amount:            sdkmath.NewInt(200 * ONE_V2),
		Status:            vaultsv2.INFLIGHT_PENDING,
		InitiatedAt:       time.Now(),
		ExpectedAt:        time.Now().Add(5 * time.Minute),
		ValueAtInitiation: sdkmath.NewInt(200 * ONE_V2),
		Origin: &vaultsv2.InflightFund_NobleOrigin{
			NobleOrigin: &vaultsv2.NobleEndpoint{
				OperationType: vaultsv2.OPERATION_TYPE_DEPOSIT,
			},
		},
		Destination: &vaultsv2.InflightFund_RemoteDestination{
			RemoteDestination: &vaultsv2.RemotePosition{
				VaultAddress: hyperlaneutil.CreateMockHexAddress("vault", 2),
				SharesHeld:   sdkmath.ZeroInt(),
				Principal:    sdkmath.NewInt(200 * ONE_V2),
				SharePrice:   sdkmath.LegacyOneDec(),
				TotalValue:   sdkmath.ZeroInt(),
			},
		},
	}
	require.NoError(t, k.SetVaultsV2InflightFund(baseCtx, inflightFund))

	// Also subtract from local funds to reflect that funds are being deployed
	require.NoError(t, k.SubtractVaultsV2LocalFunds(baseCtx, sdkmath.NewInt(200*ONE_V2)))

	// NAV stays same: pending decreased, inflight increased
	checkNAV("5-after-inflight-created",
		sdkmath.NewInt(200*ONE_V2),  // pending -200 = 200
		sdkmath.NewInt(660*ONE_V2),  // remote unchanged
		sdkmath.NewInt(200*ONE_V2),  // inflight +200 = 200
		sdkmath.NewInt(1060*ONE_V2)) // total NAV still 1060

	// === STEP 6: User makes another deposit ===
	require.NoError(t, k.Mint(baseCtx, bob.Bytes, sdkmath.NewInt(300*ONE_V2), nil))
	_, err = vaultsV2Server.Deposit(baseCtx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(300 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// NAV increases by deposit amount
	checkNAV("6-after-second-deposit",
		sdkmath.NewInt(500*ONE_V2),  // pending +300 = 500
		sdkmath.NewInt(660*ONE_V2),  // remote unchanged
		sdkmath.NewInt(200*ONE_V2),  // inflight unchanged
		sdkmath.NewInt(1360*ONE_V2)) // total NAV = 1360

	// === STEP 7: Inflight completes - moves to remote position ===
	// Remove inflight
	require.NoError(t, k.VaultsV2InflightFunds.Remove(baseCtx, inflightID))

	// Create new remote position for inflight destination
	newPosition := vaultsv2.RemotePosition{
		VaultAddress: hyperlaneutil.CreateMockHexAddress("vault", 2),
		SharesHeld:   sdkmath.NewInt(200 * ONE_V2),
		Principal:    sdkmath.NewInt(200 * ONE_V2),
		SharePrice:   sdkmath.LegacyOneDec(),
		TotalValue:   sdkmath.NewInt(200 * ONE_V2),
		LastUpdate:   time.Now(),
		Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
	}
	require.NoError(t, k.SetVaultsV2RemotePosition(baseCtx, 2, newPosition))

	// NAV stays same: inflight decreased, remote increased
	checkNAV("7-after-inflight-completed",
		sdkmath.NewInt(500*ONE_V2),  // pending unchanged
		sdkmath.NewInt(860*ONE_V2),  // remote +200 = 860
		sdkmath.NewInt(0),           // inflight -200 = 0
		sdkmath.NewInt(1360*ONE_V2)) // total NAV still 1360

	// === STEP 8: Add more funds to existing remote position ===
	additionalRemoteAmount := sdkmath.NewInt(140 * ONE_V2)
	require.NoError(t, k.SubtractVaultsV2LocalFunds(baseCtx, additionalRemoteAmount))

	secondPosition, found, err := k.GetVaultsV2RemotePosition(baseCtx, 2)
	require.NoError(t, err)
	require.True(t, found)

	secondPosition.Principal, err = secondPosition.Principal.SafeAdd(additionalRemoteAmount)
	require.NoError(t, err)
	secondPosition.SharesHeld, err = secondPosition.SharesHeld.SafeAdd(additionalRemoteAmount)
	require.NoError(t, err)
	secondPosition.TotalValue, err = secondPosition.TotalValue.SafeAdd(additionalRemoteAmount)
	require.NoError(t, err)
	secondPosition.LastUpdate = time.Now()
	require.NoError(t, k.SetVaultsV2RemotePosition(baseCtx, 2, secondPosition))

	// NAV should stay same: pending decreased, remote increased
	checkNAV("8-after-remote-top-up",
		sdkmath.NewInt(360*ONE_V2),  // pending -140 = 360
		sdkmath.NewInt(1000*ONE_V2), // remote +140 = 1000
		sdkmath.NewInt(0),           // inflight unchanged
		sdkmath.NewInt(1360*ONE_V2)) // total NAV still 1360

	// === STEP 9: Partial withdrawal from remote position ===
	partialWithdrawal := sdkmath.NewInt(120 * ONE_V2)
	require.NoError(t, k.AddVaultsV2LocalFunds(baseCtx, partialWithdrawal))

	secondPosition, found, err = k.GetVaultsV2RemotePosition(baseCtx, 2)
	require.NoError(t, err)
	require.True(t, found)

	secondPosition.Principal, err = secondPosition.Principal.SafeSub(partialWithdrawal)
	require.NoError(t, err)
	secondPosition.SharesHeld, err = secondPosition.SharesHeld.SafeSub(partialWithdrawal)
	require.NoError(t, err)
	secondPosition.TotalValue, err = secondPosition.TotalValue.SafeSub(partialWithdrawal)
	require.NoError(t, err)
	secondPosition.LastUpdate = time.Now()
	require.NoError(t, k.SetVaultsV2RemotePosition(baseCtx, 2, secondPosition))

	// NAV should stay same: pending increased, remote decreased
	checkNAV("9-after-partial-remote-withdrawal",
		sdkmath.NewInt(480*ONE_V2),  // pending +120 = 480
		sdkmath.NewInt(880*ONE_V2),  // remote -120 = 880
		sdkmath.NewInt(0),           // inflight unchanged
		sdkmath.NewInt(1360*ONE_V2)) // total NAV still 1360

	// === STEP 10: User requests withdrawal ===
	// User wants to withdraw 200 from their position
	withdrawalResp, err := vaultsV2Server.RequestWithdrawal(baseCtx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  bob.Address,
		Amount:     sdkmath.NewInt(200 * ONE_V2),
		PositionId: 1, // First deposit position
	})
	require.NoError(t, err)
	require.NotNil(t, withdrawalResp)

	// NAV should stay same: funds are locked in user position but still in vault
	checkNAV("10-after-withdrawal-request",
		sdkmath.NewInt(480*ONE_V2),  // pending unchanged
		sdkmath.NewInt(880*ONE_V2),  // remote unchanged
		sdkmath.NewInt(0),           // inflight unchanged
		sdkmath.NewInt(1360*ONE_V2)) // total NAV still 1360

	// === STEP 11: Process withdrawal queue ===
	// Move time forward past the withdrawal timeout
	futureCtx := baseCtx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})

	processResp, err := vaultsV2Server.ProcessWithdrawalQueue(futureCtx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})
	require.NoError(t, err)
	require.NotNil(t, processResp)
	assert.Equal(t, int32(1), processResp.RequestsProcessed)
	assert.Equal(t, sdkmath.NewInt(200*ONE_V2), processResp.TotalAmountProcessed)

	// NAV should stay same: funds are marked ready but still in vault
	checkNAV("11-after-withdrawal-processing",
		sdkmath.NewInt(480*ONE_V2),  // pending unchanged
		sdkmath.NewInt(880*ONE_V2),  // remote unchanged
		sdkmath.NewInt(0),           // inflight unchanged
		sdkmath.NewInt(1360*ONE_V2)) // total NAV still 1360

	// Verify withdrawal is now READY
	withdrawal, found, err := k.GetVaultsV2Withdrawal(futureCtx, withdrawalResp.RequestId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, vaultsv2.WITHDRAWAL_REQUEST_STATUS_READY, withdrawal.Status)

	// === STEP 12: User claims withdrawal ===
	claimResp, err := vaultsV2Server.ClaimWithdrawal(futureCtx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   bob.Address,
		RequestId: withdrawalResp.RequestId,
	})
	require.NoError(t, err)
	require.NotNil(t, claimResp)
	assert.Equal(t, sdkmath.NewInt(200*ONE_V2), claimResp.AmountClaimed)

	// NAV should decrease by 200: funds have left the vault
	// The withdrawal comes from local funds funds
	checkNAV("12-after-withdrawal-claimed",
		sdkmath.NewInt(280*ONE_V2),  // pending -200 = 280
		sdkmath.NewInt(880*ONE_V2),  // remote unchanged
		sdkmath.NewInt(0),           // inflight unchanged
		sdkmath.NewInt(1160*ONE_V2)) // total NAV = 1160

	t.Log("✅ NAV lifecycle test passed - NAV correctly calculated at each step including withdrawals!")
}

// TestNAVCalculationWithOracleMessage tests the actual oracle message handling
// and verifies that recalculateVaultsV2NAV is called and NAV is updated correctly
func TestNAVCalculationWithOracleMessage(t *testing.T) {
	k, vaultsV2Server, _, baseCtx, bob := setupV2Test(t)

	// Setup: Deposit and create remote position
	require.NoError(t, k.Mint(baseCtx, bob.Bytes, sdkmath.NewInt(1000*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(baseCtx, &vaultsv2.MsgDeposit{
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

	// Setup oracle
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

	// Initial NAV should be 1000 (200 pending + 800 remote)
	initialNAV, err := k.RecalculateVaultsV2NAV(baseCtx, time.Now(), positionID)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1000*ONE_V2).String(), initialNAV.String())

	// Simulate oracle NAV message: remote position value increases to 880 (10% gain)
	// Create NAV payload bytes manually (105 bytes total)
	payloadBytes := make([]byte, 105)

	// Byte 0: Message type (0x01 for NAV update)
	payloadBytes[0] = 0x01

	// Bytes 1-32: Position ID (32 bytes, big-endian)
	positionIDBytes := make([]byte, 32)
	binary.BigEndian.PutUint64(positionIDBytes[24:], positionID)
	copy(payloadBytes[1:33], positionIDBytes)

	// Bytes 33-64: Share price (1.1 * 1e18 = 1100000000000000000)
	sharePriceBig := big.NewInt(1100000000000000000)
	sharePriceBytes := make([]byte, 32)
	sharePriceBig.FillBytes(sharePriceBytes)
	copy(payloadBytes[33:65], sharePriceBytes)

	// Bytes 65-96: Shares held (800 * 1e6 = 800000000)
	sharesHeldBig := big.NewInt(800 * ONE_V2)
	sharesHeldBytes := make([]byte, 32)
	sharesHeldBig.FillBytes(sharesHeldBytes)
	copy(payloadBytes[65:97], sharesHeldBytes)

	// Bytes 97-104: Timestamp (8 bytes, big-endian)
	timestamp := time.Now().Unix()
	binary.BigEndian.PutUint64(payloadBytes[97:105], uint64(timestamp))

	mailboxID := hyperlaneutil.CreateMockHexAddress("mailbox", 1)
	message := hyperlaneutil.HyperlaneMessage{
		Origin: 8453,
		Sender: oracleAddress,
		Body:   payloadBytes,
	}

	// Handle the oracle message - this should call recalculateVaultsV2NAV internally
	result, err := k.HandleHyperlaneNAVMessage(baseCtx, mailboxID, message)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify NAV was updated: should be 1080 (200 pending + 880 remote)
	assert.Equal(t, sdkmath.NewInt(1080*ONE_V2).String(), result.UpdatedNav.String(),
		"NAV should increase from 1000 to 1080 after oracle update")

	// Verify it's persisted
	navInfo, err := k.GetVaultsV2NAVInfo(baseCtx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1080*ONE_V2).String(), navInfo.CurrentNav.String())

	t.Log("✅ Oracle NAV message correctly updated NAV through recalculateVaultsV2NAV()")
}
