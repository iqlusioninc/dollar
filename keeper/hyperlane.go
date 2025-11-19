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
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	hyperlaneutil "github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"

	vaultsv2 "dollar.noble.xyz/v3/types/vaults/v2"
)

// HyperlaneApp implementation

// Handle processes a Hyperlane message containing AUM data
// for a remote position oracle. The function performs basic authentication by
// checking the message origin and sender against the enrolled oracle record
// before applying the AUM update and recalculating the aggregate vault AUM.
func (k *Keeper) Handle(ctx context.Context, mailboxID hyperlaneutil.HexAddress, message hyperlaneutil.HyperlaneMessage) error {
	if mailboxID.IsZeroAddress() {
		return errors.Wrap(vaultsv2.ErrOperationNotPermitted, "mailbox identifier must be provided")
	}

	if err := k.checkAccountingNotInProgress(ctx); err != nil {
		return err
	}

	payload, err := vaultsv2.ParseAUMPayload(message.Body)
	if err != nil {
		return errors.Wrap(err, "unable to parse AUM payload")
	}

	oracle, found, err := k.GetVaultsV2RemotePositionOracle(ctx, payload.PositionID)
	if err != nil {
		return errors.Wrap(err, "unable to fetch remote position oracle")
	}
	if !found {
		return errors.Wrapf(vaultsv2.ErrRemotePositionNotFound, "position %d", payload.PositionID)
	}

	if message.Origin != oracle.ChainId {
		return errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "unexpected message origin %d (expected %d)", message.Origin, oracle.ChainId)
	}

	if !message.Sender.Equal(oracle.OracleAddress) {
		return errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "unexpected oracle sender %s (expected %s)", message.Sender.String(), oracle.OracleAddress.String())
	}

	if !oracle.LastUpdate.IsZero() && payload.Timestamp.Before(oracle.LastUpdate) {
		return errors.Wrapf(vaultsv2.ErrOperationNotPermitted, "stale update for position %d", payload.PositionID)
	}

	oracle.SharePrice = payload.SharePrice
	oracle.SharesHeld = payload.SharesHeld
	oracle.LastUpdate = payload.Timestamp

	if err := k.SetVaultsV2RemotePositionOracle(ctx, payload.PositionID, oracle); err != nil {
		return errors.Wrap(err, "unable to persist remote position oracle")
	}

	position, found, err := k.GetVaultsV2RemotePosition(ctx, payload.PositionID)
	if err != nil {
		return errors.Wrap(err, "unable to fetch remote position")
	}
	if found {
		position.SharePrice = payload.SharePrice
		position.SharesHeld = payload.SharesHeld
		position.TotalValue = payload.SharePrice.MulInt(payload.SharesHeld).TruncateInt()
		position.LastUpdate = payload.Timestamp

		if err := k.SetVaultsV2RemotePosition(ctx, payload.PositionID, position); err != nil {
			return errors.Wrap(err, "unable to persist remote position")
		}
	}

	_, err = k.RecalculateVaultsV2AUMFromOracle(ctx, payload.Timestamp, payload.PositionID)
	if err != nil {
		return err
	}

	// positionValue := payload.SharePrice.MulInt(payload.SharesHeld).TruncateInt()

	// &vaultsv2.OracleUpdateResult{
	// 	PositionId:    payload.PositionID,
	// 	NewSharePrice: payload.SharePrice,
	// 	NewShares:     payload.SharesHeld,
	// 	PositionValue: positionValue,
	// 	UpdatedAum:    updatedAum,
	// }, nil

	return nil
}

// TODO: Figure out how to properly implement Exists and ReceiverIsmId (or if they're needed at all)
func (k *Keeper) Exists(ctx context.Context, recipient util.HexAddress) (bool, error) {
	return true, nil
}

func (k *Keeper) ReceiverIsmId(ctx context.Context, recipient util.HexAddress) (*util.HexAddress, error) {
	return nil, nil
}

// getHyperlaneRouter returns the remote router enrolled for a Hyperlane Warp
// route. The function errors if it can't find any routers or if there are
// multiple routers. This is because, to correctly distribute yield, we need
// only one router enrolled.
func (k *Keeper) getHyperlaneRouter(ctx context.Context, tokenID uint64) (warptypes.RemoteRouter, error) {
	var routers []warptypes.RemoteRouter

	ranger := collections.NewPrefixedPairRange[uint64, uint32](tokenID)
	err := k.warp.EnrolledRouters.Walk(
		ctx, ranger,
		func(_ collections.Pair[uint64, uint32], router warptypes.RemoteRouter) (bool, error) {
			routers = append(routers, router)
			return false, nil
		},
	)
	if err != nil {
		return warptypes.RemoteRouter{}, errors.Wrapf(err, "unable to get routers for hyperlane identifier %d", tokenID)
	}

	if len(routers) != 1 {
		return warptypes.RemoteRouter{}, fmt.Errorf("expected only one router for hyperlane identifier %d", tokenID)
	}

	return routers[0], nil
}
