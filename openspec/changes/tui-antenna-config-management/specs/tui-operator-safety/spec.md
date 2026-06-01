# Delta for tui-operator-safety

## MODIFIED Requirements

### Requirement: Settings persistence requires explicit save confirmation

The system MUST NOT persist Settings changes solely because fields were edited. The system MUST persist Settings only after explicit save intent and explicit confirmation. This SHALL apply to scalar settings and staged antenna create/edit/delete changes.
(Previously: explicit save/confirm existed for field edits, without explicit antenna CRUD staging language.)

#### Scenario: Editing fields does not persist automatically

- GIVEN an operator edits one or more Settings fields
- WHEN no explicit save intent is triggered
- THEN persisted configuration remains unchanged

#### Scenario: Save intent plus confirmation persists changes

- GIVEN an operator has unsaved Settings edits
- WHEN the operator triggers save intent and confirms
- THEN the system persists the edited Settings
- AND only the intended edited values are changed

#### Scenario: Antenna CRUD remains staged until save confirmation

- GIVEN an operator adds, edits, or deletes antenna entries in Settings
- WHEN the operator leaves without save confirmation
- THEN `config.yaml` remains unchanged for antenna entries

### Requirement: Unsaved Settings exit protection

When leaving Settings with unsaved edits, the system MUST present an explicit decision prompt with save, discard, or stay. The same prompt MUST appear for unsaved antenna CRUD changes.
(Previously: exit protection covered unsaved settings edits but did not explicitly call out antenna CRUD states.)

#### Scenario: Confirm save while leaving Settings

- GIVEN unsaved Settings edits exist
- WHEN the operator chooses save and confirms
- THEN changes are persisted
- AND navigation proceeds away from Settings

#### Scenario: Discard while leaving Settings

- GIVEN unsaved Settings edits exist
- WHEN the operator chooses discard
- THEN unsaved edits are not persisted
- AND navigation proceeds away from Settings

#### Scenario: Stay in Settings

- GIVEN unsaved Settings edits exist
- WHEN the operator chooses stay
- THEN navigation away is canceled
- AND the current editable state remains visible in Settings

#### Scenario: Discard drops unsaved antenna CRUD changes

- GIVEN unsaved antenna create/edit/delete changes exist
- WHEN the operator chooses discard from the unsaved-exit prompt
- THEN staged antenna mutations are removed
- AND persisted antenna configuration remains as before editing
