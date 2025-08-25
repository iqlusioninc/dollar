package keeper

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	vaultsv2 "dollar.noble.xyz/v2/types/vaults/v2"
)

// V2OracleKeeper handles oracle-related operations for V2 vault NAV updates
type V2OracleKeeper struct {
	keeper *Keeper
}

// NewV2OracleKeeper creates a new V2OracleKeeper instance
func NewV2OracleKeeper(k *Keeper) *V2OracleKeeper {
	return &V2OracleKeeper{
		keeper: k,
	}
}

// Constants for oracle operations
const (
	// Message type identifiers
	NAVUpdateMessageType byte = 0x01

	// Payload sizes (in bytes)
	NAVPayloadSize = 105
	PositionIDSize = 32
	SharePriceSize = 32
	SharesHeldSize = 32
	TimestampSize  = 8

	// Staleness levels
	StalenessLevelFresh    = "fresh"
	StalenessLevelWarning  = "warning"
	StalenessLevelCritical = "critical"
	StalenessLevelStale    = "stale"

	// Default staleness thresholds (in seconds)
	DefaultWarningThreshold  = 3600  // 1 hour
	DefaultCriticalThreshold = 14400 // 4 hours
	DefaultMaxStaleness      = 86400 // 24 hours
)

// RegisterOracle registers a new oracle for a V2 vault position
func (k *V2OracleKeeper) RegisterOracle(
	ctx context.Context,
	positionID string,
	oracleAddress string,
	chainID uint32,
	maxStaleness int64,
	originDomain uint32,
	originMailbox string,
) (*vaultsv2.EnrolledOracle, error) {
	// Validate inputs
	if positionID == "" {
		return nil, fmt.Errorf("position ID cannot be empty")
	}
	if oracleAddress == "" {
		return nil, fmt.Errorf("oracle address cannot be empty")
	}
	if chainID == 0 {
		return nil, fmt.Errorf("chain ID must be non-zero")
	}
	if maxStaleness <= 0 {
		return nil, fmt.Errorf("max staleness must be positive")
	}

	// Check if oracle already exists for this position
	existingOracle, err := k.GetOracleForPosition(ctx, positionID)
	if err == nil && existingOracle != nil {
		return nil, fmt.Errorf("oracle already registered for position %s", positionID)
	}

	// Create enrolled oracle
	oracle := &vaultsv2.EnrolledOracle{
		PositionId:    positionID,
		OriginDomain:  originDomain,
		OracleAddress: oracleAddress,
		MaxStaleness:  maxStaleness,
		RegisteredAt:  sdk.UnwrapSDKContext(ctx).BlockTime(),
		Active:        true,
	}

	// Store oracle
	oracleID := k.generateOracleID(positionID, oracleAddress, chainID)
	if err := k.keeper.V2Collections.EnrolledOracles.Set(ctx, oracleID, *oracle); err != nil {
		return nil, fmt.Errorf("failed to store oracle: %w", err)
	}

	// Store position-to-oracle mapping
	if err := k.keeper.V2Collections.OracleMappings.Set(ctx, positionID, oracleID); err != nil {
		return nil, fmt.Errorf("failed to store oracle mapping: %w", err)
	}

	// Store oracle config
	config := vaultsv2.PositionOracleConfig{
		PositionId:    positionID,
		OriginMailbox: originMailbox,
		MaxStaleness:  maxStaleness,
	}
	if err := k.keeper.V2Collections.OracleConfigs.Set(ctx, positionID, config); err != nil {
		return nil, fmt.Errorf("failed to store oracle config: %w", err)
	}

	return oracle, nil
}

// ProcessOracleUpdate processes a V2 vault oracle update from Hyperlane
// This is called internally by the Hyperlane handler, not directly from a message
func (k *V2OracleKeeper) ProcessOracleUpdate(
	ctx context.Context,
	originDomain uint32,
	sender []byte,
	message []byte,
) (*vaultsv2.OracleUpdateResult, error) {
	// Parse NAV payload from message
	payload, err := k.ParseNAVPayload(message)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NAV payload: %w", err)
	}

	// Validate message type
	if payload.MessageType != uint32(NAVUpdateMessageType) {
		return nil, fmt.Errorf("invalid message type: expected %d, got %d", NAVUpdateMessageType, payload.MessageType)
	}

	// Get oracle for position
	oracle, err := k.GetOracleForPosition(ctx, payload.PositionId)
	if err != nil {
		return nil, fmt.Errorf("no oracle registered for position %s: %w", payload.PositionId, err)
	}

	// Validate origin domain matches
	if oracle.OriginDomain != originDomain {
		return nil, fmt.Errorf("origin domain mismatch: expected %d, got %d", oracle.OriginDomain, originDomain)
	}

	// Validate sender matches oracle address
	if !k.validateSender(oracle, sender) {
		return nil, fmt.Errorf("sender validation failed for position %s", payload.PositionId)
	}

	// Check if oracle is active
	if !oracle.Active {
		return nil, fmt.Errorf("oracle for position %s is not active", payload.PositionId)
	}

	// Check data freshness
	staleness, err := k.CheckDataFreshness(ctx, payload.PositionId, payload.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to check data freshness: %w", err)
	}

	// If data is stale, apply fallback strategy
	if staleness == StalenessLevelStale {
		fallback, err := k.GetFallbackStrategy(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get fallback strategy: %w", err)
		}

		if !fallback.UseLastKnownGood {
			return nil, fmt.Errorf("data is stale and fallback not allowed")
		}

		// Check if cached data is within acceptable age
		position, err := k.GetRemotePositionOracle(ctx, payload.PositionId)
		if err != nil {
			return nil, fmt.Errorf("no cached position data available: %w", err)
		}

		age := sdk.UnwrapSDKContext(ctx).BlockTime().Unix() - position.LastUpdate.Unix()
		if age > fallback.MaxCacheAge {
			return nil, fmt.Errorf("cached data too old: age %d exceeds max %d", age, fallback.MaxCacheAge)
		}

		// Use cached values
		return &vaultsv2.OracleUpdateResult{
			PositionId:         payload.PositionId,
			PreviousSharePrice: position.SharePrice,
			NewSharePrice:      position.SharePrice,
			PreviousShares:     position.SharesHeld,
			NewShares:          position.SharesHeld,
			PositionValue:      position.SharePrice.MulInt(position.SharesHeld).TruncateInt(),
			UpdatedNav:         math.ZeroInt(), // Will be calculated
			Success:            true,
			Error:              "",
		}, nil
	}

	// Apply NAV update
	response, err := k.ApplyNAVUpdate(ctx, oracle, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to apply NAV update: %w", err)
	}

	// Update oracle status
	if err := k.UpdateOracleStatus(ctx, oracle.PositionId, true, ""); err != nil {
		// Log error but don't fail the update
		sdk.UnwrapSDKContext(ctx).Logger().Error("failed to update oracle status", "error", err)
	}

	// Convert response to OracleUpdateResult
	return &vaultsv2.OracleUpdateResult{
		PositionId:         response.PositionId,
		PreviousSharePrice: response.PreviousSharePrice,
		NewSharePrice:      response.NewSharePrice,
		PreviousShares:     response.PreviousShares,
		NewShares:          response.NewShares,
		PositionValue:      response.PositionValue,
		UpdatedNav:         response.UpdatedNav,
		Success:            true,
		Error:              "",
	}, nil
}

// validateSender validates that the message sender matches the registered V2 vault oracle
func (k *V2OracleKeeper) validateSender(oracle *vaultsv2.EnrolledOracle, sender []byte) bool {
	// Convert oracle address to bytes for comparison
	oracleAddrBytes, err := hexStringToBytes(oracle.OracleAddress)
	if err != nil {
		return false
	}
	return bytes.Equal(sender, oracleAddrBytes)
}

// hexStringToBytes converts a hex string to bytes
func hexStringToBytes(hexStr string) ([]byte, error) {
	// Remove 0x prefix if present
	if len(hexStr) >= 2 && hexStr[0:2] == "0x" {
		hexStr = hexStr[2:]
	}
	return hex.DecodeString(hexStr)
}

// ParseNAVPayload parses a 105-byte NAV update payload for V2 vaults
func (k *V2OracleKeeper) ParseNAVPayload(message []byte) (*vaultsv2.NAVPayload, error) {
	if len(message) != NAVPayloadSize {
		return nil, fmt.Errorf("invalid payload size: expected %d, got %d", NAVPayloadSize, len(message))
	}

	payload := &vaultsv2.NAVPayload{}

	// Parse message type (1 byte at offset 0)
	payload.MessageType = uint32(message[0])

	// Parse position ID (32 bytes at offset 1)
	positionIDBytes := message[1:33]
	// Remove trailing zeros and convert to string
	payload.PositionId = string(bytes.TrimRight(positionIDBytes, "\x00"))

	// Parse share price (32 bytes at offset 33, big-endian 1e18 precision)
	sharePriceBytes := message[33:65]
	sharePriceBig := new(big.Int).SetBytes(sharePriceBytes)
	// Convert from 1e18 precision to LegacyDec
	sharePrice := math.LegacyNewDecFromBigIntWithPrec(sharePriceBig, 18)
	payload.SharePrice = sharePrice

	// Parse shares held (32 bytes at offset 65, big-endian 1e18 precision)
	sharesHeldBytes := message[65:97]
	sharesHeldBig := new(big.Int).SetBytes(sharesHeldBytes)
	// Convert from 1e18 precision to Int
	sharesHeld := math.NewIntFromBigInt(sharesHeldBig).QuoRaw(1e18)
	payload.SharesHeld = sharesHeld

	// Parse timestamp (8 bytes at offset 97, big-endian Unix timestamp)
	timestampBytes := message[97:105]
	payload.Timestamp = int64(binary.BigEndian.Uint64(timestampBytes))

	return payload, nil
}

// EncodeOracleUpdate encodes a V2 vault oracle update into the 105-byte format
func (k *V2OracleKeeper) EncodeOracleUpdate(
	positionID string,
	sharePrice math.LegacyDec,
	sharesHeld math.Int,
	timestamp int64,
) ([]byte, error) {
	message := make([]byte, NAVPayloadSize)

	// Set message type (1 byte at offset 0)
	message[0] = NAVUpdateMessageType

	// Set position ID (32 bytes at offset 1)
	positionIDBytes := []byte(positionID)
	if len(positionIDBytes) > 32 {
		return nil, fmt.Errorf("position ID too long: max 32 bytes")
	}
	copy(message[1:33], positionIDBytes)

	// Set share price (32 bytes at offset 33, big-endian 1e18 precision)
	sharePriceBig := sharePrice.MulInt64(1e18).TruncateInt().BigInt()
	sharePriceBytes := sharePriceBig.Bytes()
	if len(sharePriceBytes) > 32 {
		return nil, fmt.Errorf("share price too large")
	}
	// Right-align the bytes (big-endian)
	copy(message[65-len(sharePriceBytes):65], sharePriceBytes)

	// Set shares held (32 bytes at offset 65, big-endian 1e18 precision)
	sharesHeldBig := sharesHeld.MulRaw(1e18).BigInt()
	sharesHeldBytes := sharesHeldBig.Bytes()
	if len(sharesHeldBytes) > 32 {
		return nil, fmt.Errorf("shares held too large")
	}
	// Right-align the bytes (big-endian)
	copy(message[97-len(sharesHeldBytes):97], sharesHeldBytes)

	// Set timestamp (8 bytes at offset 97, big-endian Unix timestamp)
	binary.BigEndian.PutUint64(message[97:105], uint64(timestamp))

	return message, nil
}

// ValidateOracleMessage validates a V2 vault oracle message for a position
func (k *V2OracleKeeper) ValidateOracleMessage(
	ctx context.Context,
	positionID string,
	metadata []byte,
) (*vaultsv2.EnrolledOracle, error) {
	// Get oracle for position
	oracle, err := k.GetOracleForPosition(ctx, positionID)
	if err != nil {
		return nil, fmt.Errorf("no oracle registered for position %s: %w", positionID, err)
	}

	// Check if oracle is active
	if !oracle.Active {
		return nil, fmt.Errorf("oracle for position %s is not active", positionID)
	}

	// Parse metadata to extract origin information
	// This would integrate with Hyperlane ISM validation
	// For now, we'll do basic validation

	// TODO: Implement proper Hyperlane ISM validation
	// This would verify:
	// - Origin domain matches oracle's registered domain
	// - Sender address matches oracle's registered address
	// - Message signature is valid

	return oracle, nil
}

// ApplyNAVUpdate applies a NAV update from a V2 vault oracle
func (k *V2OracleKeeper) ApplyNAVUpdate(
	ctx context.Context,
	oracle *vaultsv2.EnrolledOracle,
	payload *vaultsv2.NAVPayload,
) (*vaultsv2.OracleUpdateResult, error) {
	// Get existing position
	position, err := k.GetRemotePositionOracle(ctx, payload.PositionId)
	var previousSharePrice math.LegacyDec
	var previousShares math.Int

	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return nil, fmt.Errorf("failed to get position: %w", err)
		}
		// New position
		previousSharePrice = math.LegacyZeroDec()
		previousShares = math.ZeroInt()
		position = &vaultsv2.RemotePositionOracle{
			PositionId: payload.PositionId,
		}
	} else {
		previousSharePrice = position.SharePrice
		previousShares = position.SharesHeld
	}

	// Check for excessive price deviation
	params, err := k.GetOracleParams(ctx)
	if err == nil && params != nil && !previousSharePrice.IsZero() {
		deviation := payload.SharePrice.Sub(previousSharePrice).Abs().Quo(previousSharePrice).MulInt64(10000)
		maxDeviation := math.LegacyNewDec(int64(params.MaxPriceDeviationBps))

		if deviation.GT(maxDeviation) {
			return nil, fmt.Errorf("price deviation %s exceeds maximum %s bps",
				deviation.String(), maxDeviation.String())
		}
	}

	// Update position
	position.SharePrice = payload.SharePrice
	position.SharesHeld = payload.SharesHeld
	position.LastUpdate = time.Unix(payload.Timestamp, 0)
	position.ChainId = oracle.OriginDomain
	position.OracleAddress = oracle.OracleAddress

	// Store updated position
	if err := k.keeper.V2Collections.RemotePositionOracles.Set(ctx, payload.PositionId, *position); err != nil {
		return nil, fmt.Errorf("failed to store position: %w", err)
	}

	// Calculate position value
	positionValue := payload.SharePrice.MulInt(payload.SharesHeld).TruncateInt()

	// Update vault NAV
	updatedNAV, err := k.CalculateVaultNAV(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate vault NAV: %w", err)
	}

	// Update vault state with new NAV
	vaultState, err := k.keeper.GetV2VaultState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get vault state: %w", err)
	}

	vaultState.TotalNav = updatedNAV
	vaultState.LastNavUpdate = sdk.UnwrapSDKContext(ctx).BlockTime()
	vaultState.SharePrice = k.keeper.calculateV2SharePrice(vaultState)

	if err := k.keeper.SetV2VaultState(ctx, vaultState); err != nil {
		return nil, fmt.Errorf("failed to update vault state: %w", err)
	}

	// Store NAV update event
	navUpdate := vaultsv2.NAVUpdate{
		PreviousNav: vaultState.TotalNav,
		NewNav:      updatedNAV,
		Timestamp:   sdk.UnwrapSDKContext(ctx).BlockTime(),
		BlockHeight: sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Reason:      fmt.Sprintf("Oracle update for position %s", payload.PositionId),
	}

	timestamp := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if err := k.keeper.V2Collections.NAVUpdates.Set(ctx, timestamp, navUpdate); err != nil {
		// Log error but don't fail the update
		sdk.UnwrapSDKContext(ctx).Logger().Error("failed to store NAV update event", "error", err)
	}

	return &vaultsv2.OracleUpdateResult{
		PositionId:         payload.PositionId,
		PreviousSharePrice: previousSharePrice,
		NewSharePrice:      payload.SharePrice,
		PreviousShares:     previousShares,
		NewShares:          payload.SharesHeld,
		PositionValue:      positionValue,
		UpdatedNav:         updatedNAV,
		Success:            true,
		Error:              "",
	}, nil
}

// CalculateVaultNAV calculates the total NAV from all V2 vault positions
func (k *V2OracleKeeper) CalculateVaultNAV(ctx context.Context) (math.Int, error) {
	totalNAV := math.ZeroInt()

	// Iterate through all oracle positions
	iter, err := k.keeper.V2Collections.RemotePositionOracles.Iterate(ctx, nil)
	if err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to iterate positions: %w", err)
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		position, err := iter.Value()
		if err != nil {
			return math.ZeroInt(), fmt.Errorf("failed to get position value: %w", err)
		}

		// Calculate position value
		positionValue := position.SharePrice.MulInt(position.SharesHeld).TruncateInt()

		// Add to total NAV
		totalNAV = totalNAV.Add(positionValue)
	}

	// Add local assets (USDC balance)
	// This would include any USDC held directly by the vault
	// TODO: Add logic to include local USDC balance

	// Subtract pending liabilities
	// This would include any pending withdrawals or fees
	// TODO: Add logic to subtract liabilities

	return totalNAV, nil
}

// CheckDataFreshness checks the freshness of V2 vault oracle data
func (k *V2OracleKeeper) CheckDataFreshness(
	ctx context.Context,
	positionID string,
	timestamp int64,
) (string, error) {
	// Get staleness config
	config, err := k.GetStalenessConfig(ctx)
	if err != nil {
		// Use defaults if no config
		config = &vaultsv2.StalenessConfig{
			WarningThreshold:  DefaultWarningThreshold,
			CriticalThreshold: DefaultCriticalThreshold,
			MaxStaleness:      DefaultMaxStaleness,
		}
	}

	// Calculate age of data
	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	age := currentTime - timestamp

	// Determine staleness level
	switch {
	case age < config.WarningThreshold:
		return StalenessLevelFresh, nil
	case age < config.CriticalThreshold:
		return StalenessLevelWarning, nil
	case age < config.MaxStaleness:
		return StalenessLevelCritical, nil
	default:
		return StalenessLevelStale, nil
	}
}

// GetOracleForPosition retrieves the oracle registered for a V2 vault position
func (k *V2OracleKeeper) GetOracleForPosition(ctx context.Context, positionID string) (*vaultsv2.EnrolledOracle, error) {
	oracleID, err := k.keeper.V2Collections.OracleMappings.Get(ctx, positionID)
	if err != nil {
		return nil, fmt.Errorf("no oracle mapping for position %s: %w", positionID, err)
	}

	oracle, err := k.keeper.V2Collections.EnrolledOracles.Get(ctx, oracleID)
	if err != nil {
		return nil, fmt.Errorf("oracle %s not found: %w", oracleID, err)
	}

	return &oracle, nil
}

// GetRemotePositionOracle retrieves a V2 vault remote position tracked by oracle
func (k *V2OracleKeeper) GetRemotePositionOracle(ctx context.Context, positionID string) (*vaultsv2.RemotePositionOracle, error) {
	position, err := k.keeper.V2Collections.RemotePositionOracles.Get(ctx, positionID)
	if err != nil {
		return nil, err
	}
	return &position, nil
}

// UpdateOracleStatus updates the status of a V2 vault oracle
func (k *V2OracleKeeper) UpdateOracleStatus(ctx context.Context, positionID string, success bool, errorMsg string) error {
	status, err := k.keeper.V2Collections.OracleStatuses.Get(ctx, positionID)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("failed to get oracle status: %w", err)
		}
		// Create new status
		status = vaultsv2.OracleStatus{
			PositionId: positionID,
		}
	}

	if success {
		status.LastSuccess = sdk.UnwrapSDKContext(ctx).BlockTime()
		status.ConsecutiveFailures = 0
		status.IsHealthy = true
		status.LastError = ""
	} else {
		status.ConsecutiveFailures++
		status.IsHealthy = status.ConsecutiveFailures < 3 // Unhealthy after 3 failures
		status.LastError = errorMsg
	}

	// Update staleness level
	if success {
		status.StalenessLevel = StalenessLevelFresh
	} else {
		staleness, _ := k.CheckDataFreshness(ctx, positionID, status.LastSuccess.Unix())
		status.StalenessLevel = staleness
	}

	return k.keeper.V2Collections.OracleStatuses.Set(ctx, positionID, status)
}

// GetOracleParams retrieves V2 vault oracle governance parameters
func (k *V2OracleKeeper) GetOracleParams(ctx context.Context) (*vaultsv2.OracleGovernanceParams, error) {
	params, err := k.keeper.V2Collections.OracleParams.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Return default params
			return &vaultsv2.OracleGovernanceParams{
				MaxUpdateInterval:    86400, // 24 hours
				MinUpdateInterval:    60,    // 1 minute
				MaxPriceDeviationBps: 1000,  // 10%
				DefaultStaleness: &vaultsv2.StalenessConfig{
					WarningThreshold:  DefaultWarningThreshold,
					CriticalThreshold: DefaultCriticalThreshold,
					MaxStaleness:      DefaultMaxStaleness,
				},
				DefaultFallback: &vaultsv2.FallbackStrategy{
					UseLastKnownGood: true,
					MaxCacheAge:      172800, // 48 hours
					AlertThreshold:   86400,  // 24 hours
				},
			}, nil
		}
		return nil, err
	}
	return &params, nil
}

// GetStalenessConfig retrieves staleness configuration for V2 vault oracles
func (k *V2OracleKeeper) GetStalenessConfig(ctx context.Context) (*vaultsv2.StalenessConfig, error) {
	params, err := k.GetOracleParams(ctx)
	if err != nil {
		return nil, err
	}
	return params.DefaultStaleness, nil
}

// GetFallbackStrategy retrieves the fallback strategy for V2 vault oracles
func (k *V2OracleKeeper) GetFallbackStrategy(ctx context.Context) (*vaultsv2.FallbackStrategy, error) {
	params, err := k.GetOracleParams(ctx)
	if err != nil {
		return nil, err
	}
	return params.DefaultFallback, nil
}

// RemoveOracle removes a V2 vault oracle from the system
func (k *V2OracleKeeper) RemoveOracle(ctx context.Context, oracleID string) error {
	// Get oracle
	oracle, err := k.keeper.V2Collections.EnrolledOracles.Get(ctx, oracleID)
	if err != nil {
		return fmt.Errorf("oracle %s not found: %w", oracleID, err)
	}

	// Remove oracle
	if err := k.keeper.V2Collections.EnrolledOracles.Remove(ctx, oracleID); err != nil {
		return fmt.Errorf("failed to remove oracle: %w", err)
	}

	// Remove mapping
	if err := k.keeper.V2Collections.OracleMappings.Remove(ctx, oracle.PositionId); err != nil {
		// Log error but don't fail
		sdk.UnwrapSDKContext(ctx).Logger().Error("failed to remove oracle mapping", "error", err)
	}

	// Remove config
	if err := k.keeper.V2Collections.OracleConfigs.Remove(ctx, oracle.PositionId); err != nil {
		// Log error but don't fail
		sdk.UnwrapSDKContext(ctx).Logger().Error("failed to remove oracle config", "error", err)
	}

	return nil
}

// generateOracleID generates a unique ID for a V2 vault oracle
func (k *V2OracleKeeper) generateOracleID(positionID, oracleAddress string, chainID uint32) string {
	return fmt.Sprintf("%s-%s-%d", positionID, oracleAddress, chainID)
}
