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
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
	"dollar.noble.xyz/v3/utils"
)

// TestNAVUpdateYieldTracking_Basic tests basic NAV update and yield accrual
func TestNAVUpdateYieldTracking_Basic(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ACT: Bob deposits 1000 USDN
	depositAmount := sdkmath.NewInt(1000 * ONE_V2)
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       depositAmount,
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ASSERT: Initial position has no yield
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, depositAmount, position.DepositAmount)
	assert.Equal(t, sdkmath.ZeroInt(), position.AccruedYield)

	// ACT: Update NAV with 10% increase (1100 total)
	newNAV := sdkmath.NewInt(1100 * ONE_V2)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: newNAV,
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// ACT: Run accounting to distribute yield
	// Use the message server to run accounting
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Position now has accrued yield
	position, found, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, depositAmount, position.DepositAmount)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), position.AccruedYield)

	// ASSERT: Vault state reflects yield
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, depositAmount, vaultState.TotalDeposits)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), vaultState.TotalAccruedYield)
	assert.Equal(t, newNAV, vaultState.TotalNav)
}

// TestNAVUpdateYieldTracking_MultipleUsers tests yield distribution among multiple users
func TestNAVUpdateYieldTracking_MultipleUsers(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()
	alice.Address = "cosmos1alice_____________"
	alice.Bytes = sdk.AccAddress("alice_______________")

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ACT: Bob deposits 600 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(600 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Alice deposits 400 USDN
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    alice.Address,
		Amount:       sdkmath.NewInt(400 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// Total deposits: 1000 USDN

	// ACT: NAV increases by 20% (1000 → 1200)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1200 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// ACT: Run accounting
	// Use the message server to run accounting
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Bob gets 60% of yield (120 USDN)
	bobPosition, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(600*ONE_V2), bobPosition.DepositAmount)
	assert.Equal(t, sdkmath.NewInt(120*ONE_V2), bobPosition.AccruedYield)

	// ASSERT: Alice gets 40% of yield (80 USDN)
	alicePosition, found, err := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(400*ONE_V2), alicePosition.DepositAmount)
	assert.Equal(t, sdkmath.NewInt(80*ONE_V2), alicePosition.AccruedYield)

	// ASSERT: Total yield matches
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(200*ONE_V2), vaultState.TotalAccruedYield)
}

// TestNAVUpdateYieldTracking_NoYieldPreference tests users who opt out of yield
func TestNAVUpdateYieldTracking_NoYieldPreference(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()
	alice.Address = "cosmos1alice_____________"
	alice.Bytes = sdk.AccAddress("alice_______________")

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ACT: Bob deposits with yield preference
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(500 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Alice deposits WITHOUT yield preference
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    alice.Address,
		Amount:       sdkmath.NewInt(500 * ONE_V2),
		ReceiveYield: false, // Opts out of yield
	})
	require.NoError(t, err)

	// Total deposits: 1000 USDN

	// ACT: NAV increases by 10% (1000 → 1100)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1100 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// ACT: Run accounting
	// Use the message server to run accounting
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Bob gets ALL the yield (100 USDN) since Alice opted out
	bobPosition, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), bobPosition.AccruedYield)

	// ASSERT: Alice gets NO yield
	alicePosition, found, err := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.ZeroInt(), alicePosition.AccruedYield)
}

// TestNAVUpdateYieldTracking_MultipleNAVUpdates tests cumulative yield across multiple NAV updates
func TestNAVUpdateYieldTracking_MultipleNAVUpdates(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ACT: Bob deposits 1000 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: First NAV update: 1000 → 1050 (5% increase)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1050 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Use the message server to run accounting
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: First yield accrued
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(50*ONE_V2), position.AccruedYield)

	// ACT: Second NAV update: 1050 → 1150 (another ~9.5% increase)
	navInfo = vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1150 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Run accounting again
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Yield is cumulative
	position, found, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(150*ONE_V2), position.AccruedYield) // 50 + 100 = 150

	// ASSERT: Vault state reflects total yield
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(150*ONE_V2), vaultState.TotalAccruedYield)
	assert.Equal(t, sdkmath.NewInt(1150*ONE_V2), vaultState.TotalNav)
}

// TestNAVUpdateYieldTracking_WithWithdrawals tests yield tracking with withdrawals
func TestNAVUpdateYieldTracking_WithWithdrawals(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ACT: Bob deposits 1000 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: NAV increases to 1100 (10% yield)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1100 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Use the message server to run accounting
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Bob has 100 USDN yield
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), position.AccruedYield)

	// ACT: Bob requests withdrawal of 500 USDN (half his deposit)
	_, err = vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  bob.Address,
		Amount:     sdkmath.NewInt(500 * ONE_V2),
		PositionId: 1,
	})
	require.NoError(t, err)

	// ACT: NAV increases to 1150 (additional yield on remaining positions)
	navInfo = vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1150 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Run accounting again
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Bob's yield continues to accrue on his remaining deposit
	// He should have 100 (initial) + 50 (new yield on 500 remaining) = 150 total yield
	position, found, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(150*ONE_V2), position.AccruedYield)
}

// TestNAVUpdateYieldTracking_NAVDecrease tests handling of NAV decreases (no negative yield)
func TestNAVUpdateYieldTracking_NAVDecrease(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ACT: Bob deposits 1000 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: First increase NAV to 1100
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1100 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Use the message server to run accounting
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Bob has 100 USDN yield
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), position.AccruedYield)

	// ACT: NAV decreases to 1050 (loss, but still above deposits)
	navInfo = vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1050 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Run accounting again
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Yield should not decrease (no negative yield distribution)
	// Total yield to distribute = 1050 - 1000 = 50 (less than before, but we only track new yield)
	position, found, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	// Yield should remain at previous high watermark
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), position.AccruedYield)
}

// TestNAVUpdateYieldTracking_CursorPagination tests cursor-based accounting with many users
func TestNAVUpdateYieldTracking_CursorPagination(t *testing.T) {
	k, vaultsV2Server, _, ctx, _ := setupV2Test(t)

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// Create 10 users with deposits
	totalDeposits := sdkmath.ZeroInt()
	for i := 0; i < 10; i++ {
		user := utils.TestAccount()
		user.Address = fmt.Sprintf("cosmos1user%d____________", i)
		user.Bytes = sdk.AccAddress(fmt.Sprintf("user%d______________", i))
		depositAmount := sdkmath.NewInt((int64(i+1) * 100 * ONE_V2)) // 100, 200, 300... USDN
		_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
			Depositor:    user.Address,
			Amount:       depositAmount,
			ReceiveYield: true,
		})
		require.NoError(t, err)
		totalDeposits = totalDeposits.Add(depositAmount)
	}
	// Total deposits: 100+200+300+...+1000 = 5500 USDN

	// ACT: NAV increases by 10%
	newNav := totalDeposits.MulRaw(11).QuoRaw(10) // 110% of deposits
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: newNav,
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// ACT: Run accounting with small batch size to test pagination
	totalProcessed := uint64(0)
	for {
		// Process 3 positions at a time
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 3,
		})
		require.NoError(t, err)
		totalProcessed += resp.PositionsProcessed

		if resp.AccountingComplete {
			assert.Equal(t, uint64(10), resp.TotalPositions)
			break
		}
	}

	// ASSERT: All users processed
	assert.Equal(t, uint64(10), totalProcessed)

	// ASSERT: Vault state reflects total yield
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	expectedYield := newNav.Sub(totalDeposits)
	assert.Equal(t, expectedYield, vaultState.TotalAccruedYield)
	assert.Equal(t, newNav, vaultState.TotalNav)
}

// TestNAVUpdateYieldTracking_ResidualHandling tests fair distribution with rounding
func TestNAVUpdateYieldTracking_ResidualHandling(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()
	alice.Address = "cosmos1alice_____________"
	alice.Bytes = sdk.AccAddress("alice_______________")
	charlie := utils.TestAccount()
	charlie.Address = "cosmos1charlie___________"
	charlie.Bytes = sdk.AccAddress("charlie_____________")

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ACT: Three users deposit amounts that will cause rounding
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(333_333_333), // ~333.33 USDN
		ReceiveYield: true,
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    alice.Address,
		Amount:       sdkmath.NewInt(333_333_333), // ~333.33 USDN
		ReceiveYield: true,
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    charlie.Address,
		Amount:       sdkmath.NewInt(333_333_334), // ~333.33 USDN (1 unit more)
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// Total: 1,000,000,000 units (1000 USDN)

	// ACT: Add yield that doesn't divide evenly (100 units total)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1_000_000_100), // 100 units of yield
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Use the message server to run accounting
	_, err = vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
		Manager:      params.Authority,
		MaxPositions: 100,
	})
	require.NoError(t, err)

	// ASSERT: Yield is distributed as fairly as possible
	bobPos, found, _ := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.True(t, found)
	alicePos, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.True(t, found)
	charliePos, found, _ := k.GetVaultsV2UserPosition(ctx, charlie.Bytes, 1)
	require.True(t, found)

	// Total distributed should equal exactly 100
	totalDistributed := bobPos.AccruedYield.Add(alicePos.AccruedYield).Add(charliePos.AccruedYield)
	assert.Equal(t, sdkmath.NewInt(100), totalDistributed)

	// Each should get approximately 33.33 units
	assert.InDelta(t, 33, bobPos.AccruedYield.Int64(), 1)
	assert.InDelta(t, 33, alicePos.AccruedYield.Int64(), 1)
	assert.InDelta(t, 34, charliePos.AccruedYield.Int64(), 1) // Charlie has slightly more deposit
}

// TestYieldTraceWithWithdrawal - detailed trace of yield accounting with withdrawal
func TestYieldTraceWithWithdrawal(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// Mint funds to Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(2000*ONE_V2), nil))

	fmt.Println("\n=== STEP 1: Bob deposits 1000 ===")
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	position, _, _ := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	vaultState, _ := k.GetVaultsV2VaultState(ctx)
	fmt.Printf("Position: deposit=%s, yield=%s, pending=%s\n",
		position.DepositAmount, position.AccruedYield, position.AmountPendingWithdrawal)
	fmt.Printf("Vault: TotalDeposits=%s, TotalYield=%s, TotalNAV=%s\n\n",
		vaultState.TotalDeposits, vaultState.TotalAccruedYield, vaultState.TotalNav)

	fmt.Println("=== STEP 2: NAV increases to 1100, run accounting ===")
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1100 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Call accounting until complete (cursor-based pagination)
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		fmt.Printf("Accounting iteration: complete=%v, positions=%d/%d\n",
			resp.AccountingComplete, resp.PositionsProcessed, resp.TotalPositions)
		if resp.AccountingComplete {
			break
		}
	}

	position, _, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	vaultState, _ = k.GetVaultsV2VaultState(ctx)
	fmt.Printf("Position: deposit=%s, yield=%s, pending=%s\n",
		position.DepositAmount, position.AccruedYield, position.AmountPendingWithdrawal)
	fmt.Printf("Vault: TotalDeposits=%s, TotalYield=%s, TotalNAV=%s\n\n",
		vaultState.TotalDeposits, vaultState.TotalAccruedYield, vaultState.TotalNav)

	fmt.Println("=== STEP 3: Bob requests withdrawal of 500 ===")
	_, err = vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  bob.Address,
		Amount:     sdkmath.NewInt(500 * ONE_V2),
		PositionId: 1,
	})
	require.NoError(t, err)

	position, _, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	vaultState, _ = k.GetVaultsV2VaultState(ctx)
	fmt.Printf("Position: deposit=%s, yield=%s, pending=%s\n",
		position.DepositAmount, position.AccruedYield, position.AmountPendingWithdrawal)
	fmt.Printf("Vault: TotalDeposits=%s, TotalYield=%s, TotalNAV=%s\n\n",
		vaultState.TotalDeposits, vaultState.TotalAccruedYield, vaultState.TotalNav)

	fmt.Println("=== STEP 4: NAV increases to 1150, run accounting ===")
	navInfo = vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1150 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	fmt.Println("BEFORE accounting:")
	fmt.Printf("  CurrentNAV: %s\n", navInfo.CurrentNav)
	fmt.Printf("  VaultState.TotalDeposits: %s\n", vaultState.TotalDeposits)
	fmt.Printf("  totalYieldToDistribute = %s - %s = %s\n",
		navInfo.CurrentNav, vaultState.TotalDeposits,
		navInfo.CurrentNav.Sub(vaultState.TotalDeposits))

	// Call accounting until complete (cursor-based pagination)
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		fmt.Printf("Accounting iteration: complete=%v, positions=%d/%d\n",
			resp.AccountingComplete, resp.PositionsProcessed, resp.TotalPositions)
		if resp.AccountingComplete {
			break
		}
	}

	position, _, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	vaultState, _ = k.GetVaultsV2VaultState(ctx)
	fmt.Printf("\nAFTER accounting:\n")
	fmt.Printf("Position: deposit=%s, yield=%s, pending=%s\n",
		position.DepositAmount, position.AccruedYield, position.AmountPendingWithdrawal)
	fmt.Printf("Vault: TotalDeposits=%s, TotalYield=%s, TotalNAV=%s\n",
		vaultState.TotalDeposits, vaultState.TotalAccruedYield, vaultState.TotalNav)

	fmt.Printf("\n=== ANALYSIS ===\n")
	fmt.Printf("Expected yield: 150\n")
	fmt.Printf("Actual yield: %s\n", position.AccruedYield)
	fmt.Printf("Match: %v\n", position.AccruedYield.Equal(sdkmath.NewInt(150*ONE_V2)))

	// Check invariant
	invariantSum := vaultState.TotalDeposits.Add(vaultState.TotalAccruedYield)
	fmt.Printf("\nInvariant check:\n")
	fmt.Printf("  TotalDeposits + TotalAccruedYield = %s\n", invariantSum)
	fmt.Printf("  TotalNAV = %s\n", vaultState.TotalNav)
	fmt.Printf("  Match: %v\n", invariantSum.Equal(vaultState.TotalNav))
}
