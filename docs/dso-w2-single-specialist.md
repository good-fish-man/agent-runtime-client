# DSO-W2 Single-Specialist Vertical Slice

Status: accepted locally on 2026-08-20

## Scope

DSO-W2 introduces the first governed dynamic delegation path. The main runtime client keeps simple requests and direct device commands on the existing fast path. A compound research request may create exactly one bounded Research Specialist through the durable Delegation Orchestrator.

The execution chain is:

```text
RunStream
  -> RoutePolicy
  -> DelegationProposal + DelegationDecision
  -> DelegatedOutcomeSpec + SubagentSpec
  -> ContextBuilder
  -> CapabilityView + ActorBinding + InvocationManifest
  -> SubagentRun + SubagentAttempt
  -> governed agent-runtime invocation
  -> DecisionTurn + ModelInvocation
  -> TypedCandidateResult
  -> external VerificationResult
```

The legacy request-scoped `SubAgentManager` is no longer registered by the production Dispatcher. A specialist cannot delegate again, receives no parent history, MCP, CLI, A2A, files, knowledge-base objects, or unrestricted tools, and may use only the read-only subset admitted by its parent capability view.

## Security Boundaries

- Context is owner-scoped, allowlisted, classified, size-bounded, content-addressed, and redacted before persistence.
- Restricted context is excluded. External prompt-like instructions are marked as untrusted evidence.
- Invocation manifests contain credential handles, never plaintext model keys.
- `agent-runtime` validates all envelope hashes, rejects unknown fields, rejects unavailable or write capabilities, consumes reserved envelope fields before model prompting, and fails closed on tampering.
- A candidate's self-reported success cannot satisfy an outcome. Verification requires independently collected evidence.

## Local Acceptance Evidence

- Fast path: 200 samples, p95 below 50 ms, zero specialist rows and zero specialist calls.
- Golden path: 20 consecutive compound research runs completed with linked proposal, run, manifest, attempt, decision turn, model invocation, typed candidate, and verification rows.
- Trace completeness in the deterministic fixture: 20/20, or 100%.
- Typed candidate validation: 20/20.
- Plaintext secret leakage in persisted W2 execution artifacts: zero in scanning fixtures.
- Full `go test ./...`: passed in `agent-runtime-client` and `agent-runtime`.
- Full `go vet ./...`: passed in both services.
- Targeted `go test -race`: passed for delegation repository/service and runtime specialist/dispatcher packages. The macOS linker emitted a non-fatal `LC_DYSYMTAB` warning.

The PostgreSQL concurrency gate passed during DSO-W1. The MySQL container-backed gate is configured in CI but was not executed locally because the container registry was unavailable; it must remain a release gate rather than being reported as a local pass.

## Exit Decision

DSO-W2 exits locally. DSO-W3 may begin. This does not freeze the draft wire schema or persistence layout; only the governed execution semantics are treated as the baseline.
