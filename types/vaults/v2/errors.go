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

import "cosmossdk.io/errors"

var (
	ErrInvalidAuthority         = errors.Register(SubmoduleName, 1, "invalid authority")
	ErrInvalidAmount            = errors.Register(SubmoduleName, 2, "invalid amount")
	ErrVaultNotFound            = errors.Register(SubmoduleName, 3, "vault not found")
	ErrRemotePositionNotFound   = errors.Register(SubmoduleName, 4, "remote position not found")
	ErrInflightNotFound         = errors.Register(SubmoduleName, 5, "inflight fund not found")
	ErrWithdrawalNotFound       = errors.Register(SubmoduleName, 6, "withdrawal request not found")
	ErrInvalidVaultState        = errors.Register(SubmoduleName, 7, "invalid vault state")
	ErrOperationNotPermitted    = errors.Register(SubmoduleName, 8, "operation not permitted")
	ErrUnimplemented            = errors.Register(SubmoduleName, 9, "feature not yet implemented")
	ErrRouteNotFound            = errors.Register(SubmoduleName, 10, "cross-chain route not found")
	ErrRouteCapacityExceeded    = errors.Register(SubmoduleName, 11, "route capacity exceeded")
	ErrInflightAlreadyExists    = errors.Register(SubmoduleName, 12, "inflight fund already exists")
	ErrInflightAlreadyCompleted = errors.Register(SubmoduleName, 13, "inflight fund already completed")
	ErrInflightAlreadyProcessed = errors.Register(SubmoduleName, 14, "inflight fund already processed")
	ErrInvalidRoute             = errors.Register(SubmoduleName, 15, "invalid route")
	ErrInsufficientLocalFunds   = errors.Register(SubmoduleName, 16, "insufficient local funds to service withdrawal")
)
