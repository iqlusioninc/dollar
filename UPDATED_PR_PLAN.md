# Updated Vaults V2 PR Breakdown Plan

## Overview
This document provides an updated breakdown of the Vaults V2 implementation into reviewable PRs, accounting for all recent changes including:
- Multi-position vault system (replacing shares-based approach)
- Cursor-based accounting with snapshots
- Enhanced inflight funds tracking for remote positions
- Message-based accounting (BeginBlock removed)
- Comprehensive test coverage

## Current State
**Base Branch**: `zaki/managed-vault-implementation` (commit: 32ce228)
**Total Changes**: ~35 files modified, 31,775 insertions, 10,013 deletions

## Recommended PR Structure (8 PRs)

### PR 1: Protocol Buffers & Core Types
**Focus**: All protobuf definitions and generated code
**Files**: 
- `proto/noble/dollar/vaults/v2/*.proto` (9 files)
- `api/vaults/v2/*.pulsar.go` (generated)
- `types/vaults/v2/*.pb.go` (generated)
- `types/vaults/v2/keys.go`
- `types/vaults/v2/errors.go`

**Key Changes**:
- Multi-position system with position IDs
- Updated withdrawal requests with position tracking
- New inflight funds messages (InitiateRemotePositionRedemption, ProcessIncomingWarpFunds)
- AccountingSnapshot and cursor-based accounting types
- EventInflightAmountMismatch for fee tracking

**Review Time**: 45-60 min (mostly mechanical review)

### PR 2: State Management & Accessors
**Focus**: State management layer with multi-position support
**Files**:
- `keeper/keeper.go` (collections setup)
- `keeper/state_vaults_v2.go`

**Key Changes**:
- UserPositionSequence for auto-increment IDs
- Composite key support for (address, positionID)
- Paginated iteration functions
- Accounting snapshot management
- Inflight funds tracking by route

**Review Time**: 45-60 min

### PR 3: Core Vault Operations
**Focus**: Deposit, withdrawal, and yield preference handlers
**Files**:
- `keeper/msg_server_vaults_v2.go` (partial - deposit/withdrawal/yield)
- `keeper/msg_server_vaults_v2_test.go` (basic tests)
- `keeper/msg_server_vaults_v2_multiposition_test.go`

**Key Changes**:
- Each deposit creates new position with unique ID
- Position-specific withdrawals and yield preferences
- Comprehensive multi-position test coverage
- Deposit velocity tracking

**Review Time**: 1.5-2 hours (core business logic)

### PR 4: Accounting System
**Focus**: Cursor-based accounting with snapshots
**Files**:
- `keeper/accounting.go`
- `keeper/msg_server_vaults_v2.go` (UpdateVaultAccounting handler)

**Key Changes**:
- Cursor-based pagination for large user sets
- Atomic snapshot commits
- Residual tracking for fair distribution
- Message-based triggering (no BeginBlock)
- Support for both shares and position-based accounting

**Review Time**: 1.5 hours (complex logic)

### PR 5: Cross-Chain & Inflight Management
**Focus**: Remote positions and inflight funds lifecycle
**Files**:
- `keeper/msg_server_vaults_v2.go` (remote operations)
- `keeper/inflight_lifecycle.go`
- `keeper/msg_server_vaults_v2_inflight_flow_test.go`

**Key Changes**:
- InitiateRemotePositionRedemption for strategist-triggered redemptions
- ProcessIncomingWarpFunds for marking funds as arrived
- Stale inflight detection and cleanup
- Route capacity enforcement
- Comprehensive inflight flow testing

**Review Time**: 1.5 hours

### PR 6: Oracle & Hyperlane Integration
**Focus**: Oracle system for remote position NAV updates
**Files**:
- `keeper/msg_server_vaults_v2.go` (oracle operations)
- `keeper/hyperlane.go`
- `keeper/hyperlane_nav.go`

**Key Changes**:
- Oracle registration and management
- Hyperlane NAV message processing
- Position value updates via cross-chain messages
- Inflight acknowledgment via oracle updates
- NAV recalculation with accounting protection

**Review Time**: 1 hour

### PR 7: Query Server Implementation
**Focus**: Complete query layer for vault data
**Files**:
- `keeper/query_server_vault_v2.go`
- `api/vaults/v2/query_grpc.pb.go`

**Key Changes**:
- UserPositions and UserPosition queries (replacing shares)
- InflightFunds queries with filtering
- Withdrawal queue with position tracking
- Comprehensive vault state queries
- Oracle and remote position queries

**Review Time**: 1 hour (mostly read-only operations)

### PR 8: Module Integration & Documentation
**Focus**: Module wiring and documentation
**Files**:
- `module.go`
- `spec/vaults-v2/*.md`
- `docs/vaults_v2_implementation_tracker.md`

**Key Changes**:
- Updated module registration
- Complete spec documentation for multi-position system
- Implementation tracker
- Migration notes (shares → positions)

**Review Time**: 30 min

## Key Implementation Changes Since Original Plan

### 1. Multi-Position Vault System
- **Before**: Single shares balance per user
- **After**: Multiple positions per user with unique IDs
- **Impact**: More flexible yield and withdrawal management

### 2. Accounting Improvements
- **Before**: BeginBlock-based full iteration
- **After**: Message-triggered cursor-based pagination with snapshots
- **Impact**: Better scalability and predictable gas costs

### 3. Enhanced Inflight Tracking
- **Before**: Basic inflight fund records
- **After**: Strategist-initiated redemptions with warp route completion
- **Impact**: Complete tracking of cross-chain fund movements

### 4. Test Coverage
- **Added**: Comprehensive multi-position tests
- **Added**: Inflight funds flow tests
- **Added**: Withdrawal queue tests

## Dependencies Between PRs

```
PR 1: Protocol Buffers
  ├── PR 2: State Management
  │     ├── PR 3: Core Vault Operations
  │     │     └── PR 4: Accounting System
  │     └── PR 5: Cross-Chain & Inflight
  │           └── PR 6: Oracle & Hyperlane
  └── PR 7: Query Server
        └── PR 8: Module Integration
```

## Review Strategy Recommendations

### For Reviewers
1. **Start with PR 1**: Understand the data structures
2. **Focus on PR 3-5**: Core business logic requiring deep review
3. **PR 2, 6-7**: Infrastructure and read operations
4. **PR 8**: Final integration check

### High-Risk Areas Needing Careful Review
1. **Accounting logic** (PR 4): Complex math, residual tracking
2. **Multi-position management** (PR 3): Ensure proper isolation
3. **Inflight funds lifecycle** (PR 5): Cross-chain state consistency
4. **Oracle trust model** (PR 6): Security implications

## Testing Coverage by PR

| PR | Test Files | Coverage |
|----|------------|----------|
| PR 3 | `msg_server_vaults_v2_test.go`, `msg_server_vaults_v2_multiposition_test.go` | Core operations |
| PR 5 | `msg_server_vaults_v2_inflight_flow_test.go` | Inflight lifecycle |
| PR 4-6 | Integration tests needed | Cross-chain flows |

## Migration Considerations

### Breaking Changes
1. Shares-based queries removed (GetUserShares, GetTotalShares)
2. Withdrawal requests now require position_id
3. SetYieldPreference requires position_id

### Data Migration
- No migration needed as base version was never deployed
- Clean slate implementation

## Estimated Review Time

| PR | Review Time | Complexity |
|----|-------------|------------|
| PR 1 | 45-60 min | Low (mostly generated) |
| PR 2 | 45-60 min | Medium |
| PR 3 | 1.5-2 hours | High |
| PR 4 | 1.5 hours | High |
| PR 5 | 1.5 hours | High |
| PR 6 | 1 hour | Medium |
| PR 7 | 1 hour | Low |
| PR 8 | 30 min | Low |
| **Total** | **8-9 hours** | |

## Next Steps

1. Close existing incomplete PRs (#60, #61)
2. Create fresh PR branches from `zaki/managed-vault-implementation`
3. Cherry-pick commits into appropriate PRs
4. Create PRs in dependency order
5. Add PR descriptions with test instructions
6. Begin review process

## Notes for PR Descriptions

Each PR should include:
1. **Summary** of changes
2. **Key files** to review
3. **Testing instructions**
4. **Breaking changes** (if any)
5. **Dependencies** on other PRs
6. **Review checklist** for important areas