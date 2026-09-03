# 01. Architecture, Startup, and Configuration

[Guide index](README.md) | [简体中文](../zh-CN/01-architecture-startup-and-configuration.md)

## Purpose

This part explains how the process starts, how dependencies are assembled, which features are optional, and how the service shuts down safely.

## Process Lifecycle

The entry point is [`main.go`](../../../main.go). Its responsibilities are deliberately small:

1. Parse the configuration-file flag.
2. Call `boot.Init` to construct the application.
3. Probe `agent-runtime` without making that probe a fatal startup dependency.
4. Start the Gin HTTP server.
5. Wait for an OS signal or an internal restart request.
6. stop accepting traffic and perform a bounded graceful shutdown.

The restart loop lives at the process boundary. Application services should request a restart through the injected channel rather than spawning a replacement process themselves.

## Composition Root

[`boot/init.go`](../../../boot/init.go) is the only place that should understand the complete object graph. It:

- loads logging and process configuration;
- opens the optional relational database;
- runs startup migrations and idempotent seeds;
- creates the gRPC Runtime client and domain gateway;
- constructs domain services, application services, and HTTP handlers;
- wires the device Hub into Runtime execution;
- wires terminal device tasks into Experience capture;
- connects approved learning/deployment artifacts to Runtime requests;
- starts supervisors and background scanners; and
- registers closers in reverse-lifetime order.

This file is long because composition is explicit. Moving business logic into `boot` would be a design regression; adding constructors and wiring is expected.

## Optional Database Mode

The database is enabled only when the configured host and database name are present. With no database, the process can still expose the limited Runtime proxy and file-backed configuration APIs. DB-backed handlers remain `nil`, and public route registration skips them.

This behavior is useful for diagnostics, but a full Athena control plane requires durable storage.

## Configuration

The configuration types live in [`config`](../../../config), and the sample file lives at [`manifest/config/config.yaml`](../../../manifest/config/config.yaml).

Important groups are:

| Group | Controls |
| --- | --- |
| `server` | listen address, mode, and management API prefix |
| `runtime` | gRPC target, Runtime HTTP target, and call timeouts |
| `db` | backend, DSN fields, pool limits, migration logging |
| `paths` | Client config, Runtime config, Skills config, uploads, and managed files |
| `control` | device WebSocket authentication and lease behavior |
| `memory` | long-term memory capture and retrieval |
| `plugins` | registry/trust configuration and package locations |
| orchestration settings | background goals, delegation, learning, and supervisors |

Secrets should come from environment variables or the launcher's protected state, not committed YAML.

## Startup Migrations and Seeds

[`infra/repository/migration`](../../../infra/repository/migration) owns schema initialization. `InitTables` performs:

- GORM migration for all active persistence objects;
- canonical table-name upgrades for older control-plane snapshots;
- explicit removal of obsolete secret-bearing columns;
- optional administrator bootstrap from environment-provided credentials;
- model-catalog upserts; and
- idempotent creation of public system Agents.

Migrations must never reset an existing user's password, role, or enabled state. Seed operations must be safe to repeat.

SQL rollback helpers for governed release stages live in [`migrations`](../../../migrations) and [`docs/migrations`](../../migrations).

## Background Components

Depending on configuration, `boot` starts several components that outlive a request:

- device control Hub and outbox processing;
- scheduled-task polling;
- durable delegation orchestration and recovery;
- Evolution scanners;
- durable Goal Supervisor; and
- other lifecycle-managed services.

Every component must accept a context or expose `Close`, tolerate restart recovery, and avoid depending on the frontend being open.

## Shutdown

The application returned by `boot.Init` owns its HTTP engine and closers. Shutdown must:

1. cancel root/background contexts;
2. stop supervisors from claiming new work;
3. release leases or mark recoverable work appropriately;
4. close Runtime and database connections; and
5. finish within the process shutdown deadline.

## Read These Files First

| File | Why |
| --- | --- |
| [`main.go`](../../../main.go) | Process boundary and restart lifecycle |
| [`boot/init.go`](../../../boot/init.go) | Complete dependency graph |
| [`config/config.go`](../../../config/config.go) | Configuration model and defaults |
| [`manifest/config/config.yaml`](../../../manifest/config/config.yaml) | Operator-facing example |
| [`infra/db/db.go`](../../../infra/db/db.go) | Database drivers, pool, and GORM logging |
| [`infra/repository/migration/migration.go`](../../../infra/repository/migration/migration.go) | Tables and startup data |

## Change Checklist

- Does a new dependency point inward through a domain/application interface?
- Is construction kept in `boot` rather than hidden in a handler?
- Does the feature work correctly when its optional dependency is disabled?
- Is startup idempotent on an existing database?
- Is the background service recoverable after an unclean stop?
- Are credentials absent from logs and committed configuration?
