# internal/core and internal/event

The pure domain: no file, network or process access (spine invariant 9). Anything observed (PR and session state, file hashes, live sessions, the clock) is a parameter.

## Contract map

| Contract rule | Code | Tests |
|---|---|---|
| C-event-log §1 slug ids (v2: slug ≤ 40, id ≤ 42) | `event.ValidateID`, `event.ValidateIDOfKind`, `event.KindOf` | `TestValidateID_EventLog1`, `TestKindOf_EventLog1` |
| §1 aliases (v3: any non-empty string without `/` or whitespace that isn't already an id) | `event.ValidateAlias` (form), `core.ValidateDecision` (not an id in use), `State.Aliases`, `State.Resolve`; carried by `aliases[]` on `TaskDrafted`, `ContractDrafted`, `ExternalAdded` and the decision record (D-alias-field) | `TestValidateAlias_EventLog1Aliases`, `TestValidateDecision_DecisionRecord2`, `TestReduceTaskEvents` |
| §1 ULIDs | `event.ULIDFromParts`, `event.ULIDGenerator`, `event.NewULID`, `event.ValidateULID`, `event.ULIDTime` | `TestULIDEncoding_EventLog1`, `TestValidateULID_EventLog1`, `TestULIDGenerator_SortsByTime` |
| §2 envelope, unknown fields kept | `event.Event` (+ `MarshalJSON`/`UnmarshalJSON`, `Extra`), `event.New`, `event.Decode`, `event.Encode`, `Event.Validate` | `TestEnvelopeRoundTripKeepsUnknownFields_EventLog2`, `TestNewAndValidate_EventLog2` |
| §2 ordering (v5: causal within a subject, D-imported-event-time-order) | `core.SortEvents`, `core.LifecycleRank`, `core.Reduce` (see "Log order" below) | `TestReduceOrderIndependence`, `TestSameTimestampLifecycleOrder_EventLog2`, `TestCausalOrderWithinSubject_DImportedEventTimeOrder` |
| §3 lanes (v4) | `core.Lanes` (table as data), `core.CheckLane` (role, subject kind, claiming session), `core.CheckLaneIn` (own task for sessions and integrators, addressee, route owner), `State.SessionTasks` (TaskDrafted.session or latest TaskSessionAttached), `State.RouteOwner`, `core.ActorIs` | `TestLaneTable_EventLog3`, `TestLaneSubjectKinds_EventLog3`, `TestLaneOwnTask_EventLog3`, `TestLaneOwnTaskSources_EventLog3`, `TestLaneDecisionRaisers_EventLog3`, `TestLaneClaimingSession_EventLog3`, `TestLaneAddressee_EventLog3`, `TestLaneRouteResolved_EventLog3` |
| §4 routing | `core.Route`, `core.RouteID`, `State.DefaultAddressee`, `State.Addressee`; a `RouteResolved` before its `RoutedToOwner` is kept in `State.EarlyResolutions` and closes the route when it appears | `TestRoute_EventLog4`, `TestEarlyRouteResolution_EventLog4` |
| §4 inbox | `core.Inbox`, `core.PendingDecisions` | `TestInbox_EventLog4`, `TestPendingDecisionsDeadlineTiebreak` |
| §3 loop amendments (v5, D-apply-amend-lane) | `Lane.ByDecision`: `TaskAmended` by the loop only with `data.by_decision` | `TestApplyAmendLane_DApplyAmendLane` |
| §5 conflicts | `core.Conflicts`, one function per rule: `LaneConflicts`, `RoleConflicts`, `DecidedTwiceConflicts`, `FrozenTwiceConflicts`, `FrozenTextChangedConflicts`, `IDCollisionConflicts`, `OrphanStepConflicts` (v5), `SpineUnapprovedConflicts` (v5) | `TestConflicts_EventLog5/*`, `TestOrphanStep_DImportedEventTimeOrder`, `TestSpineUnapproved_DPostSwitchHandEdits` |
| §6 `ContractVersioned` (v5, D-contract-version-recording) | `Contract.Pending`: a recorded version waits for `ContractFrozen`; `Version` is the highest frozen one | `TestContractVersioned_DContractVersionRecording` |
| Lead actions (C-cli-json v3, T-audit F-2026-11-20-2) | `core.LeadActions`: approve, freeze, apply | `TestLeadActions_AuditF2` |
| §6 event types (v4) | `event.Types`, one payload struct per type, `event.NewPayload`, `Event.Payload` | `TestEveryTypeHasPayload_EventLog6` |
| §6 contract blockers (v4, D-contract-blockers) | `ContractDrafted.blocked_by`, `ContractBlockersChanged`, `Contract.BlockedBy`, `core.ContractOpenBlockers`; counted in `OpenBlockers` / `DeriveTaskStatus` and `BlockedByDecision` / `Priority` | `TestContractBlockers_EventLog6` |
| §7 rule 2 (observed state is not stored) | `core.Observed`, `core.ConflictInputs` are inputs, never events | `TestDeriveTaskStatus_Spine3` |
| §9 unknown types skipped | `State.Skipped`, `event.ErrUnknownType` | `TestReduceSkipsUnknownAndBadEvents_EventLog9` |
| C-decision-record §1 fields, §2 rules 1–7 | `core.ValidateDecision` (errors are `ValidationError` of `FieldError{Field, Rule}`), `core.ValidateDecisionRaised` (no lifecycle fields, id = subject) | `TestValidateDecision_DecisionRecord2`, `TestValidateDecisionRaised_DecisionRecord1Lifecycle` |
| C-decision-record §3 lifecycle | `core.Reduce` (`Decision.Status`, `AppliedOn`, `SupersededBy`) | `TestDecisionLifecycle_DecisionRecord3` |
| Spine §3 task status | `core.DeriveTaskStatus`, `core.TaskStatuses`, `core.OpenBlockers` | `TestDeriveTaskStatus_Spine3` |
| Spine §3 decision priority | `core.Urgency` (formula in its doc comment), `core.BlockedByDecision`, `core.Priority` | `TestUrgency_Spine3Priority`, `TestPriority_Spine3Priority` |

Golden test: `testdata/log.jsonl` reduces to `testdata/state.golden.json` (state, statuses, inboxes, routes to write, conflicts). Test files embed testdata, so tests don't import `os` either. To refresh the golden after an intended change, run `go test ./internal/core -run TestGolden -v` and copy the printed result.

## Log order

`SortEvents` orders the log causally within each subject (D-imported-event-time-order, C-event-log v5 §2): one subject's events are ordered by `LifecycleRank` first and their ULID time second, so an apply always comes after its decide even when it is stamped earlier (a backdated import, clock skew). The subjects' sequences are then merged by time, always taking the earliest next event. Project-wide events (role claims, spine approvals) are not one item's lifecycle and keep plain time order. An event addressed to an alias joins the sequence of the item whose creating event declares that alias. Remaining ties break by rank, then id. A step whose prerequisite never exists is reported as `orphan-step`.

| Rank | Types |
|---|---|
| 0 drafted/raised | `TaskDrafted`, `DecisionRaised`, `ContractDrafted`, `ExternalAdded` |
| 1 approved/frozen/asked | `TaskApproved`, `ContractFrozen`, `QuestionAsked`, `TaskSessionAttached`, `RoleClaimed`, `SpineApproved` |
| 2 amended/answered/decided | `TaskAmended`, `QuestionAnswered`, `DecisionDecided`, `DecisionDefaulted`, `DecisionSuperseded`, `TaskBlockersChanged`, `ContractBlockersChanged`, `ContractVersioned`, `TaskHandoff`, `TaskDeferred`, `ExternalUpdated`, `RoutedToOwner` |
| 3 applied | `DecisionApplied`, `ExternalResolved`, `RouteResolved`, `RoleReleased`, `ConflictDetected` |
| 4 | unknown types (skipped by the reducer anyway) |

Across subjects, and within one rank of one subject, events keep plain time order.

## Readings pending a decision

Where the contracts left a choice open, the code picks one and says so in its doc comment: `approved` vs `ready` (D-task-status-approved), what clears a blocker and how far priority reaches (D-blocker-semantics), the urgency steps (D-urgency-formula), where aliases are written (D-alias-field), and lane gaps (D-lane-gaps). See the T-core handoff.
