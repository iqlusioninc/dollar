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

package keeper

import (
	"context"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	"dollar.noble.xyz/v3/types"
	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

var _ vaultsv2.QueryServer = &queryServerV2{}

type queryServerV2 struct {
	vaultsv2.UnimplementedQueryServer
	*Keeper
}

func NewQueryServerV2(keeper *Keeper) vaultsv2.QueryServer {
	return &queryServerV2{Keeper: keeper}
}

func (q queryServerV2) VaultStats(ctx context.Context, req *vaultsv2.QueryVaultStatsRequest) (*vaultsv2.QueryVaultStatsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	stats, err := q.buildVaultStatsEntry(ctx)
	if err != nil {
		return nil, err
	}

	return &vaultsv2.QueryVaultStatsResponse{Stats: stats}, nil
}

func (q queryServerV2) Stats(ctx context.Context, req *vaultsv2.QueryStatsRequest) (*vaultsv2.QueryStatsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	stats, err := q.buildVaultStatsEntry(ctx)
	if err != nil {
		return nil, err
	}

	return &vaultsv2.QueryStatsResponse{Stats: stats}, nil
}

func (q queryServerV2) Params(ctx context.Context, req *vaultsv2.QueryParamsRequest) (*vaultsv2.QueryParamsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	params, err := q.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch params")
	}

	return &vaultsv2.QueryParamsResponse{Params: params}, nil
}

func (q queryServerV2) buildVaultStatsEntry(ctx context.Context) (vaultsv2.VaultStatsEntry, error) {
	state, err := q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return vaultsv2.VaultStatsEntry{}, errors.Wrap(err, "unable to fetch vault state")
	}

	return vaultsv2.VaultStatsEntry{
		TotalDepositors:       state.TotalUsers,
		TotalDeposited:        state.TotalDeposits,
		TotalWithdrawn:        sdkmath.ZeroInt(),
		TotalFeesCollected:    sdkmath.ZeroInt(),
		TotalYieldDistributed: state.TotalAccruedYield,
		ActivePositions:       state.TotalUsers,
	}, nil
}
