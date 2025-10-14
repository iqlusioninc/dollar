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
	"strconv"

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

func (q queryServerV2) InflightFunds(ctx context.Context, req *vaultsv2.QueryInflightFundsRequest) (*vaultsv2.QueryInflightFundsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	var (
		funds []vaultsv2.InflightFundView
		total = sdkmath.ZeroInt()
	)

	err := q.IterateVaultsV2InflightFunds(ctx, func(_ string, fund vaultsv2.InflightFund) (bool, error) {
		view := vaultsv2.InflightFundView{
			TransactionId: fund.TransactionId,
			Amount:        fund.Amount.String(),
			CurrentValue:  fund.ValueAtInitiation.String(),
			InitiatedAt:   fund.InitiatedAt,
			ExpectedAt:    fund.ExpectedAt,
			Status:        fund.Status.String(),
		}

		if origin := fund.GetRemoteOrigin(); origin != nil {
			view.SourceChain = origin.HyptokenId.String()
		}
		if destination := fund.GetRemoteDestination(); destination != nil {
			view.DestinationChain = destination.VaultAddress.String()
		}
		if noble := fund.GetNobleOrigin(); noble != nil {
			view.Type = noble.OperationType.String()
		}
		if nobleDest := fund.GetNobleDestination(); nobleDest != nil {
			view.Type = nobleDest.OperationType.String()
		}
		if tracking := fund.GetProviderTracking(); tracking != nil {
			if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
				if hyperlane.OriginDomain != 0 {
					view.SourceChain = strconv.FormatUint(uint64(hyperlane.OriginDomain), 10)
				}
				if hyperlane.DestinationDomain != 0 {
					view.DestinationChain = strconv.FormatUint(uint64(hyperlane.DestinationDomain), 10)
					view.RouteId = hyperlane.DestinationDomain
				}
			}
		}

		var err error
		total, err = total.SafeAdd(fund.Amount)
		if err != nil {
			return true, errors.Wrap(err, "unable to accumulate inflight totals")
		}

		funds = append(funds, view)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	pendingDeployment, err := q.GetVaultsV2PendingDeploymentFunds(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch pending deployment funds")
	}

	pendingWithdrawalDist, err := q.GetVaultsV2PendingWithdrawalDistribution(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch pending withdrawal distribution")
	}

	return &vaultsv2.QueryInflightFundsResponse{
		InflightFunds:                 funds,
		TotalInflight:                 total.String(),
		PendingDeployment:             pendingDeployment.String(),
		PendingWithdrawalDistribution: pendingWithdrawalDist.String(),
	}, nil
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
