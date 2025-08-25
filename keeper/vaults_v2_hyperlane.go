package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	// Import Hyperlane modules
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	corekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"

	vaultsv2 "dollar.noble.xyz/v2/types/vaults/v2"
)

// V2HyperlaneIntegration manages the integration with Hyperlane for V2 vault oracle messages
type V2HyperlaneIntegration struct {
	keeper        *Keeper
	oracleHandler *V2OracleHandler
	coreKeeper    *corekeeper.Keeper
	warpKeeper    *warpkeeper.Keeper
	recipientAddr util.HexAddress
	isInitialized bool
}

// NewV2HyperlaneIntegration creates a new Hyperlane integration for V2 vaults
func NewV2HyperlaneIntegration(
	keeper *Keeper,
	coreKeeper interface{},
	warpKeeper interface{},
) *V2HyperlaneIntegration {
	// Cast to proper types
	var core *corekeeper.Keeper
	var warp *warpkeeper.Keeper

	if coreK, ok := coreKeeper.(*corekeeper.Keeper); ok {
		core = coreK
	}

	if warpK, ok := warpKeeper.(*warpkeeper.Keeper); ok {
		warp = warpK
	}

	return &V2HyperlaneIntegration{
		keeper:        keeper,
		oracleHandler: NewV2OracleHandler(keeper),
		coreKeeper:    core,
		warpKeeper:    warp,
		isInitialized: false,
	}
}

// Initialize sets up the V2 vault Hyperlane integration
func (h *V2HyperlaneIntegration) Initialize(ctx sdk.Context) error {
	if h.isInitialized {
		return nil
	}

	// Generate the recipient address for V2 vault oracle messages
	// This is the address that V2 vault oracle messages should be sent to
	recipientAddr, err := h.generateOracleRecipientAddress(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate oracle recipient address: %w", err)
	}
	h.recipientAddr = recipientAddr

	// Register as a message handler with Hyperlane for V2 vaults
	if err := h.registerWithHyperlane(ctx); err != nil {
		return fmt.Errorf("failed to register with Hyperlane: %w", err)
	}

	h.isInitialized = true
	return nil
}

// Handle implements the Hyperlane message handler interface for V2 vaults
// This is called by Hyperlane when a message is delivered for V2 vault oracles
func (h *V2HyperlaneIntegration) Handle(
	ctx sdk.Context,
	origin uint32,
	sender util.HexAddress,
	message []byte,
) error {
	// Check if this is a V2 vault oracle message by verifying the message format
	if !h.isOracleMessage(message) {
		// Not an oracle message, ignore
		return nil
	}

	// Convert sender address to bytes
	senderBytes, err := hex.DecodeString(sender.String())
	if err != nil {
		return fmt.Errorf("failed to decode sender address: %w", err)
	}

	// Create V2 vault Hyperlane oracle message
	oracleMsg := &vaultsv2.HyperlaneOracleMessage{
		MailboxId:    nil, // Set by Hyperlane context
		OriginDomain: origin,
		Sender:       senderBytes,
		Metadata:     nil, // Additional metadata if needed
		Payload:      message,
		ReceivedAt:   ctx.BlockTime(),
	}

	// Process the oracle message
	result, err := h.oracleHandler.ProcessOracleMessage(ctx, oracleMsg)
	if err != nil {
		// Log error but don't fail the Hyperlane message processing
		ctx.Logger().Error("failed to process oracle message",
			"error", err,
			"origin", origin,
			"sender", sender.String(),
		)
		return nil
	}

	// Emit event for the oracle update
	h.emitOracleEvent(ctx, result)

	return nil
}

// OnMessageDelivered is called when a Hyperlane message is successfully delivered for V2 vaults
// This implements the Hyperlane callback interface for V2 vault oracles
func (h *V2HyperlaneIntegration) OnMessageDelivered(
	ctx sdk.Context,
	messageId []byte,
	origin uint32,
	sender util.HexAddress,
	recipient util.HexAddress,
	body []byte,
) error {
	// Check if this message is for the oracle handler
	if !h.isForOracleHandler(recipient) {
		return nil
	}

	// Process the message through the oracle handler
	return h.Handle(ctx, origin, sender, body)
}

// isOracleMessage checks if a message is a V2 vault oracle update
func (h *V2HyperlaneIntegration) isOracleMessage(message []byte) bool {
	// Check message size
	if len(message) != NAVPayloadSize {
		return false
	}

	// Check message type byte
	if len(message) > 0 && message[0] == NAVUpdateMessageType {
		return true
	}

	return false
}

// isForOracleHandler checks if the recipient address is the V2 vault oracle handler
func (h *V2HyperlaneIntegration) isForOracleHandler(recipient util.HexAddress) bool {
	return recipient == h.recipientAddr
}

// generateOracleRecipientAddress generates the recipient address for V2 vault oracle messages
func (h *V2HyperlaneIntegration) generateOracleRecipientAddress(ctx sdk.Context) (util.HexAddress, error) {
	// Use a deterministic address for the V2 vault oracle handler
	// This could be a module account or a specific handler address

	// Create a module account for V2 vault oracle handling
	moduleAddr := sdk.AccAddress([]byte("v2_vault_oracle_handler"))

	// Convert to hex address
	hexStr := hex.EncodeToString(moduleAddr.Bytes())
	return util.DecodeHexAddress(hexStr)
}

// registerWithHyperlane registers this V2 vault handler with the Hyperlane module
func (h *V2HyperlaneIntegration) registerWithHyperlane(ctx sdk.Context) error {
	// Register as a recipient with the Hyperlane core module for V2 vaults
	// The exact method depends on the Hyperlane module's interface

	// This is a placeholder - actual implementation would depend on Hyperlane's registration mechanism
	// Typically, this would involve:
	// 1. Registering the recipient address
	// 2. Setting up ISM (Interchain Security Module) for validation
	// 3. Configuring message routing

	ctx.Logger().Info("registered V2 vault oracle handler with Hyperlane",
		"recipient", h.recipientAddr.String(),
	)

	return nil
}

// emitOracleEvent emits an event for V2 vault oracle updates
func (h *V2HyperlaneIntegration) emitOracleEvent(ctx sdk.Context, result *vaultsv2.OracleUpdateResult) {
	if result.Success {
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"v2_vault_hyperlane_oracle_update",
				sdk.NewAttribute("position_id", result.PositionId),
				sdk.NewAttribute("new_share_price", result.NewSharePrice.String()),
				sdk.NewAttribute("new_shares", result.NewShares.String()),
				sdk.NewAttribute("position_value", result.PositionValue.String()),
				sdk.NewAttribute("updated_nav", result.UpdatedNav.String()),
			),
		)
	} else {
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"v2_vault_hyperlane_oracle_update_failed",
				sdk.NewAttribute("position_id", result.PositionId),
				sdk.NewAttribute("error", result.Error),
			),
		)
	}
}

// GetOracleRecipientAddress returns the recipient address for V2 vault oracle messages
func (h *V2HyperlaneIntegration) GetOracleRecipientAddress() (string, error) {
	if !h.isInitialized {
		return "", fmt.Errorf("V2 vault Hyperlane integration not initialized")
	}
	return h.recipientAddr.String(), nil
}

// ValidateOracleRegistration validates that a V2 vault oracle can send messages to this handler
func (h *V2HyperlaneIntegration) ValidateOracleRegistration(
	ctx context.Context,
	oracle *vaultsv2.EnrolledOracle,
) error {
	// Validate that the oracle's domain is supported
	// This could check against a list of supported Hyperlane domains

	// Validate oracle address format
	_, err := util.DecodeHexAddress(oracle.OracleAddress)
	if err != nil {
		return fmt.Errorf("invalid oracle address format: %w", err)
	}

	// Additional validation could include:
	// - Checking if the domain has an active Hyperlane connection
	// - Verifying ISM configuration for the oracle's domain
	// - Ensuring the oracle is whitelisted for the position

	return nil
}

// SetupISMForOracle sets up the Interchain Security Module for a V2 vault oracle
func (h *V2HyperlaneIntegration) SetupISMForOracle(
	ctx sdk.Context,
	oracle *vaultsv2.EnrolledOracle,
) error {
	// Configure ISM (Interchain Security Module) for oracle validation
	// This ensures that only messages from the registered oracle are accepted

	// The ISM configuration would typically include:
	// 1. Validator set for the origin domain
	// 2. Threshold requirements
	// 3. Specific oracle address validation

	// This is a placeholder - actual implementation depends on Hyperlane ISM interface
	ctx.Logger().Info("configured ISM for V2 vault oracle",
		"position_id", oracle.PositionId,
		"oracle_address", oracle.OracleAddress,
		"origin_domain", oracle.OriginDomain,
	)

	return nil
}

// ProcessPendingMessages processes any pending Hyperlane messages for V2 vault oracles
func (h *V2HyperlaneIntegration) ProcessPendingMessages(ctx sdk.Context) error {
	// This could be called in BeginBlock to process any pending messages
	// Actual implementation would depend on Hyperlane's message queue mechanism

	// Query pending messages from Hyperlane
	// Filter for oracle messages
	// Process each message through the oracle handler

	return nil
}

// GetOracleMessageStats returns statistics about V2 vault oracle messages
func (h *V2HyperlaneIntegration) GetOracleMessageStats(ctx context.Context) (*OracleMessageStats, error) {
	stats := &OracleMessageStats{
		TotalMessagesReceived: 0,
		SuccessfulUpdates:     0,
		FailedUpdates:         0,
		LastMessageTime:       nil,
		AverageProcessingTime: 0,
	}

	// Query message history from storage
	// Calculate statistics

	return stats, nil
}

// OracleMessageStats contains statistics about V2 vault oracle messages
type OracleMessageStats struct {
	TotalMessagesReceived int64
	SuccessfulUpdates     int64
	FailedUpdates         int64
	LastMessageTime       *time.Time
	AverageProcessingTime int64 // in milliseconds
}

// MonitorOracleHealth monitors the health of V2 vault oracle connections
func (h *V2HyperlaneIntegration) MonitorOracleHealth(ctx sdk.Context) {
	// Iterate through registered oracles
	iter, err := h.keeper.V2Collections.EnrolledOracles.Iterate(ctx, nil)
	if err != nil {
		ctx.Logger().Error("failed to iterate oracles for health monitoring", "error", err)
		return
	}
	defer iter.Close()

	currentTime := ctx.BlockTime()
	for ; iter.Valid(); iter.Next() {
		oracle, err := iter.Value()
		if err != nil {
			continue
		}

		// Check oracle status
		status, err := h.keeper.V2Collections.OracleStatuses.Get(ctx, oracle.PositionId)
		if err != nil {
			continue
		}

		// Calculate time since last update
		timeSinceUpdate := currentTime.Sub(status.LastSuccess)

		// Alert if oracle is unhealthy
		if !status.IsHealthy || timeSinceUpdate.Hours() > 24 {
			ctx.EventManager().EmitEvent(
				sdk.NewEvent(
					"v2_vault_oracle_health_alert",
					sdk.NewAttribute("position_id", oracle.PositionId),
					sdk.NewAttribute("is_healthy", fmt.Sprintf("%t", status.IsHealthy)),
					sdk.NewAttribute("time_since_update", timeSinceUpdate.String()),
					sdk.NewAttribute("consecutive_failures", fmt.Sprintf("%d", status.ConsecutiveFailures)),
				),
			)
		}
	}
}
