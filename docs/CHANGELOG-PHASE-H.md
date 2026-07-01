# Phase H — UI/UX Hardening + Storage semantics

This phase ships six user-facing improvements around storage/snapshot semantics and
VM-list UX, plus three follow-up fixes (env var name, pool cache refresh, dead import).

## Backend

### H3a — Classify storage volumes as snapshots (`internal/libvirt/storage.go`)

`StorageVolume` gained three fields, set by `ListStorageVolumes`:

- `is_snapshot` (bool) — true if the volume name follows the libvirt/qemu
  convention `<vmname>.<snapname>` (no `.qcow2`/`.img` extension).
- `snapshot_of_vm_id` (string) — UUID of the VM that owns the snapshot.
- `parent_volume` (string) — name of the VM's main disk in the same pool.

Classification uses a longest-prefix match against the known domain names so
`<vm>-1` doesn't shadow `<vm>`. The classifier list is built by the new
`snapshotClassifierVms()` helper, which lists all domains once per call and
sorts them by name length descending.

### H3c — Enrich `Snapshot` with `SizeAtSnapBytes` (`internal/libvirt/domain.go`)

The `Snapshot` model gained:

- `size_at_snap_bytes` (int64, omitempty) — the libvirt `Allocated` of the
  internal snapshot volume, captured at create time.

`ListSnapshots`, `CreateSnapshot`, and `DeleteSnapshot` look up the volume
`<vmname>.<snapname>` in the disk pool and copy `Allocated` into the response
or the audit `detail.allocated_bytes` field.

`qemu-img info --output=json <disk>` was the original plan, but the local
qemu does not include the `snapshots` array in its JSON output, so the
libvirt path is the only reliable source. The data is consistent with
manual inspection: `Antes instalacion` reports `9024057344` (9 GB) and
`gnome` reports `200704` (qcow2 metadata).

### H4 — Audit `allocated_bytes` on snapshot create/delete (`internal/api/vms.go`)

`vm.snapshot_create` and `vm.snapshot_delete` audit entries now include
`detail.allocated_bytes` (int64). On create, this is the libvirt Allocated
size of the newly-created volume. On delete, this is the size of the volume
that was freed.

### H-cleanup-1 — `pool.Refresh(0)` before `lookupSnapshotVolume`

`CreateSnapshot` and `DeleteSnapshot` call `c.RefreshPool(c.DiskPoolName())`
before the snapshot-volume lookup. The libvirt connection holds its own
in-memory pool cache (separate from libvirtd's on-disk state) and a fresh
snapshot volume isn't visible to that cache until a refresh.

Refresh is best-effort: a failure here is non-fatal and just leaves the
size at 0, which the next `ListSnapshots` call will populate.

### H-cleanup-2 — Fix `WEBVM_REPO_DIR` env var name (`internal/config/config.go:59`)

The Go config was reading the env var as `WEBVM_REPO_DIR` (lowercase
`W`/`B`/`M`), but the rest of the project — `scripts/webvm-backend.service`,
`scripts/setup.sh`, the systemd unit on the production host, and the error
message in `internal/api/system.go` — uses the consistent `WEBVM_REPO_DIR`.
The mismatch made the in-app updater always see the env var as empty and
fall back to the default `/opt/webvm` (also the wrong case), which caused
`repo not found at /opt/webvm` on every update attempt.

The default path was also fixed (`/opt/webvm` → `/opt/webVM`) so a fresh
install without the env var set still resolves correctly.

## Frontend

### H3b — Storage page snapshot grouping (`src/routes/Storage.svelte`)

The storage page now groups internal snapshots under their parent volume:

- Root row renders the VM's main disk as before.
- Each child volume with `is_snapshot: true` renders as a sub-row with a
  dashed left border, an "Internal snapshot" badge, the captured
  `size_at_snap_bytes`, and a "Manage in {vm name} →" link that navigates
  to the VM detail page and scrolls to the snapshot section.
- Resize and delete controls are hidden for snapshot rows.

The VM name for the link is resolved lazily via `api.getVM(id)` (cached in
component state).

### H1 — VM list grid-only with select mode (`src/routes/Storage.svelte` → `VmList.svelte`)

- Removed the table view (~280 lines deleted).
- Added a Select toggle in the page header that switches the grid into
  multi-select mode (`?select=1` query param, persistent across refreshes).
- Cards: click without select mode navigates; click with select mode
  toggles selection. Selected cards get a ring + accent border.
- An empty selection auto-exits select mode.

### H2 — Per-VM sparkline mini-charts (`src/routes/VmList.svelte`)

Cards for running VMs now show two 80×22 px sparklines (CPU and RAM) using
the existing `Chart.svelte` component. Data is fetched from the existing
metrics collector via `loadSparklines`.

### VmDetail deep-link scroll (`src/routes/VmDetail.svelte`)

- The snapshot section has `id="snapshots"` with `scroll-mt-6`.
- A `$effect` watches the router query for `tab=snapshots` and calls
  `scrollIntoView({ behavior: 'smooth' })` (deferred via `queueMicrotask`).
- The page now renders `size_at_snap_bytes` ("X GB at creation") in the
  snapshot list using a new `bytesToStr` helper.

### H-cleanup-3 — Remove unused `DataTable` import

The import of `$lib/components/DataTable.svelte` in `Storage.svelte` was
left over from the table-view era and triggered a build-time warning
(`'DataTable' is declared but its value is never read`). Removed.

## Verified

- Frontend bundle rebuilt, embedded, and deployed. `index-Ct_ixZlc.js`
  served byte-for-byte identical to the local file (`27ef09f2...`,
  365 138 bytes). CSS `index-Cz6rWI0A.css` (62 503 bytes) matches.
- Backend binary rebuilt (md5 `4914a51fc4da9d783f0ea6dd1a2d61b2`,
  10 751 840 bytes) and installed to `/usr/local/bin/webvm-server` on the
  production host. Service restarted, `systemctl is-active` returns
  `active`.
- Snapshot create test: `caca-fix-verify` → audit entry
  `{"allocated_bytes": 200704, "snap": "caca-fix-verify"}` (previously 0
  before the `pool.Refresh` fix). Snap deleted cleanly.
- Updater test: `POST /api/system/update` returns
  `{"log": "/var/log/webvm/update.log", "status": "updating"}` — no
  longer "repo not found at /opt/webvm".

## Known issues / follow-ups (not in this phase)

- **`caca` snapshot reports 0 size**: pre-existing internal snapshot from
  before Phase H. libvirt's `ListAllStorageVolumes` does not return a
  volume for it (the internal qcow2 snapshot exists, but the
  corresponding pool volume entry is absent or hidden). The `pool.Refresh`
  fix does not surface it. Not a regression, not blocking.
- **Importer "Network error" on large uploads**: the upload+define
  pipeline is fully synchronous; large OVA/WebVM backups can exceed
  practical client-side timeouts. The XHR `onerror` handler in
  `src/lib/stores/auth.svelte.js:318` hardcodes "Network error" without
  distinguishing `ontimeout` from a true connection drop, hiding the
  real cause. `http.Server` has no timeouts (defaults are 0, so
  client-side timeouts are the only cut-off), and chi's
  `middleware.Recoverer` is wired. Next phase: refactor `importArchive`
  to accept the upload synchronously and run
  `ImportDomain`/`ImportOVA` in a goroutine that publishes progress
  through the existing `events.Hub`, with the frontend subscribing via
  SSE and using `xhr.timeout` only as a safety net.
- **SSRF hardening in `internal/api/storage.go:96-123, 488-510`**: DNS
  rebinding + 302 redirect bypass still possible. Deferred to a
  dedicated security PR.
- **CachyOS local backend (192.168.1.166:8080)**: still running in
  parallel with the production instance. Per current decision, left
  running for now; the host has the same data dir shared via
  `/opt/webVM/source -> /home/alvin/webvm/webvm-main`, so any code
  change is visible to both. No automatic start (yet), but a future
  reboot would re-launch the service via systemd.

## Files touched

- `backend/internal/config/config.go` (1 line)
- `backend/internal/libvirt/domain.go` (2 sections, ~25 lines of comments)
- `backend/internal/libvirt/storage.go` (existing helpers used; no new logic)
- `backend/internal/api/vms.go` (audit `detail.allocated_bytes` from
  `CreateSnapshot`/`DeleteSnapshot`)
- `backend/internal/models/types.go` (`Snapshot.SizeAtSnapBytes`,
  `StorageVolume.IsSnapshot`/`SnapshotOfVMID`/`ParentVolume`)
- `frontend/src/routes/Storage.svelte` (grouped view, deep-link,
  dead import removed)
- `frontend/src/routes/VmList.svelte` (grid-only, select mode, mini-charts)
- `frontend/src/routes/VmDetail.svelte` (deep-link scroll, size_at_snap_bytes)
- `frontend/src/lib/stores/auth.svelte.js` (none in this commit; deferred
  to the async-import phase)
