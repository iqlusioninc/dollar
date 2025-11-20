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


## Operational Flows

### Deposit Flow

```
User Deposit Request
    ↓
Funds transferred from user to module account
    ↓
`UserPosition` Created
    ↓
`LocalFunds` Increases
```

### Withdrawal Flow

```
Withdrawal Request
    ↓
Record amount pending withdrawal in `UserPosition`
    ↓
Wait for Liquidity

   ...

Manager marks `WithdrawalRequest` as CLAIMABLE 
    ↓
User Claims (After Delay)
    ↓
`LocalFunds` decreases
    ↓
`UserPosition` amount decreases (yield first, then principle)
    ↓
Transfer $USDN to user from module account
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
