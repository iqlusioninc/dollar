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
	"sort"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"dollar.noble.xyz/v3/keeper"
	"dollar.noble.xyz/v3/types"
	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
	"dollar.noble.xyz/v3/utils"
	"dollar.noble.xyz/v3/utils/mocks"
)

func setupVaultsV2Keeper(t *testing.T, deposits []sdkmath.Int) (*keeper.Keeper, sdk.Context, []utils.Account) {
	t.Helper()

	accountKeeper := mocks.AccountKeeper{Accounts: make(map[string]sdk.AccountI)}
	bankKeeper := mocks.BankKeeper{Balances: make(map[string]sdk.Coins)}

	k, _, ctx := mocks.DollarKeeperWithKeepers(t, bankKeeper, accountKeeper)
	bankKeeper.Restriction = k.SendRestrictionFn
	k.SetBankKeeper(bankKeeper)

	require.NoError(t, k.SetVaultsV2Params(ctx, vaultsv2.Params{
		Authority:                     "authority",
		MinDepositAmount:              sdkmath.ZeroInt(),
		MinWithdrawalAmount:           sdkmath.ZeroInt(),
		VaultEnabled:                  true,
		MaxWithdrawalRequestsPerBlock: 0,
	}))
	require.NoError(t, k.SetVaultsV2Config(ctx, vaultsv2.VaultConfig{Enabled: true}))

	moduleAddr := types.ModuleAddress.String()
	bankKeeper.Balances[moduleAddr] = sdk.NewCoins()

	server := keeper.NewMsgServerV2(k)
	accounts := make([]utils.Account, len(deposits))

	for i, amount := range deposits {
		acc := utils.TestAccount()
		accounts[i] = acc

		bankKeeper.Balances[acc.Address] = sdk.NewCoins(sdk.NewCoin("uusdn", amount))

		_, err := server.Deposit(ctx, &vaultsv2.MsgDeposit{
			Depositor:    acc.Address,
			Amount:       amount,
			ReceiveYield: true,
		})
		require.NoError(t, err)
	}

	return k, ctx, accounts
}

func TestBeginBlocker_DistributesPositiveYield(t *testing.T) {
	deposits := []sdkmath.Int{sdkmath.NewInt(100), sdkmath.NewInt(300)}
	k, ctx, accounts := setupVaultsV2Keeper(t, deposits)

	navUpdate := vaultsv2.NAVInfo{
		CurrentNav:  sdkmath.NewInt(440),
		PreviousNav: sdkmath.NewInt(400),
		LastUpdate:  time.Unix(10, 0),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navUpdate))

	require.NoError(t, k.BeginBlocker(ctx))

	alice := sdk.AccAddress(accounts[0].Bytes)
	bob := sdk.AccAddress(accounts[1].Bytes)

	alicePosition, found, err := k.GetVaultsV2UserPosition(ctx, alice)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, alicePosition.AccruedYield.Equal(sdkmath.NewInt(10)))
	require.True(t, alicePosition.DepositAmount.Equal(deposits[0]))

	bobPosition, found, err := k.GetVaultsV2UserPosition(ctx, bob)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, bobPosition.AccruedYield.Equal(sdkmath.NewInt(30)))
	require.True(t, bobPosition.DepositAmount.Equal(deposits[1]))

	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	require.True(t, state.TotalAccruedYield.Equal(sdkmath.NewInt(40)))
	require.True(t, state.TotalNav.Equal(sdkmath.NewInt(440)))
	require.True(t, state.TotalDeposits.Equal(sdkmath.NewInt(400)))
	require.True(t, state.LastNavUpdate.Equal(navUpdate.LastUpdate))
}

func TestBeginBlocker_DistributesLoss(t *testing.T) {
	deposits := []sdkmath.Int{sdkmath.NewInt(200), sdkmath.NewInt(200)}
	k, ctx, accounts := setupVaultsV2Keeper(t, deposits)

	navUpdate := vaultsv2.NAVInfo{
		CurrentNav:  sdkmath.NewInt(360),
		PreviousNav: sdkmath.NewInt(400),
		LastUpdate:  time.Unix(20, 0),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navUpdate))

	require.NoError(t, k.BeginBlocker(ctx))

	first := sdk.AccAddress(accounts[0].Bytes)
	second := sdk.AccAddress(accounts[1].Bytes)

	firstPosition, found, err := k.GetVaultsV2UserPosition(ctx, first)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, firstPosition.AccruedYield.Equal(sdkmath.NewInt(-20)))
	require.True(t, firstPosition.DepositAmount.Equal(deposits[0]))

	secondPosition, found, err := k.GetVaultsV2UserPosition(ctx, second)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, secondPosition.AccruedYield.Equal(sdkmath.NewInt(-20)))
	require.True(t, secondPosition.DepositAmount.Equal(deposits[1]))

	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	require.True(t, state.TotalAccruedYield.Equal(sdkmath.NewInt(-40)))
	require.True(t, state.TotalNav.Equal(sdkmath.NewInt(360)))
	require.True(t, state.TotalDeposits.Equal(sdkmath.NewInt(400)))
	require.True(t, state.LastNavUpdate.Equal(navUpdate.LastUpdate))
}

func TestBeginBlocker_DistributesRemainderFairly(t *testing.T) {
	deposits := []sdkmath.Int{sdkmath.NewInt(1), sdkmath.NewInt(1), sdkmath.NewInt(1)}
	k, ctx, accounts := setupVaultsV2Keeper(t, deposits)

	navUpdate := vaultsv2.NAVInfo{
		CurrentNav: sdkmath.NewInt(100),
		LastUpdate: time.Unix(30, 0),
	}
	require.NoError(t, k.SetVaultsV2NAVInfo(ctx, navUpdate))

	require.NoError(t, k.BeginBlocker(ctx))

	yields := make([]sdkmath.Int, len(accounts))
	for i, acc := range accounts {
		position, found, err := k.GetVaultsV2UserPosition(ctx, sdk.AccAddress(acc.Bytes))
		require.NoError(t, err)
		require.True(t, found)
		yields[i] = position.AccruedYield
	}

	sorted := make([]int64, len(yields))
	for i, y := range yields {
		sorted[i] = y.Int64()
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	require.Equal(t, []int64{32, 32, 33}, sorted)

	state, err := k.GetVaultsV2VaultState(ctx)
	require.NoError(t, err)
	require.True(t, state.TotalAccruedYield.Equal(sdkmath.NewInt(97)))
	require.True(t, state.TotalNav.Equal(sdkmath.NewInt(100)))
	require.True(t, state.TotalDeposits.Equal(sdkmath.NewInt(3)))
	require.True(t, state.LastNavUpdate.Equal(navUpdate.LastUpdate))
}
