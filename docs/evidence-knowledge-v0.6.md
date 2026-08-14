# Athena v0.6 Evidence Knowledge

Athena v0.6 replaces untraceable memory-as-truth with attributable, scoped, temporal knowledge. The system stores sources as Evidence and only exposes a statement as a Claim when validation can prove that accessible evidence supports it.

## Knowledge model

- `Evidence` records source type, URI, excerpt, authority, freshness, scope, sensitivity, observation time, and immutable provenance.
- `Claim` records subject, predicate, value, confidence, evidence references, owner scope, validity interval, and contradiction state.
- `Contradiction` preserves competing claims instead of silently selecting a winner.
- `KnowledgeSnapshot` pins the exact claims, evidence, and ontology used by one retrieval result.
- `Belief` is a derived read model. It is never persisted as an unquestionable fact or used to mutate policy directly.

Research pages are captured as `PAGE_OBSERVATION` Evidence only. A single page observation cannot create a Claim. Public Claims cannot depend on user confirmation or narrower private evidence. Time-sensitive Claims require an explicit expiration time.

## Retrieval

Retrieval combines structured subject/predicate matching, keyword overlap, a deterministic local vector signal, relation hints, temporal validity, source authority, and freshness. Every request has explicit result, token, duration, scope, and sensitivity budgets.

Results return their supporting Evidence, matched signals, conflict and expiry flags, budget use, and an immutable snapshot identifier. Owner filters are applied in the repository, not only in HTTP handlers.

## Controlled ontology

Ontology packs are versioned. New definitions enter as offline candidates with evaluation and Evidence references. A human reviewer must approve the candidate before a version can be created. Applying it requires an explicit migration tool execution.

Pack creation with its first version, candidate approval with version creation, and migration with current-version pointer movement are transactional. Partial ontology states are rolled back automatically.

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
