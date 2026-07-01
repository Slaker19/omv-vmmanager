# Phase B — User administration + audit log

Phase A fixed the security bugs but did not give users a way to
manage their own session, nor did it produce a security audit trail.
Phase B fills those gaps.

## B1 — Token lifecycle: logout, refresh, me, blacklist

The JWT middleware set the X-User/X-Role headers but no token
revocation existed — once issued, a token was valid for 24 h no
matter what. Phase B introduces:

- **`POST /api/auth/logout`** — revokes the current bearer token
  by adding it to an in-memory denylist. The denylist entries
  expire at the original token's `exp` (a periodic GC reclaims
  memory). Idempotent.
- **`POST /api/auth/refresh`** — accepts a still-valid token,
  looks up the *current* role of the user (so a freshly-demoted
  user gets a token with the new role), issues a new token with
  a new `jti`, and revokes the old one atomically. This is the
  recommended pattern for long sessions.
- **`GET /api/auth/me`** — returns the current user as
  `UserResponse`. Used by the frontend on app load to re-validate
  a cached token and re-sync role.
- **JTI-based blacklist** — every token now carries a `jti`
  (16 random bytes hex-encoded). The middleware checks
  `Blacklist.IsRevoked(jti, token)` on every request.

Files:
- `backend/internal/auth/jwt.go` — Claims now have JTI; new
  `Manager.Blacklist()`, `Revoke()`, `GenerateTokenWithJTI()`.
- `backend/internal/auth/blacklist.go` — `TokenBlacklist` with
  GC goroutine.
- `backend/internal/api/auth.go` — `Logout`, `Refresh`, `Me`,
  `extractBearer` helper.
- `backend/internal/api/router.go` — routes mounted.

## B2 — Audit log wiring

`/opt/webVM/audit.log` is now populated by every mutating handler:

| Event | Source |
|---|---|
| `auth.login`, `auth.login_failed` | `Login` |
| `auth.logout` | `Logout` |
| `user.create`, `user.update`, `user.delete`, `user.change_password` | `users.go`, `ChangeMyPassword` |
| `vm.create`, `vm.update`, `vm.delete` | `vms.go` |
| `vm.start`, `vm.shutdown`, `vm.forceoff`, `vm.reboot`, `vm.suspend`, `vm.resume` | `vms.go` |
| `vm.snapshot_create`, `vm.snapshot_delete`, `vm.snapshot_revert` | `vms.go` |
| `vm.disk_attach`, `vm.disk_detach` | `vms.go` |
| `vm.net_attach`, `vm.net_detach` | `vms.go` |
| `vm.clone`, `vm.boot_set`, `vm.meta_update` | `vms.go`, `metadata.go` |
| `iso.download` | `storage.go` |
| `network.create`, `network.update`, `network.delete`, `network.start`, `network.stop` | `networks.go` |
| `system.restart`, `system.update` | `system.go` |

The `auditFor(r, action, resource, detail)` helper in
`handler.go` constructs an entry from the X-User/X-Role headers
and the request IP. `audit.Entry` is `{time, user, role, action,
resource, ip, detail, error}` (JSON).

Files: `backend/internal/audit/audit.go` (logger, GC, rotation at
10 MB to `.1`).

## B3 — Frontend: account page, role gates, strength meter

### `auth.svelte.js`
- `setToken(t, u, r, mustChange)` — persists `must_change` flag.
- `setMustChange(v)` — used by the Account page to clear the
  flag without a full re-login.
- `isAdmin()` / `canMutate()` — UI role gates.
- `ApiError` class with `status`, `code`, `retryAfter` (for 429s).
- `passwordStrength(pw)` — returns `{score, label, color}` based
  on length, mixed case, digits, symbols.
- New API methods: `me()`, `logoutApi()`, `refresh()`,
  `changeMyPassword()`.

### `Login.svelte`
- Hides the "default credentials" hint after the first successful
  login (state held in component, reset on reload).
- Redirects to `/account` when the server returns
  `must_change_password=true`.
- Shows a friendlier "too many failed attempts" message with the
  `Retry-After` value for 429s.
- Adds `role="alert"` and `aria-live="assertive"` to the error
  banner.

### `Account.svelte` (NEW)
- Profile section: username, role badge, email, account created,
  last login (all from `me()`).
- Change-password form with three inputs (old/new/confirm), show
  / hide password toggle, strength meter on the new-password
  field, and a real-time "passwords do not match" check.
- Logout button that hits `/api/auth/logout` (server-side revoke)
  and then clears local state.

### `App.svelte`
- Forces `/account` while `mustChangePassword` is set.
- Redirects non-admin away from `/users`.
- On token state change, calls `/auth/me` to re-validate the
  cached username/role (so a freshly-demoted user sees the
  correct permissions).

### `Sidebar.svelte`
- Each nav item declares which roles see it. Non-admins no longer
  see the "Users" link.
- The footer has a clickable Account link showing the current
  user's name + role, with `aria-current="page"` when active.

### `Users.svelte`
- Updated to the new `UserResponse` shape (no password field).
- Prevents self-delete (also enforced server-side).
- Hides the page for non-admins.
- "Add User" form includes an optional email and a strength
  meter; creation is disabled until password is at least 8 chars.
- Edit form now also edits email and the `active` flag.
- Fixed pre-existing bug: toast was reading the username from a
  state that had just been reset (now captured into a local
  before the await).
- `role="alert"` on the error banner.

## Verification (remote at 192.168.1.105)

```text
GET  /api/auth/me           → 200 + UserResponse (no hash)
POST /api/auth/refresh      → 200 + new token (jti rotated, old blacklisted)
POST /api/auth/logout       → 200; subsequent calls with old token → 401
PUT  /api/users/me/password → 200; old password rejected, new works
RBAC matrix (admin/operator/viewer):
  Viewer GET  /api/users            → 200
  Viewer POST /api/users            → 403
  Viewer GET  /api/vms              → 200
  Viewer POST /api/vms              → 403
  Viewer POST /api/vms/X/start      → 403
  Viewer POST /api/system/restart   → 403
  Viewer DELETE /api/storage/pools  → 403
  Operator POST /api/users          → 403
  Operator DELETE /api/users/X      → 403
  Operator DELETE /api/storage/pools → 403
  Operator POST /api/system/restart → 403
  Admin POST /api/users             → 400 (validation) / 201 (success)
  Admin POST /api/system/restart    → 202
  Admin DELETE /api/users/admin     → 409 ("cannot delete the admin user")
  Admin PUT    /api/users/admin (role: operator) → 409 ("at least one active admin must remain")
```

## Files touched in Phase B

```
backend/internal/api/auth.go                          (Logout, Refresh, Me, extractBearer)
backend/internal/api/handler.go                       (auditFor helper)
backend/internal/api/metadata.go                      (audit on meta_update)
backend/internal/api/networks.go                      (audit on create/update/delete/start/stop)
backend/internal/api/router.go                        (auth routes)
backend/internal/api/system.go                        (audit on restart/update)
backend/internal/api/vms.go                           (audit on all VM lifecycle)
backend/internal/auth/blacklist.go                    (NEW: TokenBlacklist)
backend/internal/auth/jwt.go                          (JTI, Revoke, Blacklist())

frontend/src/lib/router.svelte.js                     (account route)
frontend/src/lib/stores/auth.svelte.js                (ApiError, me, refresh, logout, changeMyPassword, passwordStrength)
frontend/src/lib/components/Sidebar.svelte            (role-gated nav, Account link)
frontend/src/App.svelte                               (must-change redirect, role guard, /me refresh)
frontend/src/routes/Account.svelte                    (NEW)
frontend/src/routes/Login.svelte                      (must-change redirect, default-hint hidden, 429 handling)
frontend/src/routes/Users.svelte                      (new shape, self-delete protection, strength meter, role hide)
```
