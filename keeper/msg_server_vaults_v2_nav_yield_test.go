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

	sdkmath "cosmossdk.io/math"
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

	// ARRANGE: Mint tokens for Bob
	depositAmount := sdkmath.NewInt(1000 * ONE_V2)
	require.NoError(t, k.Mint(ctx, bob.Bytes, depositAmount, nil))

	// ACT: Bob deposits 1000 USDN
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

	// ACT: Initialize vault state by running accounting at deposit level (no yield yet)
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: depositAmount,
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))

	// Run accounting to initialize VaultState.TotalDeposits
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ACT: Update NAV with 10% increase (1100 total)
	newNAV := sdkmath.NewInt(1100 * ONE_V2)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: newNAV,
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// ACT: Run accounting to distribute yield
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint tokens for Bob and Alice
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(600*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, alice.Bytes, sdkmath.NewInt(400*ONE_V2), nil))

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

	// ACT: Initialize vault state by running accounting at deposit level (no yield yet)
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1000 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))

	// Run accounting to initialize VaultState.TotalDeposits
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ACT: NAV increases by 20% (1000 → 1200)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1200 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// ACT: Run accounting
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint funds for both users
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(500*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, alice.Bytes, sdkmath.NewInt(500*ONE_V2), nil))

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

	// ACT: Initialize vault state by running accounting at current deposit level
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1000 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))

	// Run accounting until complete
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ACT: NAV increases by 10% (1000 → 1100)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1100 * ONE_V2),
		LastUpdate: time.Now().Add(time.Hour),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// ACT: Run accounting to distribute yield
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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

	// ARRANGE: Mint tokens for Bob
	depositAmount := sdkmath.NewInt(1000 * ONE_V2)
	require.NoError(t, k.Mint(ctx, bob.Bytes, depositAmount, nil))

	// ACT: Bob deposits 1000 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       depositAmount,
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Initialize vault state
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: depositAmount,
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ACT: First NAV update: 1000 → 1050 (5% increase)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1050 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Use the message server to run accounting
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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

	// ARRANGE: Mint tokens for Bob
	depositAmount := sdkmath.NewInt(1000 * ONE_V2)
	require.NoError(t, k.Mint(ctx, bob.Bytes, depositAmount, nil))

	// ACT: Bob deposits 1000 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       depositAmount,
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Initialize vault state
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: depositAmount,
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ACT: NAV increases to 1100 (10% yield)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1100 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Use the message server to run accounting
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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

	// ARRANGE: Mint tokens for Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(1000*ONE_V2), nil))

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
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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
		depositAmount := sdkmath.NewInt((int64(i+1) * 100 * ONE_V2)) // 100, 200, 300... USDN

		// Mint tokens for user
		require.NoError(t, k.Mint(ctx, user.Bytes, depositAmount, nil))

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
	batchSize := uint32(3) // Process 3 positions per call
	positionsProcessed := uint64(0)
	iterations := 0
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: batchSize,
		})
		require.NoError(t, err)
		iterations++

		if resp.PositionsProcessed > 0 {
			positionsProcessed += resp.PositionsProcessed
		}

		if resp.AccountingComplete {
			break
		}

		// Safety check to avoid infinite loop
		require.Less(t, iterations, 20, "too many iterations, possible infinite loop")
	}

	// ASSERT: All positions were processed
	assert.Equal(t, uint64(10), positionsProcessed, "should process all 10 positions")

	// ASSERT: Total yield distributed matches expected (10% of deposits)
	expectedYield := newNav.Sub(totalDeposits)
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedYield, vaultState.TotalAccruedYield)
	assert.Equal(t, newNav, vaultState.TotalNav)
}

// TestNAVUpdateYieldTracking_ResidualHandling tests fair distribution with rounding
func TestNAVUpdateYieldTracking_ResidualHandling(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()
	charlie := utils.TestAccount()

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint tokens for all users
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(333_333_333), nil))
	require.NoError(t, k.Mint(ctx, alice.Bytes, sdkmath.NewInt(333_333_333), nil))
	require.NoError(t, k.Mint(ctx, charlie.Bytes, sdkmath.NewInt(333_333_334), nil))

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

	// ACT: Initialize vault state
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1_000_000_000),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ACT: Add yield that doesn't divide evenly (100 units total)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1_000_000_100), // 100 units of yield
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Use the message server to run accounting
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

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

// TestYieldTraceWithWithdrawal comprehensive trace test showing that pending withdrawals
// are excluded from yield calculation while still being part of the position
func TestYieldTraceWithWithdrawal(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Get params
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint tokens for Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(1000*ONE_V2), nil))

	// === STEP 1: Bob deposits 1000 ===
	t.Log("\n=== STEP 1: Bob deposits 1000 ===")
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	position, _, _ := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	t.Logf("Position: deposit=%d, yield=%d, pending=%d",
		position.DepositAmount.Int64(), position.AccruedYield.Int64(), position.AmountPendingWithdrawal.Int64())

	// Initialize accounting
	navInfo := vaultsv2.NAVInfo{CurrentNav: sdkmath.NewInt(1000 * ONE_V2), LastUpdate: time.Now()}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))
	for {
		resp, _ := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager: params.Authority, MaxPositions: 100,
		})
		if resp.AccountingComplete {
			break
		}
	}

	vaultState, _ := k.GetVaultsV2VaultState(ctx)
	t.Logf("Vault: TotalDeposits=%d, TotalYield=%d, TotalNAV=%d",
		vaultState.TotalDeposits.Int64(), vaultState.TotalAccruedYield.Int64(), vaultState.TotalNav.Int64())

	// === STEP 2: NAV increases to 1100, run accounting ===
	t.Log("\n=== STEP 2: NAV increases to 1100, run accounting ===")
	navInfo = vaultsv2.NAVInfo{CurrentNav: sdkmath.NewInt(1100 * ONE_V2), LastUpdate: time.Now()}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	for {
		resp, _ := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager: params.Authority, MaxPositions: 100,
		})
		t.Logf("Accounting iteration: complete=%t, positions=%d/%d",
			resp.AccountingComplete, resp.PositionsProcessed, resp.TotalPositionsProcessed)
		if resp.AccountingComplete {
			break
		}
	}

	position, _, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	vaultState, _ = k.GetVaultsV2VaultState(ctx)
	t.Logf("Position: deposit=%d, yield=%d, pending=%d",
		position.DepositAmount.Int64(), position.AccruedYield.Int64(), position.AmountPendingWithdrawal.Int64())
	t.Logf("Vault: TotalDeposits=%d, TotalYield=%d, TotalNAV=%d",
		vaultState.TotalDeposits.Int64(), vaultState.TotalAccruedYield.Int64(), vaultState.TotalNav.Int64())

	// === STEP 3: Bob requests withdrawal of 500 ===
	t.Log("\n=== STEP 3: Bob requests withdrawal of 500 ===")
	_, err = vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address, Amount: sdkmath.NewInt(500 * ONE_V2), PositionId: 1,
	})
	require.NoError(t, err)

	position, _, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	vaultState, _ = k.GetVaultsV2VaultState(ctx)
	t.Logf("Position: deposit=%d, yield=%d, pending=%d",
		position.DepositAmount.Int64(), position.AccruedYield.Int64(), position.AmountPendingWithdrawal.Int64())
	t.Logf("Vault: TotalDeposits=%d, TotalYield=%d, TotalNAV=%d",
		vaultState.TotalDeposits.Int64(), vaultState.TotalAccruedYield.Int64(), vaultState.TotalNav.Int64())

	// === STEP 4: NAV increases to 1150, run accounting ===
	t.Log("\n=== STEP 4: NAV increases to 1150, run accounting ===")
	t.Logf("BEFORE accounting:")
	t.Logf("  CurrentNAV: %d", 1150*ONE_V2)
	t.Logf("  VaultState.TotalDeposits: %d", vaultState.TotalDeposits.Int64())
	t.Logf("  totalYieldToDistribute = %d - %d = %d", 1150*ONE_V2, vaultState.TotalDeposits.Int64(),
		1150*ONE_V2-vaultState.TotalDeposits.Int64())

	navInfo = vaultsv2.NAVInfo{CurrentNav: sdkmath.NewInt(1150 * ONE_V2), LastUpdate: time.Now()}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	for {
		resp, _ := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager: params.Authority, MaxPositions: 100,
		})
		t.Logf("Accounting iteration: complete=%t, positions=%d/%d",
			resp.AccountingComplete, resp.PositionsProcessed, resp.TotalPositionsProcessed)
		if resp.AccountingComplete {
			break
		}
	}

	t.Log("\nAFTER accounting:")
	position, _, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	vaultState, _ = k.GetVaultsV2VaultState(ctx)
	t.Logf("Position: deposit=%d, yield=%d, pending=%d",
		position.DepositAmount.Int64(), position.AccruedYield.Int64(), position.AmountPendingWithdrawal.Int64())
	t.Logf("Vault: TotalDeposits=%d, TotalYield=%d, TotalNAV=%d",
		vaultState.TotalDeposits.Int64(), vaultState.TotalAccruedYield.Int64(), vaultState.TotalNav.Int64())

	// === ANALYSIS ===
	t.Log("\n=== ANALYSIS ===")
	expectedYield := int64(150)
	actualYield := position.AccruedYield.Int64() / ONE_V2
	t.Logf("Expected yield: %d", expectedYield)
	t.Logf("Actual yield: %d", position.AccruedYield.Int64())
	t.Logf("Match: %t", expectedYield == actualYield)

	t.Log("\nInvariant check:")
	t.Logf("  TotalDeposits + TotalAccruedYield = %d",
		vaultState.TotalDeposits.Add(vaultState.TotalAccruedYield).Int64())
	t.Logf("  TotalNAV = %d", vaultState.TotalNav.Int64())
	t.Logf("  Match: %t",
		vaultState.TotalDeposits.Add(vaultState.TotalAccruedYield).Equal(vaultState.TotalNav))
}

// TestAccountingSequentialSessions tests running accounting twice with different NAVs
// to ensure a new cursor/accounting session can be started after a previous one completes
func TestAccountingSequentialSessions(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint tokens
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(1000*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, alice.Bytes, sdkmath.NewInt(500*ONE_V2), nil))

	// ACT: Bob deposits 1000, Alice deposits 500
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    alice.Address,
		Amount:       sdkmath.NewInt(500 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// Total deposits: 1500 USDN

	// === SESSION 1: NAV = 1650 (150 yield to distribute) ===
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1650 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Run accounting until complete
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ASSERT: First session completed and cursor is cleared
	cursor, err := k.GetVaultsV2AccountingCursor(ctx)
	require.NoError(t, err)
	assert.False(t, cursor.InProgress, "cursor should not be in progress after session 1 completes")
	assert.Equal(t, "", cursor.LastProcessedUser, "cursor should be cleared after completion")

	// ASSERT: Verify yield was distributed correctly in session 1
	// Bob: 1000/1500 * 150 = 100 USDN
	// Alice: 500/1500 * 150 = 50 USDN
	bobPos1, found, _ := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(1000*ONE_V2), bobPos1.DepositAmount)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), bobPos1.AccruedYield, "Bob should have 100 USDN yield from session 1")

	alicePos1, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(500*ONE_V2), alicePos1.DepositAmount)
	assert.Equal(t, sdkmath.NewInt(50*ONE_V2), alicePos1.AccruedYield, "Alice should have 50 USDN yield from session 1")

	// ASSERT: Verify vault state after session 1
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1500*ONE_V2), vaultState.TotalDeposits, "total deposits should be 1500")
	assert.Equal(t, sdkmath.NewInt(150*ONE_V2), vaultState.TotalAccruedYield, "total yield should be 150")
	assert.Equal(t, sdkmath.NewInt(1650*ONE_V2), vaultState.TotalNav, "total NAV should be 1650")

	// === SESSION 2: NAV = 1815 (additional 165 yield to distribute) ===
	navInfo2 := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1815 * ONE_V2),
		LastUpdate: time.Now().Add(time.Hour), // Later timestamp
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo2))

	// Run accounting again for session 2
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ASSERT: Second session completed and cursor is cleared
	cursor, err = k.GetVaultsV2AccountingCursor(ctx)
	require.NoError(t, err)
	assert.False(t, cursor.InProgress, "cursor should not be in progress after session 2 completes")
	assert.Equal(t, "", cursor.LastProcessedUser, "cursor should be cleared after completion")

	// ASSERT: Verify yield accumulated correctly in session 2
	// New yield to distribute: 1815 - 1500 - 150 = 165 USDN
	// Bob: existing 100 + (1000/1500 * 165) = 100 + 110 = 210 USDN
	// Alice: existing 50 + (500/1500 * 165) = 50 + 55 = 105 USDN
	bobPos2, found, _ := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(1000*ONE_V2), bobPos2.DepositAmount)
	assert.Equal(t, sdkmath.NewInt(210*ONE_V2), bobPos2.AccruedYield, "Bob should have 210 USDN yield (100 + 110)")

	alicePos2, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(500*ONE_V2), alicePos2.DepositAmount)
	assert.Equal(t, sdkmath.NewInt(105*ONE_V2), alicePos2.AccruedYield, "Alice should have 105 USDN yield (50 + 55)")

	// ASSERT: Verify vault state after session 2
	vaultState, err = k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1500*ONE_V2), vaultState.TotalDeposits, "total deposits should still be 1500")
	assert.Equal(t, sdkmath.NewInt(315*ONE_V2), vaultState.TotalAccruedYield, "total yield should be 315 (150 + 165)")
	assert.Equal(t, sdkmath.NewInt(1815*ONE_V2), vaultState.TotalNav, "total NAV should be 1815")
}

// TestAccountingMixedYieldPreference_SameUser tests a user with multiple positions with different yield preferences
func TestAccountingMixedYieldPreference_SameUser(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint tokens for Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(2000*ONE_V2), nil))

	// ACT: Bob creates two positions - one with yield, one without
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true, // Position 1: wants yield
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: false, // Position 2: opts out
	})
	require.NoError(t, err)

	// ACT: Initialize vault state
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(2000 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ACT: NAV increases by 10% (2000 → 2200, 200 yield)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(2200 * ONE_V2),
		LastUpdate: time.Now().Add(time.Hour),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Run accounting
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ASSERT: Position 1 gets ALL the yield (200 USDN)
	pos1, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(200*ONE_V2), pos1.AccruedYield, "position 1 should get all yield")

	// ASSERT: Position 2 gets NO yield
	pos2, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 2)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.ZeroInt(), pos2.AccruedYield, "position 2 should get no yield")

	// ASSERT: Total yield distributed = 200
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(200*ONE_V2), vaultState.TotalAccruedYield)
}

// TestAccountingYieldPreferenceChange tests changing yield preference between accounting sessions
func TestAccountingYieldPreferenceChange(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Get params (accounting is always enabled in v2)
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint tokens for Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(1000*ONE_V2), nil))

	// ACT: Bob deposits with yield preference
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Initialize vault state
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1000 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ACT: NAV increases to 1100 (100 yield)
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1100 * ONE_V2),
		LastUpdate: time.Now().Add(time.Hour),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Run accounting
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ASSERT: Bob received 100 USDN yield
	bobPos, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), bobPos.AccruedYield)

	// ACT: Bob changes his yield preference to false
	_, err = vaultsV2Server.SetYieldPreference(ctx, &vaultsv2.MsgSetYieldPreference{
		User:         bob.Address,
		PositionId:   1,
		ReceiveYield: false, // Opt out of future yield
	})
	require.NoError(t, err)

	// ACT: NAV increases to 1200 (additional 100 yield available)
	navInfo = vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1200 * ONE_V2),
		LastUpdate: time.Now().Add(2 * time.Hour),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// Run accounting again
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ASSERT: Bob's yield should remain at 100 USDN (no new yield since preference is false)
	bobPos, found, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), bobPos.AccruedYield, "yield should remain at 100 USDN, no new yield accrued")

	// ASSERT: Verify vault state - yield distributed should still be 100 (no new yield distributed)
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1000*ONE_V2), vaultState.TotalDeposits)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), vaultState.TotalAccruedYield, "total accrued yield should still be 100")
}

// TestAccountingUndistributedYieldEventuallyDistributed verifies that when positions
// exist but none are eligible for yield (all have ReceiveYield==false), the undistributed
// yield is NOT locked up and gets distributed when eligible positions exist later.
func TestAccountingUndistributedYieldEventuallyDistributed(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()

	// ARRANGE: Get params
	params, _ := k.GetVaultsV2Params(ctx)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint tokens
	require.NoError(t, k.Mint(ctx, bob.Bytes, sdkmath.NewInt(1000*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, alice.Bytes, sdkmath.NewInt(500*ONE_V2), nil))

	// STEP 1: Bob deposits with ReceiveYield=false
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       sdkmath.NewInt(1000 * ONE_V2),
		ReceiveYield: false, // Bob opts out
	})
	require.NoError(t, err)

	// STEP 2: Initialize accounting (NAV=1000, no yield)
	initialNav := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1000 * ONE_V2),
		LastUpdate: time.Now(),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, initialNav))
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// STEP 3: NAV increases to 1100 (100 yield), run accounting
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1100 * ONE_V2),
		LastUpdate: time.Now().Add(time.Hour),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ASSERT: Bob gets 0 yield (opted out), but accounting runs successfully
	bobPos, found, _ := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, sdkmath.ZeroInt(), bobPos.AccruedYield, "Bob should have 0 yield (opted out)")

	// ASSERT: VaultState tracks the undistributed yield
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1000*ONE_V2), vaultState.TotalDeposits)
	assert.Equal(t, sdkmath.ZeroInt(), vaultState.TotalAccruedYield, "no yield distributed yet")
	assert.Equal(t, sdkmath.NewInt(1100*ONE_V2), vaultState.TotalNav, "NAV updated to 1100")

	// Undistributed yield = NAV - TotalDeposits - TotalAccruedYield = 1100 - 1000 - 0 = 100
	undistributedYield := vaultState.TotalNav.Sub(vaultState.TotalDeposits).Sub(vaultState.TotalAccruedYield)
	assert.Equal(t, sdkmath.NewInt(100*ONE_V2), undistributedYield, "100 yield should be undistributed")

	// STEP 4: Alice deposits with ReceiveYield=true
	// When Alice deposits 500, the underlying assets grow by 500, so NAV increases by 500
	require.NoError(t, k.Mint(ctx, alice.Bytes, sdkmath.NewInt(500*ONE_V2), nil))
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    alice.Address,
		Amount:       sdkmath.NewInt(500 * ONE_V2),
		ReceiveYield: true, // Alice receives yield
	})
	require.NoError(t, err)

	// STEP 5: Run accounting to sync VaultState with Alice's new deposit
	// IMPORTANT: NAV stays at 1100 during this accounting run to avoid distributing
	// Alice's deposit as yield. The 100 undistributed yield remains.
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ASSERT: VaultState now reflects both deposits
	// BUT accounting saw NAV=1100 and TotalDeposits will be 1500, creating temporary "negative yield"
	vaultState, err = k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1500*ONE_V2), vaultState.TotalDeposits, "1000 Bob + 500 Alice")
	assert.Equal(t, sdkmath.ZeroInt(), vaultState.TotalAccruedYield, "no yield distributed (negative yield scenario)")
	assert.Equal(t, sdkmath.NewInt(1100*ONE_V2), vaultState.TotalNav, "NAV = 1100")

	// Temporary negative undistributed yield = 1100 - 1500 - 0 = -400
	// This happens because Alice deposited but NAV hasn't been updated yet
	undistributedYield = vaultState.TotalNav.Sub(vaultState.TotalDeposits).Sub(vaultState.TotalAccruedYield)
	assert.Equal(t, sdkmath.NewInt(-400*ONE_V2), undistributedYield, "Temporarily negative: deposits exceed NAV")

	// STEP 6: NAV increases to 1650 (external assets grew by 50)
	// Total yield in system: 1650 - 1500 = 150 (100 old undistributed + 50 new)
	navInfo = vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(1650 * ONE_V2),
		LastUpdate: time.Now().Add(2 * time.Hour),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	// STEP 7: Run accounting - Alice should get ALL 150 yield
	for {
		resp, err := vaultsV2Server.UpdateVaultAccounting(ctx, &vaultsv2.MsgUpdateVaultAccounting{
			Manager:      params.Authority,
			MaxPositions: 100,
		})
		require.NoError(t, err)
		if resp.AccountingComplete {
			break
		}
	}

	// ASSERT: Alice gets ALL the yield (150 USDN) since Bob still has ReceiveYield=false
	// This includes the 100 that was "undistributed" before + 50 new yield
	alicePos, found, _ := k.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, sdkmath.NewInt(150*ONE_V2), alicePos.AccruedYield,
		"Alice should get ALL 150 yield (100 old undistributed + 50 new)")

	// ASSERT: Bob still has 0 yield
	bobPos, found, _ = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.True(t, found)
	assert.Equal(t, sdkmath.ZeroInt(), bobPos.AccruedYield, "Bob should still have 0 yield")

	// ASSERT: ALL yield is now distributed (none locked up)
	vaultState, err = k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1500*ONE_V2), vaultState.TotalDeposits, "total deposits = 1500")
	assert.Equal(t, sdkmath.NewInt(150*ONE_V2), vaultState.TotalAccruedYield, "all 150 yield distributed")
	assert.Equal(t, sdkmath.NewInt(1650*ONE_V2), vaultState.TotalNav, "NAV = 1650")

	// CRITICAL ASSERTION: No yield is locked up
	// Undistributed = NAV - Deposits - Distributed = 1650 - 1500 - 150 = 0
	undistributedYield = vaultState.TotalNav.Sub(vaultState.TotalDeposits).Sub(vaultState.TotalAccruedYield)
	assert.True(t, undistributedYield.IsZero(),
		"NO yield should be locked up - all 150 eventually distributed to Alice")
}
