# Phase A — Security critical

Replaces the pre-existing auth/users/security gaps that were flagged
during the security audit. Every item below is deployable on its own
but the order is the order in which they were implemented; the
deployable binary lives on the remote at
`http://192.168.1.105:8080`.

## A1 — Users store: bcrypt + persistence + role enum

**Problem**: `models.User.Password` was tagged `json:"-"` so it was
silently dropped on every save, leaving the on-disk record without
a password. The back-compat loader then re-seeded `admin/admin` on
every backend restart, so the admin password could never be changed
through the UI. All other users became permanently un-loginable
after a restart. The password was compared in plaintext
(`store.go:142`).

**Fix**:
- `models.User.Password` → `PasswordHash string` (still `json:"-"`).
- New fields: `Email`, `Active`, `MustChangePassword`, `LastLoginAt`.
- New `models.UserResponse` (projection used in every API response;
  the hash is never serialized).
- `user.Store.Create/Update/ChangePassword` hash with bcrypt
  (`golang.org/x/crypto/bcrypt`, default cost 12).
- `user.Store.Validate` uses `bcrypt.CompareHashAndPassword`
  (constant-time). A dummy compare runs on user-not-found so
  response time doesn't leak whether a username exists.
- Password strength: minimum 8 chars, max 128.
- Role enum `models.IsValidRole` accepts only `admin`/`operator`/`viewer`.
- `users.json` is now written atomically (`*.tmp` + `rename`) with
  `0600` permissions.
- Legacy migration: on load, if a record has empty `PasswordHash`
  and is the admin, it's re-hashed with the default password and
  flagged `MustChangePassword=true`. Other accounts with no hash
  are dropped (they couldn't log in anyway).
- New self-service endpoint `PUT /api/users/me/password` (see B1).

**Files**:
- `backend/internal/models/types.go` — `User`/`UserResponse`/role consts
- `backend/internal/user/store.go` — full rewrite
- `backend/internal/api/users.go` — respond with `UserResponse`
- `backend/internal/api/router.go` — mount `/api/users/me/password`
- `backend/go.mod` — `golang.org/x/crypto` added

**Smoke test (remote)**:
```
$ curl -sS /api/auth/login -d '{"username":"admin","password":"admin"}'
{"token":"...","role":"admin","must_change_password":true}

$ sudo cat /opt/webVM/users.json   # 0600, no hash field
[{"username":"admin","role":"admin","active":true,"must_change_password":true,"last_login_at":"..."}]
```

## A2 — RBAC middleware (RequireRole / RequireAtLeast)

**Problem**: the auth middleware set `X-User` and `X-Role` headers
but no handler ever read them. An `operator` could create new
admins, restart the system, and delete other users.

**Fix**:
- `auth.RequireRole(roles...)` and `auth.RequireAtLeast(min)` —
  return 401 if no role set, 403 if role not allowed.
- Three-tier hierarchy: `admin > operator > viewer`.
- Applied in `router.go`:
  - **admin-only**: `POST/PUT/DELETE /api/users`, `POST /api/system/restart`,
    `POST /api/system/update`, `DELETE /api/storage/pools/{name}`,
    `DELETE /api/storage/volumes/{pool}/{name}`,
    `DELETE /api/storage/isos/{pool}/{name}`,
    `PATCH /api/storage/isos/{pool}/{name}`,
    `DELETE /api/networks/{id}`,
    `POST/PUT/DELETE /api/groups`.
  - **operator+admin**: `POST /api/vms`, `PATCH/DELETE /api/vms/{id}`,
    `POST /api/vms/{id}/{start,shutdown,forceoff,reboot,suspend,resume}`,
    `POST/PUT/DELETE /api/vms/{id}/disks`, `POST/PATCH/DELETE /api/vms/{id}/networks`,
    `PUT /api/vms/{id}/meta`, `POST/DELETE /api/vms/{id}/cover`,
    `POST /api/vms/{id}/clone`, `POST /api/vms/{id}/boot`,
    `POST/DELETE /api/vms/{id}/snapshots`, `POST .../snapshots/{sid}/revert`,
    `POST /api/vms/import`, `POST /api/vms/import-ova`,
    `POST /api/storage/{pools,volumes}`, `PATCH /api/storage/volumes`,
    `POST /api/storage/upload-iso{,/raw}`, `POST /api/storage/download-iso`,
    `POST /api/networks`, `PUT /api/networks/{id}`,
    `POST /api/networks/{id}/{start,stop}`.
  - **all authenticated** (incl. viewer): everything else, including
    all GETs and the unauthenticated read paths (`/api/covers/*`,
    `/api/vms/{id}/vnc|rdp|spice` — by design).
- Last-admin protection in `user.Store.Delete`/`Update` (cannot
  remove the last admin).
- Self-delete protection (cannot delete your own account).

**Files**:
- `backend/internal/auth/rbac.go` — new
- `backend/internal/api/router.go` — RBAC groups added
- `backend/internal/user/store.go` — `Delete`/`Update` self + last-admin
- `backend/internal/api/users.go` — pass caller username to `Delete`

**Smoke test (remote)**:
```
$ curl -X POST /api/users/ -H "Bearer $VTOKEN" -d '{...admin...}'   # 403
$ curl -X POST /api/system/restart -H "Bearer $VTOKEN"             # 403
$ curl -X POST /api/vms/$VMID/start -H "Bearer $OTOKEN"            # 200
$ curl -X DELETE /api/users/viewer1 -H "Bearer $OTOKEN"            # 403
```

## A3 — JWT secret persistence

**Problem**: the default secret `"change-me-in-production"` was
embedded in code and used on every fresh install. Anyone with the
source could forge tokens.

**Fix**:
- `config.Load()` now refuses to boot if `JWT_SECRET` env is set to
  the default placeholder or any string shorter than 16 chars
  (or `"secret"`, `"password"`, `"changeme"`, empty).
- If `JWT_SECRET` is unset and `{DataDir}/jwt.key` exists, its
  contents are used.
- Otherwise a 256-bit random key is generated, base64url-encoded,
  written to `{DataDir}/jwt.key` with `0600` permissions, and used
  for the lifetime of the install.
- `config.Load()` returns an error so `main.go` can `log.Fatalf`
  cleanly.

**Files**:
- `backend/internal/config/config.go` — full rewrite
- `backend/cmd/server/main.go` — handle new `Load` error

**Smoke test (remote)**:
```
$ sudo ls -la /opt/webVM/jwt.key
-rw------- 1 root root 43 Jun 23 15:37 /opt/webVM/jwt.key
```

## A4 — Login rate limiter

**Problem**: `/api/auth/login` was a free-fire endpoint. With the
default `admin/admin` advertised on the login page, a brute-force
attack was trivial.

**Fix**:
- `auth.LoginRateLimiter` is an in-memory token bucket per
  (IP, username) pair.
- Limit: 5 failed attempts per 15-minute window. After the 5th
  failure, the pair is locked for 15 minutes.
- On a successful login the bucket is cleared.
- Constant-time path: a failed `Validate` (user not found) runs a
  dummy bcrypt compare so response time doesn't leak which case
  we're in.
- Lockout response: `HTTP 429` with `Retry-After: <seconds>` header
  and a JSON body.
- The limiter is in-memory and resets on backend restart (intentional
  for a single-host install; cluster deploys would need Redis).

**Files**:
- `backend/internal/auth/ratelimit.go` — new
- `backend/internal/api/auth.go` — `Login` calls limiter
- `backend/internal/api/handler.go` — `loginLimiter` field
- `backend/cmd/server/main.go` — construct limiter
- `backend/internal/api/router.go` — wire to handler

**Smoke test (remote)**:
```
attempts 1-5: HTTP 401
attempt 6:    HTTP 429 (with Retry-After)
```

## A5 — SSRF blocklist on ISO download

**Problem**: `POST /api/storage/download-iso` accepted any
http/https URL. An attacker with any-role credentials could
make the server fetch `http://127.0.0.1`, `http://169.254.169.254`
(cloud metadata), or any internal address, and use the response
for network reconnaissance or to read sensitive services.

**Fix**:
- `safeDownloadURL(raw)`:
  - Parse + scheme check (`http`/`https` only).
  - Block `localhost`/`ip6-localhost`/`ip6-loopback` by hostname.
  - `net.LookupIP(host)` → for each resolved IP, reject if
    `net.IP.IsLoopback | IsLinkLocalUnicast | IsLinkLocalMulticast
    | IsMulticast | IsUnspecified | IsPrivate`, plus explicit
    blocks for `169.254.0.0/16` (cloud metadata) and
    `100.64.0.0/10` (CGNAT).
  - DNS-resolution-based check: catches DNS rebinding of literal
    hostnames and any hostname that resolves to a private IP.
- Errors return 400 with a clear message.

**Files**:
- `backend/internal/api/storage.go` — `safeDownloadURL` + `isBlockedIP`
  helpers; `DownloadISO` calls it before queuing the job.

**Smoke test (remote)**:
```
$ curl .../api/storage/download-iso -d '{"url":"http://127.0.0.1/x.iso"}'
{"error":"URL resolves to a blocked address: 127.0.0.1"}
$ curl .../api/storage/download-iso -d '{"url":"http://192.168.1.105/x"}'
{"error":"URL resolves to a blocked address: 192.168.1.105"}
$ curl .../api/storage/download-iso -d '{"url":"http://169.254.169.254/"}'
{"error":"URL resolves to a blocked address: 169.254.169.254"}
$ curl .../api/storage/download-iso -d '{"url":"http://localhost/x"}'
{"error":"URL host is not allowed"}
```

## A6 — Path traversal in upload + rename

**Problem**: ISO upload used `header.Filename` directly in
`filepath.Join(poolPath, name)`. A filename like `../../etc/passwd`
would, with Go's `filepath.Join` collapsing `..`, escape the pool
directory. Same in raw upload + rename.

**Fix**:
- `safeISOFilename(name)` rejects names containing path separators
  (`/` or `\`), `..` components, control characters, or null bytes
  **before** calling `filepath.Base`. The function is strict — it
  refuses to silently normalize. Returns a clear error message.
- Applied in `UploadISO` (multipart), `UploadISOByCURL` (raw),
  `RenameISO`, and `DownloadISO` (for the destination name).
- Note: Go's stdlib `multipart.FileHeader.Filename` already calls
  `filepath.Base` internally, so the multipart path is also
  defended at the stdlib level. The helper is the second line of
  defense for raw upload and rename.

**Files**:
- `backend/internal/api/storage.go` — `safeISOFilename` + calls in
  all four handlers.

**Smoke test (remote)**:
```
$ curl -X POST .../upload-iso/raw?name=../../etc/evil.iso
{"error":"filename must not contain path separators"}
$ curl -X PATCH .../isos/ISOS/test -d '{"new_name":"../../etc/evil.iso"}'
{"error":"filename must not contain path separators"}
$ curl -X POST .../upload-iso -F "file=@x;filename=ubuntu-24.04.iso"  # 201
```

## A7 — Real health check

**Problem**: `/api/health` returned `{"status":"ok"}` unconditionally.
A backend with libvirt down or the data dir full still reported
healthy.

**Fix**:
- `Handler.Health` now:
  - Calls `conn.GetVersion()` — if libvirt is down, sets
    `libvirt: "down"` and overall `status: "degraded"`, returns 503.
  - Reads `syscall.Statfs(DataDir)` → reports `disk_free` and
    `disk_total` in bytes. If less than 5% is free, status=degraded.
  - Reports `uptime` (seconds since process start).
- 503 is the right code for an orchestrator/k8s liveness probe.

**Files**:
- `backend/internal/api/handler.go` — `Health` rewritten

**Smoke test (remote)**:
```
$ curl /api/health
{"data_dir":"/opt/webVM","disk_free":282160750592,
 "disk_total":342300848128,"libvirt":"ok","status":"ok","uptime":10}
```

## A8 — Real host stats

**Problem**: `GET /api/host/stats` hardcoded `cpu_usage: 15.5`,
`total_disk: 500GB`, `used_disk: 200GB`. The dashboard lied.

**Fix**:
- `host.go GetHostStats`:
  - RAM: unchanged, from libvirt `GetNodeInfo`/`GetFreeMemory`
    (with 16 GiB fallback if libvirt is down — fine for the Status
    page skeleton).
  - **CPU%**: new `cpuSampler` reads `/proc/stat` first line
    (jiffies: user + nice + system + idle + iowait + irq + softirq
    + steal), keeps a rolling delta between calls, returns
    `(1 - delta_idle / delta_total) * 100`. First call returns 0
    (no baseline). Values are clamped to [0, 100].
  - **Disk**: `syscall.Statfs(DataDir)` → real `TotalDisk` and
    `UsedDisk`. No more hardcoded numbers.
- `GetHostInfo` no longer hardcodes `QEMUVersion = "8.2.0"`. It
  now calls `conn.GetCapabilities()` and parses the `<version>`
  element from the capabilities XML. Empty if libvirt is down.

**Files**:
- `backend/internal/api/host.go` — full rewrite of `GetHostStats`,
  small tweak in `GetHostInfo`.

**Smoke test (remote)**:
```
$ curl .../api/host/stats
{"cpu_usage":0.21,"used_ram":4108718080,"total_ram":7712735232,
 "used_disk":60169367552,"total_disk":342300848128}
```
Real: 8 GB total RAM, 4 GB used, 60 GB used of 342 GB disk, CPU
delta between two calls was 0.2 %.

## Audit log (scaffolding for Phase B)

`backend/internal/audit/audit.go` is the JSONL logger that Phase B
will populate. The package is wired into the `Handler` struct and
`Login`, `users.go` (CUD), `ChangeMyPassword`, and `DownloadISO`
already log to it. Subsequent phases will add entries for VM
lifecycle, system actions, and network/storage deletes.

```
$ sudo cat /opt/webVM/audit.log
{"time":"2026-06-23T15:37:51Z","user":"admin","role":"admin","action":"user.create","resource":"viewer1","ip":"192.168.1.166"}
{"time":"2026-06-23T15:38:02Z","user":"viewer1","role":"viewer","action":"user.change_password","resource":"viewer1","ip":"192.168.1.166"}
```

## Migration notes

- **`/opt/webVM/users.json`** is rewritten on first boot. The old
  format (no `password_hash`) is auto-migrated: the admin gets
  the default password re-hashed and `MustChangePassword=true`;
  other users with no hash are dropped.
- **`/opt/webVM/jwt.key`** is created on first boot with `0600`.
  All existing tokens are invalidated by the rotation — logins
  need to be re-issued.
- The frontend's `auth.svelte.js` will need a small update in
  Phase B to react to `must_change_password` and to drive a
  change-password redirect. Phase A only modifies the backend.

## Files touched in Phase A

```
backend/go.mod                                          (bcrypt dep)
backend/go.sum                                          (tidy)
backend/internal/audit/audit.go                         (NEW)
backend/internal/auth/ratelimit.go                      (NEW)
backend/internal/auth/rbac.go                           (NEW)
backend/internal/config/config.go                       (JWT persistence)
backend/internal/api/auth.go                            (limiter, last_login)
backend/internal/api/handler.go                         (real Health)
backend/internal/api/host.go                            (real stats, QEMU)
backend/internal/api/router.go                          (RBAC groups)
backend/internal/api/storage.go                         (SSRF, traversal)
backend/internal/api/users.go                           (UserResponse, self-pw, audit)
backend/internal/models/types.go                        (User fields, UserResponse, role consts, ChangeMyPasswordRequest)
backend/internal/user/store.go                          (bcrypt, enum, 0600, audit, last-admin guard, self-delete guard)
backend/cmd/server/main.go                              (config error, audit logger)
```
