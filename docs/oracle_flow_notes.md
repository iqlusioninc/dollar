# Oracle Admin Flow Notes

Outline for the upcoming implementation:

- `MsgRegisterOracle`
  - authority-only
  - validates remote position exists
  - stores `RemotePositionOracle` entry (position id, chain id, oracle address, max staleness)
  - emits `EventOracleRegistered`
- `MsgUpdateOracleConfig`
  - authority-only
  - updates max staleness / activation flags
- `MsgRemoveOracle`
  - authority-only
  - removes oracle entry and marks position oracle metadata
- `MsgUpdateOracleParams`
  - updates global oracle params (e.g., max staleness thresholds)
- Add tests for registration/update/remove scenarios.
