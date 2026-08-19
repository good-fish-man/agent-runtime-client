# DSO-W5 Governed Ad-Hoc Specialists

Status: accepted locally on 2026-08-20

## Scope

DSO-W5 handles a missing exact specialist role with a temporary, declarative overlay over a reviewed base profile. It does not generate executable code, create a provider, modify a system prompt, expose a secret, or write directly to the production profile registry.

Resolution order is now:

```text
exact reviewed profile
  -> reviewed general-read profile + constrained overlay (explicitly enabled)
  -> reviewed research fallback (production default)
```

The dynamic path requires the internal run flag `athena.dso.adhoc_specialists=true`. Without it, an unknown role has zero ad-hoc production exposure and uses the reviewed research fallback.

## Immutable Governance Objects

- `SpecialistProfile` is reviewed, content-addressed, production-approved, and defines capability, context, prompt, output, and risk ceilings.
- `AdHocSpecialistOverlay` may only describe the current role and outcome, narrow capabilities and context, and set bounded output-schema parameters.
- `OverlayAdmissionDecision` independently binds the overlay hash, base-profile hash, parent capability/context snapshot, policy version, and expiry.
- `SpecialistProfileCandidate` requires at least three independent successful runs and is always `REVIEW_REQUIRED` with `activation_allowed=false`.
- `InvocationManifest.specialist_overlay_ref` makes the exact temporary definition replayable and auditable.

Strict JSON decoding rejects every unknown field. The typed overlay intentionally has no prompt, provider, secret, command, script, code, or executor field.

## Persistence And Isolation

The control plane persists:

- `os_dso_adhoc_overlay`
- `os_dso_overlay_admission`
- `os_dso_adhoc_run_outcome`
- `os_dso_profile_candidate`

Overlay and admission records are written atomically. Every lookup and outcome write is owner-scoped. The same run is idempotent; a conflicting replay is rejected. Temporary overlays expire within one hour and cannot be resolved by another owner.

## Safety Gates

- Requested capabilities must be an exact subset of both the reviewed base profile and parent task.
- Terminal, shell, command execution, Python execution, payment, purchase, file deletion, and filesystem write are denied even if accidentally present in a parent set.
- Context classes, references, and byte limits may only narrow both base and parent ceilings.
- Prompt-override phrases, script markers, shell pipelines, executable code patterns, and secret-like material are denied and audited.
- Overlay output schema replacement is denied.
- Temporary specialists cannot delegate to another specialist.
- Repeated success creates only a review candidate; it never activates or mutates a production profile.

## Local Acceptance Evidence

- A safe read-only overlay was admitted and completed through the same Proposal -> Decision -> Run -> Attempt -> Verification chain.
- Terminal, payment, file-delete, prompt-injection, and secret-bearing overlay fixtures were denied with durable reasons.
- An unknown role without the feature flag created zero overlay records and used the reviewed research profile.
- Cross-owner overlay and admission lookup returned no data.
- The first and second successes created no profile candidate; the third created one non-activating `REVIEW_REQUIRED` candidate.
- The invocation manifest bound the exact overlay reference and the frontend stream identified the temporary role, base profile, and admission decision.
- Exact persistence replay succeeded; a conflicting outcome replay returned an idempotency conflict.
- `go test ./...` passed in `agent-runtime-client`.
- `go test ./draft/dso/v0alpha` passed in `athena-protocol`.
- `npm run lint` and `npm run build` passed in `frontend/agent-ui`.

## Exit Decision

DSO-W5 exits locally. The dynamic overlay path remains opt-in until W6 replay, chaos, and production-hardening evidence is accepted. DSO-W6 may begin.

This local exit does not claim a statistically significant production-quality gain over the main-agent fallback. In accordance with the roadmap gate, automatic routing therefore remains disabled.
