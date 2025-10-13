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
	"strconv"
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
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.DepositAmount)
	assert.True(t, position.ReceiveYield)
	assert.Equal(t, math.ZeroInt(), position.AccruedYield)
	assert.Equal(t, math.ZeroInt(), position.AmountPendingWithdrawal)

	// ASSERT: User shares are created
	shares, err := k.GetVaultsV2UserShares(ctx, bob.Bytes)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), shares)

	// ASSERT: Total shares increased
	totalShares, err := k.GetVaultsV2TotalShares(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), totalShares)

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
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.DepositAmount)

	// ASSERT: User shares accumulated correctly
	shares, err := k.GetVaultsV2UserShares(ctx, bob.Bytes)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), shares)

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
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes)
	require.NoError(t, err)
	require.True(t, found)
	assert.False(t, position.ReceiveYield)

	// ACT: Second deposit with ReceiveYield = true but no override
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:            bob.Address,
		Amount:               math.NewInt(20 * ONE_V2),
		ReceiveYield:         true,
		ReceiveYieldOverride: false, // Don't override existing preference
	})
	require.NoError(t, err)

	// ASSERT: Position still has ReceiveYield = false (not overridden)
	position, _, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes)
	require.NoError(t, err)
	assert.False(t, position.ReceiveYield)

	// ACT: Third deposit with ReceiveYield = true and override
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:            bob.Address,
		Amount:               math.NewInt(10 * ONE_V2),
		ReceiveYield:         true,
		ReceiveYieldOverride: true, // Override existing preference
	})
	require.NoError(t, err)

	// ASSERT: Position now has ReceiveYield = true (overridden)
	position, _, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes)
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
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(100*ONE_V2), position.DepositAmount)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.AmountPendingWithdrawal)
	assert.Equal(t, int32(1), position.ActiveWithdrawalRequests)

	// ASSERT: User shares reduced
	shares, err := k.GetVaultsV2UserShares(ctx, bob.Bytes)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), shares)

	// ASSERT: Total shares reduced
	totalShares, err := k.GetVaultsV2TotalShares(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), totalShares)

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
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.AmountPendingWithdrawal)
	assert.Equal(t, int32(2), position.ActiveWithdrawalRequests)

	// ASSERT: User shares reduced correctly
	shares, err := k.GetVaultsV2UserShares(ctx, bob.Bytes)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), shares)
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
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes)
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
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(40*ONE_V2), position.DepositAmount)
	assert.Equal(t, math.ZeroInt(), position.AmountPendingWithdrawal)
	assert.Equal(t, int32(0), position.ActiveWithdrawalRequests)

	// ASSERT: Shares reflect remaining deposit
	shares, err := k.GetVaultsV2UserShares(ctx, bob.Bytes)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(40*ONE_V2), shares)
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
	totalShares, err := k.GetVaultsV2TotalShares(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(250*ONE_V2), totalShares)

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
	totalShares, err = k.GetVaultsV2TotalShares(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(100*ONE_V2), totalShares) // 50 from Bob, 50 from Alice

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
	_, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes)
	require.NoError(t, err)
	assert.False(t, found)

	// ASSERT: Total users decreased to 0
	state, err = k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), state.TotalUsers)

	// ASSERT: Bob received all funds back
	assert.Equal(t, math.NewInt(100*ONE_V2), bank.Balances[bob.Address].AmountOf("uusdn"))
}
