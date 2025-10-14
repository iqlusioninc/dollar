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

func (q queryServerV2) UserPosition(ctx context.Context, req *vaultsv2.QueryUserPositionRequest) (*vaultsv2.QueryUserPositionResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate and decode user address
	userAddr, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	// Get user position from state
	position, found, err := q.GetVaultsV2UserPosition(ctx, userAddr)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user position")
	}

	// If no position exists, return empty position with zero values
	if !found {
		return &vaultsv2.QueryUserPositionResponse{
			Position:        nil,
			CurrentValue:    sdkmath.ZeroInt().String(),
			UnrealizedYield: sdkmath.ZeroInt().String(),
		}, nil
	}

	// Get user shares
	userShares, err := q.GetVaultsV2UserShares(ctx, userAddr)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user shares")
	}

	// Get vault state to calculate current value
	state, err := q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	// Calculate current value based on shares
	currentValue := sdkmath.ZeroInt()
	if state.TotalShares.IsPositive() && userShares.IsPositive() {
		// currentValue = (userShares / totalShares) * totalNAV
		shareRatio := userShares.ToLegacyDec().Quo(state.TotalShares.ToLegacyDec())
		currentValue = shareRatio.MulInt(state.TotalNav).TruncateInt()
	}

	// Calculate unrealized yield = currentValue - totalDeposited
	unrealizedYield := currentValue.Sub(position.TotalDeposited)
	if unrealizedYield.IsNegative() {
		unrealizedYield = sdkmath.ZeroInt()
	}

	return &vaultsv2.QueryUserPositionResponse{
		Position:        &position,
		CurrentValue:    currentValue.String(),
		UnrealizedYield: unrealizedYield.String(),
	}, nil
}

func (q queryServerV2) NAV(ctx context.Context, req *vaultsv2.QueryNAVRequest) (*vaultsv2.QueryNAVResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Get vault state
	state, err := q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	// Get params for yield rate
	params, err := q.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch params")
	}

	// Calculate local assets (on Noble chain)
	localAssets := state.TotalDeposits.Sub(state.TotalWithdrawn)

	// Get remote positions value (sum of all remote deployments)
	remotePositionsValue := sdkmath.ZeroInt()
	err = q.IterateVaultsV2RemotePositions(ctx, func(_ uint64, position vaultsv2.RemotePosition) (bool, error) {
		var err error
		remotePositionsValue, err = remotePositionsValue.SafeAdd(position.CurrentValue)
		if err != nil {
			return true, errors.Wrap(err, "unable to accumulate remote positions")
		}
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate remote positions")
	}

	// Get inflight funds value
	inflightFundsValue := sdkmath.ZeroInt()
	err = q.IterateVaultsV2InflightFunds(ctx, func(_ string, fund vaultsv2.InflightFund) (bool, error) {
		var err error
		inflightFundsValue, err = inflightFundsValue.SafeAdd(fund.Amount)
		if err != nil {
			return true, errors.Wrap(err, "unable to accumulate inflight funds")
		}
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate inflight funds")
	}

	// Get pending withdrawals (liabilities)
	pendingWithdrawals, err := q.GetVaultsV2PendingWithdrawalDistribution(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch pending withdrawals")
	}

	// Build NAV breakdown
	navBreakdown := &vaultsv2.NAVBreakdownView{
		Local:           localAssets.String(),
		RemotePositions: remotePositionsValue.String(),
		Inflight:        inflightFundsValue.String(),
		Liabilities:     pendingWithdrawals.String(),
		Total:           state.TotalNav.String(),
	}

	return &vaultsv2.QueryNAVResponse{
		Nav:                  state.TotalNav.String(),
		YieldRate:            params.YieldRate.String(),
		LastUpdate:           state.LastNavUpdate,
		TotalDeposits:        state.TotalDeposits.String(),
		TotalAccruedYield:    state.TotalAccruedYield.String(),
		LocalAssets:          localAssets.String(),
		RemotePositionsValue: remotePositionsValue.String(),
		InflightFundsValue:   inflightFundsValue.String(),
		PendingWithdrawals:   pendingWithdrawals.String(),
		NavBreakdown:         navBreakdown,
	}, nil
}

func (q queryServerV2) WithdrawalQueue(ctx context.Context, req *vaultsv2.QueryWithdrawalQueueRequest) (*vaultsv2.QueryWithdrawalQueueResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	var (
		items        []vaultsv2.WithdrawalQueueItem
		totalPending        = sdkmath.ZeroInt()
		queueLength  uint64 = 0
	)

	// Iterate through all withdrawal requests
	err := q.IterateVaultsV2Withdrawals(ctx, func(id uint64, request vaultsv2.WithdrawalRequest) (bool, error) {
		// Calculate total amount (principal + yield if applicable)
		totalAmount := request.Amount

		// Determine status
		status := "PENDING"
		if request.Processed {
			if request.FulfilledAmount.IsPositive() {
				if request.Claimed {
					status = "CLAIMED"
				} else {
					status = "CLAIMABLE"
				}
			} else {
				status = "PROCESSING"
			}
		}

		item := vaultsv2.WithdrawalQueueItem{
			RequestId:       strconv.FormatUint(id, 10),
			User:            request.User,
			Amount:          totalAmount.String(),
			PrincipalAmount: request.Amount.String(), // For now, all is principal
			YieldAmount:     sdkmath.ZeroInt().String(),
			Timestamp:       request.RequestTime,
			Status:          status,
			Position:        queueLength + 1,
		}

		items = append(items, item)
		queueLength++

		if status == "PENDING" || status == "PROCESSING" {
			var err error
			totalPending, err = totalPending.SafeAdd(request.Amount)
			if err != nil {
				return true, errors.Wrap(err, "unable to accumulate pending withdrawals")
			}
		}

		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate withdrawals")
	}

	// Get available liquidity (local assets minus pending deployment)
	state, err := q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	localAssets := state.TotalDeposits.Sub(state.TotalWithdrawn)
	pendingDeployment, err := q.GetVaultsV2PendingDeploymentFunds(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch pending deployment")
	}

	availableLiquidity := localAssets.Sub(pendingDeployment)
	if availableLiquidity.IsNegative() {
		availableLiquidity = sdkmath.ZeroInt()
	}

	// Estimate processing time (simple heuristic: assume 1 hour per batch of 100 requests)
	estimatedProcessingTime := "0"
	if queueLength > 0 {
		hours := (queueLength + 99) / 100                            // Round up
		estimatedProcessingTime = strconv.FormatUint(hours*3600, 10) // Convert to seconds
	}

	return &vaultsv2.QueryWithdrawalQueueResponse{
		PendingRequests:         items,
		TotalPending:            totalPending.String(),
		AvailableLiquidity:      availableLiquidity.String(),
		QueueLength:             queueLength,
		EstimatedProcessingTime: estimatedProcessingTime,
	}, nil
}

func (q queryServerV2) RemotePositions(ctx context.Context, req *vaultsv2.QueryRemotePositionsRequest) (*vaultsv2.QueryRemotePositionsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	userAddr, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	var positions []vaultsv2.RemotePositionWithRoute

	// Iterate through all remote positions and filter by user
	err = q.IterateVaultsV2RemotePositions(ctx, func(id uint64, position vaultsv2.RemotePosition) (bool, error) {
		// Check if this position belongs to the user
		// Remote positions don't have a user field, so we'll return all for now
		// This would need to be enhanced based on actual data model

		// Get the chain ID for this position
		chainID, found, err := q.GetVaultsV2RemotePositionChainID(ctx, id)
		if err != nil {
			return true, errors.Wrap(err, "unable to get chain ID")
		}
		if !found {
			chainID = 0
		}

		posWithRoute := vaultsv2.RemotePositionWithRoute{
			RouteId:  chainID,
			Position: position,
		}

		positions = append(positions, posWithRoute)
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate remote positions")
	}

	return &vaultsv2.QueryRemotePositionsResponse{
		Positions: positions,
	}, nil
}

func (q queryServerV2) VaultRemotePositions(ctx context.Context, req *vaultsv2.QueryVaultRemotePositionsRequest) (*vaultsv2.QueryVaultRemotePositionsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	var (
		positions  []vaultsv2.VaultRemotePosition
		totalValue        = sdkmath.ZeroInt()
		count      uint64 = 0
	)

	// Get params for default values
	params, err := q.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch params")
	}

	// Iterate through all remote positions
	err = q.IterateVaultsV2RemotePositions(ctx, func(id uint64, position vaultsv2.RemotePosition) (bool, error) {
		// Get chain ID
		chainID, found, err := q.GetVaultsV2RemotePositionChainID(ctx, id)
		if err != nil {
			return true, errors.Wrap(err, "unable to get chain ID")
		}
		if !found {
			chainID = 0
		}

		// Build vault remote position view
		vaultPos := vaultsv2.VaultRemotePosition{
			PositionId:   id,
			VaultAddress: position.VaultAddress,
			ChainId:      chainID,
			SharesHeld:   position.SharesHeld.String(),
			SharePrice:   position.SharePrice.String(),
			Principal:    position.Principal.String(),
			CurrentValue: position.CurrentValue.String(),
			Apy:          params.YieldRate.String(), // Use global yield rate as default
			Status:       position.Status.String(),
			LastUpdate:   position.LastUpdate,
		}

		positions = append(positions, vaultPos)
		count++

		var err2 error
		totalValue, err2 = totalValue.SafeAdd(position.CurrentValue)
		if err2 != nil {
			return true, errors.Wrap(err2, "unable to accumulate total value")
		}

		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate remote positions")
	}

	return &vaultsv2.QueryVaultRemotePositionsResponse{
		Positions:      positions,
		TotalPositions: count,
		TotalValue:     totalValue.String(),
	}, nil
}

func (q queryServerV2) CrossChainRoutes(ctx context.Context, req *vaultsv2.QueryCrossChainRoutesRequest) (*vaultsv2.QueryCrossChainRoutesResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	var routes []vaultsv2.CrossChainRoute

	// Iterate through all cross-chain routes
	err := q.IterateVaultsV2CrossChainRoutes(ctx, func(_ uint32, route vaultsv2.CrossChainRoute) (bool, error) {
		routes = append(routes, route)
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate cross-chain routes")
	}

	return &vaultsv2.QueryCrossChainRoutesResponse{
		Routes: routes,
	}, nil
}

func (q queryServerV2) CrossChainRoute(ctx context.Context, req *vaultsv2.QueryCrossChainRouteRequest) (*vaultsv2.QueryCrossChainRouteResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Get the specific route
	route, found, err := q.GetVaultsV2CrossChainRoute(ctx, req.RouteId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch cross-chain route")
	}

	if !found {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "route with ID %d not found", req.RouteId)
	}

	return &vaultsv2.QueryCrossChainRouteResponse{
		Route: route,
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
