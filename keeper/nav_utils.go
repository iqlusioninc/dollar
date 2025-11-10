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

import "cosmossdk.io/math"

const basisPointsMultiplier = 10_000

// ComputeNAVChangeBps calculates the change in basis points between two NAV values
func ComputeNAVChangeBps(currentNav, previousNav math.Int) int32 {
	if previousNav.IsZero() {
		return 0
	}
	previousDec := previousNav.ToLegacyDec()
	if previousDec.IsZero() {
		return 0
	}
	delta := currentNav.ToLegacyDec().Sub(previousDec)
	changeDec := delta.MulInt(math.NewInt(basisPointsMultiplier)).QuoInt(previousNav)
	return int32(changeDec.TruncateInt64())
}
