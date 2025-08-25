package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	// Import Hyperlane types
	"github.com/bcp-innovations/hyperlane-cosmos/util"

	vaultsv2 "dollar.noble.xyz/v2/types/vaults/v2"
)

// V2OracleHandler handles oracle messages delivered by Hyperlane for V2 vaults
type V2OracleHandler struct {
	keeper       *Keeper
	oracleKeeper *V2OracleKeeper
}

// NewV2OracleHandler creates a new V2 vault oracle handler
func NewV2OracleHandler(k *Keeper) *V2OracleHandler {
	return &V2OracleHandler{
		keeper:       k,
		oracleKeeper: NewV2OracleKeeper(k),
	}
}

// HandleHyperlaneMessage processes incoming Hyperlane messages for V2 vault oracle updates
// This is called by the Hyperlane module when a message is delivered
func (h *V2OracleHandler) HandleHyperlaneMessage(
	ctx sdk.Context,
	originDomain uint32,
	sender []byte,
	recipient []byte,
	body []byte,
	metadata []byte,
) error {
	// Check if this is an oracle message by verifying the recipient
	// Oracle messages should be sent to a specific oracle handler address
	if !h.isOracleMessage(recipient) {
		// Not an oracle message, ignore
		return nil
	}

	// Validate message size
	if len(body) != NAVPayloadSize {
		return fmt.Errorf("invalid oracle message size: expected %d, got %d", NAVPayloadSize, len(body))
	}

	// Parse the NAV payload
	payload, err := h.oracleKeeper.ParseNAVPayload(body)
	if err != nil {
		return fmt.Errorf("failed to parse NAV payload: %w", err)
	}

	// Validate message type
	if payload.MessageType != uint32(NAVUpdateMessageType) {
		return fmt.Errorf("invalid message type: expected %d, got %d", NAVUpdateMessageType, payload.MessageType)
	}

	// Get oracle for position
	oracle, err := h.oracleKeeper.GetOracleForPosition(ctx, payload.PositionId)
	if err != nil {
		return fmt.Errorf("no oracle registered for position %s: %w", payload.PositionId, err)
	}

	// Validate the oracle matches the sender
	if !h.validateOracleSender(oracle, originDomain, sender) {
		return fmt.Errorf("oracle validation failed: sender mismatch for position %s", payload.PositionId)
	}

	// Check if oracle is active
	if !oracle.Active {
		return fmt.Errorf("oracle for position %s is not active", payload.PositionId)
	}

	// Check data freshness
	staleness, err := h.oracleKeeper.CheckDataFreshness(ctx, payload.PositionId, payload.Timestamp)
	if err != nil {
		return fmt.Errorf("failed to check data freshness: %w", err)
	}

	// Handle stale data according to fallback strategy
	if staleness == StalenessLevelStale {
		if err := h.handleStaleData(ctx, payload, oracle); err != nil {
			return fmt.Errorf("failed to handle stale data: %w", err)
		}
		return nil
	}

	// Apply the NAV update
	result, err := h.applyOracleUpdate(ctx, oracle, payload)
	if err != nil {
		// Update oracle status with failure
		h.oracleKeeper.UpdateOracleStatus(ctx, oracle.PositionId, false, err.Error())
		return fmt.Errorf("failed to apply oracle update: %w", err)
	}

	// Update oracle status with success
	if err := h.oracleKeeper.UpdateOracleStatus(ctx, oracle.PositionId, true, ""); err != nil {
		// Log error but don't fail the update
		ctx.Logger().Error("failed to update oracle status", "error", err)
	}

	// Emit event for oracle update
	h.emitOracleUpdateEvent(ctx, result)

	return nil
}

// Handle is the interface method for Hyperlane message handling
// This implements the Hyperlane IMessageHandler interface for V2 vaults
func (h *V2OracleHandler) Handle(
	ctx sdk.Context,
	origin uint32,
	sender util.HexAddress,
	message []byte,
) (err error) {
	// Convert HexAddress to bytes
	senderBytes, err := hex.DecodeString(sender.String())
	if err != nil {
		return fmt.Errorf("failed to decode sender address: %w", err)
	}

	// Extract recipient from message context (this would be set by Hyperlane)
	recipient := h.getOracleRecipientAddress()

	// Process the message
	return h.HandleHyperlaneMessage(ctx, origin, senderBytes, recipient, message, nil)
}

// ProcessOracleMessage is called internally when a V2 vault oracle message is received
func (h *V2OracleHandler) ProcessOracleMessage(
	ctx context.Context,
	message *vaultsv2.HyperlaneOracleMessage,
) (*vaultsv2.OracleUpdateResult, error) {
	// Parse NAV payload
	payload, err := h.oracleKeeper.ParseNAVPayload(message.Payload)
	if err != nil {
		return &vaultsv2.OracleUpdateResult{
			Success: false,
			Error:   fmt.Sprintf("failed to parse NAV payload: %v", err),
		}, nil
	}

	// Get oracle for position
	oracle, err := h.oracleKeeper.GetOracleForPosition(ctx, payload.PositionId)
	if err != nil {
		return &vaultsv2.OracleUpdateResult{
			PositionId: payload.PositionId,
			Success:    false,
			Error:      fmt.Sprintf("no oracle registered for position: %v", err),
		}, nil
	}

	// Validate origin domain matches
	if oracle.OriginDomain != message.OriginDomain {
		return &vaultsv2.OracleUpdateResult{
			PositionId: payload.PositionId,
			Success:    false,
			Error:      fmt.Sprintf("origin domain mismatch: expected %d, got %d", oracle.OriginDomain, message.OriginDomain),
		}, nil
	}

	// Apply the update
	response, err := h.oracleKeeper.ApplyNAVUpdate(ctx, oracle, payload)
	if err != nil {
		return &vaultsv2.OracleUpdateResult{
			PositionId: payload.PositionId,
			Success:    false,
			Error:      fmt.Sprintf("failed to apply NAV update: %v", err),
		}, nil
	}

	// Create successful result
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

// isOracleMessage checks if the message is intended for V2 vault oracle processing
func (h *V2OracleHandler) isOracleMessage(recipient []byte) bool {
	// Check if recipient matches the oracle handler address
	oracleHandlerAddr := h.getOracleRecipientAddress()
	return bytes.Equal(recipient, oracleHandlerAddr)
}

// getOracleRecipientAddress returns the address that V2 vault oracle messages should be sent to
func (h *V2OracleHandler) getOracleRecipientAddress() []byte {
	// This would be a specific module account or handler address for V2 vaults
	// For now, return a placeholder
	return []byte("v2_vault_oracle_handler")
}

// validateOracleSender validates that the message came from the registered V2 vault oracle
func (h *V2OracleHandler) validateOracleSender(
	oracle *vaultsv2.EnrolledOracle,
	originDomain uint32,
	sender []byte,
) bool {
	// Check domain matches
	if oracle.OriginDomain != originDomain {
		return false
	}

	// Check sender address matches oracle address
	oracleAddrBytes, err := hex.DecodeString(oracle.OracleAddress)
	if err != nil {
		return false
	}

	return bytes.Equal(sender, oracleAddrBytes)
}

// handleStaleData handles stale V2 vault oracle data according to fallback strategy
func (h *V2OracleHandler) handleStaleData(
	ctx context.Context,
	payload *vaultsv2.NAVPayload,
	oracle *vaultsv2.EnrolledOracle,
) error {
	fallback, err := h.oracleKeeper.GetFallbackStrategy(ctx)
	if err != nil {
		return fmt.Errorf("failed to get fallback strategy: %w", err)
	}

	if !fallback.UseLastKnownGood {
		return fmt.Errorf("data is stale and fallback not allowed")
	}

	// Get last known position
	position, err := h.oracleKeeper.GetRemotePositionOracle(ctx, payload.PositionId)
	if err != nil {
		return fmt.Errorf("no cached position data available: %w", err)
	}

	// Check if cached data is within acceptable age
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	age := sdkCtx.BlockTime().Unix() - position.LastUpdate.Unix()
	if age > fallback.MaxCacheAge {
		return fmt.Errorf("cached data too old: age %d exceeds max %d", age, fallback.MaxCacheAge)
	}

	// Log warning about using stale data
	sdkCtx.Logger().Warn("using stale oracle data with fallback strategy",
		"position_id", payload.PositionId,
		"data_age", age,
		"max_cache_age", fallback.MaxCacheAge,
	)

	// Alert if threshold exceeded
	if age > fallback.AlertThreshold {
		// Emit alert event
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"oracle_staleness_alert",
				sdk.NewAttribute("position_id", payload.PositionId),
				sdk.NewAttribute("data_age", fmt.Sprintf("%d", age)),
				sdk.NewAttribute("threshold", fmt.Sprintf("%d", fallback.AlertThreshold)),
			),
		)
	}

	return nil
}

// applyOracleUpdate applies the V2 vault oracle update to the state
func (h *V2OracleHandler) applyOracleUpdate(
	ctx context.Context,
	oracle *vaultsv2.EnrolledOracle,
	payload *vaultsv2.NAVPayload,
) (*vaultsv2.OracleUpdateResult, error) {
	// Apply the NAV update through the oracle keeper
	response, err := h.oracleKeeper.ApplyNAVUpdate(ctx, oracle, payload)
	if err != nil {
		return nil, err
	}

	// Convert response to result
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

// emitOracleUpdateEvent emits an event for the V2 vault oracle update
func (h *V2OracleHandler) emitOracleUpdateEvent(ctx sdk.Context, result *vaultsv2.OracleUpdateResult) {
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"v2_vault_oracle_nav_update",
			sdk.NewAttribute("position_id", result.PositionId),
			sdk.NewAttribute("new_share_price", result.NewSharePrice.String()),
			sdk.NewAttribute("new_shares", result.NewShares.String()),
			sdk.NewAttribute("position_value", result.PositionValue.String()),
			sdk.NewAttribute("updated_nav", result.UpdatedNav.String()),
			sdk.NewAttribute("success", fmt.Sprintf("%t", result.Success)),
		),
	)
}

// RegisterWithHyperlane registers this V2 vault handler with the Hyperlane module
func (h *V2OracleHandler) RegisterWithHyperlane(hyperlaneKeeper interface{}) error {
	// This would register the handler with the Hyperlane module
	// The exact implementation depends on the Hyperlane module's interface

	// Example registration (actual implementation would depend on Hyperlane module):
	// if warpKeeper, ok := hyperlaneKeeper.(*warpkeeper.Keeper); ok {
	//     return warpKeeper.RegisterHandler(h.getOracleRecipientAddress(), h)
	// }

	return nil
}

// GetOracleMetrics returns metrics about V2 vault oracle operations
func (h *V2OracleHandler) GetOracleMetrics(ctx context.Context) (*OracleMetrics, error) {
	metrics := &OracleMetrics{
		TotalOracles:      0,
		ActiveOracles:     0,
		StalePositions:    0,
		RecentUpdates:     0,
		FailedUpdates:     0,
		AverageUpdateTime: 0,
	}

	// Count total and active oracles
	iter, err := h.keeper.V2Collections.EnrolledOracles.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		oracle, err := iter.Value()
		if err != nil {
			continue
		}
		metrics.TotalOracles++
		if oracle.Active {
			metrics.ActiveOracles++
		}
	}

	// Count stale positions
	statusIter, err := h.keeper.V2Collections.OracleStatuses.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer statusIter.Close()

	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	for ; statusIter.Valid(); statusIter.Next() {
		status, err := statusIter.Value()
		if err != nil {
			continue
		}

		// Check if position is stale
		if status.StalenessLevel == StalenessLevelStale {
			metrics.StalePositions++
		}

		// Count recent updates (within last hour)
		if currentTime.Sub(status.LastSuccess).Hours() < 1 {
			metrics.RecentUpdates++
		}

		// Count failed updates
		if status.ConsecutiveFailures > 0 {
			metrics.FailedUpdates++
		}
	}

	return metrics, nil
}

// OracleMetrics contains metrics about oracle operations
type OracleMetrics struct {
	TotalOracles      int
	ActiveOracles     int
	StalePositions    int
	RecentUpdates     int
	FailedUpdates     int
	AverageUpdateTime int64
}
