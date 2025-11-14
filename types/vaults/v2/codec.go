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

package v2

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgDeposit{}, "dollar/vaults/v2/Deposit", nil)
	cdc.RegisterConcrete(&MsgRequestWithdrawal{}, "dollar/vaults/v2/RequestWithdrawal", nil)
	cdc.RegisterConcrete(&MsgClaimWithdrawal{}, "dollar/vaults/v2/ClaimWithdrawal", nil)
	cdc.RegisterConcrete(&MsgCancelWithdrawal{}, "dollar/vaults/v2/CancelWithdrawal", nil)
	cdc.RegisterConcrete(&MsgSetYieldPreference{}, "dollar/vaults/v2/SetYieldPreference", nil)
	cdc.RegisterConcrete(&MsgProcessWithdrawalQueue{}, "dollar/vaults/v2/ProcessWithdrawalQueue", nil)
	cdc.RegisterConcrete(&MsgUpdateVaultConfig{}, "dollar/vaults/v2/UpdateVaultConfig", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "dollar/vaults/v2/UpdateParams", nil)
	cdc.RegisterConcrete(&MsgCreateCrossChainRoute{}, "dollar/vaults/v2/CreateCrossChainRoute", nil)
	cdc.RegisterConcrete(&MsgUpdateCrossChainRoute{}, "dollar/vaults/v2/UpdateCrossChainRoute", nil)
	cdc.RegisterConcrete(&MsgDisableCrossChainRoute{}, "dollar/vaults/v2/DisableCrossChainRoute", nil)
	cdc.RegisterConcrete(&MsgCreateRemotePosition{}, "dollar/vaults/v2/CreateRemotePosition", nil)
	cdc.RegisterConcrete(&MsgCloseRemotePosition{}, "dollar/vaults/v2/CloseRemotePosition", nil)
	cdc.RegisterConcrete(&MsgProcessInFlightPosition{}, "dollar/vaults/v2/ProcessInFlightPosition", nil)
	cdc.RegisterConcrete(&MsgHandleStaleInflight{}, "dollar/vaults/v2/HandleStaleInflight", nil)
	cdc.RegisterConcrete(&MsgRegisterOracle{}, "dollar/vaults/v2/RegisterOracle", nil)
	cdc.RegisterConcrete(&MsgUpdateOracleConfig{}, "dollar/vaults/v2/UpdateOracleConfig", nil)
	cdc.RegisterConcrete(&MsgRemoveOracle{}, "dollar/vaults/v2/RemoveOracle", nil)
	cdc.RegisterConcrete(&MsgUpdateOracleParams{}, "dollar/vaults/v2/UpdateOracleParams", nil)
	cdc.RegisterConcrete(&MsgDeployFunds{}, "dollar/vaults/v2/DeployFunds", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
