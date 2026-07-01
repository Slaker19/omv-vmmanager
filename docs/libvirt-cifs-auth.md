# CIFS Authentication for libvirt netfs Pools

## Overview

`webvm` now supports authenticated CIFS mounts via libvirt secrets. This
enables backups to SMB3 shares that require domain credentials, Ceph
clusters that use Kerberos for netfs-style exports, and any other
scenario where the remote share refuses anonymous mounts.

The implementation follows a single source of truth: a helper in
`internal/libvirt/secret.go` is the only place that talks to libvirt
about secrets, and the API handlers go through it. The on-disk
mapping only ever stores the UUID libvirt assigned to the secret;
the actual password lives inside libvirt (as the secret's value)
and never touches our filesystem.

## Quick start

```bash
# 1. Create a CIFS pool with auth (operator role or higher).
curl -X POST http://server:8080/api/storage/pools \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "smb-backup",
    "type": "netfs",
    "path": "/mnt/smb-backup",
    "source_host": "files.example.com",
    "source_dir": "/backups",
    "source_format": "cifs",
    "source_username": "alice",
    "source_password": "hunter2",
    "purpose": "disk"
  }'

# 2. libvirt now has a secret whose UUID is in our mapping.
virsh secret-list
cat /opt/webVM/cifs-secrets.json

# 3. Rotate the password without recreating the pool.
curl -X PUT http://server:8080/api/storage/pools/smb-backup \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_username": "alice",
    "source_password": "newpass"
  }'
```

## How it works

1. Operator creates a CIFS pool with `source_username` and
   `source_password` via the API.
2. The handler validates that both fields are present together
   (HTTP 400 if only one is sent).
3. `CreateStoragePool` calls `defineCIFSSecret`, which:
   a. Defines the secret in libvirt with `SecretDefineXML`.
   b. Calls `SetValue` to put the password into libvirt's
      in-memory secret store.
   c. Persists `{poolName: secretUUID}` to
      `{cfg.DataDir}/cifs-secrets.json` with mode 0600.
4. The pool XML is generated with a `<auth type='cifs'
   username='...'>` block that references the secret UUID. libvirt
   uses that reference when mounting the share.
5. At startup, `verifyCIFSSecretsConsistency` checks every mapping
   against libvirt; missing secrets log a warning with the pool
   name and the recovery command.

## Auth field validation

| Body | Result |
|---|---|
| `source_username` + `source_password` | Defines a secret. |
| Only `source_username` | 400 `cifs auth requires both source_username and source_password` |
| Only `source_password` | 400 (same) |
| Neither | Anonymous CIFS mount (mount.cifs falls back to `-o guest`) |
| NFS pool (`source_format: nfs`) | Auth fields ignored, no `<auth>` block. |

## PUT /api/storage/pools/{name}

The update endpoint is intentionally narrow today. Supported
operations:

- **Rotate credentials**: send new `source_username` and
  `source_password`. The libvirt secret is replaced and the pool
  XML is regenerated to reference the new UUID.
- **`cifs-needs-reauth: true`**: same as rotating, but meant for
  the "libvirtd reinstalled and lost my secrets" recovery path.
  Currently the operator must re-supply the credentials (we cannot
  recover the password from anywhere we don't store it).

Unsupported operations return an error so the caller learns the
limit quickly:

- Changing `path`, `source_host`, `source_dir`, or
  `source_format` is rejected. libvirt cannot live-update these
  fields on a running pool. The intended workflow is to create a
  new pool and migrate volumes.
- `cifs-needs-reauth` on a `dir` or non-CIFS pool is rejected.

The pool must be **stopped** (not running) before reauth, to
avoid disrupting active mounts. libvirt refuses to redefine a
running pool.

## Recovery: libvirtd reinstall

When libvirtd is reinstalled, the libvirt secrets are lost (this
is expected — they're not our files). At startup,
`verifyCIFSSecretsConsistency` logs:

```
WARN cifs_secret_missing_in_libvirt pool=smb-backup uuid=...
  hint="PUT /api/storage/pools/smb-backup with cifs-needs-reauth=true to recreate"
```

To recover, the operator runs:

```bash
curl -X PUT http://server:8080/api/storage/pools/smb-backup \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "cifs-needs-reauth": true,
    "source_username": "alice",
    "source_password": "current-password"
  }'
```

The handler recreates the secret in libvirt and updates the
on-disk mapping. The pool XML is regenerated to reference the
new UUID.

## File layout

`{cfg.DataDir}/cifs-secrets.json` (chmod 0600, root-only):

```json
{
  "smb-backup": {
    "pool_name": "smb-backup",
    "secret_uuid": "abc123-def4-...",
    "created_at": 1700000000,
    "last_used_at": 0
  }
}
```

The file is included in `/opt/webVM` backups. When restoring a
backup to a host whose libvirtd is fresh, the operator must run
the `cifs-needs-reauth` recovery for each pool after the backend
comes up.

## Secret rotation

1. Change the password on the remote SMB/CIFS server.
2. Call `PUT /api/storage/pools/{name}` with the new credentials
   (and optionally `cifs-needs-reauth: true`).
3. The libvirt secret is replaced; the new UUID is persisted.

The pool must be stopped during rotation. The handler refuses to
redefine a running pool with a 500-class error so the operator
explicitly stops the pool first.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `cifs_secret_missing_in_libvirt` at startup | libvirtd reinstalled | Run `cifs-needs-reauth` for each affected pool. |
| 400 `cifs auth requires both ...` on create | Partial credentials in request body | Send both, or neither (anonymous). |
| 500 `reauth only supported for cifs pools` | `cifs-needs-reauth` on a `dir` or NFS pool | Not supported. Rotate on CIFS pools only. |
| 500 `pool is running; stop it before reauthing` | Tried to reauth a running pool | Stop the pool first (e.g. `virsh pool-stop`). |
| 500 `updating path/source is not supported` | Tried to change pool path via PUT | Create a new pool and migrate volumes. |
| 500 `libvirt set value` | Backend can't talk to libvirt | Check `systemctl status libvirtd` and that the backend runs as root. |

## Operator commands

```bash
# List all libvirt secrets (CIFS and otherwise).
virsh secret-list

# Show the XML of a specific secret.
virsh secret-dumpxml <uuid>

# Show our poolName -> secretUUID mapping.
cat /opt/webVM/cifs-secrets.json

# Check the audit log for pool create/update events.
grep storage.pool /opt/webVM/audit.log

# Watch the backend log for cifs_secrets_inconsistent warnings.
journalctl -u webvm-backend -f | grep cifs
```

## Security notes

- The on-disk file is mode 0600 and only ever contains UUIDs.
- The password is held in libvirt's memory and is the only place
  it lives. If libvirtd restarts, the secret must be re-supplied
  (this is by design — it limits the window of exposure).
- The `pass` variable inside `defineCIFSSecret` is the only
  place the cleartext password exists in our code. The
  `secret.go` file should not log, dump, or persist it. Code
  review: `grep -RIn "pass" backend/internal/libvirt/ | grep -v
  _test.go` should not show any path that touches the password
  outside `defineCIFSSecret`.
- The audit log records the action (`storage.pool.create`,
  `storage.pool.update`) but not the password.

## Internal: why no `<usage>` block?

libvirt's `SecretUsageType` enum has fixed values
(`volume`, `ceph`, `iscsi`, `tls`, `vtpm`) and **no native
`cifs` type**. For CIFS, the binding is done entirely via the
pool's `<auth type='cifs' username='...'>` reference. The
secret is a generic value; the auth block tells libvirt how to
use it.

If a future libvirt version adds a `cifs` usage type, the
`buildCIFSSecretXML` helper is the single place to update.

## Internal: rollback on failure

The `CreateStoragePool` flow is:

```
1. defineCIFSSecret          <- creates libvirt secret, persists mapping
2. buildPoolXML              <- generates <auth>-bearing XML
3. StoragePoolDefineXML      <- libvirt accepts the XML
4. pool.Build(0)             <- attempts to mount; may fail
5. pool.Create(0)            <- starts the pool; may fail
```

If any step after 1 fails, the handler calls `unsetCIFSSecret`
to roll back. This keeps the secret lifecycle tied to the pool
lifecycle — no orphan secrets after a failed create.

For the `PUT` path, the order is: lookup pool → validate
format/cifs → stop check → `defineCIFSSecret` → redefine
pool. The secret is created first so we have a valid UUID for
the pool XML; if the redefine fails, the old secret UUID is
left in libvirt until the next cleanup (the new secret
replaces it on the next successful reauth).
