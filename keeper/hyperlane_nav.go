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
	"cosmossdk.io/math"
	hyperlaneutil "github.com/bcp-innovations/hyperlane-cosmos/util"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

const basisPointsMultiplier = 10_000

// HandleHyperlaneNAVMessage processes a Hyperlane message containing NAV data
// for a remote position oracle. The function performs basic authentication by
// checking the message origin and sender against the enrolled oracle record
// before applying the NAV update and recalculating the aggregate vault NAV.
func (k *Keeper) HandleHyperlaneNAVMessage(ctx context.Context, mailboxID hyperlaneutil.HexAddress, message hyperlaneutil.HyperlaneMessage) (*vaultsv2.OracleUpdateResult, error) {
	if mailboxID.IsZeroAddress() {
		return nil, errors.Wrap(vaultsv2.ErrOperationNotPermitted, "mailbox identifier must be provided")
	}

	payload, err := vaultsv2.ParseNAVPayload(message.Body)
	if err != nil {
		return nil, errors.Wrap(err, "unable to parse NAV payload")
	}

	oracle, found, err := k.GetVaultsV2RemotePositionOracle(ctx, payload.PositionID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote position oracle")
	}
	if !found {
		return nil, errors.Wrapf(vaultsv2.ErrRemotePositionNotFound, "position %d", payload.PositionID)
	}

	if message.Origin != oracle.ChainId {
		return nil, errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "unexpected message origin %d (expected %d)", message.Origin, oracle.ChainId)
	}

	if !message.Sender.Equal(oracle.OracleAddress) {
		return nil, errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "unexpected oracle sender %s (expected %s)", message.Sender.String(), oracle.OracleAddress.String())
	}

	if !oracle.LastUpdate.IsZero() && payload.Timestamp.Before(oracle.LastUpdate) {
		return nil, errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "stale update for position %d", payload.PositionID)
	}

	oracle.SharePrice = payload.SharePrice
	oracle.SharesHeld = payload.SharesHeld
	oracle.LastUpdate = payload.Timestamp

	if err := k.SetVaultsV2RemotePositionOracle(ctx, payload.PositionID, oracle); err != nil {
		return nil, errors.Wrap(err, "unable to persist remote position oracle")
	}

	position, found, err := k.GetVaultsV2RemotePosition(ctx, payload.PositionID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch remote position")
	}
	if found {
		position.SharePrice = payload.SharePrice
		position.SharesHeld = payload.SharesHeld
		position.TotalValue = payload.SharePrice.MulInt(payload.SharesHeld).TruncateInt()
		position.LastUpdate = payload.Timestamp
		if position.TotalValue.IsPositive() {
			position.Status = vaultsv2.REMOTE_POSITION_ACTIVE
		} else {
			position.Status = vaultsv2.REMOTE_POSITION_CLOSED
		}

		if payload.InflightAckID != 0 {
			inflightID := strconv.FormatUint(payload.InflightAckID, 10)
			fund, inflightFound, err := k.GetVaultsV2InflightFund(ctx, inflightID)
			if err != nil {
				return nil, errors.Wrap(err, "unable to fetch inflight fund for acknowledgement")
			}

			if inflightFound {
				fund.Status = vaultsv2.INFLIGHT_COMPLETED
				fund.ExpectedAt = payload.Timestamp
				if fund.ProviderTracking != nil {
					if tracking := fund.ProviderTracking.GetHyperlaneTracking(); tracking != nil {
						tracking.Processed = true
					}
				}

				if err := k.SetVaultsV2InflightFund(ctx, fund); err != nil {
					return nil, errors.Wrap(err, "unable to persist inflight fund acknowledgement")
				}

				if fund.GetRemoteDestination() != nil {
					principal, err := position.Principal.SafeAdd(fund.Amount)
					if err != nil {
						return nil, errors.Wrap(err, "unable to add inflight amount to remote position principal")
					}
					position.Principal = principal
				} else if fund.GetRemoteOrigin() != nil {
					principal, err := position.Principal.SafeSub(fund.Amount)
					if err != nil {
						return nil, errors.Wrap(err, "unable to subtract inflight amount from remote position principal")
					}
					position.Principal = principal
				}
			}
		}

		if err := k.SetVaultsV2RemotePosition(ctx, payload.PositionID, position); err != nil {
			return nil, errors.Wrap(err, "unable to persist remote position")
		}
	}

	updatedNav, err := k.RecalculateVaultsV2NAV(ctx, payload.Timestamp)
	if err != nil {
		return nil, err
	}

	positionValue := payload.SharePrice.MulInt(payload.SharesHeld).TruncateInt()

	return &vaultsv2.OracleUpdateResult{
		PositionId:    payload.PositionID,
		NewSharePrice: payload.SharePrice,
		NewShares:     payload.SharesHeld,
		PositionValue: positionValue,
		UpdatedNav:    updatedNav,
	}, nil
}

// RecalculateVaultsV2NAV recalculates the vault NAV from all components:
// pending deployment funds, remote positions, and inflight funds.
// This is called when oracle updates remote position values.
func (k *Keeper) RecalculateVaultsV2NAV(ctx context.Context, timestamp time.Time) (math.Int, error) {
	// Check if accounting is currently in progress
	cursor, err := k.GetVaultsV2AccountingCursor(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch accounting cursor")
	}

	if cursor.InProgress {
		return math.ZeroInt(), errors.Wrapf(
			vaultsv2.ErrOperationNotPermitted,
			"cannot update NAV while accounting is in progress (started at %s, %d/%d positions processed for NAV %s). "+
				"Complete the current accounting session before processing oracle updates",
			cursor.StartedAt.String(),
			cursor.PositionsProcessed,
			cursor.TotalPositions,
			cursor.AccountingNav.String(),
		)
	}

	// Calculate NAV according to spec: Local Assets + Remote Positions + Inflight Funds
	total := math.ZeroInt()

	// 1. Get local vault assets (liquidity available for deployment or withdrawals)
	localFunds, err := k.GetVaultsV2LocalFunds(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to get local funds")
	}
	total, err = total.SafeAdd(localFunds)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to add local funds to NAV")
	}

	// 2. Add remote position values
	err = k.IterateVaultsV2RemotePositions(ctx, func(_ uint64, position vaultsv2.RemotePosition) (bool, error) {
		var err error
		total, err = total.SafeAdd(position.TotalValue)
		if err != nil {
			return true, errors.Wrap(err, "unable to accumulate remote position value")
		}

		return false, nil
	})
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to iterate remote position oracles")
	}

	// 3. Add inflight funds
	err = k.IterateVaultsV2InflightFunds(ctx, func(_ string, fund vaultsv2.InflightFund) (bool, error) {
		var err error
		total, err = total.SafeAdd(fund.Amount)
		if err != nil {
			return true, errors.Wrap(err, "unable to accumulate inflight funds")
		}
		return false, nil
	})
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to iterate inflight funds")
	}

	navInfo, err := k.GetVaultsV2NAVInfo(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch NAV info")
	}

	// Initialize NAV values if they are nil (first time setup)
	if navInfo.CurrentNav.IsNil() {
		navInfo.CurrentNav = math.ZeroInt()
	}
	if navInfo.PreviousNav.IsNil() {
		navInfo.PreviousNav = math.ZeroInt()
	}

	previousNav := navInfo.CurrentNav
	navInfo.PreviousNav = previousNav
	navInfo.CurrentNav = total
	navInfo.LastUpdate = timestamp
	navInfo.ChangeBps = 0

	if previousNav.IsPositive() {
		change := total.ToLegacyDec().Sub(previousNav.ToLegacyDec())
		changeBps := change.MulInt(math.NewInt(basisPointsMultiplier)).QuoInt(previousNav).TruncateInt64()
		navInfo.ChangeBps = int32(changeBps)
	}

	if err := k.SetVaultsV2NAVInfo(ctx, navInfo); err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to persist NAV info")
	}

	state, err := k.GetVaultsV2VaultState(ctx)
	if err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to fetch vault state")
	}

	state.TotalNav = total
	state.LastNavUpdate = timestamp

	if err := k.SetVaultsV2VaultState(ctx, state); err != nil {
		return math.ZeroInt(), errors.Wrap(err, "unable to persist vault state")
	}

	return total, nil
}
