# Vaults V2 Implementation - PR Breakdown

This document summarizes the 8 PRs created to break up the `zaki/managed-vault-implementation` branch into reviewable chunks.

## PR Dependency Chain

Each PR builds on the previous one:

```
managed-vault (base)
  ↓
PR #1: Protocol Buffers & Generated Code
  ↓
PR #2: State Management & Storage Layer
  ↓
PR #3: Core Message Handlers (User Operations)
  ↓
PR #4: Inflight Position Tracking & Lifecycle
  ↓
PR #5: Oracle System
  ↓
PR #6: Query Server (Complete)
  ↓
PR #7: ABCI & Module Integration
  ↓
PR #8: Documentation & Testing
```

## Branch Information

| PR # | Branch Name | Base Branch | Files Changed | Description |
|------|-------------|-------------|---------------|-------------|
| 1 | `pr/01-protocol-buffers` | `managed-vault` | 38 | Protocol buffer definitions and generated code |
| 2 | `pr/02-state-management` | `pr/01-protocol-buffers` | 1 | CRUD operations for vault state |
| 3 | `pr/03-core-message-handlers` | `pr/02-state-management` | 4 | User-facing message handlers (deposit, withdraw, etc.) |
| 4 | `pr/04-inflight-lifecycle` | `pr/03-core-message-handlers` | 1 | Cross-chain inflight fund tracking |
| 5 | `pr/05-oracle-system` | `pr/04-inflight-lifecycle` | 2 | Oracle & NAV management with Hyperlane |
| 6 | `pr/06-query-server` | `pr/05-oracle-system` | 1 | All 28 query endpoints |
| 7 | `pr/07-abci-module` | `pr/06-query-server` | 3 | BeginBlock/EndBlock and module wiring |
| 8 | `pr/08-documentation` | `pr/07-abci-module` | 2 | Implementation tracker and oracle docs |

## Estimated Review Time

- **PR #1**: 30-45 min (mostly mechanical codegen)
- **PR #2**: 30-45 min (straightforward CRUD)
- **PR #3**: 1-2 hours (core business logic)
- **PR #4**: 1 hour (inflight lifecycle)
- **PR #5**: 1 hour (oracle system)
- **PR #6**: 1.5 hours (28 query endpoints)
- **PR #7**: 45 min (ABCI integration)
- **PR #8**: 15 min (documentation only)

**Total estimated review time: 6-8 hours across 8 PRs**

## How to Review

### Sequential Review (Recommended)
Review PRs in order (1 → 8) to understand the full system architecture:

```bash
# Review PR #1
git checkout pr/01-protocol-buffers
git diff managed-vault

# Review PR #2
git checkout pr/02-state-management
git diff pr/01-protocol-buffers

# And so on...
```

### Individual PR Review
Each PR can be reviewed independently with its base:

```bash
# Example: Review just PR #4
git checkout pr/04-inflight-lifecycle
git diff pr/03-core-message-handlers
```

## Notes on PR Organization

1. **Cross-chain route handlers** are included in PR #3 (not in a separate PR) because they share the same `msg_server_vaults_v2.go` file with other message handlers.

2. **Risk controls** (deposit limits, TWAP, circuit breakers) are embedded in PR #3's message handlers rather than split out, as they're integral to the deposit/withdrawal logic.

3. **Query server** combines what was originally planned as two PRs since all queries are in a single file.

## Pushing Branches

To push all PR branches to the remote:

```bash
git push origin pr/01-protocol-buffers
git push origin pr/02-state-management
git push origin pr/03-core-message-handlers
git push origin pr/04-inflight-lifecycle
git push origin pr/05-oracle-system
git push origin pr/06-query-server
git push origin pr/07-abci-module
git push origin pr/08-documentation
```

## Creating Pull Requests

Each branch should be opened as a PR against its base:

- PR #1: `pr/01-protocol-buffers` → `managed-vault`
- PR #2: `pr/02-state-management` → `pr/01-protocol-buffers`
- PR #3: `pr/03-core-message-handlers` → `pr/02-state-management`
- And so on...

**Important**: Do not merge PR #2 until PR #1 is merged, do not merge PR #3 until PR #2 is merged, etc.

## Merging Strategy

### Option 1: Sequential Merge (Safer)
1. Merge PR #1 into `managed-vault`
2. Rebase PR #2 onto `managed-vault`, then merge
3. Rebase PR #3 onto `managed-vault`, then merge
4. Continue...

### Option 2: Cascade Merge (Faster)
1. Merge PR #1 into `managed-vault`
2. Merge PR #2 into PR #1 branch, then fast-forward merge into `managed-vault`
3. Continue the pattern...

## What's Not Included

These items from the original implementation remain on `zaki/managed-vault-implementation` but are NOT in these PRs:

- Integration tests for the full lifecycle
- Performance benchmarks
- Additional edge case handling

These can be added in follow-up PRs after the core implementation is merged.
