-- Athena v0.6 evidence knowledge rollback.
-- WARNING: this permanently removes claims, evidence, snapshots, conflicts,
-- ontology versions, candidates, and migration audit records.
-- Export all eight tables before running this script.

BEGIN;

DROP TABLE IF EXISTS os_ontology_migration;
DROP TABLE IF EXISTS os_ontology_candidate;
DROP TABLE IF EXISTS os_ontology_version;
DROP TABLE IF EXISTS os_ontology_pack;
DROP TABLE IF EXISTS os_knowledge_snapshot;
DROP TABLE IF EXISTS os_contradiction;
DROP TABLE IF EXISTS os_knowledge_claim;
DROP TABLE IF EXISTS os_evidence;

COMMIT;
