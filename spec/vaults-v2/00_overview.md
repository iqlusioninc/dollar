# V2 Vault Overview

## Introduction

This system features a single managed vault on the Noble chain that enables $USDN holders to participate in diversified yield opportunities.

## Core Concepts

### AUM Accounting

Assets Under Management (AUM) are tracked as virtual USDN in three components: `LocalFunds`, `RemotePosition` funds, and `InflightFunds`.

```
Total AUM = Local Funds
          + Σ(Remote Position Values)
          + Σ(Inflight Funds Values)
```

This value, together with a TWAP of Remote Position shares (unimplemented), serves as the core accounting mechanism for tracking the movement of funds through the system, accruing yield to user positions, and servicing withdrawals.

### Local Funds

Local Funds are the portion of the AUM accounting that are present on the Noble chain and available for deployment or for servicing user withdrawals.

### Inflight Funds

Inflight Funds are the portion of the AUM accounting that represent funds in transit between Noble and a Remote Position in either direction.

### Remote Positions

Remote Positions represent funds deposited into an [ERC-4626](https://eips.ethereum.org/EIPS/eip-4626) compatible vault on a remote chain with their own shares and share prices.

### User Positions

A User Position represents a single tranch from a user deposit, and is a liability from the perspective of accounting. It is used to track the user's original deposit principle, the portion of that principle eligible to receive yield, and the accrued yield to the user. Users do not receive negative yield; if an oracle update results in a reduction in AUM, the next accounting update will short-circuit.

### Withdrawal Queue

Withdrawals are serviced in a two-phase manner where a user submits a withdrawal request for a particular `UserPosition` and later claims that withdrawal. Once funds are available on chain to service a request, the manager may mark the request as ready to be claimed, at which point a user may claim their withdrawal.

### Oracles

Oracles are proxy smart contracts deployed for each remote vault that submit information about their corresponding `RemotePosition` to Noble via Hyperlane. Oracle messages are essential to accurately track AUM accounting through deposit and withdrawal lifecycles, yield accrual, and fresh share prices.

## Remote Position Management

### Position Lifecycle

1. **Creation**: Deploy $USDN to approved ERC-4626 compatible vault on target chain
2. **Share Tracking**: Receive and track vault shares representing the position
3. **Price Reception**: Remote chains push share price updates to Noble via Hyperlane
4. **Automatic NAV Updates**: Noble processes pushed price data to maintain current valuations
5. **Rebalancing**: Redeem shares from one vault and deposit to another
6. **Harvesting**: Yields compound within remote vaults
7. **Closure**: Redeem vault shares for $USDN and withdraw to Noble

### Cross-Chain Coordination

Remote positions leverage Hyperlane for secure cross-chain operations:

- **Deployment**: $USDN bridged via specific Hyperlane routes to deposit into ERC-4626 compatible vaults
- **Share Management**: Vault shares received and tracked for each remote position
- **Inflight Tracking**: $USDN marked as inflight per Hyperlane route ID during bridge transit
- **Route Management**: Each Hyperlane route (e.g., Noble→Hyperliquid, Base→Noble) tracked separately
- **Push-Based Price Updates**: Remote chains proactively push share prices to Noble via Hyperlane using fixed-length byte encoding for efficiency
- **Automatic Value Updates**: Noble continuously receives and applies byte-encoded price data directly from the Hyperlane Mailbox
- **Redemptions**: Vault shares redeemed for $USDN and bridged back to Noble via specific return routes
- **Completion Tracking**: Monitoring of bridge transaction completions per route
- **Emergency Recovery**: Most bridge failures will be temporary. They may be cause by something like a chain halt or relayer in the ISM going offline. In this case, it should be possible to just

### Risk Management

Each remote position is subject to:

- **Concentration Limits**: No single position > X% of total vault
- **Chain Limits**: Maximum exposure per blockchain
- **Approved Vaults Only**: Only deploy to pre-approved vault addresses
- **Push-Based Price Monitoring**: Receive continuous share price updates pushed from remote chains
- **Health Monitoring**: Automatic alerts when price pushes stop arriving or show degradation

## Inflight Funds Management

### Overview

Inflight funds represent capital that is temporarily in transit between the Noble vault and its remote positions, or between positions during rebalancing. Each inflight transaction is tracked by its specific Hyperlane route identifier, allowing precise monitoring of capital flows across different bridge paths. This capital remains fully accounted for in the NAV to ensure accurate vault valuation at all times.

### Inflight Fund Types

1. **Deposit to Position**: $USDN being deployed from the Noble vault to a remote ERC-4626 compatible vault
2. **Withdrawal from Position**: $USDN returning from redeemed vault shares to the Noble vault
3. **Rebalance Between Positions**: $USDN moving between remote vaults (via Noble after share redemption)
4. **Pending Deployment**: $USDN from deposits awaiting allocation to remote vaults
5. **Pending Withdrawal Distribution**: $USDN from redeemed shares awaiting distribution to withdrawal queue
6. **Yield Collection**: Automatic compounding within remote vaults

### Tracking Mechanism

Each inflight transaction maintains:
- **Hyperlane Route ID**: Unique route identifier
- **Transaction ID**: Hyperlane message ID for the specific transfer
- **Source/Destination Domains**: Hyperlane domain IDs for the route endpoints (1313817164 for Noble, 998 for Hyperliquid, 8453 for Base)
- **Expected Value**: $USDN amount sent including estimated bridge fees
- **Current Value**: Last known $USDN value for NAV calculation
- **Time Bounds**: Expected arrival time and maximum duration per route
- **Status Updates**: PENDING → CONFIRMED → COMPLETED lifecycle per route
- **Route-Specific Limits**: Maximum exposure allowed per Hyperlane route

### NAV Impact

Inflight funds are included in NAV calculations to prevent artificial value fluctuations:

```
During Transit:
- $USDN leaves source → Marked as inflight. WITHDRAWL_FROM_POSITION and DEPOSIT_TO_POSITION states are inflight.
- NAV unchanged ($USDN still counted)
- Hyperlane confirms → Status: CONFIRMED
- $USDN arrives → Status: COMPLETED

Special States:
- New deposits → PENDING_DEPLOYMENT until allocated
- Returned funds → PENDING_WITHDRAWAL_DISTRIBUTION until claimed
- Rebalancing → WITHDRAWAL then PENDING_DEPLOYMENT then DEPOSIT
```

### Transaction Completion

1. **Status Tracking**: Bridge transactions monitored for completion
2. **Timeout Management**: Stale transactions flagged for investigation
3. **Manual Intervention**: Failed transactions requiring intervention from the authority module.

### Risk Mitigation

- **Maximum Duration Limits**: Funds cannot remain inflight indefinitely on any route
- **Per-Route Value Caps**: Limits on inflight exposure for each Hyperlane route
- **Message Authentication**: Verification of Hyperlane message origin and integrity

## Operational Flows

### Deposit Flow with Security Checks

```
User Deposit Request
    ↓
Velocity Check → [Fail: Reject]
    ↓ Pass
Limit Checks → [Fail: Reject]
    ↓ Pass
Cooldown Check → [Fail: Reject]
    ↓ Pass
Calculate Shares (Current NAV)
    ↓
Mint Shares to User
    ↓
Update Velocity Metrics
    ↓
Mark Funds as Inflight
    ↓
Deploy Capital via Bridge
    ↓
Monitor Bridge Confirmation
    ↓
Deposit into Remote Vault
    ↓
Receive and Track Vault Shares
```

### Withdrawal Flow with Queue

```
Withdrawal Request
    ↓
Lock User Shares
    ↓
Enter Queue (FIFO)
    ↓
Record NAV Snapshot
    ↓
Wait for Liquidity
    ↓
Process Queue (Keeper/Auto)
    ↓
Mark as CLAIMABLE
    ↓
User Claims (After Delay)
    ↓
Burn Shares & Transfer $USDN
```

### Remote Position Capital Return Flow

```
Initiate Vault Share Redemption
    ↓
Redeem Shares for $USDN
    ↓
Mark Expected $USDN as Inflight
    ↓
NAV Includes Inflight $USDN Value
    ↓
Hyperlane Confirmation Received
    ↓
Mark Transaction as Completed
    ↓
Mark as PENDING_WITHDRAWAL_DISTRIBUTION
    ↓
Update Vault Liquidity
    ↓
Process Withdrawal Queue
```

## Economic Model

### Fee Structure

- **Management Fee**: Annual percentage on total AUM
- **Performance Fee**: Percentage of profits above hurdle rate
- **No Deposit/Withdrawal Fees**: Encourages participation
- **Fee Distribution**: To protocol treasury and/or stakers

### Yield Distribution

- **Automatic Compounding**: Yields increase NAV per share
- **No Manual Claims**: Value accrues to share price
- **Fair Distribution**: Proportional to share ownership
- **Tax Efficiency**: Unrealized gains until withdrawal

## Advantages Over V1

### Enhanced Capital Efficiency

- **Multi-Strategy**: Higher yields through diversification
- **Dynamic Allocation**: Respond to market opportunities
- **Cross-Chain Reach**: Access best yields anywhere
- **Professional Management**: Expert strategy selection

### Improved Security

- **Queue System**: Eliminates many attack vectors
- **Velocity Controls**: Prevents rapid manipulation
- **NAV Snapshots**: Fair value for all users
- **Oracle Redundancy**: Multiple verification sources

### Better User Experience

- **Single Token**: Users hold shares in the single Noble vault, complexity abstracted
- **Automatic Optimization**: No manual strategy switching
- **Transparent NAV**: Clear value per share
- **Predictable Withdrawals**: Queue provides certainty

## Emergency Procedures

### Pause Mechanisms

- **Deposit Pause**: Stop new deposits, allow withdrawals
- **Withdrawal Pause**: Emergency only, requires governance
- **Position Pause**: Halt specific remote positions
- **Global Pause**: Complete system halt (extreme cases)

### Recovery Procedures

- **Position Recovery**: Retrieve funds from failed positions
- **Inflight Recovery**: Manual intervention for stuck bridge transactions
- **Oracle Fallback**: Manual NAV updates if oracles fail
- **Transaction Override**: Force-complete stale inflight funds
- **Emergency Withdrawal**: Direct redemption at last known NAV
- **Governance Override**: Multi-sig can intervene if needed

## Conclusion

The Noble Dollar Vault V2 system represents a sophisticated approach to decentralized yield optimization, balancing the need for high returns with robust security mechanisms. Through innovations like the withdrawal queue, the single vault's ability to manage multiple remote positions, and comprehensive anti-manipulation controls, the system provides users with professional-grade yield strategies while maintaining the security and fairness expected in DeFi.

The architecture's flexibility allows for future enhancements and new strategy additions without requiring user migration, ensuring the system can evolve with the rapidly changing DeFi landscape while protecting user value.
