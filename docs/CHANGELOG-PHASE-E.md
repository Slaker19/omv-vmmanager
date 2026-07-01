# Phase E — Backend features

Closes long-standing gaps in the libvirt management surface that
the previous phases (Phases 14–21) didn't address: VM autostart,
disk resize, host-wide time-series metrics, and API plumbing for
future features (migration, per-vCPU/IOPS — left for a follow-up
to keep this phase shippable).

## E1 — VM autostart

libvirt already has `Domain.SetAutostart` / `GetAutostart` for
network/pool objects; this phase adds it to VMs.

- **`GET /api/vms/{id}/autostart`** — returns
  `{"autostart": bool}`.
- **`POST /api/vms/{id}/autostart`** — body `{"enabled": bool}`,
  applies, audit-logs `vm.autostart_set`.
- `libvirt.SetDomainAutostart` / `GetDomainAutostart` wrap the
  libvirt calls and add the `lookupDomain` + `defer Free()` dance.

The libvirtd autostart flag is independent of the VM's current
running state — toggling it does not affect a running VM.

## E2 — Resize a VM's disk

- **`POST /api/vms/{id}/disks/{dev}/resize`** — body
  `{"size_gb": int}`, runs `qemu-img resize` on the backing file,
  audit-logs `vm.disk_resize`.
- `libvirt.ResizeDomainDisk` finds the disk by `target` device
  (e.g. `vda`), invokes `qemu-img resize`, and returns the new
  size in bytes.
- **Constraint**: the VM must be **shut off** — `qemu-img` cannot
  take the write lock on a live qcow2 file. The handler returns a
  clear `400` with the message *"VM must be shut off to resize a
  disk; shutdown first then retry"* instead of a cryptic lock
  error. (A future phase could add a BlockResize path for live
  growth only.)

## E5 — Host-wide metrics collector

`/api/host/stats` was previously returning a one-shot snapshot.
This phase adds a background collector that samples the host
every 5 s, keeps a 1-hour ring buffer, and broadcasts each new
sample as a `host.metrics` SSE event for the frontend to consume.

- **Collector** (`libvirt/hostmetrics.go`):
  - CPU: `/proc/stat` jiffies delta (same approach as Phase A8).
  - RAM: `/proc/meminfo` — `MemTotal` and `MemAvailable`
    (preferred) or `MemFree` (fallback). kB → bytes conversion.
  - Disk: `syscall.Statfs` on the data dir (consistent with the
    Health endpoint).
  - Net: aggregate `rx_bytes`/`tx_bytes` from `/proc/net/dev`
    across all non-loopback, non-bridge interfaces (`lo`,
    `vnet*`, `virbr*`, `br-*`, `docker*` excluded). Reported as
    bytes/sec delta since the previous sample.
- **`GET /api/host/metrics`** — returns the buffered series as
  `HostMetricsSeries{Kind, Window, Points}` (window = interval ×
  capacity = 5 s × 720 = 3600 s = 1 h).
- **SSE event `host.metrics`** — each new sample is also pushed
  over the events hub so the Status page can show live charts
  without polling.
- Wired into `cmd/server/main.go` alongside the per-VM metrics
  collector.

## E3, E4 — Deferred

- **Live / offline migration** (`virDomainMigrate*`) — significant
  scope: target URI, flags, TLS, progress callbacks, the
  cross-host networking implications, and live block-migration
  options. Out of scope for this phase. Plumbing is in place
  (audit log, RBAC group) so the next phase can land it.
- **Per-vCPU CPU + disk IOPS** — the libvirt calls
  (`GetVcpuStats`, `BlockStats` `RdReq`/`WrReq`) are available;
  the existing `MetricsCollector` would just need additional
  fields. Not blocking; the aggregated series are sufficient
  for the sparkline UX.
- **Serial console streaming** — `<serial type='pty'/>` is in the
  generated XML; the API would need a WebSocket (or SSE) stream
  of the pty, plus a backend `OpenConsole` helper. This is the
  largest remaining gap but it requires a frontend terminal
  component too — best done as its own phase.

## Files touched in Phase E

```
backend/internal/libvirt/domain.go                       (SetDomainAutostart, GetDomainAutostart, ResizeDomainDisk)
backend/internal/libvirt/hostmetrics.go                  (NEW: HostMetricsCollector)
backend/internal/api/host.go                             (GetHostMetrics)
backend/internal/api/handler.go                          (hostMetrics field)
backend/internal/api/router.go                           (new routes, NewRouter signature)
backend/internal/api/vms.go                              (GetAutostart, SetAutostart, ResizeDomainDisk)
backend/cmd/server/main.go                               (start HostMetricsCollector)
```
