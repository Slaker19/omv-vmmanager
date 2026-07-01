# WebVM — Phases 14–21 Changelog

This document describes every change made in Phases 14 through 21 of the
WebVM UI/UX redesign. It is intended as a reference for future agents
working on this codebase.

The app is deployed on the remote Ubuntu 26.04 host at
`http://192.168.1.105:8080` via the `webvm-backend` systemd service.

Development happens on this CachyOS host (`192.168.1.166`); the binary
is cross-built here and `scp`'d to the remote, then installed via
`sudo install -m 0755 /tmp/webvm-backend /usr/local/bin/webvm-server`
and restarted. SSH auth uses the local askpass shim at
`/tmp/alv-askpass/ssh_askpass.sh` (password `1908`), which is also
pushed to `/tmp/alv-askpass/ssh_askpass.sh` on the remote for `sudo -A`.

## High-level summary

| # | Feature | Backend | Frontend |
|---|---------|---------|----------|
| 14 | Network bridge mode | `/api/host/interfaces` lists real host devices | Conditional form on `Networks.svelte` |
| 15 | Import dedup | `resolveUniqueDomainName()` auto-renames on collision | Toast warning when the name was renamed |
| 16 | VM identity & metadata | `<webvm:meta>` namespace, alias/cover/groups, MAC/VLAN/network edit, MAC only when off, fleet-wide collision check | Identity dialog (5 tabs) in `VmDetail.svelte` |
| 17 | VM list grid + table toggle | `VM.Alias`/`VM.Cover`/`VM.Groups` populated in `domainToVM` | Grid tiles with cover/fallback, localStorage-persisted view mode |
| 18 | Groups | `groups.json` on disk + `/api/groups` CRUD with rename/delete propagation to all VMs | Manage Groups dialog + filter chips on VM list |
| 19 | Snapshot tree | `Snapshot.ParentName` + `Snapshot.CreationTime` (epoch) | Recursive tree render, `YYYY-MM-DD HH:MM` format |
| 20 | Notes autosave on blur | (reuses Phase 16 `PUT /api/vms/{id}/meta`) | True blur autosave with status indicator |
| 21 | Metrics | `MetricsCollector` goroutine, in-memory ring buffer (720 slots = 1h @ 5s), REST + SSE | SVG `Chart` component, 6 metrics in VmDetail, CPU/RAM sparklines in table view |

## Conventions

- **Storage**: VM-level metadata (alias, cover path, groups, notes) lives
  inside the libvirt domain XML under
  `<metadata><webvm:meta xmlns="https://webvm.local/ns">…</webvm:meta></metadata>`.
  No separate database.
- **Cover images**: stored on disk at `${DATA_DIR}/covers/{uuid}.{ext}`,
  served as static via `/api/covers/{path}` (no auth — UUID-in-URL is
  the access control).
- **Group definitions**: stored at `${DATA_DIR}/groups.json`. Membership
  is in each VM's `<groups>` element.
- **Metrics**: in-memory only, lost on restart (per the brief). 1h @ 5s
  sampling via `libvirt.MetricsCollector`.

## File-by-file changes

### Backend

**`backend/internal/models/types.go`**
- Added `HostInterface{Name, Type, State, MAC}` (Phase 14)
- Added `VMMeta`, `VMMetaUpdate`, `CoverUploadResponse`, `VlanSupport`,
  `UpdateNetIfaceRequest` (Phase 16)
- Added `Alias`, `Cover`, `Groups` to `VM` (Phase 16)
- Added `ParentName`, `CreationTime` to `Snapshot` (Phase 19)
- Added `Group`, `GroupList`, `GroupUpsertRequest` (Phase 18)
- Added `MetricsSample`, `MetricsSeries`, `VMMetrics` (Phase 21)

**`backend/internal/config/config.go`**
- Added `CoversDir()` → `${DATA_DIR}/covers` (Phase 16)
- Added `GroupsFile()` → `${DATA_DIR}/groups.json` (Phase 18)

**`backend/internal/libvirt/connect.go`**
- Exported `EnsureConnected()` (was private `ensureConnected`) so the
  api package can touch the connection under the connector's own lock.

**`backend/internal/libvirt/domain.go`**
- `ImportDomain` signature changed: now returns `(uuid, resolvedName, err)`.
  Auto-resolves name collisions via `resolveUniqueDomainName`.
  Rewrites the `<name>` in the archive XML to the resolved value
  regardless of whether the caller supplied a new name. (Phase 15)
- Added `resolveUniqueDomainName(name)` helper: `LookupDomainByName`,
  on hit tries `<name>-1`, `-2`, … up to 1000. (Phase 15)
- Added `UpdateNetworkIface(id, oldMAC, req)`: updates MAC / network /
  VLAN tag of an existing interface. Enforces `state == shutoff`,
  validates MAC format, fleet-wide collision check via
  `assertMACUnique`, patches `<mac>`, `<source network=…>`, and
  `<vlan><tag id=N/></vlan>` blocks in the XML, then calls
  `dom.UpdateDeviceFlags(CONFIG)`. (Phase 16)
- Added `assertMACUnique(mac, selfMAC)` + `extractAllMACs` helper. (Phase 16)
- Added `CheckVLANSupport(networkName)`: returns `VlanSupport` based on
  libvirt version (≥ 11.0.0 required) + network XML containing
  `macTableManager='libvirt'` or an Open vSwitch. (Phase 16)
- `ListSnapshots`: now populates `ParentName` (via
  `DomainSnapshot.GetParent(0)` with a `<parent><name>` XML fallback)
  and `CreationTime` (epoch seconds) in addition to the legacy
  `CreatedAt` RFC3339 string. (Phase 19)
- Added `extractSnapshotEpoch` and `extractSnapshotParent` helpers. (Phase 19)
- `domainToVM`: now also reads `<webvm:meta>` and populates
  `vm.Alias`, `vm.Cover`, `vm.Groups`. Failures are non-fatal. (Phase 16)

**`backend/internal/libvirt/ova.go`**
- `ImportOVA` signature changed: now returns `(uuid, resolvedName, err)`.
  Same dedup logic as `ImportDomain` — extracts the effective name from
  `newName` or the OVF's `<Name>` tag, resolves, rewrites. (Phase 15)

**`backend/internal/libvirt/metadata.go` (new, Phase 16)**
- `GetVMMeta(uuid)` → reads `dom.GetMetadata(DOMAIN_METADATA_ELEMENT,
  "https://webvm.local/ns", DOMAIN_AFFECT_CONFIG)`, decodes into
  `models.VMMeta`. Returns empty struct (not error) if no metadata.
- `SetVMMeta(uuid, meta)` → marshals to XML with the `webvm` prefix
  and the `https://webvm.local/ns` namespace URI, writes via
  `dom.SetMetadata(DOMAIN_METADATA_ELEMENT, …, "webvm", namespace,
  DOMAIN_AFFECT_CONFIG)`.
- `UpdateVMMeta(uuid, upd)` → read-modify-write, applies non-nil fields
  of the update (nil groups = clear), stamps `UpdatedAt = nowUnix()`.

**`backend/internal/libvirt/metrics.go` (new, Phase 21)**
- `ringBuffer` (fixed-size circular buffer of `MetricsSample`).
- `vmMetricsState` (per-VM ring buffers + last-cumulative-counter cache
  for delta-based metrics: disk R/W bytes, net Rx/Tx bytes, CPU
  nanoseconds).
- `MetricsCollector`: 5s sampling, in-memory ring buffers (720 slots =
  1h). `Run(ctx)` is the goroutine loop; `Get(uuid)` is the REST
  accessor.
- `sampleOnce`: lists running domains, calls
  `GetCPUStats(-1, 0, 0)`, `GetInfo()` for RAM, `BlockStats` per disk
  target, `InterfaceStats` per MAC. Computes deltas, pushes samples,
  broadcasts `vm.metrics` SSE event with the per-VM `VMMetrics` payload.
- Helpers: `extractDiskTargets` (regex over `<disk>…<target dev=…>`),
  `extractIfaceMACs` (regex over `<mac address=…>`).

**`backend/internal/events/hub.go`**
- Added `Data any` to `Event` so the metrics collector can attach the
  per-VM `VMMetrics` payload. (Phase 21)

**`backend/internal/api/handler.go`**
- Added `gs *groupsStore` and `metrics *libvirt.MetricsCollector` fields
  to the `Handler` struct.

**`backend/internal/api/router.go`**
- `NewRouter` signature now also takes `*libvirt.MetricsCollector` and
  initializes `gs: newGroupsStore(cfg.GroupsFile())` + `metrics: metrics`.
- New routes:
  - `GET /api/host/interfaces` (Phase 14)
  - `GET/POST /api/groups`, `PUT/DELETE /api/groups/{name}` (Phase 18)
  - `GET /api/vms/{id}/meta`, `PUT /api/vms/{id}/meta` (Phase 16)
  - `POST /api/vms/{id}/cover`, `DELETE /api/vms/{id}/cover` (Phase 16)
  - `PATCH /api/vms/{id}/networks/{mac}` (Phase 16)
  - `GET /api/vms/{id}/vlan-support?network=…` (Phase 16)
  - `GET /api/vms/{id}/metrics` (Phase 21)
  - `GET /api/covers/{path}` (Phase 16, public — see auth whitelist)
- Import response now includes `name` (resolved) and `requested_name`
  so the frontend can toast a "renamed" warning. (Phase 15)

**`backend/internal/api/host.go`**
- Unchanged from Phases 0–13.

**`backend/internal/api/host_interfaces.go` (new, Phase 14)**
- `ListHostInterfaces`: reads `/sys/class/net/*`, filters out `lo`,
  `vnet*`, `virbr*`, anything that's a bridge (has `bridge` subdir),
  anything without a backing device (no `device` subdir). Returns
  `[{name, type, state, mac}]`. Type inferred from `/sys/class/net/<n>/type`
  (1 = ethernet, 6 = wifi, else "other").

**`backend/internal/api/metadata.go` (new, Phase 16)**
- `GetVMMeta`, `UpdateVMMeta` (thin pass-through to the libvirt helpers).
- `UploadCover`: multipart parse, 8 MB cap, magic-byte sniff (PNG/JPEG/WebP),
  writes to `${CoversDir}/{uuid}.{ext}`, updates the `<cover>` field of
  the VM's metadata. Returns `CoverUploadResponse{URL, Path, Format}`.
- `DeleteCover`: removes the file + clears the metadata field.
- `ServeCover`: static file from `${CoversDir}`. **No auth** (UUID-in-URL).
- `UpdateNetIface`: wraps `lv.UpdateNetworkIface`. Normalizes the MAC
  before passing (accepts colon/dash/dot separators, returns canonical
  `xx:xx:xx:xx:xx:xx` form).
- `CheckVLANSupport`: wraps `lv.CheckVLANSupport`.
- `GetVMMetrics`: wraps `metrics.Get(uuid)`. Returns the current ring
  buffer contents as a `VMMetrics` JSON. (Phase 21)
- `normalizeMAC`: parses arbitrary MAC format into canonical form.
  Rejects anything that doesn't decode to 6 bytes.

**`backend/internal/api/groups.go` (new, Phase 18)**
- `groupsStore`: thread-safe in-memory map + on-disk JSON at
  `${GroupsFile}`. Atomic save via `${path}.tmp` + rename. Definitions
  sorted alphabetically on save.
- `ListGroups`: returns `{groups: [{name, color, member_count}]}` with
  member counts computed by scanning all live VMs' metadata.
- `CreateGroup`: rejects duplicate names, validates color as
  `#rrggbb` hex.
- `UpdateGroup`: renames and/or recolors. On rename, scans every VM
  and replaces the old name in each VM's `<groups>` array. Best-effort:
  per-VM failures are returned in the response.
- `DeleteGroup`: removes the definition, then scrubs the tag from every
  VM's metadata. Best-effort: VM write failures are silently skipped
  (the tag is gone from the UI, leftover references in any unreachable
  VM would only show up in `virsh dumpxml`).
- `isValidHexColor`: small `#rrggbb` validator.

**`backend/internal/api/vms.go`**
- `importArchive` now captures the resolved name from the import
  function and includes it in the JSON response as `name` and
  `requested_name`. (Phase 15)

**`backend/internal/auth/jwt.go`**
- Added `/api/covers/` to the public-paths whitelist in `Middleware`,
  so cover images can be loaded by `<img src>` without a JWT.

**`backend/cmd/server/main.go`**
- Constructs `metrics := libvirt.NewMetricsCollector(lv, hub)` and
  starts `go metrics.Run(eventCtx)` alongside the event loop.
- Passes `metrics` to `api.NewRouter(…)`.

### Frontend

**`frontend/src/lib/stores/auth.svelte.js`**
- New API methods: `listHostInterfaces`, `updateNetIface`,
  `checkVLANSupport`, `getVMMeta`, `updateVMMeta`, `uploadCover`,
  `deleteCover`, `listGroups`, `createGroup`, `updateGroup`,
  `deleteGroup`, `getVMMetrics`.

**`frontend/src/lib/stores/events.svelte.js`**
- Added `onVmMetrics(fn)` listener for the new `vm.metrics` SSE event
  (Phase 21). Internally added `_metricsListeners` set and
  `es.addEventListener('vm.metrics', …)` dispatcher.

**`frontend/src/lib/components/Chart.svelte` (new, Phase 21)**
- Hand-rolled SVG line+area chart. Props: `points`, `width`, `height`,
  `color`, `fillOpacity`, `strokeWidth`, `yMax`. Auto-scales Y to the
  data range (clamped to ≥ 0); if `yMax` is set, uses it instead
  (useful for % metrics capped at 100).

**`frontend/src/routes/Networks.svelte` (Phase 14)**
- New state: `hostInterfaces`, `hostDevice`.
- Loads host interfaces on first open of the create form.
- `create()` requires `hostDevice` when `forward === 'bridge'`.
- Conditional form: bridge mode shows only the **Host Device** select;
  NAT/isolated mode shows the CIDR/DHCP/DNS fields as before. Edit
  form is read-only and shows either "Bridged to: <name>" or the
  current CIDR depending on forward mode.

**`frontend/src/routes/VmList.svelte` (Phases 15, 17, 18)**
- Phase 15: import flow now reads `res.name` from the response and
  toasts `"<requested>" already existed — imported as "<resolved>"`
  (warning level) when the name was auto-renamed. Plain "Imported as
  …" success toast otherwise.
- Phase 17: `viewMode` state (`'grid'` default), persisted to
  `localStorage` under `vm-view-mode`. Grid view renders poster tiles
  (cover image as background, fallback is a gradient + first letter).
  Table view is unchanged, with new CPU/MEM sparkline columns.
- Phase 18: filter chips above the table/grid. "All" + one per group,
  with member counts. AND-combined with the search filter. "Manage
  Groups" button in the header opens a dialog for CRUD with a fixed
  8-color palette. `loadSparklines()` fetches metrics for running VMs
  and updates on `vm.metrics` SSE.

**`frontend/src/routes/VmDetail.svelte` (Phases 16, 17 alias, 19, 20, 21)**
- Header now shows `vm.alias || vm.name` and a small `(vm.name)`
  fallback when an alias is set.
- "Identity & Notes" button added in the sidebar actions (next to
  "Edit Settings"). Opens a 5-tab dialog (alias, cover, network,
  notes, groups).
- Phase 16: `openIdentity` populates state from `api.getVMMeta` and
  fetches `vlanSupportByNetwork` for every network the VM uses.
  Alias/Notes/Groups are saved in a single `updateVMMeta` call.
  Cover tab uploads via `api.uploadCover` (XHR-based for multipart);
  existing cover is displayed, can be removed.
  Network tab: per-interface card with MAC (input), Network (select),
  VLAN tag (input). The form is fully disabled when `vm.state !==
  'shutoff'` and a warning banner explains why. Each interface has
  its own Save button. The MAC input is **only** enabled when the VM
  is shut off (per the user requirement); if a duplicate MAC is
  detected after Save, the form keeps the typed value and shows the
  error inline (`cur.error = e.message`); the user can fix and
  re-save. The backend uses `dom.UpdateDeviceFlags(CONFIG)` and
  `assertMACUnique` to enforce this.
- Phase 19: snapshot section now renders a recursive tree (new
  `snapshotNode` snippet) instead of a flat list. Indentation per
  depth level (20px per level), small "▸" connector icon. Timestamps
  formatted as `YYYY-MM-DD HH:MM` via `formatSnapshotDate(epoch)`.
  The tree is built by `buildSnapshotTree(snapshots)` which groups
  flat snapshot records by `parent_name` and sorts by `creation_time`.
- Phase 20: notes textarea now saves **on blur** via
  `saveNotesIfChanged()`. The Save button on the alias/groups tab
  still saves both at once. The notes status is shown as a small
  indicator below the textarea (`saving` spinner / `saved` check /
  `error` text), auto-clearing 2s after success.
- Phase 21: new "Metrics" card with 6 charts (CPU%, RAM%, Disk R/W,
  Net Rx/Tx). Polled via SSE `vm.metrics` and initialized on mount
  with one REST fetch. Charts are 70px tall, full container width.
  Only shown when the VM is running; otherwise shows a friendly
  message. `formatRate` helper for KB/MB/GB/s suffixes.

**No changes to**:
- `frontend/src/lib/router.svelte.js` (router is unchanged from
  Phases 0–13 — still hash-based, 90 lines, custom, no library).
- `frontend/src/App.svelte`, `frontend/src/lib/components/Sidebar.svelte`,
  `frontend/src/lib/components/Layout.svelte` (Phase 0–13 router
  fix still in place: `$derived(getRoute())`).
- Any ISO/pool/volume/snapshot/clone/export/import-disk/netifaces code
  that isn't mentioned above.

## Endpoint reference (new in this round)

| Method | Path | Description |
|--------|------|-------------|
| GET    | `/api/host/interfaces` | List real host network interfaces (no lo/vnet/virbr) |
| GET    | `/api/groups` | List all groups with member counts |
| POST   | `/api/groups` | Create a group `{name, color}` |
| PUT    | `/api/groups/{name}` | Rename / recolor a group (propagates to VMs) |
| DELETE | `/api/groups/{name}` | Delete a group (scrubs tag from all VMs) |
| GET    | `/api/vms/{id}/meta` | Read WebVM metadata |
| PUT    | `/api/vms/{id}/meta` | Partial update: alias/notes/cover/groups |
| POST   | `/api/vms/{id}/cover` | Multipart upload a cover image (PNG/JPEG/WebP, 8MB) |
| DELETE | `/api/vms/{id}/cover` | Remove cover image + clear metadata |
| GET    | `/api/covers/{path}` | **Public**, static file from `${DATA_DIR}/covers/` |
| PATCH  | `/api/vms/{id}/networks/{mac}` | Update MAC / network / VLAN tag of an interface |
| GET    | `/api/vms/_/vlan-support?network=X` | Check VLAN tagging support for a network |
| GET    | `/api/vms/{id}/metrics` | Read 1h ring buffer of CPU/RAM/Disk/Net metrics |
| (SSE)  | `/api/events` (new event `vm.metrics`) | Live metric updates every 5s |

## Modified endpoint

- `POST /api/vms/import` and `POST /api/vms/import-ova` now return:
  ```json
  {
    "status": "imported",
    "id": "<uuid>",
    "name": "<resolved-name>",
    "requested_name": "<caller-supplied-or-empty>",
    "filename": "…",
    "format": "…"
  }
  ```

## Build & deploy

The source tree on the remote is a symlink at `/opt/webVM/source`
→ `/home/alvin/webvm/webvm-main`, so the in-app updater works. The
build is done here (CachyOS) and the binary is copied over.

```bash
# On the build host (CachyOS, this machine):
cd frontend && npm run build
cp -r dist ../backend/internal/frontend/dist
cd ../backend && go build -o webvm-backend ./cmd/server

# Push the new binary + askpass shim to the remote:
DISPLAY=: SSH_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh \
  scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  /home/alvin/webvm/webvm-main/backend/webvm-backend \
  alvin@192.168.1.105:/tmp/webvm-backend

DISPLAY=: SSH_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh \
  ssh alvin@192.168.1.105 'mkdir -p /tmp/alv-askpass'
DISPLAY=: SSH_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh \
  scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  /tmp/alv-askpass/ssh_askpass.sh \
  alvin@192.168.1.105:/tmp/alv-askpass/ssh_askpass.sh

# On the remote (Ubuntu 26.04, 192.168.1.105):
DISPLAY=: SSH_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh \
  ssh alvin@192.168.1.105 '
    SUDO_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh sudo -A \
      install -m 0755 /tmp/webvm-backend /usr/local/bin/webvm-server &&
    SUDO_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh sudo -A \
      mkdir -p /opt/webVM/covers &&
    SUDO_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh sudo -A \
      systemctl restart webvm-backend &&
    sleep 2 &&
    SUDO_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh sudo -A \
      systemctl is-active webvm-backend
  '
```

The systemd unit is at `scripts/webvm-backend.service` (User=root,
DATA_DIR=/opt/webVM, ReadWritePaths=/opt/webVM /var/lib/libvirt
/var/tmp). The covers directory is created on first need and lives
under `ReadWritePaths`, so no extra configuration is required.

## Known quirks / decisions

1. **MAC edit requires VM off.** Live virtio updates are unreliable
   across drivers; we reject them at the backend and lock the form
   in the UI. Users must shut down, edit, and start again.
2. **VLAN on plain bridge is gated.** Requires libvirt ≥ 11.0.0 and
   `macTableManager='libvirt'` (or OVS). Frontend shows a yellow
   warning per-network when not supported.
3. **Cover auth is by UUID.** The `/api/covers/...` route is in the
   auth middleware's whitelist. Anyone who knows a VM's UUID can
   download its cover. This is consistent with the original design
   brief and matches the VNC console pattern.
4. **Groups are in `groups.json` on disk.** Colors and definitions
   live there; membership is in each VM's metadata. Delete
   propagates to all VMs.
5. **Metrics are in-memory only.** 720 samples × 6 series per running
   VM. Lost on service restart. Per the brief.
6. **Backend dedup applies the resolved name to the XML** even if the
   caller did not supply a new name (so the archive's name field is
   always the one that actually got created).
7. **Import response `name` field is canonical.** The frontend toasts
   based on `requested_name` vs `name`.

## Service info

- **Production deploy**: Ubuntu 26.04 at `192.168.1.105`, served at
  `http://192.168.1.105:8080`. Existing VM: `ubuntu-1` (shutoff).
- **Build host**: CachyOS at `192.168.1.166` (this machine). Cross-build
  and `scp` to the remote.
- Binary: `/usr/local/bin/webvm-server` (15 MB Go binary, embeds
  the Svelte build via `go:embed`).
- Working dir: `/opt/webVM` (DATA_DIR).
- State:
  - `/opt/webVM/covers/` — cover images
  - `/opt/webVM/groups.json` — group definitions
  - `/opt/webVM/pools/webvm-disks/`, `/opt/webVM/pools/ISOS/` — storage
- Port: `:8080`
- Auth: admin / admin (default; change in production)
- Libvirt URI: `qemu:///system`
- Askpass: `/tmp/alv-askpass/ssh_askpass.sh` (password `1908`) on both
  hosts.

To restart: `SUDO_ASKPASS=/tmp/alv-askpass/ssh_askpass.sh sudo -A systemctl restart webvm-backend` on the remote.
