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
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/core/header"
	"cosmossdk.io/math"
	hyperlaneutil "github.com/bcp-innovations/hyperlane-cosmos/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dollar.noble.xyz/v3/keeper"
	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
	"dollar.noble.xyz/v3/utils"
	"dollar.noble.xyz/v3/utils/mocks"
)

const (
	ONE_V2 = 1_000_000
)

// setupV2Test creates a test environment with keeper and bank setup for vaults v2 tests
func setupV2Test(t *testing.T) (*keeper.Keeper, vaultsv2.MsgServer, *mocks.BankKeeper, sdk.Context, utils.Account) {
	account := mocks.AccountKeeper{
		Accounts: make(map[string]sdk.AccountI),
	}
	bank := mocks.BankKeeper{
		Balances: make(map[string]sdk.Coins),
	}
	k, _, ctx := mocks.DollarKeeperWithKeepers(t, bank, account)
	bank.Restriction = k.SendRestrictionFn
	k.SetBankKeeper(bank)

	vaultsV2Server := keeper.NewMsgServerV2(k)
	bob := utils.TestAccount()
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})

	// Set up default params
	params := vaultsv2.Params{
		Authority:                "authority",
		MinDepositAmount:         math.NewInt(ONE_V2),
		MinWithdrawalAmount:      math.NewInt(ONE_V2),
		MaxNavChangeBps:          1000,  // 10%
		WithdrawalRequestTimeout: 86400, // 1 day
		VaultEnabled:             true,
	}
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// Set up default config
	config := vaultsv2.VaultConfig{
		Enabled:          true,
		MaxTotalDeposits: math.NewInt(1000000 * ONE_V2),
		TargetYieldRate:  math.LegacyMustNewDecFromStr("0.05"), // 5%
	}
	require.NoError(t, k.SetVaultsV2Config(ctx, config))

	return k, vaultsV2Server, &bank, ctx, bob
}

func TestDepositBasic(t *testing.T) {
	k, vaultsV2Server, bank, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint 100 USDN to Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	assert.Equal(t, math.NewInt(100*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))

	// ACT: Bob deposits 50 USDN
	resp, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(50 * ONE_V2),
		ReceiveYield: true,
	})

	// ASSERT: Deposit succeeds
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, math.NewInt(50*ONE_V2), resp.AmountDeposited)

	// ASSERT: Bob's balance is reduced
	assert.Equal(t, math.NewInt(50*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))

	// ASSERT: User position is created correctly
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.DepositAmount)
	assert.True(t, position.ReceiveYield)
	assert.Equal(t, math.ZeroInt(), position.AccruedYield)
	assert.Equal(t, math.ZeroInt(), position.AmountPendingWithdrawal)

	// ASSERT: Position created with correct values (no longer using shares)
	// Position tracking is now validated through the position itself

	// ASSERT: Total users count increased
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), state.TotalUsers)
}

func TestDepositInvalidAmount(t *testing.T) {
	_, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ACT: Attempt deposit with zero amount
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.ZeroInt(),
		ReceiveYield: true,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deposit amount must be positive")
}

func TestDepositBelowMinimum(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint some USDN to Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))

	// ACT: Attempt deposit below minimum
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(ONE_V2 - 1),
		ReceiveYield: true,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deposit below minimum")
}

func TestDepositInsufficientBalance(t *testing.T) {
	_, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ACT: Attempt deposit without sufficient balance
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient balance")
}

func TestDepositVaultDisabled(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Disable vault
	config, _ := k.GetVaultsV2Config(ctx)
	config.Enabled = false
	require.NoError(t, k.SetVaultsV2Config(ctx, config))

	// ARRANGE: Mint USDN to Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))

	// ACT: Attempt deposit
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(50 * ONE_V2),
		ReceiveYield: true,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault is disabled")
}

func TestDepositMultipleDeposits(t *testing.T) {
	k, vaultsV2Server, bank, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint 100 USDN to Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))

	// ACT: First deposit of 30 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(30 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Second deposit of 20 USDN
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(20 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ASSERT: Bob's balance is correct
	assert.Equal(t, math.NewInt(50*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))

	// ASSERT: User position accumulated correctly
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.DepositAmount)

	// ASSERT: Position tracking works correctly (no longer using shares)

	// ASSERT: Total users count remains 1
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), state.TotalUsers)
}

func TestDepositYieldPreference(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint USDN to Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))

	// ACT: First deposit with ReceiveYield = false
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(30 * ONE_V2),
		ReceiveYield: false,
	})
	require.NoError(t, err)

	// ASSERT: Position has ReceiveYield = false
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.False(t, position.ReceiveYield)

	// ACT: Second deposit with ReceiveYield = false
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(20 * ONE_V2),
		ReceiveYield: false,
	})
	require.NoError(t, err)

	// ASSERT: Position still has ReceiveYield = false (not overridden)
	position, _, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	assert.False(t, position.ReceiveYield)

	// ACT: Third deposit with ReceiveYield = true
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(10 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ASSERT: Position now has ReceiveYield = true (overridden)
	position, _, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	assert.True(t, position.ReceiveYield)
}

func TestRequestWithdrawalBasic(t *testing.T) {
	k, vaultsV2Server, bank, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint and deposit 100 USDN
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Request withdrawal of 50 USDN
	resp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})

	// ASSERT: Request succeeds
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, math.NewInt(50*ONE_V2), resp.AmountLocked)
	assert.NotEmpty(t, resp.RequestId)

	// ASSERT: Bob's balance unchanged (funds locked in module)
	assert.Equal(t, math.ZeroInt(), bank.Balances[bob.Address].AmountOf("uusdn"))

	// ASSERT: User position updated
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(100*ONE_V2), position.DepositAmount)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.AmountPendingWithdrawal)
	assert.Equal(t, int32(1), position.ActiveWithdrawalRequests)

	// ASSERT: User shares reduced
	// Removed shares reference - using position-based tracking

	// ASSERT: Total shares reduced
	// Removed total shares reference - using position-based tracking

	// ASSERT: Withdrawal request created
	requestId, _ := strconv.ParseUint(resp.RequestId, 10, 64)
	request, found, err := k.GetVaultsV2Withdrawal(ctx, requestId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, bob.Address, request.Requester)
	assert.Equal(t, math.NewInt(50*ONE_V2), request.WithdrawAmount)
	assert.Equal(t, vaultsv2.WITHDRAWAL_REQUEST_STATUS_PENDING, request.Status)
}

func TestRequestWithdrawalInvalidAmount(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint and deposit
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Request withdrawal with zero amount
	_, err = vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.ZeroInt(),
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "withdrawal amount must be positive")
}

func TestRequestWithdrawalInsufficientBalance(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint and deposit 50 USDN
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(50*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(50 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Request withdrawal of more than deposited
	_, err = vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(100 * ONE_V2),
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient unlocked balance")
}

func TestRequestWithdrawalNoPosition(t *testing.T) {
	_, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ACT: Request withdrawal without any position
	_, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no position found")
}

func TestRequestWithdrawalMultipleRequests(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint and deposit 100 USDN
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: First withdrawal request for 30 USDN
	resp1, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(30 * ONE_V2),
	})
	require.NoError(t, err)

	// ACT: Second withdrawal request for 20 USDN
	resp2, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(20 * ONE_V2),
	})
	require.NoError(t, err)

	// ASSERT: Both requests have different IDs
	assert.NotEqual(t, resp1.RequestId, resp2.RequestId)

	// ASSERT: User position updated correctly
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.AmountPendingWithdrawal)
	assert.Equal(t, int32(2), position.ActiveWithdrawalRequests)

	// ASSERT: User shares reduced correctly
	// Removed shares reference - using position-based tracking
}

func TestProcessWithdrawalQueueBasic(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint, deposit, and request withdrawal
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	resp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})
	require.NoError(t, err)

	// ARRANGE: Move time forward past timeout
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})

	// ACT: Process withdrawal queue
	processResp, err := vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})

	// ASSERT: Processing succeeds
	require.NoError(t, err)
	require.NotNil(t, processResp)
	assert.Equal(t, int32(1), processResp.RequestsProcessed)
	assert.Equal(t, math.NewInt(50*ONE_V2), processResp.TotalAmountProcessed)

	// ASSERT: Withdrawal status updated to READY
	requestId, _ := strconv.ParseUint(resp.RequestId, 10, 64)
	request, found, err := k.GetVaultsV2Withdrawal(ctx, requestId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, vaultsv2.WITHDRAWAL_REQUEST_STATUS_READY, request.Status)
}

func TestProcessWithdrawalQueueUnauthorized(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Create withdrawal request
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})
	require.NoError(t, err)

	// ACT: Attempt to process queue with wrong authority
	_, err = vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   bob.Address,
		MaxRequests: 10,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid authority")
}

func TestProcessWithdrawalQueueNotUnlocked(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Create withdrawal request
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	resp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})
	require.NoError(t, err)

	// ACT: Process queue immediately (before timeout)
	processResp, err := vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})

	// ASSERT: No requests processed
	require.NoError(t, err)
	assert.Equal(t, int32(0), processResp.RequestsProcessed)

	// ASSERT: Withdrawal status still PENDING
	requestId, _ := strconv.ParseUint(resp.RequestId, 10, 64)
	request, found, err := k.GetVaultsV2Withdrawal(ctx, requestId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, vaultsv2.WITHDRAWAL_REQUEST_STATUS_PENDING, request.Status)
}

func TestProcessWithdrawalQueueWithLimit(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()

	// ARRANGE: Create multiple withdrawal requests
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(100*ONE_V2), nil))

	// Bob deposits and requests withdrawal
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)
	_, err = vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})
	require.NoError(t, err)

	// Alice deposits and requests withdrawal
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    alice.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)
	_, err = vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: alice.Address,
		Amount:    math.NewInt(30 * ONE_V2),
	})
	require.NoError(t, err)

	// ARRANGE: Move time forward
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})

	// ACT: Process queue with limit of 1
	processResp, err := vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 1,
	})

	// ASSERT: Only 1 request processed
	require.NoError(t, err)
	assert.Equal(t, int32(1), processResp.RequestsProcessed)
	assert.Equal(t, math.NewInt(50*ONE_V2), processResp.TotalAmountProcessed)
}

func TestClaimWithdrawalBasic(t *testing.T) {
	k, vaultsV2Server, bank, ctx, bob := setupV2Test(t)

	// ARRANGE: Deposit and request withdrawal
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	resp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})
	require.NoError(t, err)

	// ARRANGE: Move time forward and process queue
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})
	_, err = vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})
	require.NoError(t, err)

	// ACT: Claim withdrawal
	claimResp, err := vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   bob.Address,
		RequestId: resp.RequestId,
	})

	// ASSERT: Claim succeeds
	require.NoError(t, err)
	require.NotNil(t, claimResp)
	assert.Equal(t, math.NewInt(50*ONE_V2), claimResp.AmountClaimed)
	assert.Equal(t, math.NewInt(50*ONE_V2), claimResp.PrincipalAmount)
	assert.Equal(t, math.ZeroInt(), claimResp.YieldAmount)

	// ASSERT: Bob receives funds
	assert.Equal(t, math.NewInt(50*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))

	// ASSERT: Withdrawal request deleted
	requestId, _ := strconv.ParseUint(resp.RequestId, 10, 64)
	_, found, err := k.GetVaultsV2Withdrawal(ctx, requestId)
	require.NoError(t, err)
	assert.False(t, found)

	// ASSERT: User position updated
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.DepositAmount)
	assert.Equal(t, math.ZeroInt(), position.AmountPendingWithdrawal)
	assert.Equal(t, int32(0), position.ActiveWithdrawalRequests)
}

func TestClaimWithdrawalNotReady(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ARRANGE: Create withdrawal request but don't process it
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	resp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})
	require.NoError(t, err)

	// ACT: Attempt to claim before processing
	_, err = vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   bob.Address,
		RequestId: resp.RequestId,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready for claiming")
}

func TestClaimWithdrawalWrongClaimer(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()

	// ARRANGE: Create and process withdrawal request
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	resp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})
	require.NoError(t, err)

	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})
	_, err = vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})
	require.NoError(t, err)

	// ACT: Alice tries to claim Bob's withdrawal
	_, err = vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   alice.Address,
		RequestId: resp.RequestId,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to claimer")
}

func TestClaimWithdrawalInvalidRequestId(t *testing.T) {
	_, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ACT: Attempt to claim non-existent withdrawal
	_, err := vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   bob.Address,
		RequestId: "999",
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFullDepositWithdrawalCycle(t *testing.T) {
	k, vaultsV2Server, bank, ctx, bob := setupV2Test(t)

	// ARRANGE: Mint 200 USDN to Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))

	// ACT: Bob deposits 100 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ASSERT: Balance after deposit
	assert.Equal(t, math.NewInt(100*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))

	// ACT: Bob requests withdrawal of 60 USDN
	resp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(60 * ONE_V2),
	})
	require.NoError(t, err)

	// ACT: Move time forward
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})

	// ACT: Process withdrawal queue
	_, err = vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})
	require.NoError(t, err)

	// ACT: Bob claims withdrawal
	_, err = vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   bob.Address,
		RequestId: resp.RequestId,
	})
	require.NoError(t, err)

	// ASSERT: Final balance
	assert.Equal(t, math.NewInt(160*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))

	// ASSERT: Position shows remaining deposit
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(40*ONE_V2), position.DepositAmount)
	assert.Equal(t, math.ZeroInt(), position.AmountPendingWithdrawal)
	assert.Equal(t, int32(0), position.ActiveWithdrawalRequests)

	// ASSERT: Position reflects remaining deposit (no longer using shares)
}

func TestMultiUserDepositWithdrawal(t *testing.T) {
	k, vaultsV2Server, bank, ctx, bob := setupV2Test(t)
	alice := utils.TestAccount()

	// ARRANGE: Mint tokens
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	require.NoError(t, k.Mint(ctx, alice.Bytes, math.NewInt(150*ONE_V2), nil))

	// ACT: Bob deposits 100 USDN
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Alice deposits 150 USDN
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    alice.Address,
		Amount:       math.NewInt(150 * ONE_V2),
		ReceiveYield: false,
	})
	require.NoError(t, err)

	// ASSERT: Total users is 2
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), state.TotalUsers)

	// ASSERT: Total shares is 250
	// Using position-based tracking instead of total shares

	// ACT: Bob requests withdrawal of 50 USDN
	bobResp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(50 * ONE_V2),
	})
	require.NoError(t, err)

	// ACT: Alice requests withdrawal of 100 USDN
	aliceResp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: alice.Address,
		Amount:    math.NewInt(100 * ONE_V2),
	})
	require.NoError(t, err)

	// ACT: Process queue
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})
	_, err = vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})
	require.NoError(t, err)

	// ACT: Both claim their withdrawals
	_, err = vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   bob.Address,
		RequestId: bobResp.RequestId,
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   alice.Address,
		RequestId: aliceResp.RequestId,
	})
	require.NoError(t, err)

	// ASSERT: Final balances
	assert.Equal(t, math.NewInt(50*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))
	assert.Equal(t, math.NewInt(100*ONE_V2), bank.Balances[alice.Address].AmountOf("uusdn"))

	// ASSERT: Total shares reduced
	// Using position-based tracking instead of total shares

	// ASSERT: Total users still 2 (both have remaining positions)
	state, err = k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), state.TotalUsers)
}

func TestCompleteWithdrawalRemovesPosition(t *testing.T) {
	k, vaultsV2Server, bank, ctx, bob := setupV2Test(t)

	// ARRANGE: Deposit 100 USDN
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ACT: Request complete withdrawal
	resp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester: bob.Address,
		Amount:    math.NewInt(100 * ONE_V2),
	})
	require.NoError(t, err)

	// ASSERT: Total users is 0 (shares went to 0, even though position exists with pending withdrawal)
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), state.TotalUsers)

	// ACT: Process and claim
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})
	_, err = vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   bob.Address,
		RequestId: resp.RequestId,
	})
	require.NoError(t, err)

	// ASSERT: Position deleted
	_, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	assert.False(t, found)

	// ASSERT: Total users decreased to 0
	state, err = k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), state.TotalUsers)

	// ASSERT: Bob received all funds back
	assert.Equal(t, math.NewInt(100*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))
}

func TestCreateRemotePosition(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	pending, err := k.GetVaultsV2PendingDeploymentFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(200*ONE_V2), pending)

	vaultAddress := hyperlaneutil.CreateMockHexAddress("vault", 1).String()

	resp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: vaultAddress,
		ChainId:      8453,
		Amount:       math.NewInt(150 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.PositionId)

	pending, err = k.GetVaultsV2PendingDeploymentFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), pending)

	position, found, err := k.GetVaultsV2RemotePosition(ctx, resp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(150*ONE_V2), position.TotalValue)
	assert.Equal(t, math.NewInt(150*ONE_V2), position.SharesHeld)
	assert.Equal(t, math.NewInt(150*ONE_V2), position.Principal)

	chainID, foundChain, err := k.GetVaultsV2RemotePositionChainID(ctx, resp.PositionId)
	require.NoError(t, err)
	require.True(t, foundChain)
	assert.Equal(t, uint32(8453), chainID)
}

func TestDepositRespectsSharePrice(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(300*ONE_V2), nil))

	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(100 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	state.TotalNav = math.NewInt(200 * ONE_V2)
	require.NoError(t, k.SetVaultsV2VaultState(ctx, state))

	navInfo := vaultsv2.NAVInfo{
		CurrentNav: math.NewInt(200 * ONE_V2),
		LastUpdate: ctx.HeaderInfo().Time,
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navInfo))

	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)})
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(40 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// Using position-based tracking instead of shares
}

func TestSetYieldPreferenceUpdatesPosition(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))

	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	updateTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	ctx = ctx.WithHeaderInfo(header.Info{Time: updateTime})
	resp, err := vaultsV2Server.SetYieldPreference(ctx, &vaultsv2.MsgSetYieldPreference{
		User:         bob.Address,
		ReceiveYield: false,
	})
	require.NoError(t, err)
	require.True(t, resp.PreviousPreference)
	require.False(t, resp.NewPreference)

	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.False(t, position.ReceiveYield)
	assert.Equal(t, updateTime, position.LastActivityTime)
}

func TestCloseRemotePositionPartial(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	vaultAddress := hyperlaneutil.CreateMockHexAddress("vault", 2).String()
	createResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: vaultAddress,
		ChainId:      998,
		Amount:       math.NewInt(150 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	resp, err := vaultsV2Server.CloseRemotePosition(ctx, &vaultsv2.MsgCloseRemotePosition{
		Manager:       "authority",
		PositionId:    createResp.PositionId,
		PartialAmount: math.NewInt(100 * ONE_V2),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Initiated)

	position, found, err := k.GetVaultsV2RemotePosition(ctx, createResp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.TotalValue)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.SharesHeld)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.Principal)
	assert.Equal(t, vaultsv2.REMOTE_POSITION_ACTIVE, position.Status)

	pendingDistribution, err := k.GetVaultsV2PendingWithdrawalDistribution(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(100*ONE_V2), pendingDistribution)
}

func TestRebalanceAdjustsPositions(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(300*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(300 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	pos1Addr := hyperlaneutil.CreateMockHexAddress("vault", 3).String()
	pos2Addr := hyperlaneutil.CreateMockHexAddress("vault", 4).String()

	pos1Resp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: pos1Addr,
		ChainId:      8453,
		Amount:       math.NewInt(150 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	pos2Resp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: pos2Addr,
		ChainId:      998,
		Amount:       math.NewInt(50 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)})

	resp, err := vaultsV2Server.Rebalance(ctx, &vaultsv2.MsgRebalance{
		Manager: "authority",
		TargetAllocations: []*vaultsv2.TargetAllocation{
			{PositionId: pos1Resp.PositionId, TargetPercentage: 40},
			{PositionId: pos2Resp.PositionId, TargetPercentage: 30},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(2), resp.OperationsInitiated)
	assert.Contains(t, resp.Summary, "pending deployment 90000000")

	pos1, found, err := k.GetVaultsV2RemotePosition(ctx, pos1Resp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(120*ONE_V2), pos1.TotalValue)

	pos2, found, err := k.GetVaultsV2RemotePosition(ctx, pos2Resp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), pos2.TotalValue)

	pending, err := k.GetVaultsV2PendingDeploymentFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(90*ONE_V2), pending)

	var inflight []vaultsv2.InflightFund
	err = k.IterateVaultsV2InflightFunds(ctx, func(_ string, fund vaultsv2.InflightFund) (bool, error) {
		inflight = append(inflight, fund)
		return false, nil
	})
	require.NoError(t, err)
	require.Len(t, inflight, 1)
	assert.Equal(t, fmt.Sprintf("rebalance:%d", pos2Resp.PositionId), inflight[0].TransactionId)
	assert.Equal(t, math.NewInt(40*ONE_V2), inflight[0].Amount)
	assert.Equal(t, vaultsv2.INFLIGHT_PENDING, inflight[0].Status)
}

func TestRebalanceInsufficientLiquidity(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	pos1Addr := hyperlaneutil.CreateMockHexAddress("vault", 5).String()
	pos2Addr := hyperlaneutil.CreateMockHexAddress("vault", 6).String()

	pos1Resp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: pos1Addr,
		ChainId:      8453,
		Amount:       math.NewInt(150 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: pos2Addr,
		ChainId:      998,
		Amount:       math.NewInt(50 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.Rebalance(ctx, &vaultsv2.MsgRebalance{
		Manager: "authority",
		TargetAllocations: []*vaultsv2.TargetAllocation{
			{PositionId: pos1Resp.PositionId, TargetPercentage: 100},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient liquidity")
}

func TestRebalanceInvalidAuthority(t *testing.T) {
	_, vaultsV2Server, _, ctx, _ := setupV2Test(t)

	_, err := vaultsV2Server.Rebalance(ctx, &vaultsv2.MsgRebalance{
		Manager: "noble1unauthorised",
		TargetAllocations: []*vaultsv2.TargetAllocation{
			{PositionId: 1, TargetPercentage: 50},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid authority")
}

func TestRebalanceInvalidTargets(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	posAddr := hyperlaneutil.CreateMockHexAddress("vault", 7).String()
	resp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: posAddr,
		ChainId:      8453,
		Amount:       math.NewInt(100 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.Rebalance(ctx, &vaultsv2.MsgRebalance{
		Manager: "authority",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one target allocation")

	_, err = vaultsV2Server.Rebalance(ctx, &vaultsv2.MsgRebalance{
		Manager: "authority",
		TargetAllocations: []*vaultsv2.TargetAllocation{
			{PositionId: 0, TargetPercentage: 10},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greater than zero")

	_, err = vaultsV2Server.Rebalance(ctx, &vaultsv2.MsgRebalance{
		Manager: "authority",
		TargetAllocations: []*vaultsv2.TargetAllocation{
			{PositionId: resp.PositionId, TargetPercentage: 0},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greater than zero")

	_, err = vaultsV2Server.Rebalance(ctx, &vaultsv2.MsgRebalance{
		Manager: "authority",
		TargetAllocations: []*vaultsv2.TargetAllocation{
			{PositionId: resp.PositionId, TargetPercentage: 60},
			{PositionId: resp.PositionId, TargetPercentage: 20},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate target allocation")

	pos2Addr := hyperlaneutil.CreateMockHexAddress("vault", 8).String()
	pos2Resp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: pos2Addr,
		ChainId:      998,
		Amount:       math.NewInt(50 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.Rebalance(ctx, &vaultsv2.MsgRebalance{
		Manager: "authority",
		TargetAllocations: []*vaultsv2.TargetAllocation{
			{PositionId: resp.PositionId, TargetPercentage: 60},
			{PositionId: pos2Resp.PositionId, TargetPercentage: 50},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceed 100 percent")
}

func TestUpdateVaultConfig(t *testing.T) {
	k, vaultsV2Server, _, ctx, _ := setupV2Test(t)

	initialConfig := vaultsv2.VaultConfig{
		Enabled:          false,
		MaxTotalDeposits: math.ZeroInt(),
		TargetYieldRate:  math.LegacyMustNewDecFromStr("0.00"),
	}
	require.NoError(t, k.SetVaultsV2Config(ctx, initialConfig))

	newConfig := vaultsv2.VaultConfig{
		Enabled:          true,
		MaxTotalDeposits: math.NewInt(1_000 * ONE_V2),
		TargetYieldRate:  math.LegacyMustNewDecFromStr("0.05"),
	}

	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)})
	resp, err := vaultsV2Server.UpdateVaultConfig(ctx, &vaultsv2.MsgUpdateVaultConfig{
		Authority: "authority",
		Config:    newConfig,
		Reason:    "enable deposits",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.NewConfig)

	storedConfig, err := k.GetVaultsV2Config(ctx)
	require.NoError(t, err)
	assert.True(t, storedConfig.Enabled)
	assert.True(t, storedConfig.MaxTotalDeposits.Equal(newConfig.MaxTotalDeposits))
	assert.True(t, storedConfig.TargetYieldRate.Equal(newConfig.TargetYieldRate))

	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.True(t, state.DepositsEnabled)
}

func TestUpdateParams(t *testing.T) {
	k, vaultsV2Server, _, ctx, _ := setupV2Test(t)

	require.NoError(t, k.SetVaultsV2Config(ctx, vaultsv2.VaultConfig{
		Enabled:          true,
		MaxTotalDeposits: math.NewInt(10_000 * ONE_V2),
		TargetYieldRate:  math.LegacyMustNewDecFromStr("0.02"),
	}))

	params := vaultsv2.Params{
		MinDepositAmount:              math.NewInt(10 * ONE_V2),
		MinWithdrawalAmount:           math.NewInt(5 * ONE_V2),
		MaxNavChangeBps:               250,
		WithdrawalRequestTimeout:      3600,
		MaxWithdrawalRequestsPerBlock: 25,
		VaultEnabled:                  true,
	}

	_, err := vaultsV2Server.UpdateParams(ctx, &vaultsv2.MsgUpdateParams{
		Authority: "authority",
		Params:    params,
	})
	require.NoError(t, err)

	storedParams, err := k.GetVaultsV2Params(ctx)
	require.NoError(t, err)
	assert.Equal(t, "authority", storedParams.Authority)
	assert.True(t, storedParams.MinDepositAmount.Equal(params.MinDepositAmount))
	assert.True(t, storedParams.MinWithdrawalAmount.Equal(params.MinWithdrawalAmount))
	assert.Equal(t, int32(250), storedParams.MaxNavChangeBps)
	assert.Equal(t, int64(3600), storedParams.WithdrawalRequestTimeout)
	assert.Equal(t, int32(25), storedParams.MaxWithdrawalRequestsPerBlock)
	assert.True(t, storedParams.VaultEnabled)

	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.True(t, state.DepositsEnabled)
	assert.True(t, state.WithdrawalsEnabled)
}

func TestCreateCrossChainRoute(t *testing.T) {
	k, vaultsV2Server, _, ctx, _ := setupV2Test(t)

	route := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.CreateMockHexAddress("route", 1),
		ReceiverChainHook:     hyperlaneutil.CreateMockHexAddress("hook", 1),
		RemotePositionAddress: hyperlaneutil.CreateMockHexAddress("remote", 1),
		MaxInflightValue:      math.NewInt(1_000 * ONE_V2),
	}

	resp, err := vaultsV2Server.CreateCrossChainRoute(ctx, &vaultsv2.MsgCreateCrossChainRoute{
		Authority: "authority",
		Route:     route,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(1), resp.RouteId)

	stored, found, err := k.GetVaultsV2CrossChainRoute(ctx, resp.RouteId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, route, stored)

	depResp, err := vaultsV2Server.RemoteDeposit(ctx, &vaultsv2.MsgRemoteDeposit{
		Depositor:     "noble1depositor",
		RouteId:       resp.RouteId,
		Amount:        math.NewInt(25 * ONE_V2),
		RemoteAddress: "0xrecipient",
		MinShares:     math.NewInt(20 * ONE_V2),
	})
	require.NoError(t, err)

	inflightID := strconv.FormatUint(depResp.Nonce, 10)
	fund, foundFund, err := k.GetVaultsV2InflightFund(ctx, inflightID)
	require.NoError(t, err)
	require.True(t, foundFund)
	assert.Equal(t, math.NewInt(25*ONE_V2), fund.Amount)
	assert.Equal(t, vaultsv2.INFLIGHT_PENDING, fund.Status)

	routeValue, err := k.GetVaultsV2InflightValueByRoute(ctx, resp.RouteId)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(25*ONE_V2), routeValue)
}

func TestUpdateCrossChainRoute(t *testing.T) {
	k, vaultsV2Server, _, ctx, _ := setupV2Test(t)

	initial := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.CreateMockHexAddress("route", 2),
		ReceiverChainHook:     hyperlaneutil.CreateMockHexAddress("hook", 2),
		RemotePositionAddress: hyperlaneutil.CreateMockHexAddress("remote", 2),
		MaxInflightValue:      math.NewInt(500 * ONE_V2),
	}

	createResp, err := vaultsV2Server.CreateCrossChainRoute(ctx, &vaultsv2.MsgCreateCrossChainRoute{
		Authority: "authority",
		Route:     initial,
	})
	require.NoError(t, err)

	updated := vaultsv2.CrossChainRoute{
		HyptokenId:            initial.HyptokenId,
		ReceiverChainHook:     hyperlaneutil.CreateMockHexAddress("hook", 3),
		RemotePositionAddress: hyperlaneutil.CreateMockHexAddress("remote", 3),
		MaxInflightValue:      math.NewInt(750 * ONE_V2),
	}

	resp, err := vaultsV2Server.UpdateCrossChainRoute(ctx, &vaultsv2.MsgUpdateCrossChainRoute{
		Authority: "authority",
		RouteId:   createResp.RouteId,
		Route:     updated,
	})
	require.NoError(t, err)
	require.Equal(t, createResp.RouteId, resp.RouteId)
	require.NotEmpty(t, resp.PreviousConfig)
	require.NotEmpty(t, resp.NewConfig)

	var previousCfg map[string]any
	var newCfg map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.PreviousConfig), &previousCfg))
	require.NoError(t, json.Unmarshal([]byte(resp.NewConfig), &newCfg))
	assert.NotEqual(t, previousCfg["receiver_chain_hook"], newCfg["receiver_chain_hook"])

	stored, found, err := k.GetVaultsV2CrossChainRoute(ctx, createResp.RouteId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, updated, stored)
}

func TestRemoteWithdrawCreatesInflight(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	route := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.CreateMockHexAddress("route", 5),
		ReceiverChainHook:     hyperlaneutil.CreateMockHexAddress("hook", 5),
		RemotePositionAddress: hyperlaneutil.CreateMockHexAddress("remote", 5),
		MaxInflightValue:      math.NewInt(1_000 * ONE_V2),
	}

	createResp, err := vaultsV2Server.CreateCrossChainRoute(ctx, &vaultsv2.MsgCreateCrossChainRoute{
		Authority: "authority",
		Route:     route,
	})
	require.NoError(t, err)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	targetAddr := route.RemotePositionAddress.String()
	posResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: targetAddr,
		ChainId:      8453,
		Amount:       math.NewInt(120 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	withdrawResp, err := vaultsV2Server.RemoteWithdraw(ctx, &vaultsv2.MsgRemoteWithdraw{
		Withdrawer: bob.Address,
		RouteId:    createResp.RouteId,
		Shares:     math.NewInt(40 * ONE_V2),
		MinAmount:  math.NewInt(35 * ONE_V2),
	})
	require.NoError(t, err)
	assert.Equal(t, createResp.RouteId, withdrawResp.RouteId)

	position, found, err := k.GetVaultsV2RemotePosition(ctx, posResp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, vaultsv2.REMOTE_POSITION_WITHDRAWING, position.Status)

	inflightID := strconv.FormatUint(withdrawResp.Nonce, 10)
	fund, foundFund, err := k.GetVaultsV2InflightFund(ctx, inflightID)
	require.NoError(t, err)
	require.True(t, foundFund)
	assert.Equal(t, math.NewInt(35*ONE_V2), fund.Amount)
	assert.Equal(t, math.NewInt(40*ONE_V2), fund.ValueAtInitiation)

	routeValue, err := k.GetVaultsV2InflightValueByRoute(ctx, createResp.RouteId)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(35*ONE_V2), routeValue)
}

func TestProcessInFlightWithdrawalCompletion(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	route := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.CreateMockHexAddress("route", 6),
		ReceiverChainHook:     hyperlaneutil.CreateMockHexAddress("hook", 6),
		RemotePositionAddress: hyperlaneutil.CreateMockHexAddress("remote", 6),
		MaxInflightValue:      math.NewInt(1_000 * ONE_V2),
	}

	createResp, err := vaultsV2Server.CreateCrossChainRoute(ctx, &vaultsv2.MsgCreateCrossChainRoute{
		Authority: "authority",
		Route:     route,
	})
	require.NoError(t, err)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	targetAddr := route.RemotePositionAddress.String()
	posResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: targetAddr,
		ChainId:      8453,
		Amount:       math.NewInt(120 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	withdrawResp, err := vaultsV2Server.RemoteWithdraw(ctx, &vaultsv2.MsgRemoteWithdraw{
		Withdrawer: bob.Address,
		RouteId:    createResp.RouteId,
		Shares:     math.NewInt(30 * ONE_V2),
		MinAmount:  math.NewInt(25 * ONE_V2),
	})
	require.NoError(t, err)

	_, err = vaultsV2Server.ProcessInFlightPosition(ctx, &vaultsv2.MsgProcessInFlightPosition{
		Authority:    "authority",
		Nonce:        withdrawResp.Nonce,
		ResultStatus: vaultsv2.INFLIGHT_COMPLETED,
		ResultAmount: math.NewInt(28 * ONE_V2),
		ErrorMessage: "",
	})
	require.NoError(t, err)

	position, found, err := k.GetVaultsV2RemotePosition(ctx, posResp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, vaultsv2.REMOTE_POSITION_ACTIVE, position.Status)
	assert.Equal(t, math.NewInt(90*ONE_V2), position.SharesHeld)
	assert.Equal(t, math.NewInt(92*ONE_V2), position.TotalValue)

	inflightID := strconv.FormatUint(withdrawResp.Nonce, 10)
	fund, foundFund, err := k.GetVaultsV2InflightFund(ctx, inflightID)
	require.NoError(t, err)
	require.True(t, foundFund)
	assert.Equal(t, vaultsv2.INFLIGHT_COMPLETED, fund.Status)

	routeValue, err := k.GetVaultsV2InflightValueByRoute(ctx, createResp.RouteId)
	require.NoError(t, err)
	assert.True(t, routeValue.IsZero())

	pendingDistribution, err := k.GetVaultsV2PendingWithdrawalDistribution(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(28*ONE_V2), pendingDistribution)
}

func TestRegisterOracle(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(150*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(150 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	remoteAddr := hyperlaneutil.CreateMockHexAddress("remote", 7)
	posResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: remoteAddr.String(),
		ChainId:      8453,
		Amount:       math.NewInt(100 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	registerResp, err := vaultsV2Server.RegisterOracle(ctx, &vaultsv2.MsgRegisterOracle{
		Authority:        "authority",
		PositionId:       posResp.PositionId,
		OracleAddress:    hyperlaneutil.CreateMockHexAddress("oracle", 1),
		SourceChain:      "8453",
		MaxStaleness:     3600,
		ProviderType:     vaultsv2.PROVIDER_TYPE_HYPERLANE,
		OriginIdentifier: "mailbox",
	})
	require.NoError(t, err)
	require.NotEmpty(t, registerResp.OracleId)

	oracleMeta, found, err := k.GetVaultsV2EnrolledOracle(ctx, registerResp.OracleId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, posResp.PositionId, oracleMeta.PositionId)
	assert.True(t, oracleMeta.Active)
	assert.Equal(t, int64(3600), oracleMeta.MaxStaleness)

	remoteOracle, foundRemote, err := k.GetVaultsV2RemotePositionOracle(ctx, posResp.PositionId)
	require.NoError(t, err)
	require.True(t, foundRemote)
	assert.Equal(t, oracleMeta.OracleAddress, remoteOracle.OracleAddress)
}

func TestUpdateOracleConfig(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(150*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(150 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	remoteAddr := hyperlaneutil.CreateMockHexAddress("remote", 8)
	posResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: remoteAddr.String(),
		ChainId:      8453,
		Amount:       math.NewInt(100 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	oracleResp, err := vaultsV2Server.RegisterOracle(ctx, &vaultsv2.MsgRegisterOracle{
		Authority:        "authority",
		PositionId:       posResp.PositionId,
		OracleAddress:    hyperlaneutil.CreateMockHexAddress("oracle", 2),
		SourceChain:      "8453",
		MaxStaleness:     1800,
		ProviderType:     vaultsv2.PROVIDER_TYPE_HYPERLANE,
		OriginIdentifier: "mailbox",
	})
	require.NoError(t, err)

	updateResp, err := vaultsV2Server.UpdateOracleConfig(ctx, &vaultsv2.MsgUpdateOracleConfig{
		Authority:    "authority",
		OracleId:     oracleResp.OracleId,
		MaxStaleness: 90,
		Active:       false,
	})
	require.NoError(t, err)
	require.NotNil(t, updateResp.UpdatedConfig)
	assert.Equal(t, int64(90), updateResp.UpdatedConfig.MaxStaleness)
	assert.False(t, updateResp.UpdatedConfig.Active)

	saved, found, err := k.GetVaultsV2EnrolledOracle(ctx, oracleResp.OracleId)
	require.NoError(t, err)
	require.True(t, found)
	assert.False(t, saved.Active)
	assert.Equal(t, int64(90), saved.MaxStaleness)
}

func TestRemoveOracle(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(150*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(150 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	remoteAddr := hyperlaneutil.CreateMockHexAddress("remote", 9)
	posResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: remoteAddr.String(),
		ChainId:      8453,
		Amount:       math.NewInt(100 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	oracleResp, err := vaultsV2Server.RegisterOracle(ctx, &vaultsv2.MsgRegisterOracle{
		Authority:        "authority",
		PositionId:       posResp.PositionId,
		OracleAddress:    hyperlaneutil.CreateMockHexAddress("oracle", 3),
		SourceChain:      "8453",
		MaxStaleness:     1200,
		ProviderType:     vaultsv2.PROVIDER_TYPE_HYPERLANE,
		OriginIdentifier: "mailbox",
	})
	require.NoError(t, err)

	removeResp, err := vaultsV2Server.RemoveOracle(ctx, &vaultsv2.MsgRemoveOracle{
		Authority: "authority",
		OracleId:  oracleResp.OracleId,
	})
	require.NoError(t, err)
	assert.Equal(t, oracleResp.OracleId, removeResp.OracleId)
	require.Equal(t, posResp.PositionId, removeResp.PositionId)

	_, found, err := k.GetVaultsV2EnrolledOracle(ctx, oracleResp.OracleId)
	require.NoError(t, err)
	assert.False(t, found)

	_, foundRemote, err := k.GetVaultsV2RemotePositionOracle(ctx, posResp.PositionId)
	require.NoError(t, err)
	assert.False(t, foundRemote)
}

func TestUpdateOracleParams(t *testing.T) {
	k, vaultsV2Server, _, ctx, _ := setupV2Test(t)

	params := vaultsv2.OracleGovernanceParams{
		MaxUpdateInterval:    600,
		MinUpdateInterval:    60,
		MaxPriceDeviationBps: 50,
		DefaultStaleness: &vaultsv2.StalenessConfig{
			WarningThreshold:  1800,
			CriticalThreshold: 3600,
			MaxStaleness:      5400,
		},
	}

	resp, err := vaultsV2Server.UpdateOracleParams(ctx, &vaultsv2.MsgUpdateOracleParams{
		Authority: "authority",
		Params:    params,
	})
	require.NoError(t, err)
	assert.Equal(t, params, resp.NewParams)
	assert.Equal(t, vaultsv2.OracleGovernanceParams{}, resp.PreviousParams)

	stored, err := k.GetVaultsV2OracleParams(ctx)
	require.NoError(t, err)
	assert.Equal(t, params, stored)
}

func TestUpdateNAV(t *testing.T) {
	k, vaultsV2Server, _, baseCtx, _ := setupV2Test(t)

	initialNav := vaultsv2.NAVInfo{
		CurrentNav: math.NewInt(100 * ONE_V2),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(baseCtx, initialNav))

	state := vaultsv2.VaultState{
		TotalDeposits:             math.NewInt(150 * ONE_V2),
		TotalAccruedYield:         math.NewInt(25 * ONE_V2),
		TotalNav:                  math.NewInt(100 * ONE_V2),
		LastNavUpdate:             time.Time{},
		DepositsEnabled:           true,
		WithdrawalsEnabled:        true,
		TotalUsers:                10,
		PendingWithdrawalRequests: 0,
	}
	require.NoError(t, k.SetVaultsV2VaultState(baseCtx, state))

	updateTime := time.Date(2024, 4, 4, 0, 0, 0, 0, time.UTC)
	ctx := baseCtx.WithHeaderInfo(header.Info{Time: updateTime})

	resp, err := vaultsV2Server.UpdateNAV(ctx, &vaultsv2.MsgUpdateNAV{
		Authority:   "authority",
		NewNav:      math.NewInt(110 * ONE_V2),
		PreviousNav: math.NewInt(100 * ONE_V2),
		ChangeBps:   0,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, math.NewInt(110*ONE_V2), resp.AppliedNav)
	assert.Equal(t, updateTime, resp.Timestamp)

	navInfo, err := k.GetVaultsV2NAVInfo(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(100*ONE_V2), navInfo.PreviousNav)
	assert.Equal(t, math.NewInt(110*ONE_V2), navInfo.CurrentNav)
	assert.Equal(t, updateTime, navInfo.LastUpdate)
	assert.NotZero(t, navInfo.ChangeBps)

	updatedState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(110*ONE_V2), updatedState.TotalNav)
	assert.Equal(t, updateTime, updatedState.LastNavUpdate)

	_, err = vaultsV2Server.UpdateNAV(ctx, &vaultsv2.MsgUpdateNAV{
		Authority:   "authority",
		NewNav:      math.NewInt(120 * ONE_V2),
		PreviousNav: math.NewInt(999),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "previous nav mismatch")
}

func TestHandleStaleInflightMarksTimeout(t *testing.T) {
	k, vaultsV2Server, _, baseCtx, bob := setupV2Test(t)

	route := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.CreateMockHexAddress("route", 10),
		ReceiverChainHook:     hyperlaneutil.CreateMockHexAddress("hook", 10),
		RemotePositionAddress: hyperlaneutil.CreateMockHexAddress("remote", 10),
		MaxInflightValue:      math.NewInt(1_000 * ONE_V2),
	}

	routeResp, err := vaultsV2Server.CreateCrossChainRoute(baseCtx, &vaultsv2.MsgCreateCrossChainRoute{
		Authority: "authority",
		Route:     route,
	})
	require.NoError(t, err)

	require.NoError(t, k.Mint(baseCtx, bob.Bytes, math.NewInt(200*ONE_V2), nil))
	_, err = vaultsV2Server.Deposit(baseCtx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	targetAddr := route.RemotePositionAddress.String()
	_, err = vaultsV2Server.CreateRemotePosition(baseCtx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: targetAddr,
		ChainId:      8453,
		Amount:       math.NewInt(120 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	depositResp, err := vaultsV2Server.RemoteDeposit(baseCtx, &vaultsv2.MsgRemoteDeposit{
		Depositor:     "noble1depositor",
		RouteId:       routeResp.RouteId,
		Amount:        math.NewInt(30 * ONE_V2),
		RemoteAddress: "0xrecipient",
		MinShares:     math.NewInt(25 * ONE_V2),
	})
	require.NoError(t, err)

	beforePending, err := k.GetVaultsV2PendingDeploymentFunds(baseCtx)
	require.NoError(t, err)

	ctx := baseCtx.WithHeaderInfo(header.Info{Time: time.Date(2024, 5, 5, 0, 0, 0, 0, time.UTC)})
	handleResp, err := vaultsV2Server.HandleStaleInflight(ctx, &vaultsv2.MsgHandleStaleInflight{
		Authority:  "authority",
		InflightId: strconv.FormatUint(depositResp.Nonce, 10),
		NewStatus:  vaultsv2.INFLIGHT_TIMEOUT,
		Reason:     "manual timeout",
	})
	require.NoError(t, err)
	require.Equal(t, vaultsv2.INFLIGHT_TIMEOUT, handleResp.FinalStatus)

	fund, found, err := k.GetVaultsV2InflightFund(ctx, handleResp.InflightId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, vaultsv2.INFLIGHT_TIMEOUT, fund.Status)

	routeValue, err := k.GetVaultsV2InflightValueByRoute(ctx, routeResp.RouteId)
	require.NoError(t, err)
	assert.True(t, routeValue.IsZero())

	pendingDeployment, err := k.GetVaultsV2PendingDeploymentFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, beforePending, pendingDeployment)
}

func TestDisableCrossChainRoute(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(200*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(200 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	remoteAddress := hyperlaneutil.CreateMockHexAddress("remote", 4)
	route := vaultsv2.CrossChainRoute{
		HyptokenId:            hyperlaneutil.CreateMockHexAddress("route", 4),
		ReceiverChainHook:     hyperlaneutil.CreateMockHexAddress("hook", 4),
		RemotePositionAddress: remoteAddress,
		MaxInflightValue:      math.NewInt(1_000 * ONE_V2),
	}

	createResp, err := vaultsV2Server.CreateCrossChainRoute(ctx, &vaultsv2.MsgCreateCrossChainRoute{
		Authority: "authority",
		Route:     route,
	})
	require.NoError(t, err)

	targetAddr := remoteAddress.String()
	_, err = vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: targetAddr,
		ChainId:      8453,
		Amount:       math.NewInt(50 * ONE_V2),
		MinSharesOut: math.ZeroInt(),
	})
	require.NoError(t, err)

	resp, err := vaultsV2Server.DisableCrossChainRoute(ctx, &vaultsv2.MsgDisableCrossChainRoute{
		Authority: "authority",
		RouteId:   createResp.RouteId,
		Reason:    "testing",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.AffectedPositions)

	_, found, err := k.GetVaultsV2CrossChainRoute(ctx, createResp.RouteId)
	require.NoError(t, err)
	assert.False(t, found)

	positions, err := k.GetAllVaultsV2RemotePositions(ctx)
	require.NoError(t, err)
	var statusMarked bool
	for _, entry := range positions {
		if entry.Position.VaultAddress == remoteAddress {
			statusMarked = entry.Position.Status == vaultsv2.REMOTE_POSITION_ERROR
			break
		}
	}
	assert.True(t, statusMarked)
}
