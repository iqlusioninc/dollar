package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	vaultsv2 "dollar.noble.xyz/v2/types/vaults/v2"
)

// RegisterOracle implements vaultsv2.MsgServer for V2 vault oracle registration
func (k vaultV2MsgServer) RegisterOracle(ctx context.Context, msg *vaultsv2.MsgRegisterOracle) (*vaultsv2.MsgRegisterOracleResponse, error) {
	// Validate authority
	if msg.Authority != k.authority {
		return nil, fmt.Errorf("invalid authority: expected %s, got %s", k.authority, msg.Authority)
	}

	// Validate inputs
	if msg.PositionId == "" {
		return nil, fmt.Errorf("position ID cannot be empty")
	}
	if msg.OracleAddress == "" {
		return nil, fmt.Errorf("oracle address cannot be empty")
	}
	if msg.ChainId == 0 {
		return nil, fmt.Errorf("chain ID must be non-zero")
	}
	if msg.MaxStaleness <= 0 {
		return nil, fmt.Errorf("max staleness must be positive")
	}
	if msg.OriginDomain == 0 {
		return nil, fmt.Errorf("origin domain must be non-zero")
	}
	if msg.OriginMailbox == "" {
		return nil, fmt.Errorf("origin mailbox cannot be empty")
	}

	// Get V2 oracle keeper
	oracleKeeper := NewV2OracleKeeper(k.Keeper)

	// Register the oracle
	oracle, err := oracleKeeper.RegisterOracle(
		ctx,
		msg.PositionId,
		msg.OracleAddress,
		msg.ChainId,
		msg.MaxStaleness,
		msg.OriginDomain,
		msg.OriginMailbox,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register oracle: %w", err)
	}

	// Generate oracle ID
	oracleID := fmt.Sprintf("%s-%s-%d", msg.PositionId, msg.OracleAddress, msg.ChainId)

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"oracle_registered",
			sdk.NewAttribute("oracle_id", oracleID),
			sdk.NewAttribute("position_id", msg.PositionId),
			sdk.NewAttribute("oracle_address", msg.OracleAddress),
			sdk.NewAttribute("chain_id", fmt.Sprintf("%d", msg.ChainId)),
			sdk.NewAttribute("origin_domain", fmt.Sprintf("%d", msg.OriginDomain)),
		),
	)

	return &vaultsv2.MsgRegisterOracleResponse{
		OracleId:     oracleID,
		PositionId:   msg.PositionId,
		RegisteredAt: oracle.RegisteredAt,
	}, nil
}

// UpdateOracleConfig implements vaultsv2.MsgServer for V2 vault oracle configuration updates
func (k vaultV2MsgServer) UpdateOracleConfig(ctx context.Context, msg *vaultsv2.MsgUpdateOracleConfig) (*vaultsv2.MsgUpdateOracleConfigResponse, error) {
	// Validate authority
	if msg.Authority != k.authority {
		return nil, fmt.Errorf("invalid authority: expected %s, got %s", k.authority, msg.Authority)
	}

	// Validate oracle ID
	if msg.OracleId == "" {
		return nil, fmt.Errorf("oracle ID cannot be empty")
	}

	// Get existing oracle
	oracle, err := k.V2Collections.EnrolledOracles.Get(ctx, msg.OracleId)
	if err != nil {
		return nil, fmt.Errorf("oracle %s not found: %w", msg.OracleId, err)
	}

	// Update configuration
	if msg.MaxStaleness > 0 {
		oracle.MaxStaleness = msg.MaxStaleness

		// Also update the position config
		config, err := k.V2Collections.OracleConfigs.Get(ctx, oracle.PositionId)
		if err == nil {
			config.MaxStaleness = msg.MaxStaleness
			if err := k.V2Collections.OracleConfigs.Set(ctx, oracle.PositionId, config); err != nil {
				return nil, fmt.Errorf("failed to update position config: %w", err)
			}
		}
	}

	oracle.Active = msg.Active

	// Store updated oracle
	if err := k.V2Collections.EnrolledOracles.Set(ctx, msg.OracleId, oracle); err != nil {
		return nil, fmt.Errorf("failed to update oracle: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"oracle_config_updated",
			sdk.NewAttribute("oracle_id", msg.OracleId),
			sdk.NewAttribute("active", fmt.Sprintf("%t", msg.Active)),
			sdk.NewAttribute("max_staleness", fmt.Sprintf("%d", oracle.MaxStaleness)),
		),
	)

	return &vaultsv2.MsgUpdateOracleConfigResponse{
		OracleId:      msg.OracleId,
		UpdatedConfig: &oracle,
	}, nil
}

// RemoveOracle implements vaultsv2.MsgServer for V2 vault oracle removal
func (k vaultV2MsgServer) RemoveOracle(ctx context.Context, msg *vaultsv2.MsgRemoveOracle) (*vaultsv2.MsgRemoveOracleResponse, error) {
	// Validate authority
	if msg.Authority != k.authority {
		return nil, fmt.Errorf("invalid authority: expected %s, got %s", k.authority, msg.Authority)
	}

	// Validate oracle ID
	if msg.OracleId == "" {
		return nil, fmt.Errorf("oracle ID cannot be empty")
	}

	// Get oracle to get position ID
	oracle, err := k.V2Collections.EnrolledOracles.Get(ctx, msg.OracleId)
	if err != nil {
		return nil, fmt.Errorf("oracle %s not found: %w", msg.OracleId, err)
	}

	// Get V2 oracle keeper
	oracleKeeper := NewV2OracleKeeper(k.Keeper)

	// Remove the oracle
	if err := oracleKeeper.RemoveOracle(ctx, msg.OracleId); err != nil {
		return nil, fmt.Errorf("failed to remove oracle: %w", err)
	}

	removedAt := sdk.UnwrapSDKContext(ctx).BlockTime()

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"oracle_removed",
			sdk.NewAttribute("oracle_id", msg.OracleId),
			sdk.NewAttribute("position_id", oracle.PositionId),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("removed_at", removedAt.String()),
		),
	)

	return &vaultsv2.MsgRemoveOracleResponse{
		OracleId:   msg.OracleId,
		PositionId: oracle.PositionId,
		RemovedAt:  removedAt,
	}, nil
}

// UpdateOracleParams implements vaultsv2.MsgServer for V2 vault oracle parameter updates
func (k vaultV2MsgServer) UpdateOracleParams(ctx context.Context, msg *vaultsv2.MsgUpdateOracleParams) (*vaultsv2.MsgUpdateOracleParamsResponse, error) {
	// Validate authority
	if msg.Authority != k.authority {
		return nil, fmt.Errorf("invalid authority: expected %s, got %s", k.authority, msg.Authority)
	}

	// Validate parameters
	if msg.Params.MaxUpdateInterval <= 0 {
		return nil, fmt.Errorf("max update interval must be positive")
	}
	if msg.Params.MinUpdateInterval <= 0 {
		return nil, fmt.Errorf("min update interval must be positive")
	}
	if msg.Params.MinUpdateInterval >= msg.Params.MaxUpdateInterval {
		return nil, fmt.Errorf("min update interval must be less than max update interval")
	}
	if msg.Params.MaxPriceDeviationBps < 0 || msg.Params.MaxPriceDeviationBps > 10000 {
		return nil, fmt.Errorf("max price deviation must be between 0 and 10000 basis points")
	}

	// Validate staleness config
	if msg.Params.DefaultStaleness != nil {
		if msg.Params.DefaultStaleness.WarningThreshold <= 0 {
			return nil, fmt.Errorf("warning threshold must be positive")
		}
		if msg.Params.DefaultStaleness.CriticalThreshold <= msg.Params.DefaultStaleness.WarningThreshold {
			return nil, fmt.Errorf("critical threshold must be greater than warning threshold")
		}
		if msg.Params.DefaultStaleness.MaxStaleness <= msg.Params.DefaultStaleness.CriticalThreshold {
			return nil, fmt.Errorf("max staleness must be greater than critical threshold")
		}
	}

	// Validate fallback strategy
	if msg.Params.DefaultFallback != nil {
		if msg.Params.DefaultFallback.MaxCacheAge <= 0 {
			return nil, fmt.Errorf("max cache age must be positive")
		}
		if msg.Params.DefaultFallback.AlertThreshold <= 0 {
			return nil, fmt.Errorf("alert threshold must be positive")
		}
	}

	// Get previous parameters
	previousParams, _ := k.V2Collections.OracleParams.Get(ctx)

	// Store new parameters
	if err := k.V2Collections.OracleParams.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("failed to update oracle parameters: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"oracle_params_updated",
			sdk.NewAttribute("max_update_interval", fmt.Sprintf("%d", msg.Params.MaxUpdateInterval)),
			sdk.NewAttribute("min_update_interval", fmt.Sprintf("%d", msg.Params.MinUpdateInterval)),
			sdk.NewAttribute("max_price_deviation_bps", fmt.Sprintf("%d", msg.Params.MaxPriceDeviationBps)),
		),
	)

	return &vaultsv2.MsgUpdateOracleParamsResponse{
		PreviousParams: previousParams,
		NewParams:      msg.Params,
	}, nil
}
