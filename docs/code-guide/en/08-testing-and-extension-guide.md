# 08. Testing and Extension Guide

[Guide index](README.md) | [简体中文](../zh-CN/08-testing-and-extension-guide.md)

## Purpose

This chapter explains how to validate changes and how to add behavior without creating a second execution path or weakening the control plane's safety guarantees.

## Test Layers

### Unit tests

Keep deterministic policy, mapping, validation, redaction, state transitions, and aggregation tests close to their package. These should run without external services.

### Repository tests

Repository/store tests prove owner scoping, transactions, idempotency, optimistic revisions, leases, and migration behavior. Use the repository's test database helpers rather than assuming a developer database.

### Integration tests

Integration tests cross application/domain/infra seams, such as Experience-to-Learning, approved-artifact resolution, or a Runtime mapping round trip. They should still avoid claiming real desktop effects without a device.

### E2E and Golden Journeys

Real browser, desktop, installer, backup/restore, and signed-package journeys require external E2E execution. Results are persisted through Operations and are distinct from unit/preflight success.

## Standard Commands

```bash
go test ./...
go vet ./...
gofmt -w <changed-go-files>
git diff --check
```

Use focused tests while iterating, then run the full suite before declaring the change complete.

## Debugging by Trace

Every user request should have one trace ID propagated through:

```text
HTTP middleware
  -> context-aware application logs
  -> GORM logger
  -> gRPC metadata
  -> Runtime logs
  -> device task/action/observation records
```

Wrap errors at meaningful operation boundaries so the chain shows what failed. Log the complete terminal chain once rather than printing the same error at every return.

For long operations, log structured start/end events with duration, operation, model/tool/capability identity, trace ID, and terminal status. Never log credentials or unredacted prompts by default.

## Adding an Endpoint

1. Decide whether it belongs to Runtime, management, control, or internal API.
2. Define a DTO/use-case contract instead of binding persistence objects directly.
3. Implement authorization and owner scoping in the service/store.
4. Keep the handler thin and use the shared response/error types.
5. Register the route in the authoritative router.
6. Add handler validation/authorization tests and service behavior tests.
7. Update the bilingual route/subsystem documentation.

## Adding a Capability or Device Action

1. Define the capability/action in the shared Athena protocol, not a frontend-only JSON shape.
2. Specify risk, approval, timeout, idempotency, expected effects, and Observation schema.
3. Add device capability reporting and executor support.
4. Route through the Hub with lease/fencing validation.
5. Feed the real Observation back through Runtime continuation.
6. Verify the effect before reporting success.
7. Test cancellation, timeout, reconnect, duplicate delivery, and stale results.

Do not add website-specific business logic to the Client when a universal semantic browser capability can express the action.

## Adding a Durable State Machine

Separate immutable definitions/decisions from mutable execution attempts. Typical objects include proposal, policy decision, run, attempt, event, observation, verification, and terminal summary.

Required properties:

- stable IDs and idempotency keys;
- optimistic revision or transactional locking;
- lease/fencing for active ownership;
- append-only audit events;
- explicit terminal states;
- restart recovery; and
- rejection of late/stale results.

## Adding an Evolution Feature

Online execution may produce immutable Experience evidence. A separate offline/governed path may aggregate patterns and propose a declarative candidate. It must pass evaluation and review before an immutable build can expose it to Runtime.

Generated code must not be directly executed as a learned Skill.

## Architectural Guardrails

- No handler-to-GORM shortcuts for business behavior.
- No API keys, passwords, or full private prompts in browser responses/logs.
- No Action success without correlated Observation/effect evidence.
- No dynamic specialist bypass around the main execution chain.
- No learning candidate promoted by one online success.
- No background feature tied to frontend lifecycle.
- No package activation without exact signature/hash/permission governance.
- No compatibility workaround that silently corrupts current protocol semantics.

## Where Tests Live

| Concern | Typical location |
| --- | --- |
| HTTP binding/auth | `api/http/**/*_test.go` |
| Use-case behavior | `application/service/**/*_test.go` |
| DTO/entity mapping | `application/assembler`, `infra/runtime` tests |
| Domain rules | `domain/**/*_test.go` |
| Store/migration behavior | `infra/repository/**/*_test.go` |
| Runtime protocol mapping | `infra/runtime/*_test.go` |
| Release/E2E evidence | external runner + Operations evidence API |

## Documentation Definition of Done

When code ownership or behavior changes:

- update both locale chapters;
- update [the package reference](package-reference.md) if paths change;
- keep source links valid;
- describe the invariant and data flow, not only the new type name; and
- link detailed protocol documents rather than duplicating schemas that can drift.
