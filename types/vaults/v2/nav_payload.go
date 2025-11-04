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

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"cosmossdk.io/math"
)

const (
	navPayloadSize       = 105
	navUpdateMessageType = 0x01
	navDecimalPrecision  = int64(1_000_000_000_000_000_000) // 1e18 precision
)

// NAVUpdatePayload represents the decoded NAV oracle update carried inside a
// Hyperlane message body.
type NAVUpdatePayload struct {
	MessageType uint8
	PositionID  uint64
	SharePrice  math.LegacyDec
	SharesHeld  math.Int
	Timestamp   time.Time
}

// ParseNAVPayload decodes the fixed-length NAV oracle payload into a strongly
// typed representation. All numeric values are expected to be big-endian
// encoded.
func ParseNAVPayload(body []byte) (NAVUpdatePayload, error) {
	if len(body) != navPayloadSize {
		return NAVUpdatePayload{}, fmt.Errorf("invalid NAV payload size: expected %d, got %d", navPayloadSize, len(body))
	}

	if body[0] != navUpdateMessageType {
		return NAVUpdatePayload{}, fmt.Errorf("invalid NAV payload message type 0x%02x", body[0])
	}

	positionBig := new(big.Int).SetBytes(body[1:33])
	if !positionBig.IsUint64() {
		return NAVUpdatePayload{}, fmt.Errorf("position identifier exceeds uint64 range")
	}

	sharePriceBig := new(big.Int).SetBytes(body[33:65])
	sharesHeldBig := new(big.Int).SetBytes(body[65:97])
	timestamp := int64(binary.BigEndian.Uint64(body[97:105]))
	scale := math.NewInt(navDecimalPrecision)
	sharePrice := math.LegacyNewDecFromBigInt(sharePriceBig).QuoInt(scale)
	sharesHeld := math.NewIntFromBigInt(sharesHeldBig)

	return NAVUpdatePayload{
		MessageType: body[0],
		PositionID:  positionBig.Uint64(),
		SharePrice:  sharePrice,
		SharesHeld:  sharesHeld,
		Timestamp:   time.Unix(timestamp, 0).UTC(),
	}, nil
}
