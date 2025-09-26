# Hyperlane Remote Position Contract

## Overview
This document describes the `HyperlaneRemotePosition` contract, an on-chain
component that custodies USDN bridged through Hyperlane and drives a single
remote Noble vault position. Each deployment of the contract represents one
position in Noble's accounting, holding the ERC-4626 vault shares (for example a
BoringVault deployment) and reporting lifecycle updates back to Noble. The
contract mirrors the Noble Vaults v2 protobuf definitions so that on-chain
activity can be reflected one-for-one in Noble's
`noble.dollar.vaults.v1.PositionEntry` records and aggregated `Stats` totals.

### Design Goals
- **Per-position isolation:** model each Noble position with its own contract so
  upgrades, pausing, and accounting can be managed independently.
- **Safety first:** custody USDN securely, isolating protocol solvency from
  downstream strategies by holding vault shares directly and enforcing
  conservative admin practices.
- **Hyperlane aware:** respond to cross-chain messages that deliver bridged
  USDN, and emit follow-up messages to Noble once funds are staged, deployed, or
  released.
- **Vault agnostic:** support any ERC-4626 implementation with minimal glue
  code, covering BoringVault and similar vaults.
- **Operational clarity:** expose explicit states for idle funds, deployed
  positions, and in-flight reallocations to simplify accounting and monitoring.
- **Admin ergonomics:** provide admin-triggered flows to free liquidity, stage
  deployments, and send confirmations back to Noble without relying on
  time-based automation.

## High-Level Architecture

```
Hyperlane Router ──▶ HyperlaneRemotePosition ──▶ ERC-4626 Vault (BoringVault)
                             │                        ▲
                             ▼                        │
                        Noble Messenger ◀─────────────┘
```

The contract implements the Hyperlane `IMessageRecipient` interface. It receives
bridged USDN, tracks them in an **Idle** bucket, and can move them into
**Deployed** vault positions by depositing into an ERC-4626 vault. During a
withdrawal or redeployment process, the contract transitions amounts to an
**InFlight** bucket to represent pending remote settlement.

## Contract Components

### Key Roles
- **Admin (owner):** single address or timelock with authority over strategy
  configuration, liquidity movements, and outbound messaging. Admin-triggered
  functions are protected with access control.
- **Hyperlane mailbox:** trusted router that calls `handle()` on incoming
  messages. Valid senders are enforced via the Hyperlane message context.
- **Noble remote executor:** entity receiving outbound messages; identified by a
  destination domain and mailbox address.

### Storage Layout
- `IERC20 public immutable usdn` – canonical USDN token on this domain.
- `IMailbox public immutable mailbox` – Hyperlane mailbox used for send/receive.
- `uint32 public immutable nobleDomain` – Hyperlane domain for Noble.
- `bytes32 public nobleRecipient` – identifier for the remote Noble handler.
- `IVaultsCodec public immutable codec` – ABI/protobuf encoding helper for Noble
  payloads.
- `VaultType public immutable vaultType` – Noble vault classification backed by
  this remote position (`STAKED` or `FLEXIBLE`).
- `IERC4626 public strategy` – ERC-4626 vault used to deploy funds (may be zero
  for staked positions that remain idle).
- `VaultBucket private bucket` – consolidated accounting for idle, deployed, and
  in-flight balances belonging to this position.
- `mapping(bytes32 => PendingReceipt) public pendingReceipts` – tracks expected
  Hyperlane receipts keyed by `receiptId`.
- `mapping(bytes32 => InflightDispatch) public inflightDispatches` – tracks
  outbound unlocks awaiting acknowledgement.
- `PausedState public pausedState` – operational pause bitmask mirroring Noble's
  `PausedType`.

Supporting structs:

```solidity
struct VaultBucket {
    uint256 idleAssets;       // USDN waiting to be allocated
    uint256 deployedShares;   // ERC-4626 shares backing the position
    uint256 inflightAssets;   // USDN earmarked for remote settlement
    uint64  nextPositionIdx;  // monotonically increasing index mirrored to Noble
}

struct PendingReceipt {
    uint256 amount;           // USDN expected from Hyperlane
    LiquidityState targetState; // Desired bucket once funds land (Idle by default)
    uint64  remoteIndex;      // Noble's notion of the next position index
    bool    acknowledged;     // Flip once handle() confirms arrival
    bool    exists;           // Ensures duplicate registration is avoided
}

struct InflightDispatch {
    uint256 amount;           // Assets still outstanding for this unlock
    bytes32 receiptId;        // Noble receipt identifier
    bool    acknowledged;     // True once Noble confirms settlement
    bool    exists;           // Guards against unknown message IDs
}
```

### Interfaces
- `IERC20` – standard token interface with `approve`, `transfer`, `transferFrom`.
- `IERC4626` – subset of ERC-4626 functions (`deposit`, `withdraw`, `redeem`,
  `previewWithdraw`, `convertToAssets`).
- `IMessageRecipient` – Hyperlane receive hook.
- `IMailbox` – Hyperlane send interface (`dispatch`).
- `IVaultsCodec` – helper that encodes outbound Noble
  `noble.dollar.vaults.v1` payloads (`MsgUnlock`, `PositionEntry`, `Stats`).

### Events
- `NobleRecipientUpdated(bytes32 previousRecipient, bytes32 newRecipient)`
- `StrategySet(vaults.v1.VaultType vault, address strategy)`
- `PausedStateUpdated(PausedState previousState, PausedState newState)`
- `FundsReceived(bytes32 receiptId, vaults.v1.VaultType vault, uint256 amount)`
- `LiquidityDeployed(vaults.v1.VaultType vault, uint256 assets, uint256 shares, uint64 index, bytes32 messageId)`
- `LiquidityFreed(vaults.v1.VaultType vault, uint256 assets, uint256 sharesBurned)`
- `InflightMarked(bytes32 messageId, vaults.v1.VaultType vault, uint256 assets, bytes32 receiptId)`
- `InflightAcknowledged(bytes32 messageId, vaults.v1.VaultType vault, uint256 assetsRemaining)`
- `OutboundMessage(bytes32 messageId, bytes payload)`
- `ReceiptRegistered(bytes32 receiptId, vaults.v1.VaultType vault, LiquidityState state, uint256 amount, uint64 remoteIndex)`

### Access Control
The contract uses OpenZeppelin `Ownable`. Critical mutations (strategy change,
liquidity movements, Hyperlane dispatch, pause configuration) require `onlyOwner`.

## Core Flows

### 1. Receiving Hyperlane Funds
1. The admin pre-registers each expected receipt via
   `registerExpectedReceipt(receiptId, targetState, amount, remoteIndex)` using
   metadata supplied off-chain.
2. Hyperlane router calls `handle(origin, sender, message)` delivering USDN to
   the contract. The message body is the ABI-encoded `bytes32 receiptId`.
3. The contract verifies `origin` and `sender` match the authorized Noble vaults
   manager, then looks up the `PendingReceipt` keyed by `receiptId`.
4. Upon success it marks the receipt acknowledged, increments `bucket.idleAssets`
   by `amount`, and updates `bucket.nextPositionIdx = max(nextPositionIdx, remoteIndex + 1)`
   so local state remains aligned with Noble's sequencing.
5. Emits `FundsReceived` and leaves the capital in the idle bucket until the
   admin chooses the next action.

### 2. Deploying Into the Vault
1. Admin invokes `deploy(uint256 assets, uint256 minShares, bytes auxData)` when
   idle capital should be allocated into the configured strategy.
2. Require that a strategy is configured (non-zero address) and that
   `assets <= bucket.idleAssets`.
3. Approve the ERC-4626 vault for `assets` and call `deposit(assets, address(this))`.
4. Update `bucket.idleAssets -= assets` and `bucket.deployedShares += sharesReceived`.
5. Compute the up-to-date valuation via `convertToAssets(bucket.deployedShares)`
   and dispatch a Noble `PositionEntry` payload using `_dispatch`.
6. Emit `LiquidityDeployed` with the Noble index pulled from
   `bucket.nextPositionIdx++`.

### 3. Freeing Liquidity
1. Admin calls `freeLiquidity(uint256 assets, uint256 minAssetsOut)` to move
   capital back to the idle bucket.
2. Determine the required shares via `strategy.previewWithdraw(assets)` and
   ensure they do not exceed `bucket.deployedShares`.
3. Execute `withdraw`, capture the actual assets received, and enforce the
   `minAssetsOut` guard.
4. Update accounting (`deployedShares -= sharesBurned`, `idleAssets += assetsReceived`)
   and emit `LiquidityFreed`.

### 4. Marking Funds In-Flight
1. When funds must be released to Noble, the admin calls
   `markInflight(bytes32 receiptId, uint256 assets, bytes auxData)`.
2. Require `assets <= bucket.idleAssets`, decrement idle balances, and increment
   `bucket.inflightAssets`.
3. Encode a `MsgUnlock` payload containing the receipt ID, vault type, and
   amount, then dispatch it via Hyperlane.
4. Persist an `InflightDispatch` keyed by the Hyperlane message ID so settlement
   can be reconciled later, and emit `InflightMarked`.

### 5. Completing In-Flight Settlement
1. Once Noble acknowledges the unlock (off-chain or via follow-up messaging),
   the admin calls `acknowledgeInflight(bytes32 messageId, uint256 settledAssets)`.
2. Locate the `InflightDispatch`, ensure `settledAssets <= amount`, and decrement
   `bucket.inflightAssets` and the outstanding amount.
3. If the dispatch is fully settled mark it acknowledged and emit
   `InflightAcknowledged` with the remaining balance (zero for full settlement).

### 6. Reporting Stats
1. Admin calls `reportStats(uint256 flexibleDistributedRewards, uint64 flexibleUsers, uint64 stakedUsers)`
   when Noble's aggregate statistics should be updated.
2. The contract derives its own total principal by summing idle, in-flight, and
   deployed assets. Depending on `vaultType` it populates either the flexible or
   staked principal in the outbound `Stats` payload.
3. Dispatch the encoded payload and rely on Noble to reconcile distributed
   rewards and user counts.

### 7. Admin Controls
- `setStrategy` swaps or clears the ERC-4626 vault (must share the USDN asset).
- `setNobleRecipient` updates the trusted Noble sender address.
- `setPausedState` allows the admin to disable deposits (`LOCK`), unlocks
  (`UNLOCK`), or both (`ALL`) while still receiving bridged funds.

## Noble Messaging Payloads
- **Inbound (`handle`)** – ABI-encoded `bytes32 receiptId`. All metadata about
  the lock (vault type, amount, Noble index) is provided when the admin calls
  `registerExpectedReceipt`, letting the contract reconcile the transfer without
  decoding protobuf payloads.
- **Outbound deployment acknowledgement** – encoded via `codec.encodePositionEntry`
  with the contract address, `vaultType`, Noble index, deployed principal, and
  the latest valuation.
- **Outbound liquidity release** – encoded via `codec.encodeMsgUnlock`, pointing
  to the original `receiptId`, `vaultType`, and amount being freed. Noble reduces
  the remote position accordingly.
- **Outbound status updates** – encoded via `codec.encodeStats` whenever total
  principal or distribution accounting changes materially.

## Security Considerations
- **Reentrancy:** guard state-mutating functions with `nonReentrant`.
- **Slippage:** enforce `minShares`/`minAssetsOut` to defend against MEV during
  vault interactions.
- **Auth:** restrict `handle` to the trusted mailbox and Noble sender, rejecting
  unexpected payloads or domains.
- **Pausability:** optional circuit breaker allowing the admin to pause
  deployments or unlocks while still accepting bridged funds.
- **Accounting audits:** emit events that mirror state transitions so off-chain
  observers can reconcile the single-position bookkeeping.

## Sequence Diagrams

### Deploy Flow
```
Admin               HyperlaneRemotePosition        BoringVault           Noble
  │ deploy(a)                │                        │                   │
  │─────────────────────────▶│ approve+deposit        │                   │
  │                          │───────────────▶│ deposit assets          │
  │                          │◀───────────────│ shares minted           │
  │                          │ bucket.deployedShares += …                │
  │                          │ dispatch PositionEntry payload ─────────▶ │
```

### Redeem & Stage Flow
```
Admin               HyperlaneRemotePosition        BoringVault           Noble
  │ freeLiquidity(a)         │                        │                   │
  │─────────────────────────▶│ withdraw               │                   │
  │                          │───────────────▶│ withdraw assets         │
  │                          │◀───────────────│ shares burned           │
  │ markInflight(r,a,data)   │                                    │
  │─────────────────────────▶│ idle→inflight, dispatch MsgUnlock ───────▶│
```

## Testing Strategy
- **Unit tests** for Hyperlane message handling, vault deposits/withdrawals, and
  state accounting edge cases on a per-position basis.
- **Fork tests** interacting with live BoringVault deployments to ensure
  compatibility with share accounting and approvals.
- **Property tests** verifying that `idleAssets + convertToAssets(deployedShares)
  + inflightAssets` stays conserved (modulo fees) for the single position.

## Future Extensions
- Deploy multiple `HyperlaneRemotePosition` instances and manage them via a
  higher-level registry that maps Noble vaults to addresses.
- Integrate with automation to trigger deployments based on threshold balances.
- Add keeper roles for routine operations while keeping admin authority for
  critical changes.
- Track USDN valuation using Chainlink feeds to enforce deposit caps or drawdown
  thresholds.
