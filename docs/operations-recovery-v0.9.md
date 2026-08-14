# Operations and Recovery Runbook v0.9

## Control plane

Authenticated users can read `/operations/health`. Backup inventory, creation, verification, and restore require an administrator session. The Launcher-only pre-update endpoint accepts only `X-Athena-Internal-Token`; an unset token rejects every request.

Device ownership uses database-backed leases and monotonic fencing tokens. Only the current lease owner may keep a device online. A stale process cannot overwrite a newer owner's state, and disconnect marks a device offline only after ownership/lease validation.

## Encrypted backups

The Launcher configures absolute paths for `pg_dump`, `pg_restore`, the backup directory, and a 256-bit key file. PostgreSQL custom-format output is streamed directly through chunked AES-GCM encryption; plaintext dumps are not written to disk. Every artifact has a SHA-256 digest. The canonical manifest is authenticated with HMAC-SHA256 using the protected backup key, binding the backup ID, artifact inventory, sizes, digests, and key identity.

Backup lifecycle:

1. `POST /operations/backups` creates a recovery point.
2. `POST /operations/backups/{id}/verify` verifies the manifest HMAC, artifact digest, size, and every AES-GCM authentication tag.
3. `POST /operations/backups/{id}/restore` with `validate_only=true` performs all checks without changing PostgreSQL.
4. A real restore additionally requires `confirmation: "RESTORE {id}"` and the verified manifest SHA-256. `pg_restore` runs with `--exit-on-error --single-transaction`, preventing a partially applied database from being reported as restored.

The Launcher creates a backup before replacing an installed local release. If backup creation fails, old services remain running and the update remains retryable.

## State loss

Launcher installation identity is mirrored in mode-0600 regular files under `~/.athena/secrets`. If `state.json` is lost, the database password, Browser Vault key, backup key, internal token, and Device ID are recovered from those files. Symlinks, non-regular files, world/group-accessible permissions, oversized state, and read/replace races are rejected. If state and a recovery secret disagree, startup fails closed instead of silently rotating the identity.

Back up `~/.athena/secrets` separately from database artifacts. Losing both the state and backup encryption key makes encrypted backups intentionally unrecoverable.

## Privacy export and erasure

Authenticated users can export their own retained Memory and Experience data from the Experience workspace. Bulk erasure uses separate explicit confirmations for Memory and Experience. Memory private fields are cleared from soft-delete records; Experience payloads and derived evaluation data are physically removed while a minimal audit tombstone remains. Owner scope is enforced in the repository transaction, not supplied by the browser request body.

## Restore drill

1. Stop user traffic and create a fresh backup.
2. Verify the selected recovery point.
3. Run validate-only restore.
4. Confirm the backup ID and manifest digest out of band.
5. Stop active workers, execute restore, restart services, and check `/operations/health`.
6. Verify login, Agent inventory, conversations, goals, plugin registry, and one read-only task.
7. Preserve logs and manifests as drill evidence.
