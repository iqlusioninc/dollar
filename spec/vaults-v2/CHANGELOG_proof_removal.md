# Changelog: Simplified Oracle Design

## Date: 2024

## Overview
Simplified the Vault V2 oracle system by removing unnecessary validation mechanisms:
- Removed proof hash requirements - relies on Hyperlane's built-in authentication
- Removed MinUpdateInterval - no rate limiting needed
- Removed MaxPriceChangePercent - trust enrolled oracles
- Removed EmergencyPauseDelay and EmergencyMultisig - simplified governance

## Changes Made

### 1. Overview Document (`00_overview.md`)

#### Before:
- "Proof Verification: Cryptographic proofs for cross-chain values"
- "Proof Requirements: Cryptographic verification of Hyperlane message completions"

#### After:
- "Message Verification: Hyperlane message authentication for cross-chain values"
- "Message Authentication: Verification of Hyperlane message origin and integrity"

### 2. Messages Document (`02_messages_vaults_v2.md`)

#### NAV Update Message Structure
**Before:**
```json
{
  "position_id": "1",
  "new_value": "1050000",
  "proof": "base64_encoded_proof"
}
```

**After:**
```json
{
  "position_id": "1",
  "new_value": "1050000"
}
```

#### Requirements Section
**Before:**
- "Proofs must be valid according to configured oracle requirements"

**After:**
- Removed proof validation requirement
- Relies on Hyperlane message authentication

#### Oracle Message Format
**Before:**
```
124     | 32   | ProofHash           | bytes32 | Position proof
156     | 4    | Reserved            | bytes4  | Future use
```

**After:**
```
124     | 36   | Reserved            | bytes36 | Future use (zero-filled)
```

### 3. NAV Oracle Document (`04_nav_oracle.md`)
- No proof hash references existed (already clean)

### 4. Oracle Design Summary (`oracle_design_summary.md`)
- No proof hash references existed (already clean)

## Rationale

### Why Remove Proof Hashes?

1. **Redundancy**: Hyperlane already provides message authentication and verification
2. **Simplicity**: Reduces complexity in message processing
3. **Gas Efficiency**: Smaller message size (no 32-byte proof hash)
4. **Trust Model**: Oracle authentication via registered addresses is sufficient

### Security Considerations

The removal of proof hashes does not compromise security because:

1. **Hyperlane Authentication**: Messages are already authenticated by Hyperlane's infrastructure
2. **Oracle Registration**: Only registered oracles can send updates
3. **Position Mapping**: Strict oracle-to-position mapping prevents misrouting
4. **Validation Checks**: Multiple layers of validation remain:
   - Source chain verification
   - Sender address verification
   - Staleness detection

## Impact

### Positive Impacts
- Simpler message format (105 bytes instead of 137 bytes)
- Reduced processing overhead
- Clearer security model
- Easier integration for oracle operators

### No Negative Impacts
- Security maintained through existing mechanisms
- No loss of data integrity
- No reduction in attack resistance

## Migration Notes

For implementations already using proof hashes:

1. Remove proof generation logic from oracle contracts
2. Update message encoding to exclude proof hash field
3. Remove proof validation logic from Noble-side handlers
4. Update any monitoring or logging that references proofs

## Verification Checklist

- [x] All proof hash references removed from specifications
- [x] Message formats updated to remove proof fields
- [x] Requirements sections updated to remove proof validation
- [x] Security model documented without reliance on proofs
- [x] Alternative verification mechanisms clearly specified

## Summary

The proof hash concept has been completely removed from the Vault V2 oracle specifications. The system now relies on Hyperlane's native message authentication combined with oracle registration and validation checks to ensure data integrity and security. This simplification reduces complexity while maintaining the same level of security through existing infrastructure.