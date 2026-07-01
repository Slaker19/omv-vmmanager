# Fork notes: webVM → omv-vmmanager

This document tracks the changes made when forking
[Slaker19/webvm](https://github.com/Slaker19/webvm) into
`omv-vmmanager/omv-vmmanager`. The intent is to keep a record of
what's different, and what must change when syncing upstream.

## Why fork

The upstream `webVM` is a self-contained VM management UI that uses
libvirt under the hood. OpenMediaVault already has a KVM plugin
(`openmediavault-kvm`) for basic VM management. This fork ships a
**drop-in plugin package** for OMV 7 that runs webVM's more advanced
VM UI (OVA, backup, cloning, modern Svelte 5 frontend) alongside
the official plugin.

The fork also adapts defaults to be OMV-friendly: data dir under
`/opt/openmediavault/vmmanager`, integration with the shared-folder
machinery, binary/service renamed to `omv-vmmanager` so it doesn't
clash with anything else on the host.

## What changed (vs upstream `main`)

### Renamed / moved

| Old (upstream) | New (fork) | Why |
|----------------|------------|-----|
| `module webvm` | `module omv-vmmanager` | branding |
| `webvm-backend` (binary) | `omv-vmmanager` (binary) | branding |
| `webvm-backend.service` | `omv-vmmanager.service` | branding |
| `webvm-backend.logrotate` | `omv-vmmanager.logrotate` | branding |
| `webvm-backup.sh` | `vmmanager-backup.sh` | branding |
| `webvm-disks` (libvirt pool) | `vmmanager-disks` | branding |
| `/opt/webVM` (default DATA_DIR) | `/opt/openmediavault/vmmanager` (OMV) or `/opt/omv-vmmanager` (bare-metal) | OMV layout |
| `/var/log/webvm` | `/var/log/vmmanager` | branding |
| `/mnt/webvm-backup` | `/mnt/vmmanager-backup` | branding |
| `webvm-<host>-<ts>` (backup filename) | `vmmanager-<host>-<ts>` | branding |
| `WEBVM_REPO_DIR` env | `REPO_DIR` (WEBVM_REPO_DIR still accepted) | cleaner name |
| `WEBVM_VERSION` env | `VMMANAGER_VERSION` (WEBVM_VERSION still accepted) | cleaner name |
| `WEBVM_BUILD_TIME` env | `VMMANAGER_BUILD_TIME` (still accepts old) | cleaner name |
| `WEBVM_LOG_FILE` env | `VMMANAGER_LOG_FILE` (still accepts old) | cleaner name |
| `WEBVM_TRUST_PROXY` env | `VMMANAGER_TRUST_PROXY` | cleaner name |
| `WEBVM_TRUSTED_RATELIMIT_CIDRS` env | `VMMANAGER_TRUSTED_RATELIMIT_CIDRS` | cleaner name |

### Intentionally kept

| Item | Why |
|------|-----|
| XML metadata namespace `https://webvm.local/ns` | VMs created by upstream keep parsing |
| `JwtIssuer: "omv-vmmanager"` in `auth/jwt.go` | This one IS renamed; the old `"webvm"` would still be valid but shouldn't be issued by new builds |
| `webvmlibvirt` → `vmmlibvirt` alias in `cmd/migrate-disk-names` | Just a local Go import alias, no API effect |
| Internal Go module name `omv-vmmanager` (in `go.mod`) | Required because the build expects the import path to match |
| `frontend/src/lib/brand.js` SITE_NAME | Updated to "OMV VM Manager" |

### Adapter / new

| Item | Where | What |
|------|-------|------|
| `config.isOMVHost()` | `backend/internal/config/config.go` | Detects OMV via `/etc/openmediavault/config.xml`, picks the right default DATA_DIR |
| `EnvironmentFile=-/etc/default/omv-vmmanager` | `scripts/omv-vmmanager.service` | Lets the .deb postinst set `DATA_DIR` without rewriting the unit |
| `make deb` | `Makefile` | Builds the OMV plugin `.deb` (only when `debian/` is present, which is Phase 1 of the plugin work) |
| DATA_DIR auto-detection | `scripts/setup.sh` | Uses `/opt/openmediavault/vmmanager` if OMV is detected, `/opt/omv-vmmanager` otherwise |

## OMV-specific integration points (TODO / Phase 1+)

These will land in subsequent phases; this fork is Phase 0 (rebrand):

- [ ] `debian/` directory with proper OMV plugin structure
- [ ] PHP module + RPC for plugin enable/disable
- [ ] Salt state for service lifecycle
- [ ] UI manifest under `/usr/share/openmediavault/workbench/navigation.d/`
- [ ] Shared-folder integration: read OMV `/etc/openmediavault/config.xml`
      to pre-fill storage pool mount points
- [ ] Auth integration: optionally accept OMV session cookies in addition
      to the local JWT
- [ ] Network integration: detect OMV-managed bridges (e.g. `omvbr0`) and
      offer them in the network picker
- [ ] Phased PR to `OpenMediaVault-Plugin-Developers` repo

## Syncing from upstream

The fork is intended to track upstream `main` closely. The rebrand
is a one-time change; future syncs use a `git merge` strategy with
manual conflict resolution for:
- `go.mod` (keep our `module` line)
- `scripts/omv-vmmanager.service` (keep our OMV-aware config)
- `frontend/src/lib/brand.js` (keep our SITE_NAME)
- `AGENTS.md` (keep our OMV sections)
- Any new user-facing string that should be `omv-vmmanager`-branded

Upstream changes to the `webVM-*` test fixtures in
`internal/backupstore/*_test.go` will need to be ported to the
`vmmanager-*` equivalents as part of the merge.

## License

This fork is under the same MIT license as upstream. See `LICENSE.md`.
