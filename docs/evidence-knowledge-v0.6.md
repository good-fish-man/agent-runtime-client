# Athena v0.6 Evidence Knowledge

Athena v0.6 replaces untraceable memory-as-truth with attributable, scoped, temporal knowledge. The system stores sources as Evidence and only exposes a statement as a Claim when validation can prove that accessible evidence supports it.

## Knowledge model

- `Evidence` records source type, URI, excerpt, server-derived trust profile, authority, freshness, scope, sensitivity, observation time, and immutable provenance.
- `Claim` records subject, predicate, value, confidence, evidence references, owner or organization scope, validity interval, semantic vector, relations, and contradiction state.
- `Contradiction` preserves competing claims instead of silently selecting a winner.
- `KnowledgeSnapshot` pins the exact claims, evidence, and ontology used by one retrieval result.
- `Belief` is a derived read model. It is never persisted as an unquestionable fact or used to mutate policy directly.

Callers cannot assign official status, authority, freshness, accessibility, provenance, or active Claim state. Research pages are captured as `PAGE_OBSERVATION` Evidence only, while explicit user confirmations use a separate endpoint and remain user-scoped. A single page observation cannot create a Claim. Public Claims cannot depend on user confirmation or narrower private evidence. Time-sensitive Claims receive a bounded validity horizon.

## Retrieval

Retrieval combines structured subject/predicate matching, keyword overlap, persisted semantic vectors, bounded relation traversal, temporal validity, source authority, and freshness. Every request has explicit result, token, duration, scope, organization, authority, source-type, sensitivity, relation-depth, and time budgets.

Results return their supporting Evidence, matched signals, explicit `FACT`, `CONFLICTED`, `EXPIRED`, `STALE_EVIDENCE`, or `RETRACTED` determination, budget use, and an immutable snapshot identifier. Owner and organization filters are applied in the repository, not only in HTTP handlers. Runtime requests inject a bounded evidence context into the model prompt and bind the exact snapshot checksum to the resulting RunManifest.

Unresolved contradictions stay visible. Reviewers must provide a note and may keep one Claim, mark all Claims uncertain, or retract all competing Claims through `POST /knowledge/contradictions/:id/resolve`.

## Controlled ontology

Ontology packs are versioned and use typed entity/relation definitions. New definitions enter as offline candidates with server-produced evaluation checks and Evidence references. A human reviewer must approve the candidate before a version can be created. Applying it requires a separately approved migration whose typed steps are executed by the server migrator and recorded in a tool execution receipt. Caller-provided booleans cannot impersonate evaluation, approval, or execution.

Pack creation with its first version, candidate approval with version creation, and migration with current-version pointer movement are transactional and revision-checked. Partial ontology states are rolled back automatically.

## Public API boundaries

- `POST /knowledge/evidence/confirmations` stores an explicit user confirmation with server-assigned trust metadata.
- `POST /knowledge/claims` creates a user Claim only from accessible, persisted Evidence owned by the same principal.
- `POST /knowledge/retrieve` performs budgeted hybrid retrieval and creates a checksum-bound snapshot.
- Ontology candidate review, migration review, and migration execution are separate operations.

## Database objects

The service initializes eight tables:

- `os_knowledge_claim`
- `os_evidence`
- `os_contradiction`
- `os_knowledge_snapshot`
- `os_ontology_pack`
- `os_ontology_version`
- `os_ontology_candidate`
- `os_ontology_migration`

## Verification

```bash
go test ./application/service/knowledge ./application/service/runtime ./infra/repository/repo/knowledge ./api/http/router/public ./infra/repository/migration
go test ./...
go vet ./...
```

Database rollback is documented in `docs/migrations/v0.6-knowledge-rollback.sql`. Export evidence and audit records before running it.
