// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {IERC4626} from "@openzeppelin/contracts/token/ERC20/extensions/IERC4626.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import {IMailbox} from "@hyperlane-xyz/contracts/interfaces/IMailbox.sol";

/// @notice Noble vault type enumeration shared across interfaces.
enum VaultType {
    UNSPECIFIED,
    STAKED,
    FLEXIBLE
}

/// @notice Hyperlane recipient interface implemented by the proxy.
interface IMessageRecipient {
    function handle(uint32 origin, bytes32 sender, bytes calldata message) external;
}

/// @notice Interface for encoding/decoding Noble Vaults protobuf payloads.
interface IVaultsCodec {
    struct PositionEntryPayload {
        address provider;
        VaultType vault;
        uint64 index;
        uint256 principal;
        uint256 amount;
        bytes auxData;
    }

    struct MsgUnlockPayload {
        bytes32 receiptId;
        VaultType vault;
        uint256 amount;
        bytes auxData;
    }

    struct StatsPayload {
        uint256 flexiblePrincipal;
        uint64 flexibleUsers;
        uint256 flexibleDistributedRewards;
        uint256 stakedPrincipal;
        uint64 stakedUsers;
    }

    function encodePositionEntry(PositionEntryPayload calldata payload) external view returns (bytes memory);

    function encodeMsgUnlock(MsgUnlockPayload calldata payload) external view returns (bytes memory);

    function encodeStats(StatsPayload calldata payload) external view returns (bytes memory);
}

/// @title HyperlaneRemotePosition
/// @notice Custodies USDN bridged over Hyperlane routes and manages a single Noble vault position.
contract HyperlaneRemotePosition is IMessageRecipient, Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    /// @notice Operational pause bitmask mirroring Noble's PausedType.
    enum PausedState {
        NONE,
        LOCK,
        UNLOCK,
        ALL
    }

    /// @notice Internal liquidity state used to reconcile pending receipts.
    enum LiquidityState {
        Idle,
        Deployed,
        InFlight
    }

    /// @notice Tracks idle, deployed, and inflight balances for this position.
    struct VaultBucket {
        uint256 idleAssets;
        uint256 deployedShares;
        uint256 inflightAssets;
        uint64 nextPositionIdx;
    }

    /// @notice Records an expected inbound Hyperlane receipt.
    struct PendingReceipt {
        uint256 amount;
        LiquidityState targetState;
        uint64 remoteIndex;
        bool acknowledged;
        bool exists;
    }

    /// @notice Metadata for outbound unlock dispatches awaiting acknowledgement.
    struct InflightDispatch {
        uint256 amount;
        bytes32 receiptId;
        bool acknowledged;
        bool exists;
    }

    /// @notice Global USDN token held by the proxy.
    IERC20 public immutable usdn;

    /// @notice Hyperlane mailbox for dispatching and receiving messages.
    IMailbox public immutable mailbox;

    /// @notice Noble domain identifier for outbound dispatches.
    uint32 public immutable nobleDomain;

    /// @notice Noble contract expected to send inbound messages.
    bytes32 public nobleRecipient;

    /// @notice Codec responsible for protobuf payload encoding/decoding.
    IVaultsCodec public immutable codec;

    /// @notice Noble vault classification represented by this remote position.
    VaultType public immutable vaultType;

    /// @notice Strategy used to deploy funds for this remote position.
    IERC4626 public strategy;

    /// @notice Accounting bucket for this remote position.
    VaultBucket private bucket;

    /// @notice Expected inbound Hyperlane receipts keyed by receiptId.
    mapping(bytes32 => PendingReceipt) public pendingReceipts;

    /// @notice Outbound unlock dispatches awaiting settlement keyed by Hyperlane message id.
    mapping(bytes32 => InflightDispatch) public inflightDispatches;

    /// @notice Current paused bitmask controlling operations.
    PausedState public pausedState;

    event NobleRecipientUpdated(bytes32 indexed previousRecipient, bytes32 indexed newRecipient);
    event StrategySet(VaultType indexed vault, address indexed strategy);
    event PausedStateUpdated(PausedState previousState, PausedState newState);
    event FundsReceived(bytes32 indexed receiptId, VaultType vault, uint256 amount);
    event LiquidityDeployed(VaultType vault, uint256 assets, uint256 shares, uint64 index, bytes32 messageId);
    event LiquidityFreed(VaultType vault, uint256 assets, uint256 sharesBurned);
    event InflightMarked(bytes32 indexed messageId, VaultType vault, uint256 assets, bytes32 receiptId);
    event InflightAcknowledged(bytes32 indexed messageId, VaultType vault, uint256 assetsRemaining);
    event OutboundMessage(bytes32 indexed messageId, bytes payload);
    event ReceiptRegistered(
        bytes32 indexed receiptId,
        VaultType vault,
        LiquidityState state,
        uint256 amount,
        uint64 remoteIndex
    );

    constructor(
        address _owner,
        IERC20 _usdn,
        IMailbox _mailbox,
        uint32 _nobleDomain,
        bytes32 _nobleRecipient,
        IVaultsCodec _codec,
        VaultType _vaultType
    ) Ownable() {
        require(_owner != address(0), "owner zero");
        require(address(_usdn) != address(0), "usdn zero");
        require(address(_mailbox) != address(0), "mailbox zero");
        require(address(_codec) != address(0), "codec zero");
        require(_vaultType != VaultType.UNSPECIFIED, "invalid vault");
        usdn = _usdn;
        mailbox = _mailbox;
        nobleDomain = _nobleDomain;
        nobleRecipient = _nobleRecipient;
        codec = _codec;
        vaultType = _vaultType;
        _transferOwnership(_owner);
    }

    /// @notice Updates the trusted Noble recipient used for message validation.
    function setNobleRecipient(bytes32 newRecipient) external onlyOwner {
        require(newRecipient != bytes32(0), "recipient zero");
        bytes32 previous = nobleRecipient;
        nobleRecipient = newRecipient;
        emit NobleRecipientUpdated(previous, newRecipient);
    }

    /// @notice Returns the accounting bucket for this remote position.
    function getBucket() external view returns (VaultBucket memory) {
        return bucket;
    }

    /// @notice Configures the ERC-4626 strategy for this remote position.
    function setStrategy(IERC4626 newStrategy) external onlyOwner {
        if (address(newStrategy) != address(0)) {
            require(newStrategy.asset() == address(usdn), "asset mismatch");
        }
        strategy = newStrategy;
        emit StrategySet(vaultType, address(newStrategy));
    }

    /// @notice Adjusts the paused state of the proxy.
    function setPausedState(PausedState newState) external onlyOwner {
        PausedState previous = pausedState;
        pausedState = newState;
        emit PausedStateUpdated(previous, newState);
    }

    /// @notice Registers an expected inbound receipt so that the proxy can reconcile upon arrival.
    function registerExpectedReceipt(
        bytes32 receiptId,
        LiquidityState targetState,
        uint256 amount,
        uint64 remoteIndex
    ) external onlyOwner {
        require(amount > 0, "amount zero");
        PendingReceipt storage receipt = pendingReceipts[receiptId];
        receipt.targetState = targetState;
        receipt.amount = amount;
        receipt.remoteIndex = remoteIndex;
        receipt.acknowledged = false;
        receipt.exists = true;
        emit ReceiptRegistered(receiptId, vaultType, targetState, amount, remoteIndex);
    }

    /// @notice Hyperlane callback invoked when Noble locks USDN for this proxy.
    function handle(uint32 origin, bytes32 sender, bytes calldata message) external override nonReentrant {
        require(msg.sender == address(mailbox), "unauthorised caller");
        require(origin == nobleDomain, "invalid domain");
        require(sender == nobleRecipient, "invalid sender");

        require(message.length == 32, "invalid message");
        bytes32 receiptId = abi.decode(message, (bytes32));

        PendingReceipt storage receipt = pendingReceipts[receiptId];
        require(receipt.exists, "unknown receipt");
        require(!receipt.acknowledged, "already ack");
        require(receipt.amount > 0, "amount zero");
        receipt.acknowledged = true;

        bucket.idleAssets += receipt.amount;
        uint64 expectedNext = receipt.remoteIndex + 1;
        if (expectedNext > bucket.nextPositionIdx) {
            bucket.nextPositionIdx = expectedNext;
        }

        emit FundsReceived(receiptId, vaultType, receipt.amount);
    }

    /// @notice Deploy idle USDN into the configured vault strategy.
    function deploy(uint256 assets, uint256 minShares, bytes calldata auxData)
        external
        onlyOwner
        nonReentrant
        returns (bytes32 messageId)
    {
        _requireNotPausedForLock();
        IERC4626 currentStrategy = strategy;
        require(address(currentStrategy) != address(0), "strategy unset");
        require(assets > 0, "assets zero");
        require(assets <= bucket.idleAssets, "insufficient idle");

        bucket.idleAssets -= assets;

        usdn.safeApprove(address(currentStrategy), 0);
        usdn.safeApprove(address(currentStrategy), assets);

        uint256 shares = currentStrategy.deposit(assets, address(this));
        require(shares >= minShares, "slippage shares");

        bucket.deployedShares += shares;

        uint64 index = bucket.nextPositionIdx++;
        uint256 valuation = currentStrategy.convertToAssets(bucket.deployedShares);

        IVaultsCodec.PositionEntryPayload memory payload = IVaultsCodec.PositionEntryPayload({
            provider: address(this),
            vault: vaultType,
            index: index,
            principal: assets,
            amount: valuation,
            auxData: auxData
        });

        bytes memory encoded = codec.encodePositionEntry(payload);
        messageId = _dispatch(encoded);

        emit LiquidityDeployed(vaultType, assets, shares, index, messageId);
    }

    /// @notice Withdraw funds from the strategy back to idle liquidity.
    function freeLiquidity(uint256 assets, uint256 minAssetsOut) external onlyOwner nonReentrant {
        _requireNotPausedForUnlock();
        IERC4626 currentStrategy = strategy;
        require(address(currentStrategy) != address(0), "strategy unset");
        require(assets > 0, "assets zero");

        uint256 sharesNeeded = currentStrategy.previewWithdraw(assets);
        require(sharesNeeded <= bucket.deployedShares, "insufficient shares");

        uint256 balanceBefore = usdn.balanceOf(address(this));
        uint256 sharesBurned = currentStrategy.withdraw(assets, address(this), address(this));
        uint256 balanceAfter = usdn.balanceOf(address(this));
        uint256 assetsReceived = balanceAfter - balanceBefore;
        require(assetsReceived >= minAssetsOut, "insufficient assets");

        bucket.deployedShares -= sharesBurned;
        bucket.idleAssets += assetsReceived;

        emit LiquidityFreed(vaultType, assetsReceived, sharesBurned);
    }

    /// @notice Move idle liquidity into an in-flight state and notify Noble of the release.
    function markInflight(bytes32 receiptId, uint256 assets, bytes calldata auxData)
        external
        onlyOwner
        nonReentrant
        returns (bytes32 messageId)
    {
        require(assets > 0, "assets zero");
        require(assets <= bucket.idleAssets, "insufficient idle");

        bucket.idleAssets -= assets;
        bucket.inflightAssets += assets;

        IVaultsCodec.MsgUnlockPayload memory payload = IVaultsCodec.MsgUnlockPayload({
            receiptId: receiptId,
            vault: vaultType,
            amount: assets,
            auxData: auxData
        });

        bytes memory encoded = codec.encodeMsgUnlock(payload);
        messageId = _dispatch(encoded);

        InflightDispatch storage dispatchInfo = inflightDispatches[messageId];
        dispatchInfo.amount = assets;
        dispatchInfo.receiptId = receiptId;
        dispatchInfo.acknowledged = false;
        dispatchInfo.exists = true;

        emit InflightMarked(messageId, vaultType, assets, receiptId);
    }

    /// @notice Confirms settlement of an outbound unlock, freeing the in-flight bucket.
    function acknowledgeInflight(bytes32 messageId, uint256 settledAssets) external onlyOwner nonReentrant {
        InflightDispatch storage dispatchInfo = inflightDispatches[messageId];
        require(dispatchInfo.exists, "unknown dispatch");
        require(!dispatchInfo.acknowledged, "already ack");
        require(settledAssets <= dispatchInfo.amount, "excess settle");

        bucket.inflightAssets -= settledAssets;
        dispatchInfo.amount -= settledAssets;

        if (dispatchInfo.amount == 0) {
            dispatchInfo.acknowledged = true;
        }

        emit InflightAcknowledged(messageId, vaultType, dispatchInfo.amount);
    }

    /// @notice Emits an updated Stats payload to Noble.
    function reportStats(uint256 flexibleDistributedRewards, uint64 flexibleUsers, uint64 stakedUsers)
        external
        onlyOwner
        nonReentrant
        returns (bytes32 messageId)
    {
        uint256 totalPrincipal = _totalPrincipal();
        uint256 flexiblePrincipal = vaultType == VaultType.FLEXIBLE ? totalPrincipal : 0;
        uint256 stakedPrincipal = vaultType == VaultType.STAKED ? totalPrincipal : 0;
        IVaultsCodec.StatsPayload memory payload = IVaultsCodec.StatsPayload({
            flexiblePrincipal: flexiblePrincipal,
            flexibleUsers: flexibleUsers,
            flexibleDistributedRewards: flexibleDistributedRewards,
            stakedPrincipal: stakedPrincipal,
            stakedUsers: stakedUsers
        });

        bytes memory encoded = codec.encodeStats(payload);
        messageId = _dispatch(encoded);
    }

    /// @notice Computes the combined principal tracked across idle, deployed, and inflight buckets.
    function _totalPrincipal() internal view returns (uint256) {
        uint256 deployedAssets;
        IERC4626 currentStrategy = strategy;
        if (address(currentStrategy) != address(0) && bucket.deployedShares > 0) {
            deployedAssets = currentStrategy.convertToAssets(bucket.deployedShares);
        }
        return bucket.idleAssets + bucket.inflightAssets + deployedAssets;
    }

    function _dispatch(bytes memory payload) internal returns (bytes32 messageId) {
        messageId = mailbox.dispatch(nobleDomain, nobleRecipient, payload);
        emit OutboundMessage(messageId, payload);
    }

    function _requireNotPausedForLock() internal view {
        require(pausedState != PausedState.LOCK && pausedState != PausedState.ALL, "lock paused");
    }

    function _requireNotPausedForUnlock() internal view {
        require(pausedState != PausedState.UNLOCK && pausedState != PausedState.ALL, "unlock paused");
    }
}
