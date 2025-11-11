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

import sdk "github.com/cosmos/cosmos-sdk/types"

const SubmoduleName = "dollar/vaults/v2"

var (
	ParamsKey                        = []byte("vaults/v2/params")
	VaultConfigurationKey            = []byte("vaults/v2/vault_config")
	UserPositionPrefix               = []byte("vaults/v2/user_position/")
	UserPositionCountPrefix          = []byte("vaults/v2/user_position_count/")
	UserNextPositionIDPrefix         = []byte("vaults/v2/user_next_position_id/")
	AUMKey                           = []byte("vaults/v2/aum")
	AUMInfoKey                       = []byte("vaults/v2/aum_info")
	VaultStateKey                    = []byte("vaults/v2/vault_state")
	AccountingCursorKey              = []byte("vaults/v2/accounting_cursor")
	AccountingSnapshotPrefix         = []byte("vaults/v2/accounting_snapshot/")
	LastAUMUpdateKey                 = []byte("vaults/v2/last_aum_update")
	RemotePositionPrefix             = []byte("vaults/v2/remote_position/")
	InflightFundsPrefix              = []byte("vaults/v2/inflight_funds/")
	InflightRoutesKey                = []byte("vaults/v2/inflight_routes")
	TotalInflightValueKey            = []byte("vaults/v2/total_inflight_value")
	InflightValueByRoutePrefix       = []byte("vaults/v2/inflight_value_by_route/")
	InflightNextIDKey                = []byte("vaults/v2/inflight_next_id")
	RemotePositionNextIDKey          = []byte("vaults/v2/remote_position_next_id")
	RemotePositionChainPrefix        = []byte("vaults/v2/remote_position_chain/")
	LocalFundsKey                    = []byte("vaults/v2/local_funds")
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
	EnrolledOraclePrefix             = []byte("vaults/v2/enrolled_oracles/")
	OracleParamsKey                  = []byte("vaults/v2/oracle_params")
	CrossChainRoutePrefix            = []byte("vaults/v2/cross_chain_route/")
	CrossChainRouteNextIDKey         = []byte("vaults/v2/cross_chain_route_next_id")
	BlockDepositVolumePrefix         = []byte("vaults/v2/block_deposit_volume/")
	AUMSnapshotsPrefix               = []byte("vaults/v2/aum_snapshots/")
	AUMSnapshotNextIDKey             = []byte("vaults/v2/aum_snapshot_next_id")
	CircuitBreakerActiveKey          = []byte("vaults/v2/circuit_breaker_active")
	CircuitBreakerTripsPrefix        = []byte("vaults/v2/circuit_breaker_trips/")
	CircuitBreakerNextIDKey          = []byte("vaults/v2/circuit_breaker_next_id")
)

// GetUserPositionKey creates the composite key for a user's specific position
func GetUserPositionKey(address []byte, positionID uint64) []byte {
	return append(append(UserPositionPrefix, address...), sdk.Uint64ToBigEndian(positionID)...)
}

// GetUserPositionCountKey creates the key for tracking user's position count
func GetUserPositionCountKey(address []byte) []byte {
	return append(UserPositionCountPrefix, address...)
}

// GetUserNextPositionIDKey creates the key for the next position ID for a user
func GetUserNextPositionIDKey(address []byte) []byte {
	return append(UserNextPositionIDPrefix, address...)
}

// ParseUserPositionKey extracts address and positionID from a composite key
func ParseUserPositionKey(key []byte) (address []byte, positionID uint64) {
	// Remove prefix
	keyWithoutPrefix := key[len(UserPositionPrefix):]
	// Address is all bytes except last 8 (uint64)
	address = keyWithoutPrefix[:len(keyWithoutPrefix)-8]
	// Position ID is last 8 bytes
	positionID = sdk.BigEndianToUint64(keyWithoutPrefix[len(keyWithoutPrefix)-8:])
	return
}

// GetAccountingSnapshotKey creates the composite key for an accounting snapshot
func GetAccountingSnapshotKey(address []byte, positionID uint64) []byte {
	return append(append(AccountingSnapshotPrefix, address...), sdk.Uint64ToBigEndian(positionID)...)
}

// ParseAccountingSnapshotKey extracts address and positionID from a composite key
func ParseAccountingSnapshotKey(key []byte) (address []byte, positionID uint64) {
	// Remove prefix
	keyWithoutPrefix := key[len(AccountingSnapshotPrefix):]
	// Address is all bytes except last 8 (uint64)
	address = keyWithoutPrefix[:len(keyWithoutPrefix)-8]
	// Position ID is last 8 bytes
	positionID = sdk.BigEndianToUint64(keyWithoutPrefix[len(keyWithoutPrefix)-8:])
	return
}
