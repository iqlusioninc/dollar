# Required Changes: CreateRemotePosition and Rebalance

**Issue:** Current implementation has incorrect separation of concerns between registering remote positions and allocating funds to them.

**Date:** 2025-10-25

---

## Problem Summary

### Current Behavior (INCORRECT):

1. **CreateRemotePosition** (keeper/msg_server_vaults_v2.go:956-1032):
   - ❌ Initializes position with `SharesHeld = msg.Amount`
   - ❌ Initializes position with `Principal = msg.Amount`
   - ❌ Initializes position with `TotalValue = msg.Amount`
   - ❌ Subtracts `msg.Amount` from PendingDeploymentFunds
   - ❌ Does NOT create InflightFund entry
   - ❌ Does NOT bridge funds via Hyperlane warp

2. **Rebalance** (keeper/msg_server_vaults_v2.go:1121-1370):
   - ✅ Creates InflightFund entries for fund movements
   - ❌ Does NOT actually bridge funds via Hyperlane warp
   - ❌ Just updates state, relies on external keeper

### Intended Behavior (CORRECT):

1. **CreateRemotePosition** should:
   - ✅ Register a remote vault as a deployment target
   - ✅ Initialize with **ZERO** funds (SharesHeld=0, Principal=0, TotalValue=0)
   - ✅ Store vault address and chain ID mapping
   - ✅ Do NOT touch PendingDeploymentFunds
   - ✅ Do NOT create InflightFund entry (no funds moving yet)

2. **Rebalance** should:
   - ✅ Create InflightFund entries (already does this)
   - ✅ **Actually call Hyperlane warp to bridge funds**
   - ✅ Update remote position values when funds are in-flight
   - ✅ Subtract from PendingDeploymentFunds when bridging

---

## Required Code Changes

### Change 1: Fix CreateRemotePosition (Lines 996-1016)

**Current Code:**
```go
position := vaultsv2.RemotePosition{
    VaultAddress: vaultAddress,
    SharesHeld:   msg.Amount,      // ❌ WRONG
    Principal:    msg.Amount,      // ❌ WRONG
    SharePrice:   sdkmath.LegacyOneDec(),
    TotalValue:   msg.Amount,      // ❌ WRONG
    LastUpdate:   headerInfo.Time,
    Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
}

if err := m.SetVaultsV2RemotePosition(ctx, positionID, position); err != nil {
    return nil, sdkerrors.Wrap(err, "unable to store remote position")
}

if err := m.SetVaultsV2RemotePositionChainID(ctx, positionID, msg.ChainId); err != nil {
    return nil, sdkerrors.Wrap(err, "unable to store remote position chain id")
}

if err := m.SubtractVaultsV2PendingDeploymentFunds(ctx, msg.Amount); err != nil {  // ❌ WRONG
    return nil, sdkerrors.Wrap(err, "unable to update pending deployment funds")
}
```

**Corrected Code:**
```go
// CreateRemotePosition just registers the vault - no funds allocated yet
position := vaultsv2.RemotePosition{
    VaultAddress: vaultAddress,
    SharesHeld:   sdkmath.ZeroInt(),      // ✅ Start with zero
    Principal:    sdkmath.ZeroInt(),      // ✅ Start with zero
    SharePrice:   sdkmath.LegacyOneDec(),
    TotalValue:   sdkmath.ZeroInt(),      // ✅ Start with zero
    LastUpdate:   headerInfo.Time,
    Status:       vaultsv2.REMOTE_POSITION_ACTIVE,
}

if err := m.SetVaultsV2RemotePosition(ctx, positionID, position); err != nil {
    return nil, sdkerrors.Wrap(err, "unable to store remote position")
}

if err := m.SetVaultsV2RemotePositionChainID(ctx, positionID, msg.ChainId); err != nil {
    return nil, sdkerrors.Wrap(err, "unable to store remote position chain id")
}

// ✅ Do NOT subtract from PendingDeploymentFunds
// Funds will be allocated via Rebalance message
```

**Also update MsgCreateRemotePosition proto** (proto/noble/dollar/vaults/v2/tx.proto:446):
```protobuf
// MsgCreateRemotePosition registers a new remote vault as a deployment target.
// This does NOT allocate funds - use MsgRebalance to allocate capital to positions.
message MsgCreateRemotePosition {
  option (cosmos.msg.v1.signer) = "manager";

  string manager = 1;
  string vault_address = 2;  // ERC-4626 vault address (hex encoded)
  uint32 chain_id = 3;       // Hyperlane domain ID

  // Remove 'amount' field - no longer needed
  // Remove 'min_shares_out' field - no longer needed
}
```

---

### Change 2: Add Hyperlane Warp Bridging to Rebalance (After Line 1334)

**Current Code (Lines 1313-1334):**
```go
fund := vaultsv2.InflightFund{
    Id:                strconv.FormatUint(inflightID, 10),
    TransactionId:     fmt.Sprintf("rebalance:%d", entry.ID),
    Amount:            adj.amount,
    ValueAtInitiation: adj.amount,
    InitiatedAt:       headerInfo.Time,
    ExpectedAt:        headerInfo.Time,
    Status:            vaultsv2.INFLIGHT_PENDING,
    Origin:            &vaultsv2.InflightFund_NobleOrigin{NobleOrigin: &vaultsv2.NobleEndpoint{OperationType: vaultsv2.OPERATION_TYPE_REBALANCE}},
    Destination:       &vaultsv2.InflightFund_RemoteDestination{RemoteDestination: destination},
    ProviderTracking: &vaultsv2.ProviderTrackingInfo{
        TrackingInfo: &vaultsv2.ProviderTrackingInfo_HyperlaneTracking{
            HyperlaneTracking: &vaultsv2.HyperlaneTrackingInfo{
                DestinationDomain: entry.ChainID,
            },
        },
    },
}

if err := m.SetVaultsV2InflightFund(ctx, fund); err != nil {
    return nil, sdkerrors.Wrap(err, "unable to persist inflight fund")
}

// ❌ MISSING: Actually bridge the funds via Hyperlane warp
```

**Corrected Code:**
```go
fund := vaultsv2.InflightFund{
    Id:                strconv.FormatUint(inflightID, 10),
    TransactionId:     fmt.Sprintf("rebalance:%d", entry.ID),
    Amount:            adj.amount,
    ValueAtInitiation: adj.amount,
    InitiatedAt:       headerInfo.Time,
    ExpectedAt:        headerInfo.Time,
    Status:            vaultsv2.INFLIGHT_PENDING,
    Origin:            &vaultsv2.InflightFund_NobleOrigin{NobleOrigin: &vaultsv2.NobleEndpoint{OperationType: vaultsv2.OPERATION_TYPE_REBALANCE}},
    Destination:       &vaultsv2.InflightFund_RemoteDestination{RemoteDestination: destination},
    ProviderTracking: &vaultsv2.ProviderTrackingInfo{
        TrackingInfo: &vaultsv2.ProviderTrackingInfo_HyperlaneTracking{
            HyperlaneTracking: &vaultsv2.HyperlaneTrackingInfo{
                DestinationDomain: entry.ChainID,
            },
        },
    },
}

if err := m.SetVaultsV2InflightFund(ctx, fund); err != nil {
    return nil, sdkerrors.Wrap(err, "unable to persist inflight fund")
}

// ✅ ADD: Get cross-chain route for this position
route, routeFound, err := m.GetVaultsV2CrossChainRoute(ctx, entry.ChainID)
if err != nil {
    return nil, sdkerrors.Wrap(err, "unable to fetch cross-chain route")
}
if !routeFound {
    return nil, sdkerrors.Wrapf(vaultsv2.ErrRouteNotFound, "route for chain %d not found", entry.ChainID)
}

// ✅ ADD: Enforce route capacity before bridging
if err := m.EnforceRouteCapacity(ctx, entry.ChainID, adj.amount); err != nil {
    return nil, err
}

// ✅ ADD: Track inflight value for this route
if err := m.AddVaultsV2InflightValueByRoute(ctx, entry.ChainID, adj.amount); err != nil {
    return nil, sdkerrors.Wrap(err, "unable to track inflight value")
}

// ✅ ADD: Actually bridge funds via Hyperlane warp
sdkCtx := sdk.UnwrapSDKContext(ctx)
messageID, transferErr := m.warp.RemoteTransferCollateral(
    sdkCtx,
    route.HyptokenId,                    // Token ID for USDN on Hyperlane
    types.ModuleAddress.String(),        // From: dollar module account
    entry.ChainID,                       // Destination chain (Hyperlane domain)
    entry.Position.VaultAddress,         // To: Remote ERC-4626 vault address
    adj.amount,                          // Amount to bridge
    nil,                                 // Metadata (optional)
    math.ZeroInt(),                      // Hook gas limit (use default)
    sdk.NewCoin(m.denom, math.ZeroInt()), // Max Hyperlane fee (0 = use default)
    nil,                                 // Hook metadata (optional)
)

if transferErr != nil {
    // Revert state changes if bridge fails
    _ = m.SubtractVaultsV2InflightValueByRoute(ctx, entry.ChainID, adj.amount)
    _ = m.DeleteVaultsV2InflightFund(ctx, fund.Id)
    return nil, sdkerrors.Wrapf(transferErr, "unable to bridge funds to chain %d", entry.ChainID)
}

// ✅ ADD: Update inflight fund with Hyperlane message ID
if hyperlaneTracking := fund.ProviderTracking.GetHyperlaneTracking(); hyperlaneTracking != nil {
    hyperlaneTracking.MessageId = messageID
    if err := m.SetVaultsV2InflightFund(ctx, fund); err != nil {
        return nil, sdkerrors.Wrap(err, "unable to update inflight fund with message ID")
    }
}

// ✅ ADD: Emit inflight fund created event
if err := m.EmitInflightCreatedEvent(
    ctx,
    fund.Id,
    entry.ChainID,
    vaultsv2.OPERATION_TYPE_REBALANCE,
    adj.amount,
    msg.Manager,
    "noble",
    fmt.Sprintf("chain-%d", entry.ChainID),
    fund.ExpectedAt,
); err != nil {
    // Log but don't fail
    m.logger.Error("unable to emit inflight created event", "error", err)
}
```

---

## NAV Impact Analysis

### Before Changes:
```
CreateRemotePosition:
NAV = (PendingDeployment - amount) + (RemotePositions + amount) + Inflight - Withdrawals
    = unchanged (but incorrectly tracked - no inflight entry)
```

### After Changes:
```
CreateRemotePosition:
NAV = PendingDeployment + RemotePositions + Inflight - Withdrawals
    = unchanged (position registered with zero funds)

Rebalance (when bridging):
NAV = (PendingDeployment - amount) + RemotePositions + (Inflight + amount) - Withdrawals
    = unchanged (funds moved from pending to inflight, correctly tracked)
```

**NAV invariant maintained correctly ✅**

---

## Message Flow Example

### Correct Flow:

```
1. User deposits 1,000 USDN
   → PendingDeploymentFunds = 1,000
   → NAV = 1,000

2. Authority calls CreateRemotePosition(vault_address="0xABC...", chain_id=8453)
   → Creates RemotePosition #1 with TotalValue=0, SharesHeld=0
   → PendingDeploymentFunds = 1,000 (unchanged)
   → NAV = 1,000

3. Authority calls CreateRemotePosition(vault_address="0xDEF...", chain_id=998)
   → Creates RemotePosition #2 with TotalValue=0, SharesHeld=0
   → PendingDeploymentFunds = 1,000 (unchanged)
   → NAV = 1,000

4. Authority calls Rebalance([{position_id: 1, target: 60%}, {position_id: 2, target: 40%}])
   → Allocates 600 to Position #1:
     - Creates InflightFund with amount=600, status=PENDING
     - Bridges 600 USDN via Hyperlane warp to chain 8453
     - PendingDeploymentFunds = 400
     - Inflight[route=8453] = 600
   → Allocates 400 to Position #2:
     - Creates InflightFund with amount=400, status=PENDING
     - Bridges 400 USDN via Hyperlane warp to chain 998
     - PendingDeploymentFunds = 0
     - Inflight[route=998] = 400
   → NAV = 0 (pending) + 0 (remote) + 1,000 (inflight) = 1,000 ✅

5. Hyperlane relay completes, funds arrive at Position #1
   → Authority calls ProcessInFlightPosition(inflight_id, status=COMPLETED)
     - RemotePosition #1: TotalValue = 600, SharesHeld = 600
     - Inflight[route=8453] = 0
     - NAV = 0 (pending) + 600 (remote) + 400 (inflight) = 1,000 ✅

6. Hyperlane relay completes, funds arrive at Position #2
   → Authority calls ProcessInFlightPosition(inflight_id, status=COMPLETED)
     - RemotePosition #2: TotalValue = 400, SharesHeld = 400
     - Inflight[route=998] = 0
     - NAV = 0 (pending) + 1,000 (remote) + 0 (inflight) = 1,000 ✅
```

---

## Testing Requirements

After implementing these changes, add tests for:

1. **CreateRemotePosition with zero initialization**
   - Verify position created with all zero values
   - Verify PendingDeploymentFunds not touched
   - Verify no InflightFund created

2. **Rebalance with actual Hyperlane bridging**
   - Mock warp.RemoteTransferCollateral
   - Verify InflightFund created with PENDING status
   - Verify route capacity enforced
   - Verify inflight value tracking
   - Verify PendingDeploymentFunds decreased
   - Verify Hyperlane warp called with correct parameters
   - Test bridge failure handling (state rollback)

3. **Full lifecycle integration test**
   - CreateRemotePosition → Rebalance → ProcessInFlightPosition
   - Verify NAV maintained throughout
   - Verify position values updated correctly

---

## Summary

**Two-line fix summary:**
1. `CreateRemotePosition`: Initialize position with **zero funds**, don't touch PendingDeploymentFunds
2. `Rebalance`: Actually **call Hyperlane warp** to bridge funds to remote vaults

This creates proper separation: CreateRemotePosition = "register target", Rebalance = "allocate funds".
