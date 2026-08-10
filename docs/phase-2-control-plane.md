# Phase 2: Device WebSocket Control Plane

Status: complete

## Implemented

- Launcher opens and maintains the outbound `athena.agent.v3` WebSocket.
- `HELLO` registers device identity, platform, architecture, and capabilities.
- Runtime Client records device online/offline state and applies a 45-second heartbeat deadline.
- An unclaimed online device binds to the first authenticated user who explicitly binds it or routes an Action to it. Cross-user listing and routing are rejected.
- Actions route only to an online device that advertised the requested capability.
- Tasks, Actions, and Observations are persisted in `agent_control_task`, `agent_control_action`, and `agent_control_observation` when the database is enabled.
- `idempotency_key` has a unique database index; completed Observations are reused after retries and service restarts.
- Action deadlines and explicit Stop requests send `CANCEL` to Launcher. Cancelled and expired Observations are persisted.
- Closing an SSE/frontend connection detaches presentation from execution. The bounded background task and Agent Observation loop continue.
- Closing the Wails window hides it instead of terminating Launcher; opening Athena again focuses the existing instance.

## Recovery APIs

- `GET /v1/control/devices`
- `POST /v1/control/devices/:device_id/bind`
- `GET /v1/control/tasks?conversation_id=<id>`
- `GET /v1/control/tasks/:task_id`
- `POST /v1/control/actions`

Streaming calls return `X-Athena-Task-ID`, which is exposed through CORS and can be used to recover task state after the UI reconnects.

## Acceptance

Automated coverage verifies WebSocket Action/Observation correlation, user-exclusive binding, conversation cancellation, timeout/cancellation propagation, duplicate handling, frontend disconnect detachment, heartbeat-compatible reconnection, race safety, and production desktop compilation.

The database must be enabled for restart-safe history and deduplication. Without a database, the same protocol works with process-local state only.
