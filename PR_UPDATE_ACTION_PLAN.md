# PR Update Action Plan

## Current Situation

### Existing PRs
- **PR #60**: State management (pr/02-state-management) - OPEN
- **PR #61**: Protocol buffers (pr/01-protocol-buffers) - OPEN  
- **PR #50**: Original draft (zaki/managed-vault) - DRAFT

### Recent Major Changes on `zaki/managed-vault-implementation`
1. **Multi-position vault system** (replacing shares)
2. **Cursor-based accounting** with snapshots
3. **Enhanced inflight funds tracking**
4. **Complete test coverage** added

## Recommended Actions

### Option 1: Update Existing PRs (Simpler)
1. **Close PRs #60 and #61** (outdated structure)
2. **Update PR #50** with latest changes from `zaki/managed-vault-implementation`
3. **Create 1-2 follow-up PRs** for specific enhancements:
   - PR for multi-position system
   - PR for inflight funds tracking

**Pros**: Less PR management, faster to review
**Cons**: Larger PRs, harder to review thoroughly

### Option 2: Fresh PR Structure (Recommended)
1. **Close all existing PRs** (#50, #60, #61)
2. **Create new PR branches** from current `zaki/managed-vault-implementation`
3. **Split into 8 focused PRs** as per UPDATED_PR_PLAN.md

**Pros**: Cleaner review process, better organization
**Cons**: More setup work initially

## Immediate Next Steps (Option 2)

### Step 1: Clean up existing PRs
```bash
# Close outdated PRs
gh pr close 60
gh pr close 61
gh pr close 50  # Or keep as reference
```

### Step 2: Create new branch structure
```bash
# From zaki/managed-vault-implementation (32ce228)
git checkout zaki/managed-vault-implementation

# Create PR branches
git checkout -b pr/01-protocol-buffers-v2
git checkout -b pr/02-state-management-v2
git checkout -b pr/03-core-operations-v2
git checkout -b pr/04-accounting-system-v2
git checkout -b pr/05-inflight-management-v2
git checkout -b pr/06-oracle-hyperlane-v2
git checkout -b pr/07-query-server-v2
git checkout -b pr/08-module-docs-v2
```

### Step 3: Cherry-pick commits into appropriate branches

For each PR branch, cherry-pick or filter the relevant files:

#### PR 1: Protocol Buffers
```bash
git checkout pr/01-protocol-buffers-v2
# Include: proto/*, types/vaults/v2/*.pb.go, api/vaults/v2/*
```

#### PR 2: State Management
```bash
git checkout pr/02-state-management-v2
# Include: keeper/keeper.go, keeper/state_vaults_v2.go
```

#### PR 3: Core Operations
```bash
git checkout pr/03-core-operations-v2
# Include: keeper/msg_server_vaults_v2.go (deposit/withdraw/yield)
# Include: keeper/msg_server_vaults_v2_test.go
# Include: keeper/msg_server_vaults_v2_multiposition_test.go
```

#### PR 4: Accounting System
```bash
git checkout pr/04-accounting-system-v2
# Include: keeper/accounting.go
# Include: UpdateVaultAccounting handler
```

#### PR 5: Inflight Management
```bash
git checkout pr/05-inflight-management-v2
# Include: keeper/inflight_lifecycle.go
# Include: keeper/msg_server_vaults_v2_inflight_flow_test.go
# Include: Remote operations handlers
```

#### PR 6: Oracle & Hyperlane
```bash
git checkout pr/06-oracle-hyperlane-v2
# Include: keeper/hyperlane*.go
# Include: Oracle handlers
```

#### PR 7: Query Server
```bash
git checkout pr/07-query-server-v2
# Include: keeper/query_server_vault_v2.go
```

#### PR 8: Module & Docs
```bash
git checkout pr/08-module-docs-v2
# Include: module.go, spec/*, docs/*
```

### Step 4: Create PRs with detailed descriptions

Template for each PR:

```markdown
## Summary
[What this PR implements]

## Key Changes
- Change 1
- Change 2

## Files to Review
- `file1.go` - [what to look for]
- `file2.go` - [what to look for]

## Testing
```bash
# How to test this PR
go test ./keeper -run TestSpecificTest
```

## Dependencies
- Depends on: PR #X
- Blocked by: None

## Breaking Changes
- [List any breaking changes]

## Checklist
- [ ] Core logic reviewed
- [ ] Tests passing
- [ ] No security issues
```

## Timeline Estimate

| Task | Time |
|------|------|
| Close old PRs | 5 min |
| Create branch structure | 30 min |
| Split code into PRs | 2-3 hours |
| Write PR descriptions | 1 hour |
| **Total Setup** | **4-5 hours** |

## Alternative: Simplified 3-PR Approach

If 8 PRs is too complex, consider 3 larger PRs:

### PR A: Infrastructure (Proto + State)
- All protobuf files
- State management
- ~40% of changes

### PR B: Core Logic (Operations + Accounting + Inflight)
- All message handlers
- Accounting system
- Inflight lifecycle
- ~40% of changes

### PR C: Integration (Query + Module + Tests)
- Query server
- Module wiring
- All tests
- Documentation
- ~20% of changes

This would reduce review overhead while still maintaining logical separation.

## Decision Needed

1. **8 PR approach**: Maximum reviewability, ~8-9 hours total review time
2. **3 PR approach**: Balanced approach, ~6-7 hours total review time
3. **1 PR update**: Fastest to implement, ~4-5 hours review time

## Recommendation

Given the significant changes (multi-position system, new inflight tracking), I recommend:
1. **Use the 3-PR simplified approach** for faster review
2. **Keep PR #50 as reference** (don't close)
3. **Create fresh PRs** with clear descriptions
4. **Add integration tests** in a follow-up PR

This balances thoroughness with practical review constraints.