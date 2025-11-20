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

A User Position represents a single tranch from a user deposit, and is a liability from the perspective of accounting. It is used to track the user's original deposit principle, the portion of that principle eligible to receive yield, and the accrued yield to the user. Users do not receive negative yield; if an oracle update results in a reduction in AUM, the next accounting update will short-circuit. A `UserPosition` may be configured to not receive yield.

### Yield accrual

Yield accrues to User Positions when the manager triggers an accounting update. When the `Σ(Remote Position Values)` portion of AUM has increased since the last accounting update, a proportion of the increase is applied to `UserPostition.AccruedYield` for each eligible position.

### Withdrawal Queue

Withdrawals are serviced in two phases. First, a user submits a withdrawal request for a particular `UserPosition`, and second, claims that withdrawal after a period of time. Once funds are available on chain to service a request, the manager may mark the request as ready to be claimed, at which point a user may claim their withdrawal.

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
Wait for request to be marked claimable

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

### Capital Deployment to Remote Position Flow

```
Manager initiates fund deployment to a new or existing RemotePosition
    ↓
InflightFunds created
    ↓
LocalFunds decreases
    ↓
Warp message created
    ↓
Wait for oracle update

   ...

Hyperlane confirmation received
    ↓
RemotePosition updated (shares, total value, principle)
    ↓
InflightFunds completed
```

### Remote Position Capital Return Flow

```
Manager signals a withdrawal from remote position ahead of time
    ↓
`InflightFunds` created
    ↓
`RemotePosition` amount reduced ahead of time (so that net AUM change == 0)
    ↓
Wait oracle update

   ...

Hyperlane Confirmation Received
    ↓
Update `RemotePosition` shares
    ↓
Wait for funds to arrive in module account

   ...

Manager initiates processing of received funds 
    ↓
Mark `InflightFunds` as completed
    ↓
`LocalFunds` increases
```

### Yield Distribution Flow

```
Oracle message updates one or more RemotePositions
    ↓
AUM recalculated 

   ...

Manager initiates yield distribution
    ↓
Check new AUM > previous distribution AUM (remote portion only) 
    ↓
Calculate total yield increase since previous distribution
    ↓
Apply proportional increase to each eligible UserPosition
```
 
