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

const SubmoduleName = "dollar/vaults/v2"

var (
	ParamsKey                        = []byte("vaults/v2/params")
	VaultConfigurationKey            = []byte("vaults/v2/vault_config")
	UserPositionPrefix               = []byte("vaults/v2/user_position/")
	NAVKey                           = []byte("vaults/v2/nav")
	NAVInfoKey                       = []byte("vaults/v2/nav_info")
	VaultStateKey                    = []byte("vaults/v2/vault_state")
	LastNAVUpdateKey                 = []byte("vaults/v2/last_nav_update")
	RemotePositionPrefix             = []byte("vaults/v2/remote_position/")
	InflightFundsPrefix              = []byte("vaults/v2/inflight_funds/")
	InflightRoutesKey                = []byte("vaults/v2/inflight_routes")
	TotalInflightValueKey            = []byte("vaults/v2/total_inflight_value")
	InflightValueByRoutePrefix       = []byte("vaults/v2/inflight_value_by_route/")
	PendingDeploymentFundsKey        = []byte("vaults/v2/pending_deployment")
	PendingWithdrawalDistributionKey = []byte("vaults/v2/pending_withdrawal_dist")
	WithdrawalQueuePrefix            = []byte("vaults/v2/withdrawal_queue/")
	WithdrawalQueueSequenceKey       = []byte("vaults/v2/withdrawal_queue_seq")
	WithdrawalQueueNextIDKey         = []byte("vaults/v2/withdrawal_next_id")
	PendingWithdrawalsKey            = []byte("vaults/v2/pending_withdrawals")
	UserSharesPrefix                 = []byte("vaults/v2/user_shares/")
	TotalSharesKey                   = []byte("vaults/v2/total_shares")
	DepositLimitsKey                 = []byte("vaults/v2/deposit_limits")
	UserDepositHistoryPrefix         = []byte("vaults/v2/user_deposit_history/")
	DepositVelocityPrefix            = []byte("vaults/v2/deposit_velocity/")
	RemotePositionOraclesPrefix      = []byte("vaults/v2/remote_position_oracles/")
	OracleUpdatesPrefix              = []byte("vaults/v2/oracle_updates/")
	OracleRouteConfigsPrefix         = []byte("vaults/v2/oracle_route_configs/")
	LastOracleUpdatePrefix           = []byte("vaults/v2/last_oracle_update/")
)
