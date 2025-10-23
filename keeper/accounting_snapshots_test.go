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

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
	"dollar.noble.xyz/v3/utils"
)

// TestAccountingSnapshotSinglePosition tests accounting snapshots for a single position per user
func TestAccountingSnapshotSinglePosition(t *testing.T) {
	keeper, _, _, ctx, _ := setupV2Test(t)

	// Create a user with a single position
	userAddr := sdk.AccAddress("user1address______")
	positionID := uint64(1)
	depositAmount := math.NewInt(1000 * ONE_V2)
	accruedYield := math.NewInt(50 * ONE_V2)

	position := vaultsv2.UserPosition{
		DepositAmount:            depositAmount,
		AccruedYield:             accruedYield,
		FirstDepositTime:         time.Now(),
		LastActivityTime:         time.Now(),
		ReceiveYield:             true,
		AmountPendingWithdrawal:  math.ZeroInt(),
		ActiveWithdrawalRequests: 0,
		PositionId:               positionID,
	}

	// Store the position
	err := keeper.SetVaultsV2UserPosition(ctx, userAddr, positionID, position)
	require.NoError(t, err)

	// Create accounting snapshot
	snapshot := vaultsv2.AccountingSnapshot{
		User:          userAddr.String(),
		PositionId:    positionID,
		DepositAmount: depositAmount.Add(math.NewInt(100 * ONE_V2)),
		AccruedYield:  accruedYield.Add(math.NewInt(25 * ONE_V2)),
		AccountingNav: math.NewInt(2000 * ONE_V2),
		CreatedAt:     time.Now(),
	}

	// Set the snapshot
	err = keeper.SetVaultsV2AccountingSnapshot(ctx, snapshot)
	require.NoError(t, err)

	// Retrieve the snapshot
	retrievedSnapshot, found, err := keeper.GetVaultsV2AccountingSnapshot(ctx, userAddr, positionID)
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, snapshot.User, retrievedSnapshot.User)
	assert.Equal(t, snapshot.PositionId, retrievedSnapshot.PositionId)
	assert.Equal(t, snapshot.DepositAmount, retrievedSnapshot.DepositAmount)
	assert.Equal(t, snapshot.AccruedYield, retrievedSnapshot.AccruedYield)
	assert.Equal(t, snapshot.AccountingNav, retrievedSnapshot.AccountingNav)

	// Test snapshot not found for different position ID
	_, found, err = keeper.GetVaultsV2AccountingSnapshot(ctx, userAddr, 999)
	require.NoError(t, err)
	assert.False(t, found)

	// Delete the snapshot
	err = keeper.DeleteVaultsV2AccountingSnapshot(ctx, userAddr, positionID)
	require.NoError(t, err)

	// Verify it's deleted
	_, found, err = keeper.GetVaultsV2AccountingSnapshot(ctx, userAddr, positionID)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestAccountingSnapshotMultiplePositions tests accounting snapshots for multiple positions per user
func TestAccountingSnapshotMultiplePositions(t *testing.T) {
	keeper, _, _, ctx, _ := setupV2Test(t)

	// Create a user with multiple positions
	userAddr := sdk.AccAddress("user2address______")
	
	positions := []struct {
		id            uint64
		depositAmount math.Int
		accruedYield  math.Int
	}{
		{1, math.NewInt(1000 * ONE_V2), math.NewInt(50 * ONE_V2)},
		{2, math.NewInt(2000 * ONE_V2), math.NewInt(100 * ONE_V2)},
		{3, math.NewInt(500 * ONE_V2), math.NewInt(25 * ONE_V2)},
	}

	// Store positions
	for _, pos := range positions {
		position := vaultsv2.UserPosition{
			DepositAmount:            pos.depositAmount,
			AccruedYield:             pos.accruedYield,
			FirstDepositTime:         time.Now(),
			LastActivityTime:         time.Now(),
			ReceiveYield:             true,
			AmountPendingWithdrawal:  math.ZeroInt(),
			ActiveWithdrawalRequests: 0,
			PositionId:               pos.id,
		}
		err := keeper.SetVaultsV2UserPosition(ctx, userAddr, pos.id, position)
		require.NoError(t, err)
	}

	// Create snapshots for each position
	snapshots := make([]vaultsv2.AccountingSnapshot, len(positions))
	for i, pos := range positions {
		snapshots[i] = vaultsv2.AccountingSnapshot{
			User:          userAddr.String(),
			PositionId:    pos.id,
			DepositAmount: pos.depositAmount.Add(math.NewInt(100 * ONE_V2)),
			AccruedYield:  pos.accruedYield.Add(math.NewInt(10 * ONE_V2)),
			AccountingNav: math.NewInt(2000 * ONE_V2),
			CreatedAt:     time.Now(),
		}
		err := keeper.SetVaultsV2AccountingSnapshot(ctx, snapshots[i])
		require.NoError(t, err)
	}

	// Retrieve and verify each snapshot
	for _, snapshot := range snapshots {
		retrieved, found, err := keeper.GetVaultsV2AccountingSnapshot(ctx, userAddr, snapshot.PositionId)
		require.NoError(t, err)
		require.True(t, found, "snapshot for position %d should be found", snapshot.PositionId)

		assert.Equal(t, snapshot.User, retrieved.User)
		assert.Equal(t, snapshot.PositionId, retrieved.PositionId)
		assert.Equal(t, snapshot.DepositAmount, retrieved.DepositAmount)
		assert.Equal(t, snapshot.AccruedYield, retrieved.AccruedYield)
		assert.Equal(t, snapshot.AccountingNav, retrieved.AccountingNav)
	}

	// Test that snapshots are independent - deleting one doesn't affect others
	err := keeper.DeleteVaultsV2AccountingSnapshot(ctx, userAddr, positions[1].id)
	require.NoError(t, err)

	// Verify position 2 snapshot is deleted
	_, found, err := keeper.GetVaultsV2AccountingSnapshot(ctx, userAddr, positions[1].id)
	require.NoError(t, err)
	assert.False(t, found)

	// Verify other snapshots still exist
	_, found, err = keeper.GetVaultsV2AccountingSnapshot(ctx, userAddr, positions[0].id)
	require.NoError(t, err)
	assert.True(t, found)

	_, found, err = keeper.GetVaultsV2AccountingSnapshot(ctx, userAddr, positions[2].id)
	require.NoError(t, err)
	assert.True(t, found)
}

// TestAccountingSnapshotCommitMultiPosition tests the commit process for multiple positions
func TestAccountingSnapshotCommitMultiPosition(t *testing.T) {
	keeper, _, _, ctx, _ := setupV2Test(t)

	// Create multiple users with multiple positions each
	users := []sdk.AccAddress{
		sdk.AccAddress("user1address______"),
		sdk.AccAddress("user2address______"),
	}

	// Setup positions and snapshots
	for userIdx, userAddr := range users {
		for posID := uint64(1); posID <= 2; posID++ {
			// Create original position
			originalDeposit := math.NewInt((int64(userIdx+1)*1000 + int64(posID)*100) * ONE_V2)
			originalYield := math.NewInt((int64(userIdx+1)*50 + int64(posID)*10) * ONE_V2)

			position := vaultsv2.UserPosition{
				DepositAmount:            originalDeposit,
				AccruedYield:             originalYield,
				FirstDepositTime:         time.Now(),
				LastActivityTime:         time.Now(),
				ReceiveYield:             true,
				AmountPendingWithdrawal:  math.ZeroInt(),
				ActiveWithdrawalRequests: 0,
				PositionId:               posID,
			}
			err := keeper.SetVaultsV2UserPosition(ctx, userAddr, posID, position)
			require.NoError(t, err)

			// Create accounting snapshot with updated values
			updatedDeposit := originalDeposit.Add(math.NewInt(100 * ONE_V2))
			updatedYield := originalYield.Add(math.NewInt(25 * ONE_V2))

			snapshot := vaultsv2.AccountingSnapshot{
				User:          userAddr.String(),
				PositionId:    posID,
				DepositAmount: updatedDeposit,
				AccruedYield:  updatedYield,
				AccountingNav: math.NewInt(2000 * ONE_V2),
				CreatedAt:     time.Now(),
			}
			err = keeper.SetVaultsV2AccountingSnapshot(ctx, snapshot)
			require.NoError(t, err)
		}
	}

	// Commit all snapshots
	err := keeper.CommitVaultsV2AccountingSnapshots(ctx)
	require.NoError(t, err)

	// Verify all positions were updated correctly and snapshots were deleted
	for userIdx, userAddr := range users {
		for posID := uint64(1); posID <= 2; posID++ {
			// Check that position was updated
			position, found, err := keeper.GetVaultsV2UserPosition(ctx, userAddr, posID)
			require.NoError(t, err)
			require.True(t, found)

			expectedDeposit := math.NewInt((int64(userIdx+1)*1000 + int64(posID)*100 + 100) * ONE_V2)
			expectedYield := math.NewInt((int64(userIdx+1)*50 + int64(posID)*10 + 25) * ONE_V2)

			assert.Equal(t, expectedDeposit, position.DepositAmount)
			assert.Equal(t, expectedYield, position.AccruedYield)

			// Check that snapshot was deleted
			_, found, err = keeper.GetVaultsV2AccountingSnapshot(ctx, userAddr, posID)
			require.NoError(t, err)
			assert.False(t, found, "snapshot should be deleted after commit for user %s position %d", userAddr.String(), posID)
		}
	}
}

// TestAccountingSnapshotIteration tests iterating over all snapshots
func TestAccountingSnapshotIteration(t *testing.T) {
	keeper, _, _, ctx, _ := setupV2Test(t)

	// Create snapshots for multiple users and positions
	expectedSnapshots := []vaultsv2.AccountingSnapshot{}

	// Create proper test accounts
	user1 := utils.TestAccount()
	user2 := utils.TestAccount() 
	user3 := utils.TestAccount()
	users := []string{user1.Address, user2.Address, user3.Address}
	for _, user := range users {
		for posID := uint64(1); posID <= 3; posID++ {
			snapshot := vaultsv2.AccountingSnapshot{
				User:          user,
				PositionId:    posID,
				DepositAmount: math.NewInt(int64(posID) * 1000 * ONE_V2),
				AccruedYield:  math.NewInt(int64(posID) * 50 * ONE_V2),
				AccountingNav: math.NewInt(2000 * ONE_V2),
				CreatedAt:     time.Now(),
			}
			err := keeper.SetVaultsV2AccountingSnapshot(ctx, snapshot)
			require.NoError(t, err)
			expectedSnapshots = append(expectedSnapshots, snapshot)
		}
	}

	// Iterate and collect all snapshots
	var collectedSnapshots []vaultsv2.AccountingSnapshot
	err := keeper.IterateVaultsV2AccountingSnapshots(ctx, func(snapshot vaultsv2.AccountingSnapshot) (bool, error) {
		collectedSnapshots = append(collectedSnapshots, snapshot)
		return false, nil
	})
	require.NoError(t, err)

	// Verify we got all snapshots
	assert.Equal(t, len(expectedSnapshots), len(collectedSnapshots))

	// Verify each snapshot exists in collected snapshots
	for _, expected := range expectedSnapshots {
		found := false
		for _, collected := range collectedSnapshots {
			if expected.User == collected.User && expected.PositionId == collected.PositionId {
				assert.Equal(t, expected.DepositAmount, collected.DepositAmount)
				assert.Equal(t, expected.AccruedYield, collected.AccruedYield)
				found = true
				break
			}
		}
		assert.True(t, found, "expected snapshot for user %s position %d not found", expected.User, expected.PositionId)
	}
}

// TestAccountingSnapshotClearAll tests clearing all snapshots
func TestAccountingSnapshotClearAll(t *testing.T) {
	keeper, _, _, ctx, _ := setupV2Test(t)

	// Create multiple snapshots
	// Create proper test accounts
	user1 := utils.TestAccount()
	user2 := utils.TestAccount() 
	user3 := utils.TestAccount()
	users := []string{user1.Address, user2.Address, user3.Address}
	for _, user := range users {
		for posID := uint64(1); posID <= 2; posID++ {
			snapshot := vaultsv2.AccountingSnapshot{
				User:          user,
				PositionId:    posID,
				DepositAmount: math.NewInt(1000 * ONE_V2),
				AccruedYield:  math.NewInt(50 * ONE_V2),
				AccountingNav: math.NewInt(2000 * ONE_V2),
				CreatedAt:     time.Now(),
			}
			err := keeper.SetVaultsV2AccountingSnapshot(ctx, snapshot)
			require.NoError(t, err)
		}
	}

	// Verify snapshots exist
	count := 0
	err := keeper.IterateVaultsV2AccountingSnapshots(ctx, func(snapshot vaultsv2.AccountingSnapshot) (bool, error) {
		count++
		return false, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 6, count) // 3 users * 2 positions each

	// Clear all snapshots
	err = keeper.ClearAllVaultsV2AccountingSnapshots(ctx)
	require.NoError(t, err)

	// Verify all snapshots are deleted
	count = 0
	err = keeper.IterateVaultsV2AccountingSnapshots(ctx, func(snapshot vaultsv2.AccountingSnapshot) (bool, error) {
		count++
		return false, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestAccountingSnapshotPaginatedIteration tests the new paginated iteration with multi-position
func TestAccountingSnapshotPaginatedIteration(t *testing.T) {
	keeper, _, _, ctx, _ := setupV2Test(t)

	// Create test users with multiple positions
	users := []sdk.AccAddress{
		sdk.AccAddress("user1address______"),
		sdk.AccAddress("user2address______"),
		sdk.AccAddress("user3address______"),
	}

	totalPositions := 0
	for _, userAddr := range users {
		for posID := uint64(1); posID <= 3; posID++ {
			position := vaultsv2.UserPosition{
				DepositAmount:            math.NewInt(int64(posID) * 1000 * ONE_V2),
				AccruedYield:             math.NewInt(int64(posID) * 50 * ONE_V2),
				FirstDepositTime:         time.Now(),
				LastActivityTime:         time.Now(),
				ReceiveYield:             true,
				AmountPendingWithdrawal:  math.ZeroInt(),
				ActiveWithdrawalRequests: 0,
				PositionId:               posID,
			}
			err := keeper.SetVaultsV2UserPosition(ctx, userAddr, posID, position)
			require.NoError(t, err)
			totalPositions++
		}
	}

	// Test paginated iteration with different page sizes
	testCases := []uint32{1, 2, 5, 10, 100}

	for _, pageSize := range testCases {
		t.Run(fmt.Sprintf("PageSize_%d", pageSize), func(t *testing.T) {
			var allCollected []string
			startAfter := ""
			iterations := 0
			maxIterations := 20 // Safety limit

			for iterations < maxIterations {
				lastProcessed, count, err := keeper.IterateVaultsV2UserPositionsPaginated(
					ctx,
					startAfter,
					pageSize,
					func(address sdk.AccAddress, positionID uint64, position vaultsv2.UserPosition) error {
						// Collect identifier for verification
						identifier := fmt.Sprintf("%s:%d", address.String(), positionID)
						allCollected = append(allCollected, identifier)
						return nil
					},
				)
				require.NoError(t, err)

				if count == 0 {
					break
				}

				if count < pageSize {
					// This should be the last page
					break
				}

				startAfter = lastProcessed
				iterations++
			}

			// Verify we collected all positions
			assert.Equal(t, totalPositions, len(allCollected), "should collect all positions with page size %d", pageSize)

			// Verify no duplicates
			seen := make(map[string]bool)
			for _, id := range allCollected {
				assert.False(t, seen[id], "duplicate position found: %s", id)
				seen[id] = true
			}
		})
	}
}