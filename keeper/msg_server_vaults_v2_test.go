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
		Authority:                "address",
		MaxAumChangeBps:          1000,  // 10%
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
	assert.Equal(t, math.ZeroInt(), position.TotalPendingWithdrawal)

	// ASSERT: Total users count increased and state updated correctly
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

	// ARRANGE: Set minimum deposit amount
	params, err := k.GetVaultsV2Params(ctx)
	require.NoError(t, err)
	params.MinDepositAmount = math.NewInt(10 * ONE_V2)
	require.NoError(t, k.SetVaultsV2Params(ctx, params))

	// ARRANGE: Mint some USDN to Bob
	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(100*ONE_V2), nil))

	// ACT: Attempt deposit below minimum
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(1),
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

	// ASSERT: First position has first deposit amount
	position1, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(30*ONE_V2), position1.DepositAmount)

	// ASSERT: Second position has second deposit amount
	position2, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 2)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(20*ONE_V2), position2.DepositAmount)

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

	// ACT: Second deposit with ReceiveYield = true
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(30 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ASSERT: Position has ReceiveYield = true (new position created)
	position, _, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 2)
	require.NoError(t, err)
	assert.True(t, position.ReceiveYield)

	// ACT: Third deposit with ReceiveYield = true
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(40 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// ASSERT: Third position has ReceiveYield = true
	position, _, err = k.GetVaultsV2UserPosition(ctx, bob.Bytes, 3)
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
		Requester:  bob.Address,
		Amount:     math.NewInt(50 * ONE_V2),
		PositionId: 1,
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
	assert.Equal(t, math.NewInt(50*ONE_V2), position.TotalPendingWithdrawal)
	assert.Equal(t, int32(1), position.ActiveWithdrawalRequests)

	// ASSERT: Vault state updated
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), vaultState.TotalPendingWithdrawal)

	// ASSERT: Withdrawal request created
	requestId := resp.RequestId
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
		Requester:  bob.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient balance in position")
}

func TestRequestWithdrawalNoPosition(t *testing.T) {
	_, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	// ACT: Request withdrawal without any position
	_, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  bob.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found for requester")
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
		Requester:  bob.Address,
		Amount:     math.NewInt(30 * ONE_V2),
		PositionId: 1,
	})
	require.NoError(t, err)

	// ACT: Second withdrawal request for 20 USDN
	resp2, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  bob.Address,
		Amount:     math.NewInt(20 * ONE_V2),
		PositionId: 1,
	})
	require.NoError(t, err)

	// ASSERT: Both requests have different IDs
	assert.NotEqual(t, resp1.RequestId, resp2.RequestId)

	// ASSERT: User position updated correctly
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.TotalPendingWithdrawal)
	assert.Equal(t, int32(2), position.ActiveWithdrawalRequests)
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
		Requester:  bob.Address,
		Amount:     math.NewInt(50 * ONE_V2),
		PositionId: 1,
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
	requestId := resp.RequestId
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
		Requester:  bob.Address,
		Amount:     math.NewInt(50 * ONE_V2),
		PositionId: 1,
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
		Requester:  bob.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
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
	requestId := resp.RequestId
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
		Requester:  bob.Address,
		Amount:     math.NewInt(50 * ONE_V2),
		PositionId: 1,
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
		Requester:  alice.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
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
		Requester:  bob.Address,
		Amount:     math.NewInt(50 * ONE_V2),
		PositionId: 1,
	})
	require.NoError(t, err)

	// ARRANGE: Move time forward and process queue
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})
	_, err = vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})
	require.NoError(t, err)

	// Check LocalFunds before claim (should be 100 from deposit)
	localFundsBefore, err := k.GetVaultsV2LocalFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(100*ONE_V2), localFundsBefore)

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
	requestId := resp.RequestId
	_, found, err := k.GetVaultsV2Withdrawal(ctx, requestId)
	require.NoError(t, err)
	assert.False(t, found)

	// ASSERT: User position updated
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.DepositAmount)
	assert.Equal(t, math.ZeroInt(), position.TotalPendingWithdrawal)
	assert.Equal(t, int32(0), position.ActiveWithdrawalRequests)

	// ASSERT: LocalFunds reduced by withdrawal amount (100 - 50 claimed = 50)
	localFundsAfter, err := k.GetVaultsV2LocalFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), localFundsAfter)
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
		Requester:  bob.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
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
		Requester:  bob.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
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
		RequestId: 999,
	})

	// ASSERT: Error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestClaimWithdrawalInsufficientLocalFunds(t *testing.T) {
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
		Requester:  bob.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
	})
	require.NoError(t, err)

	// ARRANGE: Move time forward and process queue to mark withdrawal as READY
	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)})
	_, err = vaultsV2Server.ProcessWithdrawalQueue(ctx, &vaultsv2.MsgProcessWithdrawalQueue{
		Authority:   "authority",
		MaxRequests: 10,
	})
	require.NoError(t, err)

	// ARRANGE: Store initial state before reducing LocalFunds
	positionBefore, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)

	requestBefore, found, err := k.GetVaultsV2Withdrawal(ctx, resp.RequestId)
	require.NoError(t, err)
	require.True(t, found)

	stateBefore, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)

	// ARRANGE: Reduce LocalFunds to be less than withdrawal amount
	// LocalFunds is currently 100, reduce by 70 to leave only 30 (< 50 withdrawal amount)
	require.NoError(t, k.SubtractVaultsV2LocalFunds(ctx, math.NewInt(70*ONE_V2)))
	localFundsBeforeClaim, err := k.GetVaultsV2LocalFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(30*ONE_V2), localFundsBeforeClaim)

	// ACT: Attempt to claim withdrawal with insufficient LocalFunds
	_, err = vaultsV2Server.ClaimWithdrawal(ctx, &vaultsv2.MsgClaimWithdrawal{
		Claimer:   bob.Address,
		RequestId: resp.RequestId,
	})

	// ASSERT: Claim fails with ErrInsufficientLocalFunds
	require.Error(t, err)
	require.ErrorIs(t, err, vaultsv2.ErrInsufficientLocalFunds)

	// ASSERT: Withdrawal request still exists and is still READY
	requestAfter, found, err := k.GetVaultsV2Withdrawal(ctx, resp.RequestId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, requestBefore.Status, requestAfter.Status)
	assert.Equal(t, vaultsv2.WITHDRAWAL_REQUEST_STATUS_READY, requestAfter.Status)

	// ASSERT: User position unchanged
	positionAfter, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, positionBefore.DepositPendingWithdrawal, positionAfter.DepositPendingWithdrawal)
	assert.Equal(t, positionBefore.YieldPendingWithdrawal, positionAfter.YieldPendingWithdrawal)
	assert.Equal(t, positionBefore.TotalPendingWithdrawal, positionAfter.TotalPendingWithdrawal)
	assert.Equal(t, positionBefore.ActiveWithdrawalRequests, positionAfter.ActiveWithdrawalRequests)
	assert.Equal(t, positionBefore.DepositAmount, positionAfter.DepositAmount)
	assert.Equal(t, positionBefore.AccruedYield, positionAfter.AccruedYield)

	// ASSERT: LocalFunds unchanged (still 30)
	localFundsAfter, err := k.GetVaultsV2LocalFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(30*ONE_V2), localFundsAfter)

	// ASSERT: No coins transferred to user
	assert.Equal(t, math.ZeroInt(), bank.Balances[bob.Address].AmountOf("uusdn"))

	// ASSERT: Vault state unchanged
	stateAfter, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, stateBefore.PendingWithdrawalRequests, stateAfter.PendingWithdrawalRequests)
	assert.Equal(t, stateBefore.TotalDepositPendingWithdrawal, stateAfter.TotalDepositPendingWithdrawal)
	assert.Equal(t, stateBefore.TotalYieldPendingWithdrawal, stateAfter.TotalYieldPendingWithdrawal)
	assert.Equal(t, stateBefore.TotalPendingWithdrawal, stateAfter.TotalPendingWithdrawal)
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
		Requester:  bob.Address,
		Amount:     math.NewInt(60 * ONE_V2),
		PositionId: 1,
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
	assert.Equal(t, math.ZeroInt(), position.TotalPendingWithdrawal)
	assert.Equal(t, int32(0), position.ActiveWithdrawalRequests)
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

	// ASSERT: Total deposits is 250
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(250*ONE_V2), vaultState.TotalDeposits)

	// ACT: Bob requests withdrawal of 50 USDN
	bobResp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  bob.Address,
		Amount:     math.NewInt(50 * ONE_V2),
		PositionId: 1,
	})
	require.NoError(t, err)

	// ACT: Alice requests withdrawal of 100 USDN
	aliceResp, err := vaultsV2Server.RequestWithdrawal(ctx, &vaultsv2.MsgRequestWithdrawal{
		Requester:  alice.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
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

	// ASSERT: Total deposits reduced
	vaultState, err = k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(100*ONE_V2), vaultState.TotalDeposits) // 50 from Bob, 50 from Alice

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
		Requester:  bob.Address,
		Amount:     math.NewInt(100 * ONE_V2),
		PositionId: 1,
	})
	require.NoError(t, err)

	// ASSERT: Total users is still 1 (position exists with pending withdrawal)
	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), state.TotalUsers)

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

	// ASSERT: Position is empty
	position, found, err := k.GetVaultsV2UserPosition(ctx, bob.Bytes, 1)
	assert.Zero(t, position.AccruedYield)
	assert.Zero(t, position.DepositAmount)

	// ASSERT: Position deleted
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

	pending, err := k.GetVaultsV2LocalFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(200*ONE_V2), pending)

	vaultAddress := hyperlaneutil.CreateMockHexAddress("vault", 1).String()

	resp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: vaultAddress,
		ChainId:      8453,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.PositionId)

	// Manually simulate deployment of 150 to the remote position
	require.NoError(t, k.SubtractVaultsV2LocalFunds(ctx, math.NewInt(150*ONE_V2)))
	position, found, err := k.GetVaultsV2RemotePosition(ctx, resp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	position.TotalValue = math.NewInt(150 * ONE_V2)
	position.Principal = math.NewInt(150 * ONE_V2)
	position.SharesHeld = math.NewInt(150 * ONE_V2)
	position.SharePrice = math.LegacyOneDec()
	require.NoError(t, k.SetVaultsV2RemotePosition(ctx, resp.PositionId, position))

	pending, err = k.GetVaultsV2LocalFunds(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(50*ONE_V2), pending)

	position, found, err = k.GetVaultsV2RemotePosition(ctx, resp.PositionId)
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

	aumInfo := vaultsv2.AUMInfo{
		CurrentAum: math.NewInt(200 * ONE_V2),
		LastUpdate: ctx.HeaderInfo().Time,
	}
	require.NoError(t, k.SetVaultsV2AUMInfo(ctx, aumInfo))

	ctx = ctx.WithHeaderInfo(header.Info{Time: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)})
	_, err = vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(40 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	// Check total deposits increased
	// In the position-based system (no shares), deposits are simply additive
	// First deposit: 100, Second deposit: 40, Total: 140
	vaultState, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.NewInt(140*ONE_V2), vaultState.TotalDeposits)
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
		PositionId:   1,
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
	})
	require.NoError(t, err)

	// Manually simulate deployment of funds to the remote position
	deployAmount := math.NewInt(200 * ONE_V2)
	require.NoError(t, k.SubtractVaultsV2LocalFunds(ctx, deployAmount))
	position, found, err := k.GetVaultsV2RemotePosition(ctx, createResp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	position.TotalValue = deployAmount
	position.Principal = deployAmount
	position.SharesHeld = deployAmount
	position.SharePrice = math.LegacyOneDec()
	require.NoError(t, k.SetVaultsV2RemotePosition(ctx, createResp.PositionId, position))

	resp, err := vaultsV2Server.CloseRemotePosition(ctx, &vaultsv2.MsgCloseRemotePosition{
		Manager:       "authority",
		PositionId:    createResp.PositionId,
		PartialAmount: math.NewInt(150 * ONE_V2),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Initiated)

	position, found, err = k.GetVaultsV2RemotePosition(ctx, createResp.PositionId)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.TotalValue)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.SharesHeld)
	assert.Equal(t, math.NewInt(50*ONE_V2), position.Principal)
	// Position is active (has positive value and shares)
	assert.True(t, position.TotalValue.IsPositive() && position.SharesHeld.IsPositive())
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
		MinDepositAmount:              math.ZeroInt(),
		MinWithdrawalAmount:           math.ZeroInt(),
		MaxAumChangeBps:               250,
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
	assert.Equal(t, int32(250), storedParams.MaxAumChangeBps)
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

	// Note: RemoteDeposit has been removed as it's now handled by the manager on the remote chain
	// Test route creation is still valid
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

// TestRemoteWithdrawCreatesInflight has been removed
// RemoteWithdraw is now handled by the manager on the remote chain

func TestRegisterOracle(t *testing.T) {
	k, vaultsV2Server, _, ctx, bob := setupV2Test(t)

	require.NoError(t, k.Mint(ctx, bob.Bytes, math.NewInt(150*ONE_V2), nil))
	_, err := vaultsV2Server.Deposit(ctx, &vaultsv2.MsgDeposit{
		Depositor:    bob.Address,
		Amount:       math.NewInt(150 * ONE_V2),
		ReceiveYield: true,
	})
	require.NoError(t, err)

	remoteAddr := hyperlaneutil.CreateMockHexAddress("vault", 1)
	posResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: remoteAddr.String(),
		ChainId:      8453,
	})
	require.NoError(t, err)

	registerResp, err := vaultsV2Server.RegisterOracle(ctx, &vaultsv2.MsgRegisterOracle{
		Authority:        "authority",
		PositionId:       posResp.PositionId,
		OracleAddress:    hyperlaneutil.CreateMockHexAddress("oracle", 1),
		SourceChain:      "8453",
		MaxStaleness:     3600,
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

	vaultAddress := hyperlaneutil.CreateMockHexAddress("vault", 1)
	posResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: vaultAddress.String(),
		ChainId:      8453,
	})
	require.NoError(t, err)

	oracleResp, err := vaultsV2Server.RegisterOracle(ctx, &vaultsv2.MsgRegisterOracle{
		Authority:        "authority",
		PositionId:       posResp.PositionId,
		OracleAddress:    hyperlaneutil.CreateMockHexAddress("oracle", 2),
		SourceChain:      "8453",
		MaxStaleness:     1800,
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

	remoteAddr := hyperlaneutil.CreateMockHexAddress("vault", 1)
	posResp, err := vaultsV2Server.CreateRemotePosition(ctx, &vaultsv2.MsgCreateRemotePosition{
		Manager:      "authority",
		VaultAddress: remoteAddr.String(),
		ChainId:      8453,
	})
	require.NoError(t, err)

	oracleResp, err := vaultsV2Server.RegisterOracle(ctx, &vaultsv2.MsgRegisterOracle{
		Authority:        "authority",
		PositionId:       posResp.PositionId,
		OracleAddress:    hyperlaneutil.CreateMockHexAddress("oracle", 3),
		SourceChain:      "8453",
		MaxStaleness:     1200,
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
	var positionFound bool
	for _, entry := range positions {
		if entry.Position.VaultAddress == remoteAddress {
			positionFound = true
			// After route disable, position should still exist and its timestamp updated
			assert.NotNil(t, entry.Position.LastUpdate)
			break
		}
	}
	assert.True(t, positionFound, "Position should still exist after route disable")
}
