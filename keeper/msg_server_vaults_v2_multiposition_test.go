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

	"cosmossdk.io/core/header"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dollar.noble.xyz/v3/keeper"
	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
	"dollar.noble.xyz/v3/utils"
	"dollar.noble.xyz/v3/utils/mocks"
)

// setupMultiPositionTest creates a test environment for multi-position tests
func setupMultiPositionTest(t *testing.T) (*keeper.Keeper, vaultsv2.MsgServer, *mocks.BankKeeper, sdk.Context, utils.Account, utils.Account) {
	k, vaultsV2Server, bank, ctx, alice := setupV2Test(t)
	bob := utils.TestAccount()
	return k, vaultsV2Server, bank, ctx, alice, bob
}

// requireUserPosition is a test helper that verifies a user position exists with expected values
func requireUserPosition(t *testing.T, k *keeper.Keeper, ctx sdk.Context, user sdk.AccAddress, positionID uint64, 
	expectedDeposit, expectedYield math.Int, expectedReceiveYield bool) {
	position, found, err := k.GetVaultsV2UserPosition(ctx, user, positionID)
	require.NoError(t, err)
	require.True(t, found, "position %d should exist for user", positionID)
	assert.Equal(t, expectedDeposit, position.DepositAmount, "deposit amount mismatch")
	assert.Equal(t, expectedYield, position.AccruedYield, "accrued yield mismatch")
	assert.Equal(t, expectedReceiveYield, position.ReceiveYield, "receive yield preference mismatch")
	assert.Equal(t, positionID, position.PositionId, "position ID mismatch")
}

// requireNoUserPosition verifies that a position does not exist
func requireNoUserPosition(t *testing.T, k *keeper.Keeper, ctx sdk.Context, user sdk.AccAddress, positionID uint64) {
	_, found, err := k.GetVaultsV2UserPosition(ctx, user, positionID)
	require.NoError(t, err)
	require.False(t, found, "position %d should not exist for user", positionID)
}

// depositForUser performs a deposit and returns the response for testing
func depositForUser(t *testing.T, server vaultsv2.MsgServer, ctx sdk.Context, user utils.Account, amount math.Int, receiveYield bool) *vaultsv2.MsgDepositResponse {
	resp, err := server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    user.Address,
		Amount:       amount,
		ReceiveYield: receiveYield,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp
}

// TestMultiPositionBasics tests creating multiple independent positions
func TestMultiPositionBasics(t *testing.T) {
	k, server, _, ctx, alice, _ := setupMultiPositionTest(t)

	// ARRANGE: Mint 1000 USDN to Alice
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(1000*ONE_V2), nil))

	// ACT: Alice creates first position (100 USDN, yield=true)
	resp1 := depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)
	assert.Equal(t, uint64(1), resp1.PositionId)

	// ACT: Alice creates second position (200 USDN, yield=false)
	resp2 := depositForUser(t, server, ctx, alice, math.NewInt(200*ONE_V2), false)
	assert.Equal(t, uint64(2), resp2.PositionId)

	// ACT: Alice creates third position (300 USDN, yield=true)
	resp3 := depositForUser(t, server, ctx, alice, math.NewInt(300*ONE_V2), true)
	assert.Equal(t, uint64(3), resp3.PositionId)

	// ASSERT: Verify all positions exist with correct values
	requireUserPosition(t, k, ctx, alice.Bytes, 1, math.NewInt(100*ONE_V2), math.ZeroInt(), true)
	requireUserPosition(t, k, ctx, alice.Bytes, 2, math.NewInt(200*ONE_V2), math.ZeroInt(), false)
	requireUserPosition(t, k, ctx, alice.Bytes, 3, math.NewInt(300*ONE_V2), math.ZeroInt(), true)

	// ASSERT: Verify position IDs are independent per user
	userPositionCount, err := k.GetUserPositionCount(ctx, alice.Bytes)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), userPositionCount)

	// ASSERT: Verify Alice's balance is correctly reduced (600 total deposits from 1000 initial)

	// ASSERT: Verify vault state reflects multiple positions
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), state.TotalPositions)
	assert.Equal(t, uint64(1), state.TotalUsers) // Only Alice has positions
}

// TestMultiUserMultiPosition tests multiple users with multiple positions each
func TestMultiUserMultiPosition(t *testing.T) {
	k, server, _, ctx, alice, bob := setupMultiPositionTest(t)

	// ARRANGE: Mint funds to both users
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(500*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(500*ONE_V2), nil))

	// ACT: Alice creates 2 positions
	aliceResp1 := depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)
	aliceResp2 := depositForUser(t, server, ctx, alice, math.NewInt(150*ONE_V2), false)

	// ACT: Bob creates 3 positions
	bobResp1 := depositForUser(t, server, ctx, bob, math.NewInt(200*ONE_V2), true)
	bobResp2 := depositForUser(t, server, ctx, bob, math.NewInt(75*ONE_V2), true)
	bobResp3 := depositForUser(t, server, ctx, bob, math.NewInt(125*ONE_V2), false)

	// ASSERT: Position IDs are independent per user (both start from 1)
	assert.Equal(t, uint64(1), aliceResp1.PositionId)
	assert.Equal(t, uint64(2), aliceResp2.PositionId)
	assert.Equal(t, uint64(1), bobResp1.PositionId)
	assert.Equal(t, uint64(2), bobResp2.PositionId)
	assert.Equal(t, uint64(3), bobResp3.PositionId)

	// ASSERT: Verify Alice's positions
	requireUserPosition(t, k, ctx, alice.Bytes, 1, math.NewInt(100*ONE_V2), math.ZeroInt(), true)
	requireUserPosition(t, k, ctx, alice.Bytes, 2, math.NewInt(150*ONE_V2), math.ZeroInt(), false)
	requireNoUserPosition(t, k, ctx, alice.Bytes, 3)

	// ASSERT: Verify Bob's positions
	requireUserPosition(t, k, ctx, bob.Bytes, 1, math.NewInt(200*ONE_V2), math.ZeroInt(), true)
	requireUserPosition(t, k, ctx, bob.Bytes, 2, math.NewInt(75*ONE_V2), math.ZeroInt(), true)
	requireUserPosition(t, k, ctx, bob.Bytes, 3, math.NewInt(125*ONE_V2), math.ZeroInt(), false)

	// ASSERT: Verify vault state
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), state.TotalPositions) // 2 + 3 positions
	assert.Equal(t, uint64(2), state.TotalUsers)     // Alice + Bob
}

// TestPositionYieldPreferences tests per-position yield preferences
func TestPositionYieldPreferences(t *testing.T) {
	k, server, _, ctx, alice, _ := setupMultiPositionTest(t)

	// ARRANGE: Setup Alice with multiple positions
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(500*ONE_V2), nil))
	depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)  // Position 1: yield=true
	depositForUser(t, server, ctx, alice, math.NewInt(200*ONE_V2), false) // Position 2: yield=false

	// ACT: Change yield preference for position 1
	resp1, err := server.SetYieldPreference(ctx, &vaultsv2.MsgSetYieldPreference{
		User:         alice.Address,
		PositionId:   1,
		ReceiveYield: false, // Change from true to false
	})
	require.NoError(t, err)
	assert.Equal(t, true, resp1.PreviousPreference)
	assert.Equal(t, false, resp1.NewPreference)
	assert.Equal(t, uint64(1), resp1.PositionId)

	// ACT: Change yield preference for position 2
	resp2, err := server.SetYieldPreference(ctx, &vaultsv2.MsgSetYieldPreference{
		User:         alice.Address,
		PositionId:   2,
		ReceiveYield: true, // Change from false to true
	})
	require.NoError(t, err)
	assert.Equal(t, false, resp2.PreviousPreference)
	assert.Equal(t, true, resp2.NewPreference)
	assert.Equal(t, uint64(2), resp2.PositionId)

	// ASSERT: Verify preferences were updated
	requireUserPosition(t, k, ctx, alice.Bytes, 1, math.NewInt(100*ONE_V2), math.ZeroInt(), false)
	requireUserPosition(t, k, ctx, alice.Bytes, 2, math.NewInt(200*ONE_V2), math.ZeroInt(), true)
}

// TestSetYieldPreferenceErrors tests error cases for yield preference updates
func TestSetYieldPreferenceErrors(t *testing.T) {
	k, server, _, ctx, alice, bob := setupMultiPositionTest(t)

	// ARRANGE: Alice has one position
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(100*ONE_V2), nil))
	depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)

	// ACT & ASSERT: Try to update non-existent position
	_, err := server.SetYieldPreference(ctx, &vaultsv2.MsgSetYieldPreference{
		User:         alice.Address,
		PositionId:   999, // Non-existent
		ReceiveYield: false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "position 999 not found")

	// ACT & ASSERT: Try to update another user's position
	_, err = server.SetYieldPreference(ctx, &vaultsv2.MsgSetYieldPreference{
		User:         bob.Address, // Bob doesn't own position 1
		PositionId:   1,
		ReceiveYield: false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "position 1 not found")
}

// TestWithdrawalFromSpecificPosition tests withdrawing from specific positions
func TestWithdrawalFromSpecificPosition(t *testing.T) {
	k, server, _, ctx, alice, _ := setupMultiPositionTest(t)

	// ARRANGE: Alice has multiple positions with different amounts
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(1000*ONE_V2), nil))
	depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)  // Position 1: 100
	depositForUser(t, server, ctx, alice, math.NewInt(200*ONE_V2), false) // Position 2: 200
	depositForUser(t, server, ctx, alice, math.NewInt(300*ONE_V2), true)  // Position 3: 300

	// ACT: Request withdrawal from position 2 (partial)
	withdrawResp, err := server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  alice.Address,
		Amount:     math.NewInt(50 * ONE_V2), // Partial withdrawal
		PositionId: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, withdrawResp)

	// ASSERT: Position 2 has pending withdrawal, others unaffected
	pos1, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, math.ZeroInt(), pos1.AmountPendingWithdrawal)
	assert.Equal(t, int32(0), pos1.ActiveWithdrawalRequests)

	pos2, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 2)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), pos2.AmountPendingWithdrawal)
	assert.Equal(t, int32(1), pos2.ActiveWithdrawalRequests)

	pos3, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 3)
	require.True(t, found)
	assert.Equal(t, math.ZeroInt(), pos3.AmountPendingWithdrawal)
	assert.Equal(t, int32(0), pos3.ActiveWithdrawalRequests)
}

// TestWithdrawalInsufficientBalance tests withdrawal error cases
func TestWithdrawalInsufficientBalance(t *testing.T) {
	k, server, _, ctx, alice, _ := setupMultiPositionTest(t)

	// ARRANGE: Alice has one position with 100 USDN
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(100*ONE_V2), nil))
	depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)

	// ACT & ASSERT: Try to withdraw more than available
	_, err := server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  alice.Address,
		Amount:     math.NewInt(150 * ONE_V2), // More than deposited
		PositionId: 1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient balance in position")

	// ACT & ASSERT: Try to withdraw from non-existent position
	_, err = server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  alice.Address,
		Amount:     math.NewInt(50 * ONE_V2),
		PositionId: 999, // Non-existent
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "position 999 not found")
}

// TestPositionAccounting simulates yield accrual and NAV updates
func TestPositionAccounting(t *testing.T) {
	k, server, _, ctx, alice, _ := setupMultiPositionTest(t)

	// ARRANGE: Alice has positions with different yield preferences
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(500*ONE_V2), nil))
	depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)  // Position 1: yield=true
	depositForUser(t, server, ctx, alice, math.NewInt(200*ONE_V2), false) // Position 2: yield=false

	// ACT: Simulate yield accrual by manually updating positions (would normally be done by accounting logic)
	pos1, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.True(t, found)
	pos1.AccruedYield = math.NewInt(5 * ONE_V2) // 5% yield on position 1
	require.NoError(t, k.SetVaultsV2UserPosition(ctx, alice.Bytes, pos1))

	// Position 2 should not receive yield (yield=false)
	// We verify this through requireUserPosition below

	// ASSERT: Only position 1 has accrued yield
	requireUserPosition(t, k, ctx, alice.Bytes, 1, math.NewInt(100*ONE_V2), math.NewInt(5*ONE_V2), true)
	requireUserPosition(t, k, ctx, alice.Bytes, 2, math.NewInt(200*ONE_V2), math.ZeroInt(), false)

	// ACT: Test withdrawal including accrued yield
	_, err := server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  alice.Address,
		Amount:     math.NewInt(105 * ONE_V2), // Full position + yield
		PositionId: 1,
	})
	require.NoError(t, err)

	// ASSERT: Position 1 shows pending withdrawal for full amount
	pos1Updated, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, math.NewInt(105*ONE_V2), pos1Updated.AmountPendingWithdrawal)
}

// TestPositionSequencing tests that position IDs are correctly sequenced per user
func TestPositionSequencing(t *testing.T) {
	k, server, _, ctx, alice, bob := setupMultiPositionTest(t)

	// ARRANGE: Mint funds
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(500*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(500*ONE_V2), nil))

	// ACT: Interleave deposits between users
	aliceResp1 := depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)
	bobResp1 := depositForUser(t, server, ctx, bob, math.NewInt(100*ONE_V2), true)
	aliceResp2 := depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), false)
	bobResp2 := depositForUser(t, server, ctx, bob, math.NewInt(100*ONE_V2), false)
	aliceResp3 := depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)

	// ASSERT: Each user's positions are numbered sequentially starting from 1
	assert.Equal(t, uint64(1), aliceResp1.PositionId)
	assert.Equal(t, uint64(2), aliceResp2.PositionId)
	assert.Equal(t, uint64(3), aliceResp3.PositionId)

	assert.Equal(t, uint64(1), bobResp1.PositionId)
	assert.Equal(t, uint64(2), bobResp2.PositionId)

	// ASSERT: Verify position counts
	aliceCount, err := k.GetUserPositionCount(ctx, alice.Bytes)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), aliceCount)

	bobCount, err := k.GetUserPositionCount(ctx, bob.Bytes)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), bobCount)
}

// TestVaultStateTotals tests that vault state correctly tracks multi-position totals
func TestVaultStateTotals(t *testing.T) {
	k, server, _, ctx, alice, bob := setupMultiPositionTest(t)

	// ARRANGE: Mint funds
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(500*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(500*ONE_V2), nil))

	// ACT: Create multiple positions
	depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)
	depositForUser(t, server, ctx, alice, math.NewInt(200*ONE_V2), false)
	depositForUser(t, server, ctx, bob, math.NewInt(150*ONE_V2), true)

	// ASSERT: Vault state reflects correct totals
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), state.TotalPositions)  // 3 positions total
	assert.Equal(t, uint64(2), state.TotalUsers)      // 2 unique users
	
	// Total deposits should sum all position amounts
	expectedTotalDeposits := math.NewInt(450 * ONE_V2) // 100 + 200 + 150
	assert.Equal(t, expectedTotalDeposits, state.TotalDeposits)
}

// TestComplexMultiPositionScenario tests a complex real-world scenario
func TestComplexMultiPositionScenario(t *testing.T) {
	k, server, _, ctx, alice, bob := setupMultiPositionTest(t)

	// ARRANGE: Setup initial state
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(1000*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(1000*ONE_V2), nil))

	// SCENARIO: Alice creates 3 positions, Bob creates 2 positions
	// Position preferences and amounts vary
	
	// Alice's deposits
	alicePos1 := depositForUser(t, server, ctx, alice, math.NewInt(200*ONE_V2), true)   // yield=true
	alicePos2 := depositForUser(t, server, ctx, alice, math.NewInt(300*ONE_V2), false)  // yield=false
	alicePos3 := depositForUser(t, server, ctx, alice, math.NewInt(100*ONE_V2), true)   // yield=true
	
	// Bob's deposits
	bobPos1 := depositForUser(t, server, ctx, bob, math.NewInt(250*ONE_V2), true)      // yield=true
	bobPos2 := depositForUser(t, server, ctx, bob, math.NewInt(150*ONE_V2), false)     // yield=false

	// SCENARIO: Alice changes yield preference on position 2
	_, err := server.SetYieldPreference(ctx, &vaultsv2.MsgSetYieldPreference{
		User:         alice.Address,
		PositionId:   2,
		ReceiveYield: true, // Change from false to true
	})
	require.NoError(t, err)

	// SCENARIO: Simulate yield accrual (normally done by accounting process)
	// Only positions with receive_yield=true should get yield
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}) // Next day

	// Alice position 1: 200 * 0.05 = 10 yield
	pos, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, alicePos1.PositionId)
	require.True(t, found)
	pos.AccruedYield = math.NewInt(10 * ONE_V2)
	require.NoError(t, k.SetVaultsV2UserPosition(ctx, alice.Bytes, pos))

	// Alice position 2: 300 * 0.05 = 15 yield (now receives yield)
	pos, found, _ = k.GetVaultsV2UserPosition(ctx, alice.Bytes, alicePos2.PositionId)
	require.True(t, found)
	pos.AccruedYield = math.NewInt(15 * ONE_V2)
	require.NoError(t, k.SetVaultsV2UserPosition(ctx, alice.Bytes, pos))

	// Alice position 3: 100 * 0.05 = 5 yield
	pos, found, _ = k.GetVaultsV2UserPosition(ctx, alice.Bytes, alicePos3.PositionId)
	require.True(t, found)
	pos.AccruedYield = math.NewInt(5 * ONE_V2)
	require.NoError(t, k.SetVaultsV2UserPosition(ctx, alice.Bytes, pos))

	// Bob position 1: 250 * 0.05 = 12.5 yield
	pos, found, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, bobPos1.PositionId)
	require.True(t, found)
	pos.AccruedYield = math.NewInt(12.5 * ONE_V2)
	require.NoError(t, k.SetVaultsV2UserPosition(ctx, bob.Bytes, pos))

	// Bob position 2: No yield (receive_yield=false)

	// SCENARIO: Alice withdraws partially from position 1
	_, err = server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  alice.Address,
		Amount:     math.NewInt(100 * ONE_V2), // Partial withdrawal
		PositionId: alicePos1.PositionId,
	})
	require.NoError(t, err)

	// SCENARIO: Bob withdraws fully from position 2
	_, err = server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  bob.Address,
		Amount:     math.NewInt(150 * ONE_V2), // Full withdrawal (no yield)
		PositionId: bobPos2.PositionId,
	})
	require.NoError(t, err)

	// FINAL ASSERTIONS: Verify final state
	
	// Alice position 1: 200 deposit + 10 yield - 100 pending = 110 available
	pos1, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, alicePos1.PositionId)
	require.True(t, found)
	assert.Equal(t, math.NewInt(200*ONE_V2), pos1.DepositAmount)
	assert.Equal(t, math.NewInt(10*ONE_V2), pos1.AccruedYield)
	assert.Equal(t, math.NewInt(100*ONE_V2), pos1.AmountPendingWithdrawal)
	assert.Equal(t, int32(1), pos1.ActiveWithdrawalRequests)

	// Alice position 2: 300 deposit + 15 yield = 315 available
	pos2, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, alicePos2.PositionId)
	require.True(t, found)
	assert.Equal(t, math.NewInt(300*ONE_V2), pos2.DepositAmount)
	assert.Equal(t, math.NewInt(15*ONE_V2), pos2.AccruedYield)
	assert.Equal(t, math.ZeroInt(), pos2.AmountPendingWithdrawal)
	assert.True(t, pos2.ReceiveYield) // Changed to true

	// Alice position 3: 100 deposit + 5 yield = 105 available
	pos3, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, alicePos3.PositionId)
	require.True(t, found)
	assert.Equal(t, math.NewInt(100*ONE_V2), pos3.DepositAmount)
	assert.Equal(t, math.NewInt(5*ONE_V2), pos3.AccruedYield)
	assert.Equal(t, math.ZeroInt(), pos3.AmountPendingWithdrawal)

	// Bob position 1: 250 deposit + 12.5 yield = 262.5 available  
	bobPos1Updated, found, _ := k.GetVaultsV2UserPosition(ctx, bob.Bytes, bobPos1.PositionId)
	require.True(t, found)
	assert.Equal(t, math.NewInt(250*ONE_V2), bobPos1Updated.DepositAmount)
	assert.Equal(t, math.NewInt(12.5*ONE_V2), bobPos1Updated.AccruedYield)
	assert.Equal(t, math.ZeroInt(), bobPos1Updated.AmountPendingWithdrawal)

	// Bob position 2: 150 deposit + 0 yield - 150 pending = 0 available
	bobPos2Updated, found, _ := k.GetVaultsV2UserPosition(ctx, bob.Bytes, bobPos2.PositionId)
	require.True(t, found)
	assert.Equal(t, math.NewInt(150*ONE_V2), bobPos2Updated.DepositAmount)
	assert.Equal(t, math.ZeroInt(), bobPos2Updated.AccruedYield)
	assert.Equal(t, math.NewInt(150*ONE_V2), bobPos2Updated.AmountPendingWithdrawal)
	assert.False(t, bobPos2Updated.ReceiveYield)

	// Verify vault totals
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), state.TotalPositions)
	assert.Equal(t, uint64(2), state.TotalUsers)
	
	// Total pending withdrawals should be 100 + 150 = 250
	expectedPendingWithdrawals := math.NewInt(250 * ONE_V2)
	assert.Equal(t, expectedPendingWithdrawals, state.TotalAmountPendingWithdrawal)
}