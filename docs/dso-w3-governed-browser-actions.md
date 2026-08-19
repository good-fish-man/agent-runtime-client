# DSO-W3 Governed Browser Action Chain

Status: accepted locally on 2026-08-20

## Scope

DSO-W3 connects a specialist decision to a real desktop browser action without creating a second execution authority. Every mutable browser operation now follows one durable chain:

```text
DecisionTurn
  -> ActionProposal
  -> immutable PlanCandidate
  -> ExecutionContext
  -> independent PolicyDecision
  -> PlanRun
  -> GovernedActionAttempt
  -> resource-version recheck
  -> action-scoped ResourceLease
  -> ActionDispatcher / Control Hub
  -> Launcher observation
  -> VerificationResult
  -> receive_observation DecisionTurn on the same specialist attempt
```

`ActionProposal` remains non-executable. A policy denial never reaches the device dispatcher. Approval-required actions remain waiting, and an action without a returned observation is recorded as `UNKNOWN_OUTCOME`, never as success.

## Resource Authority

- Launcher observations identify a browser resource as `browser://session/<session>/tab/<tab>`.
- `resource_version` is the perception fingerprint, with a deterministic content hash fallback.
- Runtime Client reads the resource twice: once while planning and once immediately before lease acquisition.
- A version change invalidates the action before device dispatch.
- Read operations use shared leases; mutations use an exclusive lease.
- A nullable unique active key provides cross-instance single-writer fencing for a browser tab.

## Persistence

The following records are durable and independently inspectable:

- `os_dso_action_proposal`
- `os_dso_plan_candidate`
- `os_dso_execution_context`
- `os_dso_action_policy_decision`
- `os_dso_action_plan_run`
- `os_dso_governed_action_attempt`
- `os_dso_action_verification`

The existing `os_resource_lease`, `os_decision_turn`, and event stream complete the trace. Device observations are appended to the originating specialist attempt instead of being treated as a separate run.

## Local Acceptance Evidence

- 50/50 deterministic governed browser runs persisted complete proposal, plan, policy, run, attempt, lease, observation, and verification chains.
- A 20-writer database race produced exactly one exclusive lease winner for the same tab.
- Page drift and stale expected versions were rejected before device dispatch.
- Policy denial produced zero device dispatches.
- Cancellation before dispatch produced zero device actions.
- Lost observations produced `UNKNOWN_OUTCOME` and preserved the originating error.
- Device observations were appended as `receive_observation` turns on the same specialist attempt.
- The delegation package contains no direct Launcher dependency; all execution crosses `ActionDispatcher`.
- Full `go test ./...` passed in `agent-runtime-client`.
- Protocol validation and browser resource-identity unit tests passed.

The Launcher's complete browser package includes local HTTP listener tests. Those tests remain a release-environment gate when the execution sandbox disallows loopback listeners; this document does not report them as a local pass until run outside that restriction.

## Exit Decision

DSO-W3 exits locally. DSO-W4 may begin. The governed execution semantics are accepted; the draft wire schema and persistence layout remain evolvable until the protocol freeze milestone.
