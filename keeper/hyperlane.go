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
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
)

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
