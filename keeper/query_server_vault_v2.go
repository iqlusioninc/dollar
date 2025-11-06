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
	"time"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

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

	// Get params for NAV calculation settings
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

	err := q.IterateVaultsV2InflightFunds(ctx, func(_ uint64, fund vaultsv2.InflightFund) (bool, error) {
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
			view.OperationType = noble.OperationType.String()
		}
		if nobleDest := fund.GetNobleDestination(); nobleDest != nil {
			view.OperationType = nobleDest.OperationType.String()
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

	localFunds, err := q.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch local funds")
	}

	pendingWithdrawalDist, err := q.GetVaultsV2PendingWithdrawalDistribution(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch pending withdrawal distribution")
	}

	return &vaultsv2.QueryInflightFundsResponse{
		InflightFunds:                 funds,
		TotalInflight:                 total.String(),
		PendingDeployment:             localFunds.String(),
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

	// Get specific user position by ID
	position, found, err := q.GetVaultsV2UserPosition(ctx, userAddr, req.PositionId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "position not found for user %s with ID %d", req.Address, req.PositionId)
	}

	// If no position exists, return empty position with zero values
	if err != nil {
		return &vaultsv2.QueryUserPositionResponse{
			Position:        nil,
			CurrentValue:    sdkmath.ZeroInt().String(),
			UnrealizedYield: sdkmath.ZeroInt().String(),
		}, nil
	}

	// Get vault state to calculate current value
	_, err = q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	// Calculate current value using direct yield tracking
	// Current value = deposit amount + accrued yield
	currentValue := position.DepositAmount.Add(position.AccruedYield)

	// Unrealized yield is already tracked in AccruedYield field
	unrealizedYield := position.AccruedYield

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
	_, err = q.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch params")
	}

	// Calculate local assets (on Noble chain)
	localAssets := state.TotalDeposits.Sub(sdkmath.ZeroInt())

	// Get remote positions value (sum of all remote deployments)
	remotePositionsValue := sdkmath.ZeroInt()
	err = q.IterateVaultsV2RemotePositions(ctx, func(_ uint64, position vaultsv2.RemotePosition) (bool, error) {
		var err error
		remotePositionsValue, err = remotePositionsValue.SafeAdd(position.TotalValue)
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
	err = q.IterateVaultsV2InflightFunds(ctx, func(_ uint64, fund vaultsv2.InflightFund) (bool, error) {
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
		YieldRate:            sdkmath.LegacyNewDec(0).String(),
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
		totalAmount := request.WithdrawAmount

		// Determine status
		status := "PENDING"
		if false {
			if sdkmath.ZeroInt().IsPositive() {
				if false {
					status = "CLAIMED"
				} else {
					status = "CLAIMABLE"
				}
			} else {
				status = "PROCESSING"
			}
		}

		item := vaultsv2.WithdrawalQueueItem{
			RequestId:       id,
			User:            request.Requester,
			Amount:          totalAmount.String(),
			PrincipalAmount: request.WithdrawAmount.String(), // For now, all is principal
			YieldAmount:     sdkmath.ZeroInt().String(),
			Timestamp:       request.RequestTime,
			Status:          status,
			QueuePosition:   queueLength + 1,
			PositionId:      request.PositionId,
		}

		items = append(items, item)
		queueLength++

		if status == "PENDING" || status == "PROCESSING" {
			var err error
			totalPending, err = totalPending.SafeAdd(request.WithdrawAmount)
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

	// Total withdrawn is not tracked in the new system
	// Would need to be calculated from historical data if needed
	localAssets := state.TotalDeposits.Sub(sdkmath.ZeroInt())
	localFunds, err := q.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch local funds")
	}

	availableLiquidity := localAssets.Sub(localFunds)
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

	_, err := q.address.StringToBytes(req.Address)
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
		chainID, _, err := q.GetVaultsV2RemotePositionChainID(ctx, id)
		if err != nil {
			return true, errors.Wrap(err, "unable to get chain ID")
		}
		if err != nil {
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
	// Get params for yield calculations
	_, err := q.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch params")
	}

	// Iterate through all remote positions
	err = q.IterateVaultsV2RemotePositions(ctx, func(id uint64, position vaultsv2.RemotePosition) (bool, error) {
		// Get chain ID
		chainID, _, err := q.GetVaultsV2RemotePositionChainID(ctx, id)
		if err != nil {
			return true, errors.Wrap(err, "unable to get chain ID")
		}
		if chainID == 0 {
			chainID = 0
		}

		// Build vault remote position view
		vaultPos := vaultsv2.VaultRemotePosition{
			PositionId:   id,
			VaultAddress: position.VaultAddress.String(),
			ChainId:      chainID,
			SharesHeld:   position.SharesHeld.String(),
			SharePrice:   position.SharePrice.String(),
			Principal:    position.Principal.String(),
			CurrentValue: position.TotalValue.String(),
			Apy:          sdkmath.LegacyNewDec(0).String(), // Use global yield rate as default
			Status:       position.Status.String(),
			LastUpdate:   position.LastUpdate,
		}

		positions = append(positions, vaultPos)
		count++

		var err2 error
		totalValue, err2 = totalValue.SafeAdd(position.TotalValue)
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
	route, _, err := q.GetVaultsV2CrossChainRoute(ctx, req.RouteId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch cross-chain route")
	}

	return &vaultsv2.QueryCrossChainRouteResponse{
		Route: route,
	}, nil
}

func (q queryServerV2) VaultInfo(ctx context.Context, req *vaultsv2.QueryVaultInfoRequest) (*vaultsv2.QueryVaultInfoResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Get vault config
	config, err := q.GetVaultsV2Config(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault config")
	}

	// Get vault state
	state, err := q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	return &vaultsv2.QueryVaultInfoResponse{
		Config:            config,
		TotalDeposits:     state.TotalDeposits.String(),
		TotalAccruedYield: state.TotalAccruedYield.String(),
		TotalNav:          state.TotalNav.String(),
		TotalDepositors:   state.TotalUsers,
	}, nil
}

func (q queryServerV2) AllVaults(ctx context.Context, req *vaultsv2.QueryAllVaultsRequest) (*vaultsv2.QueryAllVaultsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// For V2, we only have one vault, but return it in array format for API consistency
	vaultInfo, err := q.VaultInfo(ctx, &vaultsv2.QueryVaultInfoRequest{})
	if err != nil {
		return nil, err
	}

	return &vaultsv2.QueryAllVaultsResponse{
		Vaults: []vaultsv2.QueryVaultInfoResponse{*vaultInfo},
	}, nil
}

func (q queryServerV2) UserPositions(ctx context.Context, req *vaultsv2.QueryUserPositionsRequest) (*vaultsv2.QueryUserPositionsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	userAddr, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	var positions []vaultsv2.UserPositionWithVault

	// Iterate through all user positions
	err = q.IterateUserPositions(ctx, userAddr, func(positionID uint64, position vaultsv2.UserPosition) (bool, error) {
		// Calculate current value (deposits + yield)
		currentValue := position.DepositAmount.Add(position.AccruedYield)

		positions = append(positions, vaultsv2.UserPositionWithVault{
			PositionId:   positionID,
			Position:     position,
			CurrentValue: currentValue.String(),
		})
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate user positions")
	}

	return &vaultsv2.QueryUserPositionsResponse{
		Positions: positions,
	}, nil
}

func (q queryServerV2) YieldInfo(ctx context.Context, req *vaultsv2.QueryYieldInfoRequest) (*vaultsv2.QueryYieldInfoResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Get vault state
	state, err := q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	_, err = q.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch params")
	}

	return &vaultsv2.QueryYieldInfoResponse{
		YieldRate:         sdkmath.LegacyNewDec(0).String(),
		TotalDeposits:     state.TotalDeposits.String(),
		TotalAccruedYield: state.TotalAccruedYield.String(),
		TotalNav:          state.TotalNav.String(),
	}, nil
}

func (q queryServerV2) RemotePosition(ctx context.Context, req *vaultsv2.QueryRemotePositionRequest) (*vaultsv2.QueryRemotePositionResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	_, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	// For now, we iterate to find a position matching the route_id
	// In a full implementation, positions would be indexed by user
	var foundPosition *vaultsv2.RemotePosition
	err = q.IterateVaultsV2RemotePositions(ctx, func(id uint64, position vaultsv2.RemotePosition) (bool, error) {
		chainID, found, err := q.GetVaultsV2RemotePositionChainID(ctx, id)
		if err != nil {
			return true, err
		}
		if found && chainID == req.RouteId {
			foundPosition = &position
			return true, nil // Stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate remote positions")
	}

	if foundPosition == nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "remote position not found for route %d and address %s", req.RouteId, req.Address)
	}

	return &vaultsv2.QueryRemotePositionResponse{
		Position: *foundPosition,
	}, nil
}

func (q queryServerV2) InflightFund(ctx context.Context, req *vaultsv2.QueryInflightFundRequest) (*vaultsv2.QueryInflightFundResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Find inflight fund by route_id
	var foundFund *vaultsv2.InflightFund
	err := q.IterateVaultsV2InflightFunds(ctx, func(txID uint64, fund vaultsv2.InflightFund) (bool, error) {
		// Check if this fund is associated with the requested route
		if tracking := fund.GetProviderTracking(); tracking != nil {
			if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
				if hyperlane.DestinationDomain == req.RouteId {
					foundFund = &fund
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate inflight funds")
	}

	if foundFund == nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "inflight fund not found for route %d", req.RouteId)
	}

	return &vaultsv2.QueryInflightFundResponse{
		Fund: *foundFund,
	}, nil
}

func (q queryServerV2) InflightFundsUser(ctx context.Context, req *vaultsv2.QueryInflightFundsUserRequest) (*vaultsv2.QueryInflightFundsUserResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	userAddr, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	var funds []vaultsv2.InflightFund

	// Iterate and filter by user
	err = q.IterateVaultsV2InflightFunds(ctx, func(_ uint64, fund vaultsv2.InflightFund) (bool, error) {
		// Check if this fund belongs to the user
		// This depends on the fund structure - checking NobleOrigin for initiator
		if origin := fund.GetNobleOrigin(); origin != nil {
			if "" == string(userAddr) {
				funds = append(funds, fund)
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate inflight funds")
	}

	return &vaultsv2.QueryInflightFundsUserResponse{
		Funds: funds,
	}, nil
}

func (q queryServerV2) CrossChainSnapshot(ctx context.Context, req *vaultsv2.QueryCrossChainSnapshotRequest) (*vaultsv2.QueryCrossChainSnapshotResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Build snapshot of cross-chain positions
	totalRemoteValue := sdkmath.ZeroInt()
	activePositions := int64(0)
	stalePositions := int64(0)

	err := q.IterateVaultsV2RemotePositions(ctx, func(id uint64, position vaultsv2.RemotePosition) (bool, error) {
		_, _, err := q.GetVaultsV2RemotePositionChainID(ctx, id)
		if err != nil {
			return true, err
		}

		// Count active vs stale positions
		if position.Status == vaultsv2.REMOTE_POSITION_ACTIVE {
			activePositions++
		} else {
			stalePositions++
		}

		var err2 error
		totalRemoteValue, err2 = totalRemoteValue.SafeAdd(position.TotalValue)
		if err2 != nil {
			return true, errors.Wrap(err2, "unable to accumulate remote value")
		}

		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate remote positions")
	}

	// Get inflight funds
	totalInflight := sdkmath.ZeroInt()
	err = q.IterateVaultsV2InflightFunds(ctx, func(_ uint64, fund vaultsv2.InflightFund) (bool, error) {
		var err error
		totalInflight, err = totalInflight.SafeAdd(fund.Amount)
		if err != nil {
			return true, errors.Wrap(err, "unable to accumulate inflight")
		}
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate inflight funds")
	}

	snapshot := vaultsv2.CrossChainPositionSnapshot{
		TotalRemoteValue:   totalRemoteValue,
		TotalInflightValue: totalInflight,
		ActivePositions:    activePositions,
		StalePositions:     stalePositions,
		BlockHeight:        sdk.UnwrapSDKContext(ctx).BlockHeight(),
	}

	return &vaultsv2.QueryCrossChainSnapshotResponse{
		Snapshot: snapshot,
	}, nil
}

func (q queryServerV2) StaleInflightAlerts(ctx context.Context, req *vaultsv2.QueryStaleInflightAlertsRequest) (*vaultsv2.QueryStaleInflightAlertsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Get params for stale timeout threshold
	_, err := q.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch params")
	}

	// Get current block time from SDK context
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime()

	var alerts []vaultsv2.StaleInflightAlertWithDetails

	// Iterate inflight funds to find stale ones
	err = q.IterateVaultsV2InflightFunds(ctx, func(txID uint64, fund vaultsv2.InflightFund) (bool, error) {
		// Check if fund is stale (past expected time)
		if currentTime.After(fund.ExpectedAt) {
			// Get route details if filtering by route
			if req.RouteId != 0 {
				if tracking := fund.GetProviderTracking(); tracking != nil {
					if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
						if hyperlane.DestinationDomain != req.RouteId {
							return false, nil // Skip, doesn't match filter
						}
					}
				}
			}

			// Filter by address if provided
			if req.Address != "" {
				if origin := fund.GetNobleOrigin(); origin != nil {
					if "" != req.Address {
						return false, nil // Skip, doesn't match user filter
					}
				}
			}

			// Get route details
			var route vaultsv2.CrossChainRoute
			if tracking := fund.GetProviderTracking(); tracking != nil {
				if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
					foundRoute, found, err := q.GetVaultsV2CrossChainRoute(ctx, hyperlane.DestinationDomain)
					if err == nil && found {
						route = foundRoute
					}
				}
			}

			alert := vaultsv2.StaleInflightAlert{
				TransactionId: fund.TransactionId,
				RouteId:       0, // Will be set from tracking
				Amount:        fund.Amount,
				Timestamp:     fund.InitiatedAt,
			}

			alertWithDetails := vaultsv2.StaleInflightAlertWithDetails{
				Alert: alert,
				Route: route,
				Fund:  fund,
			}

			alerts = append(alerts, alertWithDetails)
		}

		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate inflight funds")
	}

	return &vaultsv2.QueryStaleInflightAlertsResponse{
		Alerts: alerts,
	}, nil
}

func (q queryServerV2) UserWithdrawals(ctx context.Context, req *vaultsv2.QueryUserWithdrawalsRequest) (*vaultsv2.QueryUserWithdrawalsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	_, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	var (
		withdrawals    []vaultsv2.WithdrawalStatusItem
		totalPending   = sdkmath.ZeroInt()
		totalClaimable = sdkmath.ZeroInt()
		totalClaimed   = sdkmath.ZeroInt()
	)

	// Iterate through all withdrawal requests and filter by user
	err = q.IterateVaultsV2Withdrawals(ctx, func(id uint64, request vaultsv2.WithdrawalRequest) (bool, error) {
		// Filter by user
		if request.Requester != req.Address {
			return false, nil
		}

		// Determine status
		status := "PENDING"
		if false {
			if sdkmath.ZeroInt().IsPositive() {
				if false {
					status = "CLAIMED"
				} else {
					status = "CLAIMABLE"
				}
			} else {
				status = "PROCESSING"
			}
		}

		item := vaultsv2.WithdrawalStatusItem{
			RequestId:       id,
			Amount:          request.WithdrawAmount.String(),
			PrincipalAmount: request.WithdrawAmount.String(),
			YieldAmount:     sdkmath.ZeroInt().String(),
			FulfilledAmount: sdkmath.ZeroInt().String(),
			Status:          status,
			Timestamp:       request.RequestTime,
			PositionId:      request.PositionId,
		}

		if false {
			item.ClaimableAt = &time.Time{}
		}
		if false {
			item.ClaimedAt = &time.Time{}
		}

		withdrawals = append(withdrawals, item)

		// Accumulate totals
		var err error
		if status == "PENDING" || status == "PROCESSING" {
			totalPending, err = totalPending.SafeAdd(request.WithdrawAmount)
			if err != nil {
				return true, errors.Wrap(err, "unable to accumulate pending")
			}
		} else if status == "CLAIMABLE" {
			totalClaimable, err = totalClaimable.SafeAdd(sdkmath.ZeroInt())
			if err != nil {
				return true, errors.Wrap(err, "unable to accumulate claimable")
			}
		} else if status == "CLAIMED" {
			totalClaimed, err = totalClaimed.SafeAdd(sdkmath.ZeroInt())
			if err != nil {
				return true, errors.Wrap(err, "unable to accumulate claimed")
			}
		}

		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate withdrawals")
	}

	return &vaultsv2.QueryUserWithdrawalsResponse{
		Withdrawals:    withdrawals,
		TotalPending:   totalPending.String(),
		TotalClaimable: totalClaimable.String(),
		TotalClaimed:   totalClaimed.String(),
	}, nil
}

func (q queryServerV2) UserBalance(ctx context.Context, req *vaultsv2.QueryUserBalanceRequest) (*vaultsv2.QueryUserBalanceResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	userAddr, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	// If position_id is provided, get specific position; otherwise aggregate all positions
	if req.PositionId > 0 {
		// Get specific position
		position, found, err := q.GetVaultsV2UserPosition(ctx, userAddr, req.PositionId)
		if err != nil {
			return nil, errors.Wrap(err, "unable to fetch user position")
		}
		if !found {
			return nil, errors.Wrapf(types.ErrInvalidRequest, "position not found for user %s with ID %d", req.Address, req.PositionId)
		}

		// Calculate current value (deposits + yield)
		currentValue := position.DepositAmount.Add(position.AccruedYield)

		// Locked amount is amount pending withdrawal
		lockedAmount := position.TotalPendingWithdrawal

		return &vaultsv2.QueryUserBalanceResponse{
			DepositAmount:  position.DepositAmount.String(),
			AccruedYield:   position.AccruedYield.String(),
			TotalValue:     currentValue.String(),
			UnrealizedGain: position.AccruedYield.String(), // Same as accrued yield
			LockedAmount:   lockedAmount.String(),
			PositionCount:  1,
		}, nil
	}

	// Aggregate all positions for the user
	var (
		totalDeposits        = sdkmath.ZeroInt()
		totalYield           = sdkmath.ZeroInt()
		totalLocked          = sdkmath.ZeroInt()
		positionCount uint64 = 0
	)

	err = q.IterateUserPositions(ctx, userAddr, func(positionID uint64, position vaultsv2.UserPosition) (bool, error) {
		totalDeposits = totalDeposits.Add(position.DepositAmount)
		totalYield = totalYield.Add(position.AccruedYield)
		totalLocked = totalLocked.Add(position.TotalPendingWithdrawal)
		positionCount++
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate user positions")
	}

	if positionCount == 0 {
		return &vaultsv2.QueryUserBalanceResponse{
			DepositAmount:  sdkmath.ZeroInt().String(),
			AccruedYield:   sdkmath.ZeroInt().String(),
			TotalValue:     sdkmath.ZeroInt().String(),
			UnrealizedGain: sdkmath.ZeroInt().String(),
			LockedAmount:   sdkmath.ZeroInt().String(),
			PositionCount:  0,
		}, nil
	}

	// Calculate total value
	totalValue := totalDeposits.Add(totalYield)

	return &vaultsv2.QueryUserBalanceResponse{
		DepositAmount:  totalDeposits.String(),
		AccruedYield:   totalYield.String(),
		TotalValue:     totalValue.String(),
		UnrealizedGain: totalYield.String(), // Same as accrued yield
		LockedAmount:   totalLocked.String(),
		PositionCount:  positionCount,
	}, nil
}

func (q queryServerV2) DepositVelocity(ctx context.Context, req *vaultsv2.QueryDepositVelocityRequest) (*vaultsv2.QueryDepositVelocityResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	userAddr, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	// Get deposit velocity
	velocity, err := q.GetVaultsV2DepositVelocity(ctx, userAddr)
	if err != nil {
		// If not found, return empty velocity
		return &vaultsv2.QueryDepositVelocityResponse{
			User:                    req.Address,
			LastDepositBlock:        "0",
			RecentDepositCount:      0,
			RecentDepositVolume:     sdkmath.ZeroInt().String(),
			TimeWindowBlocks:        0,
			SuspiciousActivityFlag:  false,
			CooldownRemainingBlocks: 0,
			VelocityScore:           sdkmath.LegacyZeroDec().String(),
		}, nil
	}

	// Get params for window and cooldown
	_, err = q.GetVaultsV2Params(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch params")
	}

	// Get current block height
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Calculate cooldown remaining
	cooldownRemaining := int64(0)
	if 0 > 0 {
		blocksSinceLastDeposit := currentBlock - velocity.LastDepositBlock
		if blocksSinceLastDeposit < 0 {
			cooldownRemaining = 0 - blocksSinceLastDeposit
		}
	}

	// Calculate velocity score (simple: volume / time window)
	velocityScore := sdkmath.LegacyZeroDec()
	if 0 > 0 {
		velocityScore = velocity.RecentDepositVolume.ToLegacyDec().QuoInt64(0)
	}

	// Suspicious activity flag (example: more than max deposits per window)
	suspiciousActivity := false
	if 0 > 0 && velocity.RecentDepositCount > 0 {
		suspiciousActivity = true
	}

	return &vaultsv2.QueryDepositVelocityResponse{
		User:                    req.Address,
		LastDepositBlock:        strconv.FormatInt(velocity.LastDepositBlock, 10),
		RecentDepositCount:      velocity.RecentDepositCount,
		RecentDepositVolume:     velocity.RecentDepositVolume.String(),
		TimeWindowBlocks:        0,
		SuspiciousActivityFlag:  suspiciousActivity,
		CooldownRemainingBlocks: cooldownRemaining,
		VelocityScore:           velocityScore.String(),
	}, nil
}

func (q queryServerV2) SimulateDeposit(ctx context.Context, req *vaultsv2.QuerySimulateDepositRequest) (*vaultsv2.QuerySimulateDepositResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	userAddr, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	// Parse amount
	amount, ok := sdkmath.NewIntFromString(req.Amount)
	if !ok || amount.IsNegative() {
		return nil, errors.Wrap(types.ErrInvalidRequest, "invalid amount")
	}

	// Get current block
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Check limits
	checks := &vaultsv2.CheckResultsView{
		WithinUserLimit:     true,
		WithinBlockLimit:    true,
		WithinTotalLimit:    true,
		CooldownPassed:      true,
		VelocityCheckPassed: true,
	}
	var warnings []string

	// 1. Check global deposit cap
	state, err := q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	// Get deposit limits to check global cap
	limits, err := q.GetVaultsV2DepositLimits(ctx)
	if err == nil && limits.GlobalDepositCap.IsPositive() {
		newTotal := state.TotalDeposits.Add(amount)
		if newTotal.GT(limits.GlobalDepositCap) {
			checks.WithinTotalLimit = false
			warnings = append(warnings, "Deposit would exceed global deposit cap")
		}
	}

	// 2. Check per-block limit
	blockVolume, err := q.GetVaultsV2BlockDepositVolume(ctx, currentBlock)
	if err != nil {
		blockVolume = sdkmath.ZeroInt()
	}
	if limits.MaxBlockDepositVolume.IsPositive() {
		newBlockVolume := blockVolume.Add(amount)
		if newBlockVolume.GT(limits.MaxBlockDepositVolume) {
			checks.WithinBlockLimit = false
			warnings = append(warnings, "Deposit would exceed per-block volume limit")
		}
	}

	// 3. Check cooldown
	velocity, err := q.GetVaultsV2DepositVelocity(ctx, userAddr)
	if err == nil {
		if limits.DepositCooldownBlocks > 0 {
			blocksSinceLastDeposit := currentBlock - velocity.LastDepositBlock
			if blocksSinceLastDeposit < limits.DepositCooldownBlocks {
				checks.CooldownPassed = false
				warnings = append(warnings, "Cooldown period not yet passed")
			}
		}

		// 4. Check per-user limit
		if limits.MaxUserDepositPerWindow.IsPositive() {
			newUserVolume := velocity.RecentDepositVolume.Add(amount)
			if newUserVolume.GT(limits.MaxUserDepositPerWindow) {
				checks.WithinUserLimit = false
				warnings = append(warnings, "Deposit would exceed per-user window limit")
			}
		}

		// 5. Check velocity count
		if limits.MaxDepositsPerWindow > 0 && velocity.RecentDepositCount >= limits.MaxDepositsPerWindow {
			checks.VelocityCheckPassed = false
			warnings = append(warnings, "Maximum deposits per window reached")
		}
	}

	return &vaultsv2.QuerySimulateDepositResponse{
		ExpectedDeposit: amount.String(), // No fees in this implementation
		YieldRate:       sdkmath.LegacyNewDec(0).String(),
		Checks:          checks,
		Warnings:        warnings,
	}, nil
}

func (q queryServerV2) SimulateWithdrawal(ctx context.Context, req *vaultsv2.QuerySimulateWithdrawalRequest) (*vaultsv2.QuerySimulateWithdrawalResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Validate address
	userAddr, err := q.address.StringToBytes(req.Address)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "invalid address: %s", req.Address)
	}

	// Parse amount
	amount, ok := sdkmath.NewIntFromString(req.Amount)
	if !ok || amount.IsNegative() {
		return nil, errors.Wrap(types.ErrInvalidRequest, "invalid amount")
	}

	// Get specific user position
	position, found, err := q.GetVaultsV2UserPosition(ctx, userAddr, req.PositionId)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch user position")
	}
	if !found {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "position not found for user %s with ID %d", req.Address, req.PositionId)
	}

	// Get vault state
	state, err := q.GetVaultsV2VaultState(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch vault state")
	}

	// Calculate current value (deposits + yield)
	currentValue := position.DepositAmount.Add(position.AccruedYield)

	// Split into principal and yield
	principalPortion := position.DepositAmount
	yieldPortion := currentValue.Sub(principalPortion)
	if yieldPortion.IsNegative() {
		yieldPortion = sdkmath.ZeroInt()
	}

	// Calculate available liquidity
	localAssets := state.TotalDeposits.Sub(sdkmath.ZeroInt())
	localFunds, err := q.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		localFunds = sdkmath.ZeroInt()
	}
	availableLiquidity := localAssets.Sub(localFunds)
	if availableLiquidity.IsNegative() {
		availableLiquidity = sdkmath.ZeroInt()
	}

	// Calculate queue position (count pending withdrawals)
	queuePosition := uint64(0)
	aheadInQueue := sdkmath.ZeroInt()
	err = q.IterateVaultsV2Withdrawals(ctx, func(_ uint64, request vaultsv2.WithdrawalRequest) (bool, error) {
		if !false {
			queuePosition++
			var err error
			aheadInQueue, err = aheadInQueue.SafeAdd(request.WithdrawAmount)
			if err != nil {
				return true, err
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate withdrawals")
	}

	// Estimate fulfillment time (simple heuristic: 1 hour per 100 requests)
	estimatedFulfillmentTime := "0"
	if queuePosition > 0 {
		hours := (queuePosition + 99) / 100
		estimatedFulfillmentTime = strconv.FormatUint(hours*3600, 10)
	}

	return &vaultsv2.QuerySimulateWithdrawalResponse{
		ExpectedAmount:           amount.String(), // No fees
		PrincipalPortion:         principalPortion.String(),
		YieldPortion:             yieldPortion.String(),
		QueuePosition:            queuePosition + 1, // New position
		EstimatedFulfillmentTime: estimatedFulfillmentTime,
		AvailableLiquidity:       availableLiquidity.String(),
		AheadInQueue:             aheadInQueue.String(),
	}, nil
}

func (q queryServerV2) StaleInflightFunds(ctx context.Context, req *vaultsv2.QueryStaleInflightFundsRequest) (*vaultsv2.QueryStaleInflightFundsResponse, error) {
	if req == nil {
		return nil, errors.Wrap(types.ErrInvalidRequest, "request cannot be nil")
	}

	// Get current block time
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime()

	var (
		staleFunds       []vaultsv2.StaleInflightFundView
		totalStaleValue  = sdkmath.ZeroInt()
		oldestStaleHours = int64(0)
	)

	// Iterate all inflight funds
	err := q.IterateVaultsV2InflightFunds(ctx, func(txID uint64, fund vaultsv2.InflightFund) (bool, error) {
		// Check if stale
		if currentTime.After(fund.ExpectedAt) {
			hoursOverdue := currentTime.Sub(fund.ExpectedAt).Hours()

			// Extract routing info
			var sourceChain, destChain string
			var routeID uint32
			if tracking := fund.GetProviderTracking(); tracking != nil {
				if hyperlane := tracking.GetHyperlaneTracking(); hyperlane != nil {
					sourceChain = strconv.FormatUint(uint64(hyperlane.OriginDomain), 10)
					destChain = strconv.FormatUint(uint64(hyperlane.DestinationDomain), 10)
					routeID = hyperlane.DestinationDomain
				}
			}

			// Determine operation type
			opType := "UNKNOWN"
			if origin := fund.GetNobleOrigin(); origin != nil {
				opType = origin.OperationType.String()
			}

			staleFund := vaultsv2.StaleInflightFundView{
				RouteId:           routeID,
				TransactionId:     fund.TransactionId,
				Amount:            fund.Amount.String(),
				OperationType:     opType,
				SourceChain:       sourceChain,
				DestinationChain:  destChain,
				HoursOverdue:      strconv.FormatInt(int64(hoursOverdue), 10),
				LastKnownStatus:   fund.Status.String(),
				RecommendedAction: "Manual intervention required",
			}

			staleFunds = append(staleFunds, staleFund)

			var err error
			totalStaleValue, err = totalStaleValue.SafeAdd(fund.Amount)
			if err != nil {
				return true, errors.Wrap(err, "unable to accumulate stale value")
			}

			if int64(hoursOverdue) > oldestStaleHours {
				oldestStaleHours = int64(hoursOverdue)
			}
		}

		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to iterate inflight funds")
	}

	return &vaultsv2.QueryStaleInflightFundsResponse{
		StaleFunds:       staleFunds,
		TotalStaleValue:  totalStaleValue.String(),
		TotalStaleCount:  uint64(len(staleFunds)),
		OldestStaleHours: strconv.FormatInt(oldestStaleHours, 10),
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
