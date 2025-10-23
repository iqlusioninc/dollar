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
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
	"dollar.noble.xyz/v3/utils"
)

// TestAccountingMultiPositionFlow tests the complete accounting flow for users with multiple positions
func TestAccountingMultiPositionFlow(t *testing.T) {
	keeper, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// Create multiple users with multiple positions each  
	alice := utils.TestAccount()

	// Mint coins for both users
	require.NoError(t, keeper.Mint(ctx, bob.Bytes, math.NewInt(5000*ONE_V2), nil))
	require.NoError(t, keeper.Mint(ctx, alice.Bytes, math.NewInt(5000*ONE_V2), nil))

	// Bob creates 2 positions
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(1000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(2000 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// Alice creates 1 position
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    alice.Address,
		Amount:       math.NewInt(1500 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// Verify positions were created correctly
	bobPos1, found, err := keeper.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(1000*ONE_V2), bobPos1.DepositAmount)

	bobPos2, found, err := keeper.GetVaultsV2UserPosition(ctx, bob.Bytes, 2)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(2000*ONE_V2), bobPos2.DepositAmount)

	alicePos1, found, err := keeper.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(1500*ONE_V2), alicePos1.DepositAmount)

	// Set up NAV for yield distribution
	totalDeposits := math.NewInt(4500 * ONE_V2) // 1000 + 2000 + 1500
	newNAV := totalDeposits.Add(math.NewInt(450 * ONE_V2)) // 10% yield = 450 USDN
	navInfo := vaultsv2.NAVInfo{
		CurrentNav: newNAV,
		LastUpdate: time.Now(),
	}
	require.NoError(t, keeper.SetVaultsV2NAVInfo(ctx, navInfo))

	// Manually create accounting snapshots (simulating accounting process)
	snapshots := []vaultsv2.AccountingSnapshot{
		{
			User:          bob.Address,
			PositionId:    1,
			DepositAmount: bobPos1.DepositAmount,
			AccruedYield:  math.NewInt(100 * ONE_V2), // 1000 * 10% = 100
			AccountingNav: newNAV,
			CreatedAt:     time.Now(),
		},
		{
			User:          bob.Address,
			PositionId:    2,
			DepositAmount: bobPos2.DepositAmount,
			AccruedYield:  math.NewInt(200 * ONE_V2), // 2000 * 10% = 200
			AccountingNav: newNAV,
			CreatedAt:     time.Now(),
		},
		{
			User:          alice.Address,
			PositionId:    1,
			DepositAmount: alicePos1.DepositAmount,
			AccruedYield:  math.NewInt(150 * ONE_V2), // 1500 * 10% = 150
			AccountingNav: newNAV,
			CreatedAt:     time.Now(),
		},
	}

	// Set all snapshots
	for _, snapshot := range snapshots {
		err := keeper.SetVaultsV2AccountingSnapshot(ctx, snapshot)
		require.NoError(t, err)
	}

	// Verify all snapshots can be retrieved independently
	for _, expected := range snapshots {
		addr, err := sdk.AccAddressFromBech32(expected.User)
		require.NoError(t, err)
		
		retrieved, found, err := keeper.GetVaultsV2AccountingSnapshot(ctx, addr, expected.PositionId)
		require.NoError(t, err)
		require.True(t, found)
		
		assert.Equal(t, expected.User, retrieved.User)
		assert.Equal(t, expected.PositionId, retrieved.PositionId)
		assert.Equal(t, expected.DepositAmount, retrieved.DepositAmount)
		assert.Equal(t, expected.AccruedYield, retrieved.AccruedYield)
	}

	// Commit all snapshots
	err = keeper.CommitVaultsV2AccountingSnapshots(ctx)
	require.NoError(t, err)

	// Verify positions were updated correctly
	updatedBobPos1, found, err := keeper.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(1000*ONE_V2), updatedBobPos1.DepositAmount)
	assert.Equal(t, math.NewInt(100*ONE_V2), updatedBobPos1.AccruedYield)

	updatedBobPos2, found, err := keeper.GetVaultsV2UserPosition(ctx, bob.Bytes, 2)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(2000*ONE_V2), updatedBobPos2.DepositAmount)
	assert.Equal(t, math.NewInt(200*ONE_V2), updatedBobPos2.AccruedYield)

	updatedAlicePos1, found, err := keeper.GetVaultsV2UserPosition(ctx, alice.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(1500*ONE_V2), updatedAlicePos1.DepositAmount)
	assert.Equal(t, math.NewInt(150*ONE_V2), updatedAlicePos1.AccruedYield)

	// Verify all snapshots were deleted after commit
	for _, expected := range snapshots {
		addr, err := sdk.AccAddressFromBech32(expected.User)
		require.NoError(t, err)
		
		_, found, err := keeper.GetVaultsV2AccountingSnapshot(ctx, addr, expected.PositionId)
		require.NoError(t, err)
		assert.False(t, found, "snapshot should be deleted after commit for user %s position %d", expected.User, expected.PositionId)
	}
}