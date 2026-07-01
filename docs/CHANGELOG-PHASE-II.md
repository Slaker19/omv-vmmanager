# WebVM Phase II — Changelog

Release: **phase2-backup-format-v2** (2026-06-26)
Hotfix on top of v1: fixes a config-archive tar race that
surfaced in production within an hour of v1's deploy.
The 9 Phase I bug fixes + the 4 Phase II architecture
commits from v1 are unchanged; v2 adds one extra commit
(A5) on top.

## A5. Config-archive tar race (Phase II hotfix)

Production error after the v1 deploy:

```
Manual · target t_b1e5d62bf5a7aab6
write config: archive/tar: missed writing 4096 bytes
```

Root cause: the config-archive code path did `f.Stat()` to
get the size for the tar header, then `io.Copy(tw, f)` to
write the body. If the file was truncated between those two
calls — typically by logrotate running while the backup was
in flight, or by the running backend appending to
`audit.log` and then rolling — `io.Copy` returned EOF before
writing all the declared bytes, and the next `WriteHeader`
in the tar stream failed with "missed writing N bytes"
(where N was the truncated tail, commonly 4096 for one
block).

Fix (`internal/backupstore/producer.go`): the config
writer now reads the file fully into memory in a single
`os.ReadFile` call and uses `len(data)` for the tar
header size. The bytes we read are the bytes we declare.
No stat, no read, no race.

Memory: this path is for config files only (audit.log
capped at 10 MiB). A 10 MiB buffer in memory is fine.
The VM disk path (`writeDiskToTar`) continues to use
streaming `io.Copy` because a 100 GiB qcow2 does not fit
in memory.

Regression test added:
`TestWriteFileToTarResistsTruncateDuringRead` simulates
the race by writing 8 KiB, truncating to 4 KiB before
the call, and asserting that the resulting tar is
self-consistent (header size matches body size). The
old code would have failed at `tw.Close()` with
"missed writing 4096 bytes" — the exact error the
operator saw.

## I-1. BackupNow: 500 → 4xx with sentinels (bug #1)

`POST /api/backup/targets/{id}/run` always returned **500**, even
for user-fixable errors like "target not found" (should be 404)
or "target disabled" (409) or "path is not writable" (400). The
error string reached the toast but the HTTP code lied.

The fix introduces a sentinel-error pattern in
`internal/backupstore/errors.go` and a `backupErrorStatus`
helper in `api/backup.go` that maps each sentinel to the
right HTTP code:

| Sentinel                  | Status | Meaning |
|---------------------------|--------|---------|
| `ErrTargetNotFound`       | 404    | URL refers to a non-existent target |
| `ErrTargetDisabled`       | 409    | Target is paused |
| `ErrScheduleNotFound`     | 404    | (same, for schedules) |
| `ErrInvalidCron`          | 400    | Cron expression doesn't parse |
| `ErrTargetPathUnwritable` | 400    | Path denied by the safety check |
| (any other error)         | 500    | Real server-side problem |

## I-2. Cron validation at submit time (bug #2)

`Store.CreateSchedule` / `UpdateSchedule` accepted any
non-empty cron string. The cron library would silently drop
unparseable values inside `Runner.addSchedule` — the user
got a "schedule added" toast and a row in the table, but
the schedule never fired. The same pattern of silent
failures was visible in `sweepStuckJobs` (see I-3) and
`SetScheduleLastRun` (I-4).

The fix validates with `cron.ParseStandard` at the moment
of submit. Bad input returns `ErrInvalidCron` (400) with
the parser's error message verbatim.

## I-3. sweepStuckJobs normalises running+EndedAt (bug #3)

A job with `status="running"` AND a non-zero `EndedAt` is
a contradiction (EndedAt is set on terminal status). The
previous code had a `continue` for this case — the comment
in the source said "treat as stuck too" but the code did
the opposite. Contradictory records stayed in `jobs.json`
forever. The sweep now normalises them like any other
stuck job and preserves the pre-existing `EndedAt`.

## I-4. SetScheduleLastRun returns error (bug #9)

`Store.SetScheduleLastRun` had signature
`func(id, status, errMsg, nextRun)` returning nothing. Save
errors were silently swallowed. The new signature returns
`error`; the runner logs the failure with structured
`backup_schedule_update_failed` fields. A disk-full
incident now leaves a log line, not a stale `last_status`.

## I-5. Path safety + restore timeout/size cap (bugs #6 + #8)

Two related foot-guns that compose into a real attack:

- **#6**: `Store.CreateTarget` did `os.MkdirAll(path, 0755)`
  without any check. A target pointing at `/etc/webvm-backups`
  would happily try to write into it.

- **#8**: `RestoreBackup` ran `tar -xzf` with no timeout and
  no size cap. A stuck tar (slow NFS, corrupt stream) could
  hang the HTTP request; a zip-bomb tar.gz could fill the
  disk.

Both fixed in one commit because the blast radius of either
bug is reduced by fixing the other. The deny-list in
`path_safety.go` covers `/etc`, `/usr`, `/boot`, `/proc`,
`/sys`, `/bin`, `/sbin`, `/lib`, `/lib64`, `/var/lib/{dpkg,
rpm, libvirt}`. The app's dataDir is always allowed so the
bootstrap "default" target works.

Restore caps:
- `MaxRestoreSourceBytes` = 100 GiB (refuse up front)
- `MaxRestoreExtractedBytes` = 500 GiB (refuse post-extract)
- `MaxRestoreDuration` = 30 min (kill the tar)

Vars (not consts) so tests can dial them down.

## I-6. Unique filename avoids same-second double-click race (bug #5)

Two clicks on "Backup now" inside the same second produced
the same filename (UTC 1-second resolution), the second
`os.Create` failed with `EEXIST`, and the operator saw a
misleading 400. The fix composes the filename as
`webvm-<host>-<UTC-nano>-<rand6hex>.tar.gz` and uses
`O_EXCL`. The nanosecond timestamp plus a cryptographically
random 6-hex suffix makes a collision ~1 in 16M. The
filename validator now accepts the new format AND the
legacy 16-char-UTC format, so archives written by previous
versions of the binary remain deletable from the Files tab.

## I-7. Unified pointer convention for Update* (bugs #4 + #7)

`UpdateTarget` / `UpdateSchedule` mixed two conventions:
"empty string = don't change" on `name`/`path`/`type`/
`cron`/`targetID`, "nil pointer = don't change" on `Enabled`.
This made it impossible to express "set Enabled to false"
as a real update — the API silently treated it as "leave
alone". With pointers for all fields, `nil` everywhere
means "leave alone" and `*x` means "set to x". The handler
structs in `api/backup.go` match. The same convention will
be used by the future "edit schedule" UI in Phase II-D so
the UI doesn't have to work around ambiguity later.

## II-1. Shared per-VM archive producer (architecture)

`internal/backupstore/producer.go` defines
`ProduceVMArchive(ctx, vm, opts, w) (ProducerResult, error)`
— the single source of truth for the bytes of a per-VM
WebVM archive (`domain.xml` + `disks/<base>` + `manifest.txt`,
optionally zstd- or gzip-compressed, optionally with
qemu-img repack). Two callers funnel through it:

- `Connector.ExportDomain` (browser download)
- `Runner.writeBackup` (scheduled + manual backup)

The two code paths no longer drift.

## II-2. WebVM-style backup format

Each backup run now produces **N+1 archives**:

```
webvm-<host>-<UTC>-<rand>-<vmname>.tar.zst   (one per in-scope VM)
webvm-<host>-<UTC>-<rand>-config.tar.zst     (always 1, app state)
```

The N per-VM archives are byte-compatible with the
browser-downloaded WebVM export, so a restore is the same
`ImportDomain` path either way. The config archive carries
the app metadata (users, config, schedules, pool-purposes,
nodes, audit tail) so a disk loss doesn't leave the
operator re-creating the deployment from scratch.

The Job model gets `Filenames []string` + `Files []JobFile`
(Kind in `{config, vm}`, VMID for per-VM entries). The
singular `Filename` field is kept (now points at the
config tar) so the existing Jobs tab UI keeps working.

The runner's signature changes:

- `NewRunnerWithConfig` gains `VMXMLSource` (per-VM libvirt
  XML, wired to `libvirt.Connector.GetDomainXML` in main.go).
  Optional; nil means a placeholder `domain.xml`.
- `writeBackup` returns `([]JobFile, int64, error)`. The
  runner's `RunOnce` populates the Job from the list.

## II-3. Restore handles N+1 files per run

The HTTP handler now accepts three request shapes:

```jsonc
{"filename": "webvm-...-vm-1.tar.zst"}                  // single file
{"run":      "20260625T120000.000000000Z-aabbcc"}      // whole run
{"files":    ["<file1>", "<file2>", ...]}              // explicit list
```

Output layout:

```
dataDir/restore-<ts>/config/...          (config tar contents)
dataDir/restore-<ts>/<vmname>/...       (per-VM tar contents)
dataDir/restore-<ts>/RESTORE_MANIFEST.json
```

Each subdir has its own per-archive `RESTORE_MANIFEST.json`
so the operator can see what came from where without
re-walking the tree. Tar is invoked with the right
decompression flag (`.tar.gz` → `-z`; `.tar.zst` → `--zstd`
with a fallback to piping through the `zstd` CLI).

## II-4. ExportDomain = thin shim over the producer

`Connector.ExportDomain` is now a 10-line shim: it does
the libvirt-specific glue (domain lookup, XML fetch, disk
validation) and calls `backupstore.ProduceVMArchive`.
The bytes the browser downloads are byte-for-byte
identical to the per-VM tars the runner stores.

A new dependency edge: `internal/libvirt → internal/backupstore`.
It is one-way (backupstore does not import libvirt) and
matches the architecture: libvirt answers "what does a VM
look like?", backupstore answers "how do we serialise it?".

The OVA path (`Connector.ExportDomain` for the
VirtualBox/VMware/libvirt OVA format) is unchanged — it
targets a different output format (`domain.ovf` +
`domain.mf` + `disks/*.vmdk`) and uses its own producer in
`internal/libvirt/ova.go`.

## Verified

- **Build**: `cd backend && go build ./...` clean.
- **Tests**: `cd backend && go test ./...` — 48 tests, all green.
- **Deploy**: binary deployed to `.130` (systemd), `version=phase2-backup-format-v1`
  visible in startup logs. Backup store loaded 2 targets, 1
  schedule, no errors. Libvirt connected to `qemu:///system`.

## Known issues

- `.163` (docker) is not rebuilt in this release. The docker
  image for `phase1.7-bis-backup-fix-v3` predates this commit.
  The operator can rebuild via `WEBVM_VERSION=phase2-backup-format-v1
  bash scripts/deploy-docker.sh` if needed.
- Restore is sandboxed: it extracts to `dataDir/restore-<ts>/`
  and never overwrites the running dataDir. The operator
  must move files into place (config tar: `cp -a`; per-VM
  tar: re-import via the existing VM Import flow).
- Phase I tar.gz files remain readable for deletion but
  cannot be restored (the restore code path requires
  Phase II's structure).
- The `webvm-phase2-backup-format-v1` does not include
  frontend changes (UI for editing schedules, multi-file
  restore UI, SelectedList). Those are Phase II-D, next.

## Files touched

- `backend/internal/backupstore/errors.go` (NEW)
- `backend/internal/backupstore/path_safety.go` (NEW)
- `backend/internal/backupstore/producer.go` (NEW)
- `backend/internal/backupstore/producer_test.go` (NEW)
- `backend/internal/backupstore/runner.go` (major rewrite)
- `backend/internal/backupstore/store.go` (sentinels, validators, pointer convention)
- `backend/internal/backupstore/runner_test.go` (updated)
- `backend/internal/backupstore/store_test.go` (5 new tests)
- `backend/internal/api/backup.go` (status code mapping, RestoreRun handler)
- `backend/internal/libvirt/domain.go` (ExportDomain shim, removed 8 helpers)
- `backend/internal/api/vms.go` (ExportDomain signature change)
- `backend/cmd/server/main.go` (NewRunnerWithConfig takes 6 args)
- 48 tests, all green.
