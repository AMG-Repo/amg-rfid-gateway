# Delta for Generic RFID Protocol Framing

## ADDED Requirements

### Requirement: Protocol-aware runtime dispatch safety

Runtime packet handling MUST select a protocol handler by configured antenna protocol. Generic framing and decoding SHALL run only through the Generic handler for antennas whose effective protocol is `generic`.

#### Scenario: Generic antenna uses existing generic behavior

- GIVEN an antenna with effective protocol `generic`
- WHEN a complete frame reaches runtime dispatch
- THEN the generic parser/decoder path is executed
- AND generic outcomes remain consistent with existing generic framing requirements

#### Scenario: Non-generic antenna does not parse as generic accidentally

- GIVEN an antenna with effective protocol `zebra`
- WHEN a frame reaches runtime dispatch
- THEN the system does not execute generic parsing for that antenna
- AND processing is delegated to the selected non-generic handler contract
- AND the current `zebra` handler reports an explicit unsupported-protocol outcome without storing tag data

### Requirement: Unknown protocol fails safely at runtime boundaries

If an antenna resolves to an unknown or unsupported protocol at runtime, the system MUST fail safely with explicit error signaling from the selected handler and MUST NOT parse or persist data using the generic path by fallback.

#### Scenario: Runtime receives unsupported protocol selection

- GIVEN an antenna whose resolved protocol is unsupported
- WHEN runtime dispatch is invoked
- THEN dispatch returns an explicit unsupported-protocol failure
- AND no tag data is emitted or stored for that frame

#### Scenario: Failure isolation for mixed protocol deployments

- GIVEN multiple antennas where one has unsupported protocol and others are valid
- WHEN frames are processed concurrently
- THEN unsupported-protocol failures remain isolated to the invalid antenna
- AND valid generic antennas continue normal processing
