# Vaults V2 Implementation Tracker

This document tracks the remaining work needed to bring the Vaults V2
implementation into alignment with the protobuf API and product spec.

## Overview

| Area | Description | Status | Notes |
| ---- | ----------- | ------ | ----- |
| Core message handlers | Implement keeper logic for the gRPC/Msg service (config, NAV, oracle, cross-chain routes, inflight processing) | 🔄 In progress | Starting with user/config/admin messages |
| Inflight lifecycle | Model funds in transit per route, handle Hyperlane acknowledgements, feed into NAV/pending queues | ⏳ Pending | Depends on the message handler foundation |
| Share accounting & risk controls | Enforce NAV-based share pricing, deposit velocity/cooldowns, block limits | ⏳ Pending | Requires NAV pipeline updates |
| Query parity | Expose the full `Query` service (NAV views, user positions, inflight breakdowns, deposit velocity, etc.) | ⏳ Pending | Mirrors state filling work |
| Spec-driven tests | End-to-end tests covering cross-chain flows, inflight recovery, oracle lifecycle | ⏳ Pending | Add once functionality is in place |

## Detailed Tasks

### 1. Core Message Handlers (current focus)

- [x] `MsgSetYieldPreference`
- [x] `MsgUpdateVaultConfig`
- [x] `MsgUpdateParams`
- [x] Cross-chain route lifecycle
  - [x] `MsgCreateCrossChainRoute`
  - [x] `MsgUpdateCrossChainRoute`
  - [x] `MsgDisableCrossChainRoute`
- [x] Remote operation initiators
  - [x] `MsgRemoteDeposit`
  - [x] `MsgRemoteWithdraw`
  - [x] `MsgProcessInFlightPosition`
- [ ] Oracle administration
  - [ ] `MsgRegisterOracle`
  - [ ] `MsgUpdateOracleConfig`
  - [ ] `MsgRemoveOracle`
  - [ ] `MsgUpdateOracleParams`
- [ ] NAV management
  - [ ] `MsgUpdateNAV`
  - [ ] `MsgHandleStaleInflight`

### 2. Inflight Lifecycle

- Define route keying and aggregate tracking (total per route, caps, status).
- Create inflight entries for all outbound operations (deposit, withdraw, rebalance).
- Handle Hyperlane acknowledgements and status transitions.
- Feed arrivals into pending deployment/withdrawal queues.
- Emit events and surface via queries.

### 3. Share Accounting & Risk Controls

- NAV-aware share mint/burn for deposits/withdrawals.
- Deposit limits: per-user, per-block, global.
- Velocity tracking & cooldown enforcement.
- TWAP/guarded share price logic.

### 4. Query Parity

- Implement missing RPC handlers in `QueryServer`.
- Map data structures into response views (NAV, inflight breakdowns, user positions, deposit velocity).
- Add pagination/filters where protobufs expect them.

### 5. Spec-Driven Tests

- Deposit/withdrawal velocity scenarios.
- Cross-chain rebalance with Hyperlane acknowledgement.
- Oracle registration + NAV update flow.
- Stale inflight detection and remediation.
- Query correctness/unit tests.

## Current Work Log

- 2025-03-17: Initial gap analysis completed; tracker document created.
- 2025-03-17: Scoped focus on core message handlers (Part 1).
- 2025-03-17: Implemented `MsgSetYieldPreference`, `MsgUpdateVaultConfig`, and `MsgUpdateParams`; added unit coverage.
- 2025-03-17: Implemented cross-chain route lifecycle (`Create`, `Update`, `Disable`) with state storage and tests.
- 2025-03-17: Implemented remote operation initiators (`RemoteDeposit`, `RemoteWithdraw`, `ProcessInFlightPosition`) with inflight tracking and tests.
