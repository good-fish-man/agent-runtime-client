-- Destructive rollback for Athena v0.7 durable orchestration.
-- Export goal history before running this script. It intentionally leaves all
-- user, conversation, memory, model, knowledge, and deployment tables intact.
BEGIN;

DROP TABLE IF EXISTS os_schedule_trigger;
DROP TABLE IF EXISTS os_specialist_run;
DROP TABLE IF EXISTS os_goal_checkpoint;
DROP TABLE IF EXISTS os_goal_task;
DROP TABLE IF EXISTS os_goal;

COMMIT;
