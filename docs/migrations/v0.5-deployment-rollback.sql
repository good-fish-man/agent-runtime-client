-- Athena v0.5 deployment governance rollback.
-- WARNING: this permanently removes release, manifest, exposure, and audit data.
-- Export the nine tables before running this script.

BEGIN;

DROP TABLE IF EXISTS os_compensation;
DROP TABLE IF EXISTS os_rollback;
DROP TABLE IF EXISTS os_canary_metric;
DROP TABLE IF EXISTS os_canary_sample;
DROP TABLE IF EXISTS os_shadow_result;
DROP TABLE IF EXISTS os_exposure;
DROP TABLE IF EXISTS os_run_manifest;
DROP TABLE IF EXISTS os_promotion;
DROP TABLE IF EXISTS os_agent_build;

COMMIT;
