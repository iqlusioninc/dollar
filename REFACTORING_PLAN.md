# Vaults V2 Refactoring Plan: Multi-Position Direct Yield Tracking

## Overview

This document outlines the plan to refactor the Vaults V2 system to use **direct yield tracking with multiple positions per user** instead of the current hybrid shares + positions model.

## Current State Problems

1. **Dual Tracking System**: Code maintains both shares-based tracking AND position-based tracking
   - `VaultsV2UserShares` - ERC-4626 style shares
   - `VaultsV2UserPositions` - Direct deposit_amount + accrued_yield
   
2. **Single Position Per User**: Users can only have one position per address

3. **Inconsistency**: Spec documents describe shares-based model, proto defines position-based model, code implements both

## Target State

### Design Principles

1. **Multiple Independent Positions**: Each user can have multiple positions, each with independent:
   - `deposit_amount` (principal)
   - `accrued_yield` (accumulated yield)
   - `first_deposit_time` (when position was created)
   - `receive_yield` (yield preference for this position)

2. **No Shares**: Remove all shares-based tracking entirely
   - No `VaultsV2UserShares`
   - No `VaultsV2TotalShares`
   - No share price calculations
   - No TWAP NAV for share pricing

3. **One Deposit = One Position**: Each deposit creates a new position (simplifies yield tracking)
   - No deposits into existing positions
   - Withdrawals can be full or partial from any position

4. **Composite Key**: Positions keyed by `(address, position_id)`
   - Per-user position sequence for auto-incrementing IDs
   - Position IDs are unique within a user, not globally

### Data Model Changes

#### New Collections

```go
// Per-user position ID sequence
VaultsV2UserPositionSequence  collections.Map[[]byte, uint64]

// Positions with composite key (address + position_id)
VaultsV2UserPositions         collections.Map[collections.Pair[[]byte, uint64], vaultsv2.UserPosition]

// Accounting snapshots with composite key
VaultsV2AccountingSnapshots   collections.Map[collections.Pair[[]byte, uint64], vaultsv2.AccountingSnapshot]
```

#### Removed Collections

```go
// REMOVE these:
VaultsV2UserShares            collections.Map[[]byte, math.Int]
VaultsV2TotalShares           collections.Item[math.Int]
```

#### Updated VaultState

```go
type VaultState {
  total_deposits         math.Int  // Sum of all position deposit_amounts
  total_accrued_yield    math.Int  // Sum of all position accrued_yields
  total_nav              math.Int  // total_deposits + total_accrued_yield
  total_users            uint64    // Number of unique users with positions
  total_positions        uint64    // Total number of positions across all users
  // ... rest stays the same
}
```

## Implementation Plan

### Phase 1: Proto & Code Generation ✅ COMPLETED

- [x] Update `UserPosition` proto to include `position_id` field
- [x] Update `AccountingSnapshot` proto to include `position_id` field
- [x] Update `MsgDeposit` to remove `position_id` and `receive_yield_override` (always creates new position)
- [x] Update `MsgDepositResponse` to return created `position_id`
- [x] Update `MsgRequestWithdrawal` to include `position_id` field
- [x] Update `MsgSetYieldPreference` to include `position_id` field
- [x] Regenerate protobuf code

### Phase 2: Keeper Structure Updates ✅ COMPLETED

#### 2.1 Update Keys (types/vaults/v2/keys.go) ✅
- [x] Added `UserPositionSequencePrefix = []byte("vaults/v2/user_position_seq/")` at line 28
- [x] Updated `UserPositionPrefix = []byte("vaults/v2/user_position/")` at line 29 for composite keys
- [x] `AccountingSnapshotPrefix` already supports composite keys at line 34

#### 2.2 Update Keeper (keeper/keeper.go) ✅
- [x] Added `VaultsV2UserPositionSequence collections.Map[[]byte, uint64]` at line 83
- [x] Updated `VaultsV2UserPositions collections.Map[collections.Pair[[]byte, uint64], vaultsv2.UserPosition]` at line 84
- [x] Updated `VaultsV2AccountingSnapshots collections.Map[collections.Pair[[]byte, uint64], vaultsv2.AccountingSnapshot]` at line 96
- [x] Share-based collections already removed from keeper struct

#### 2.3 Update Keeper Constructor ✅
- [x] `VaultsV2UserPositionSequence` properly initialized at line 191
- [x] `VaultsV2UserPositions` uses `PairKeyCodec(BytesKey, Uint64Key)` at line 192  
- [x] `VaultsV2AccountingSnapshots` uses composite key codec at line 204

### Phase 3: State Accessor Functions ✅ COMPLETED

#### 3.1 Position Sequence Functions ✅
- [x] `GetNextUserPositionID(ctx, address)` implemented in keeper/state_vaults_v2.go
- [x] Function handles auto-increment for per-user position sequences

#### 3.2 Position Accessors ✅  
- [x] `GetVaultsV2UserPosition(ctx, address, positionID)` updated to use composite key
- [x] `SetVaultsV2UserPosition(ctx, address, position)` updated for composite key storage
- [x] Functions properly handle `(address, positionID)` composite key pattern

#### 3.3 Position Iteration Functions ✅
- [x] Core iteration functionality implemented for multi-position support
- [x] Position accessors support composite key iteration

#### 3.4 Share Functions ✅
- [x] Share-based functions already removed from codebase
- [x] No share-related logic remaining in state accessors

#### 3.5 Accounting Snapshot Functions ✅
- [x] `GetVaultsV2AccountingSnapshot` and `SetVaultsV2AccountingSnapshot` use composite keys
- [x] Snapshot functions support `(address, positionID)` pattern

### Phase 4: Message Handlers ✅ COMPLETED

#### 4.1 Update Deposit Handler ✅
- [x] **keeper/msg_server_vaults_v2.go:296-425** - Deposit handler fully implements new multi-position model:
  - ✅ Uses `GetNextUserPositionID()` for auto-increment position IDs  
  - ✅ Creates new independent position for each deposit (lines 367-376)
  - ✅ No shares logic - direct position creation with `deposit_amount` and `accrued_yield`
  - ✅ Updates vault totals correctly (total_deposits, total_positions)
  - ✅ Returns `position_id` in response (lines 421-424)
  - ✅ Tracks first position per user for total user count

#### 4.2 Update Withdrawal Request Handler ✅  
- [x] **keeper/msg_server_vaults_v2.go:427-542** - RequestWithdrawal handler fully updated:
  - ✅ Takes `position_id` parameter and validates specific position (lines 443-449)
  - ✅ Calculates available balance: `deposit_amount + accrued_yield - pending_withdrawal` (lines 452-463)
  - ✅ Updates position-specific withdrawal tracking (lines 468-476)
  - ✅ Creates withdrawal request with position context (lines 511-523)
  - ✅ No share-related logic in withdrawal flow

#### 4.3 Update SetYieldPreference Handler ✅
- [x] **keeper/msg_server_vaults_v2.go:544-591** - SetYieldPreference handler updated:
  - ✅ Takes `position_id` parameter for per-position yield settings (line 556)
  - ✅ Updates specific position's `receive_yield` preference (lines 567-573)
  - ✅ Emits events with position ID context (lines 575-584)
  - ✅ Returns position-specific response (lines 586-590)

#### 4.4 ProcessWithdrawalQueue Status
- ✅ Withdrawal processing already handles position-based logic
- ✅ Withdrawal requests include `position_id` for proper tracking
- 📝 **Note**: Proportional deduction from `deposit_amount` and `accrued_yield` during processing is handled in withdrawal queue processing

#### 4.5 Share Calculation Functions Status  
- ✅ Core message handlers (Deposit, RequestWithdrawal, SetYieldPreference) have NO shares logic
- 📝 **Note**: `calculateTWAPNav` function still exists but is used for remote position NAV calculations
- 📝 **Note**: Share-related code remains in remote position handlers (RemoteDeposit, RemoteWithdraw) - this is intentional as remote positions may use different accounting models

**Summary**: Phase 4 is **COMPLETE** ✅ - All core user-facing message handlers implement the multi-position direct yield tracking model without shares.

### Phase 5: Accounting Logic (keeper/accounting.go)

#### 5.1 Update Accounting Cursor

**Current:**
```go
type AccountingCursor {
    last_processed_user      string
    positions_processed      uint64
    total_positions          uint64
}
```

**New:**
```go
type AccountingCursor {
    last_processed_user         string
    last_processed_position_id  uint64  // NEW
    positions_processed         uint64
    total_positions             uint64
}
```

#### 5.2 Update Accounting Session Logic

**Current flow:**
1. Iterate users
2. For each user, get their single position
3. Calculate yield based on share price
4. Update position

**New flow:**
1. Iterate ALL positions (all users, all position IDs)
2. For each position:
   - Calculate yield based on time elapsed and NAV growth
   - Create accounting snapshot
3. Commit snapshots atomically

**Key changes:**
```go
func (k *Keeper) UpdateVaultAccounting(ctx, maxPositions) error {
    // Get or create accounting cursor
    cursor, err := k.GetVaultsV2AccountingCursor(ctx)
    
    // Walk positions starting from cursor position
    lastUser, lastPosID, count, err := k.WalkUserPositionsPaginated(
        ctx,
        cursor.LastProcessedUser,
        cursor.LastProcessedPositionId,  // NEW
        maxPositions,
        func(addr sdk.AccAddress, posID uint64, pos vaultsv2.UserPosition) error {
            // Calculate yield for this position
            newYield := calculateYieldForPosition(pos, navGrowth, timeElapsed)
            
            // Create snapshot
            snapshot := vaultsv2.AccountingSnapshot{
                User:           addr.String(),
                PositionId:     posID,  // NEW
                DepositAmount:  pos.DepositAmount,
                AccruedYield:   pos.AccruedYield.Add(newYield),
                AccountingNav:  cursor.AccountingNav,
                CreatedAt:      now,
            }
            
            // Save snapshot
            return k.SetVaultsV2AccountingSnapshot(ctx, snapshot)
        },
    )
    
    // Update cursor
    cursor.LastProcessedUser = lastUser
    cursor.LastProcessedPositionId = lastPosID  // NEW
    cursor.PositionsProcessed += count
    
    // If complete, commit all snapshots
    if cursor.PositionsProcessed >= cursor.TotalPositions {
        return k.commitAccountingSnapshots(ctx)
    }
}
```

#### 5.3 Update Snapshot Commit Logic

```go
func (k *Keeper) commitAccountingSnapshots(ctx) error {
    // Iterate all snapshots
    err := k.IterateAccountingSnapshots(ctx, func(addr sdk.AccAddress, posID uint64, snapshot AccountingSnapshot) error {
        // Get position
        position, found, err := k.GetVaultsV2UserPosition(ctx, addr, posID)
        
        // Update from snapshot
        position.AccruedYield = snapshot.AccruedYield
        position.LastActivityTime = snapshot.CreatedAt
        
        // Save updated position
        return k.SetVaultsV2UserPosition(ctx, addr, position)
    })
    
    // Clear all snapshots
    // Reset cursor
}
```

### Phase 6: Query Handlers (keeper/query_server_vault_v2.go)

#### 6.1 Add New Queries

```go
// Query a specific position
rpc UserPosition(QueryUserPositionRequest) returns (QueryUserPositionResponse)

message QueryUserPositionRequest {
    string user = 1;
    uint64 position_id = 2;
}

message QueryUserPositionResponse {
    UserPosition position = 1;
}

// Query all positions for a user
rpc UserPositions(QueryUserPositionsRequest) returns (QueryUserPositionsResponse)

message QueryUserPositionsRequest {
    string user = 1;
    cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QueryUserPositionsResponse {
    repeated UserPosition positions = 1;
    cosmos.base.query.v1beta1.PageResponse pagination = 2;
}

// Query vault state (update to use totals instead of shares)
rpc VaultState(QueryVaultStateRequest) returns (QueryVaultStateResponse)
```

#### 6.2 Update Existing Queries

- **Remove** all share-related queries
- Update `VaultState` query to return:
  - `total_deposits` (sum of all position deposit_amounts)
  - `total_accrued_yield` (sum of all position accrued_yields)
  - `total_nav` (total_deposits + total_accrued_yield)
  - `total_positions` (count of positions)
  - `total_users` (count of unique users)

### Phase 7: Update Tests (keeper/msg_server_vaults_v2_test.go)

#### 7.1 Update Test Helpers

```go
// Update assertions
func requireUserPosition(t, keeper, user, positionID, expectedDeposit, expectedYield) {
    position, found, err := keeper.GetVaultsV2UserPosition(ctx, user, positionID)
    require.True(t, found)
    require.Equal(t, expectedDeposit, position.DepositAmount)
    require.Equal(t, expectedYield, position.AccruedYield)
}

// REMOVE share-related assertions:
// - requireUserShares
// - requireTotalShares
```

#### 7.2 Update Test Cases

**Deposit tests:**
```go
func TestDeposit(t *testing.T) {
    // First deposit creates position 1
    res1, err := msgServer.Deposit(ctx, &MsgDeposit{
        Depositor:     alice,
        Amount:        sdk.NewInt(1000),
        ReceiveYield:  true,
    })
    require.Equal(t, uint64(1), res1.PositionId)
    
    // Second deposit creates position 2
    res2, err := msgServer.Deposit(ctx, &MsgDeposit{
        Depositor:     alice,
        Amount:        sdk.NewInt(500),
        ReceiveYield:  false,
    })
    require.Equal(t, uint64(2), res2.PositionId)
    
    // Verify both positions exist
    pos1, found, _ := keeper.GetVaultsV2UserPosition(ctx, alice, 1)
    require.True(t, found)
    require.Equal(t, sdk.NewInt(1000), pos1.DepositAmount)
    require.True(t, pos1.ReceiveYield)
    
    pos2, found, _ := keeper.GetVaultsV2UserPosition(ctx, alice, 2)
    require.True(t, found)
    require.Equal(t, sdk.NewInt(500), pos2.DepositAmount)
    require.False(t, pos2.ReceiveYield)
}
```

**Withdrawal tests:**
```go
func TestWithdrawal(t *testing.T) {
    // Create position
    depositRes, _ := msgServer.Deposit(ctx, ...)
    posID := depositRes.PositionId
    
    // Request withdrawal from specific position
    _, err := msgServer.RequestWithdrawal(ctx, &MsgRequestWithdrawal{
        Requester:  alice,
        Amount:     sdk.NewInt(500),
        PositionId: posID,
    })
    
    // Verify position updated
    pos, _, _ := keeper.GetVaultsV2UserPosition(ctx, alice, posID)
    require.Equal(t, sdk.NewInt(500), pos.AmountPendingWithdrawal)
}
```

#### 7.3 Add Multi-Position Tests

```go
func TestMultiplePositions(t *testing.T) {
    // User creates 3 positions
    // Verify each has independent yield tracking
    // Withdraw from one position
    // Verify other positions unaffected
}

func TestPositionYieldPreferences(t *testing.T) {
    // Create position with receive_yield = true
    // Create position with receive_yield = false
    // Run accounting
    // Verify only first position received yield
}
```

### Phase 8: Update Spec Documentation

#### 8.1 Update State Spec (spec/vaults-v2/01_state_vaults_v2.md)

- Replace `UserShares` and `TotalShares` sections with `UserPositions` section
- Document composite key structure
- Document position sequence per user
- Update `VaultState` structure

#### 8.2 Update Messages Spec (spec/vaults-v2/02_messages_vaults_v2.md)

- Update `MsgDeposit` - removes position_id parameter, always creates new
- Update `MsgRequestWithdrawal` - requires position_id parameter
- Update `MsgSetYieldPreference` - requires position_id parameter
- Document that each deposit creates independent position

#### 8.3 Update Queries Spec (spec/vaults-v2/03_queries_vaults_v2.md)

- Add `UserPosition` query
- Add `UserPositions` query
- Update `VaultState` query response
- Remove share-related queries

#### 8.4 Update Overview (spec/vaults-v2/00_overview.md)

- Remove all references to shares and NAV per share
- Document multi-position model
- Update economic model section
- Update all flow diagrams

### Phase 9: Migration Considerations

#### 9.1 State Migration

**Option A: Require full withdrawal from V2 before upgrade**
- Simplest approach
- Users must withdraw all funds before upgrade
- After upgrade, they deposit fresh into new system

**Option B: Automated migration**
```go
func MigrateV2State(ctx sdk.Context, keeper Keeper) error {
    // For each user with shares:
    //   1. Calculate their total value (shares * NAV per share)
    //   2. Create single position with:
    //      - position_id = 1
    //      - deposit_amount = total value
    //      - accrued_yield = 0
    //      - first_deposit_time = migration time
    //   3. Delete their share entry
    
    // Delete total shares
    // Update vault state to new structure
}
```

**Recommendation:** Option A is safer and simpler for this breaking change.

#### 9.2 Genesis Export/Import

- Update `genesis.go` to export/import positions with composite keys
- Update genesis validation
- Ensure position sequences are preserved

### Phase 10: Validation & Testing

#### 10.1 Unit Tests
- [ ] All keeper state functions (get, set, iterate)
- [ ] Position sequence generation
- [ ] Deposit creates new position
- [ ] Withdrawal from specific position
- [ ] Yield preference per position
- [ ] Accounting across multiple positions
- [ ] Position deletion when empty

#### 10.2 Integration Tests
- [ ] Multiple users with multiple positions
- [ ] Accounting with mixed yield preferences
- [ ] Withdrawal queue with multi-position
- [ ] Total calculations (deposits, yield, NAV)

#### 10.3 Edge Cases
- [ ] User with 100+ positions
- [ ] Accounting pagination with position boundaries
- [ ] Withdrawal exceeding position balance
- [ ] Position ID overflow (unlikely but handle)

## Implementation Order

1. ✅ **Phase 1**: Proto updates (COMPLETED)
2. ✅ **Phase 2**: Keeper structure (COMPLETED)
3. ✅ **Phase 3**: State accessor functions (COMPLETED)
4. ✅ **Phase 4**: Message handlers (deposit, withdrawal) - COMPLETED
5. **Phase 5**: Accounting logic
6. **Phase 6**: Query handlers
7. **Phase 7**: Update tests
8. **Phase 8**: Update spec docs
9. **Phase 9**: Migration planning
10. **Phase 10**: Validation

## Breaking Changes

### API Changes
- `MsgDeposit`: removed `receive_yield_override` and `position_id` fields
- `MsgDepositResponse`: added `position_id` field
- `MsgRequestWithdrawal`: added `position_id` field (required)
- `MsgSetYieldPreference`: added `position_id` field (required)
- All queries returning user balances now require `position_id`

### State Changes
- Removed: `vaults/v2/user_shares/*`
- Removed: `vaults/v2/total_shares`
- Changed: `vaults/v2/user_position/*` now uses composite key
- Added: `vaults/v2/user_position_seq/*`
- Changed: `VaultState` structure (no more total_shares)

### Behavioral Changes
- Each deposit creates a new independent position
- Cannot add to existing positions
- Yield tracked per-position, not per-share
- Users must specify position_id for withdrawals

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Breaking state compatibility | High | Require full V2 withdrawal before upgrade |
| Accounting complexity with many positions | Medium | Pagination, cursor-based processing |
| User confusion (multiple positions) | Low | Clear UI/UX, good documentation |
| Position ID conflicts | Low | Per-user sequences, careful testing |
| Query performance with many positions | Medium | Proper indexing, pagination |

## Success Criteria

- [ ] All unit tests pass
- [ ] All integration tests pass
- [ ] Build succeeds without errors
- [ ] Spec documentation updated and accurate
- [ ] No shares-based code remains
- [ ] Multi-position workflow works end-to-end
- [ ] Accounting correctly handles multiple positions per user
- [ ] Performance acceptable with 100+ positions per user

## Estimated Effort

- Phase 2-3 (Keeper): 2-3 hours
- Phase 4 (Message handlers): 3-4 hours
- Phase 5 (Accounting): 2-3 hours
- Phase 6 (Queries): 1-2 hours
- Phase 7 (Tests): 3-4 hours
- Phase 8 (Docs): 1-2 hours
- **Total: 12-18 hours**

## Questions for Review

1. Should we set a maximum number of positions per user? (e.g., 1000)
2. Should position IDs be globally unique or per-user unique? (Recommendation: per-user)
3. Do we need a "merge positions" feature in the future?
4. Should we allow transferring positions between addresses?
5. How should we handle accounting if a user has 1000+ positions?

## Next Steps

After plan approval:
1. Begin Phase 2 (Keeper structure updates)
2. Create PR with Phase 2-3 changes for review
3. Once approved, proceed with Phases 4-6
4. Final PR with tests and documentation
