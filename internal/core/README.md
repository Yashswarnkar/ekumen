# internal/core and internal/event

The pure domain: no file, network or process access (spine invariant 9). Anything observed (PR and session state, file hashes, live sessions, the clock) is a parameter.

## Contract map

| Contract rule | Code | Tests |
|---|---|---|
| C-event-log §1 slug ids (v2: slug ≤ 40, id ≤ 42) | `event.ValidateID`, `event.ValidateIDOfKind`, `event.KindOf` | `TestValidateID_EventLog1`, `TestKindOf_EventLog1` |
| §1 aliases | `event.ValidateAlias`, `State.Aliases`, `State.Resolve` (aliases come from an optional `aliases` field, see D-alias-field) | `TestValidateAlias_EventLog1Aliases`, `TestReduceTaskEvents` |
| §1 ULIDs | `event.ULIDFromParts`, `event.ULIDGenerator`, `event.NewULID`, `event.ValidateULID`, `event.ULIDTime` | `TestULIDEncoding_EventLog1`, `TestValidateULID_EventLog1`, `TestULIDGenerator_SortsByTime` |
| §2 envelope, unknown fields kept | `event.Event` (+ `MarshalJSON`/`UnmarshalJSON`, `Extra`), `event.New`, `event.Decode`, `event.Encode`, `Event.Validate` | `TestEnvelopeRoundTripKeepsUnknownFields_EventLog2`, `TestNewAndValidate_EventLog2` |
| §2 ordering by id | `core.SortEvents`, `core.Reduce` | `TestReduceOrderIndependence` |
| §3 lanes | `core.Lanes` (table as data), `core.CheckLane` (role, subject kind), `core.CheckLaneIn` (own task, addressee, routed to orchestrator) | `TestLaneTable_EventLog3`, `TestLaneSubjectKinds_EventLog3`, `TestLaneOwnTask_EventLog3`, `TestLaneAddressee_EventLog3`, `TestLaneRouteResolved_EventLog3` |
| §4 routing | `core.Route`, `core.RouteID`, `State.DefaultAddressee`, `State.Addressee` | `TestRoute_EventLog4` |
| §4 inbox | `core.Inbox`, `core.PendingDecisions` | `TestInbox_EventLog4`, `TestPendingDecisionsDeadlineTiebreak` |
| §5 conflicts | `core.Conflicts`, one function per rule: `LaneConflicts`, `RoleConflicts`, `DecidedTwiceConflicts`, `FrozenTwiceConflicts`, `FrozenTextChangedConflicts`, `IDCollisionConflicts` | `TestConflicts_EventLog5/*` |
| §6 event types | `event.Types`, one payload struct per type, `event.NewPayload`, `Event.Payload` | `TestEveryTypeHasPayload_EventLog6` |
| §7 rule 2 (observed state is not stored) | `core.Observed`, `core.ConflictInputs` are inputs, never events | `TestDeriveTaskStatus_Spine3` |
| §9 unknown types skipped | `State.Skipped`, `event.ErrUnknownType` | `TestReduceSkipsUnknownAndBadEvents_EventLog9` |
| C-decision-record §1 fields, §2 rules 1–7 | `core.ValidateDecision` (errors are `ValidationError` of `FieldError{Field, Rule}`), `core.ValidateDecisionRaised` (no lifecycle fields, id = subject) | `TestValidateDecision_DecisionRecord2`, `TestValidateDecisionRaised_DecisionRecord1Lifecycle` |
| C-decision-record §3 lifecycle | `core.Reduce` (`Decision.Status`, `AppliedOn`, `SupersededBy`) | `TestDecisionLifecycle_DecisionRecord3` |
| Spine §3 task status | `core.DeriveTaskStatus`, `core.TaskStatuses`, `core.OpenBlockers` | `TestDeriveTaskStatus_Spine3` |
| Spine §3 decision priority | `core.Urgency` (formula in its doc comment), `core.BlockedByDecision`, `core.Priority` | `TestUrgency_Spine3Priority`, `TestPriority_Spine3Priority` |

Golden test: `testdata/log.jsonl` reduces to `testdata/state.golden.json` (state, statuses, inboxes, routes to write, conflicts). Test files embed testdata, so tests don't import `os` either. To refresh the golden after an intended change, run `go test ./internal/core -run TestGolden -v` and copy the printed result.

## Readings pending a decision

Where the contracts left a choice open, the code picks one and says so in its doc comment: `approved` vs `ready` (D-task-status-approved), what clears a blocker and how far priority reaches (D-blocker-semantics), the urgency steps (D-urgency-formula), where aliases are written (D-alias-field), and lane gaps (D-lane-gaps). See the T-core handoff.
