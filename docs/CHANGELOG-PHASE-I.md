# Phase I — Backup v2 hardening + 500 fix

The 1.7 release shipped backup v2 (multi-target, schedules, per-VM
selection, manual cleanup, visual cron picker). The follow-up
"1.7-bis" pass added dialog-API casing fixes, an edit-target
flow, and the dist-sync guard that finally matched the .163
embed and tree. **Phase I** closes three bugs that surfaced
during the post-deploy smoke tests, and adds the production
artefact pipeline (binary + Docker images + GHCR tags) that
the project has been missing since the CI workflow was
written.

## I-1 — Fix `POST /api/backup/targets/{id}/run` 500

The 4 manual-run attempts against the `xddxdf` target on .130
all returned 500 with a generic `{"error":"...","job":{...}}`
body. The handler's `err.Error()` was never logged, so the
backend.log only showed `http_request ... status=500
duration_ms=0 bytes=346` and the toast in the UI was empty.

### Root cause (runner.go:243-253, the original code)

`writeBackup` invoked tar with `-C filepath.Dir(r.dataDir)`
(`/opt`) and a `--transform 's,^/opt/webVM,webvm,'` regex.
It also handed tar **basenames** for the metadata files
(`users.json`, `groups.json`, `config.json`, …). Tar
resolved those basenames against `-C /opt`, looked for
`/opt/users.json` (does not exist — lives at
`/opt/webVM/users.json`), and exited 2 with `Cannot stat:
No such file or directory` to stderr. The handler caught
the non-zero exit and returned 500. **Every run failed the
same way.**

Verified by sshing to .130 and replaying the exact tar
command with the runner's own args — `EXIT=2`, `bytes: 45`
(stub archive with only a single zero byte). The fix had
to be one of:

1. Switch `-C` to `r.dataDir` so basenames resolve to
   `/opt/webVM/users.json`. **Picked.** This is the smallest
   behavioural change and keeps the archive layout
   predictable.
2. Strip the `-C` and pass absolute paths. Rejected — the
   `--transform` regex would still be wrong because tar
   rewrites leading `/` to `opt/...` after `Removing leading
   `'/' from member names`, which the original transform
   regex `s,^/opt/webVM,webvm,` did not match.

The new `--transform 's,^,webVM/,'` (instead of
`s,^/opt/webVM,webvm,`) prefixes every archive entry with
`webVM/`, matching what `RestoreBackup` already extracts.
The archive contents on a successful run:

```
webVM/api-tokens.json
webVM/audit.log
webVM/config.json
webVM/groups.json
webVM/jwt.key
webVM/nodes.json
webVM/pool-purposes.json
webVM/source
webVM/users.json
```

## I-2 — Replace broken `--exclude=*.100M-and-larger`

The original code passed
`fmt.Sprintf("--exclude=*.%dM-and-larger", maxSizeMB)` to
tar. **GNU tar's `--exclude` is a glob filter, not a size
filter** — the pattern was silently matched against nothing
and a 5 GB qcow2 happily made it into every backup. The
backups grew to multi-GB in a few minutes, which is what
`webvm-backup.sh` (the legacy bash backup script) had
worked around with a pre-pass `find ... -size -100M`.

The fix moves the size filter into Go in
`collectVMFiles(tgt, maxSizeBytes)`. Each candidate disk
source is `os.Stat`'d; anything over the cap is skipped
with a `backup_skip_oversize_disk` log line carrying the
size, the cap, and the VM id. A summary line
`backup_vm_filter_summary` is emitted at the end of
collection with the totals.

Same function also drops out-of-tree disk sources (a
hand-edited domain XML pointing at `/var/lib/libvirt/...`)
with `backup_skip_out_of_tree_disk` and a warning, rather
than letting them land in the archive with the wrong
layout on restore. The defence-in-depth `cdrom` and
`/pools/ISOS/` skips are unchanged.

## I-3 — `RecordJob` returns the canonical job (in-memory + disk divergence)

The 500 was the visible bug, but the smoke test exposed a
second one. After a `tar` failure, the runner's local
`job` was updated to `status="error"`, then
`r.store.UpdateJob(job)` was called. The in-memory map
showed `error`, but the API GET `/api/backup/jobs` returned
`running`. The `jobs.json` on disk also said `running`.

### Root cause (store.go:609-620, the original code)

```go
func (s *Store) RecordJob(j Job) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if j.ID == "" {
        j.ID = newID("j")   // <-- mutates a COPY
    }
    ...
    s.jobs[j.ID] = &j
    return s.saveJobs()
}
```

`Job` is a value type, not a pointer. Assigning `j.ID =
newID("j")` mutates the **local copy** in `RecordJob`'s
stack frame. The caller's `job` still has `ID == ""`.
`RecordJob` then stores `&j` (the local copy) in the map
and returns nothing. The runner's local `job` keeps
`ID=""`. When `writeBackup` later fails, the runner calls
`UpdateJob(job)` with `ID=""`, and `UpdateJob` returns
`"job not found"` because `s.jobs[""]` doesn't exist.
That error was thrown away by `_ = r.store.UpdateJob(job)`,
so the on-disk `jobs.json` was never updated either. The
Jobs tab showed `running` forever, the toast correctly
showed the error, and the operator assumed the page was
broken.

### Fix

- `RecordJob` now returns `(Job, error)`. The returned job
  carries the generated `ID`. The runner copies it back:
  ```go
  job, err := r.store.RecordJob(job)
  if err != nil { return job, err }
  ```
- The runner's later `UpdateJob(job)` finds the right
  entry, and the in-memory + disk state converge.
- `UpdateJob`'s silent `_ =` is replaced with a logged
  error so any future `saveJobs` failure is visible in
  `backend.log` instead of leaving the operator to wonder.

This was a **real** production bug, not a corner case —
any backup that fails for any reason (read-only target
path, libvirt transient, tar error, audit.log over 10 MB
gets dropped, etc.) would have left a stuck `running` job
on disk.

## I-4 — Sweeper for stuck "running" jobs

Even with I-3 fixed, a `kill -9` between `RecordJob` and
`UpdateJob` (OOM, hardware reset, ops typo) still leaves a
job in `running` state on disk. The Jobs tab would show
"Running" forever. The UI's `Cancel` button has no
implementation (the runner has no cancellation handle), so
a restart is the only recovery.

`Store.New` now calls `sweepStuckJobs(s)` immediately after
`load`. Every job with `status="running"` and
`ended_at.IsZero()` is rewritten to
`status="error"`, `ended_at=now`, `error="aborted_by_restart:
job was running when the backend stopped"`. The disk write
is best-effort; if `saveJobs` fails the in-memory state is
still fixed and the next restart will retry.

**Verified on .130**: the 5 jobs stuck in the field
(`j_caf174b1aa65bf9c`, `j_5f2d9e9f23587be5`,
`j_98607a56b4af3335`, `j_4ee5dcd8a17d4b0f`,
`j_2f530abaa1bf04c3`) were all marked `aborted_by_restart`
on the first post-fix restart. The Jobs tab now shows
them with a clear error and the next backup on the same
target doesn't fail with a duplicate-key issue.

## I-5 — Tar command is now context-bounded + structured-logged

`writeBackup` used `exec.Command("tar", ...).Run()` with
no timeout. A tar that hit a slow disk or NFS mount could
block the HTTP request for the full server `WriteTimeout`
(30 min). Replaced with `exec.CommandContext(ctx, "tar",
...)` and a 10 min deadline.

A successful run emits:

```json
{"msg":"backup_run_starting","target":"t_b1e5d62bf5a7aab6","meta":10,"vm":0,"max_size_mb":100,"out":"/opt/webVM/backup/default/webvm-webvm-20260625T214148Z.tar.gz"}
{"msg":"http_request", "path":"/api/backup/targets/default/run","status":200,"duration_ms":18,"bytes":227}
```

A failed run emits (in addition to the 500):

```json
{"level":"ERROR","msg":"backup_run_failed","target":"t_b1e5d62bf5a7aab6","err":"exit status 2","stderr":"tar: ...: Cannot stat: No such file or directory\ntar: Exiting with failure status due to previous errors"}
```

Plus per-step error logs (`backup_mkdir_failed`,
`backup_create_failed`, `backup_skip_oversize_disk`,
`backup_skip_out_of_tree_disk`,
`backup_job_update_failed`) so any future bug in the
backup pipeline has a precise log line to grep for instead
of a 500 with no trail.

## I-6 — Empty-archive fallback

GNU tar refuses to create an empty archive
(`Cowardly refusing to create an empty archive`, exit 2).
This is the right thing for tar, but in the runner's case
"empty" can legitimately mean "all metadata files were
filtered out and all VM disks were over the cap." Rather
than surface that as a backup error, `writeBackup` writes
a `.backup_manifest` to the dataDir with the filter
decisions and includes it in the archive. The manifest is
cleaned up on the next non-empty run. The operator can
read the archive and see immediately why the backup is
tiny.

## I-7 — Frontend `Backup.svelte` follow-ups (1.7-bis UI)

`0fcdc7f` (1.7-bis UI fixes) shipped in the same release:

- 22 dialog props renamed from `onconfirm`/`oncancel` →
  `onConfirm`/`onCancel` across `Backup.svelte` (12),
  `Nodes.svelte` (4), `Account.svelte` (6) to match
  `ConfirmDialog.svelte`'s actual API.
- The VM selector inside the Add-Target form was using a
  `Checkbox onchange` prop that doesn't exist. Replaced
  with a native `<input type="checkbox">` whose
  `onchange` mutates the `newTargetVMIDs` Set. The
  selector now actually does what the UI implies.
- An **edit target** flow was added. `editTarget(t)`
  pre-fills the form, and `addTarget()` branches on
  `editingTarget` (POST if null, PUT if set). The Path
  field is `<Input disabled>` for the default target and
  the payload's `path` is dropped server-side as a
  defence-in-depth. The `Enabled` Switch only renders in
  edit mode.
- `$effect(prevAddTargetOpen)` resets the form on
  `open → closed` so closing via the X or ESC button
  doesn't leave stale state behind.

## I-8 — Production artefact pipeline

Until this phase, the project had a CI workflow that
pushed to `ghcr.io/slaker19/webvm-{backend,frontend}` but
no local script to reproduce the build + tag outside CI.
That made operator-led hotfixes (like this one) hard to
verify before pushing, and impossible to ship as a single
binary without going through the full CI run.

The new pipeline (see `Makefile`):

```bash
make build                  # backend binary → backend/webvm-backend
make docker                 # both images → webvm-backend:latest, webvm-frontend:latest
make docker-push            # tags for GHCR and pushes (needs GHCR_TOKEN)
```

The `docker-push` target tags with three names per image
to match the CI convention: `latest`, the human version
(`phase1.7-bis-backup-fix-v3`), and the short SHA
(`25d0198`). The version is read from `git describe
--tags --always` when available, falling back to `dev`.

### Bug found while wiring this up

The CI's frontend build was silently broken. The
frontend Dockerfile had `COPY nginx.conf ...` but the
build context was the repo root, where `nginx.conf` does
not exist (it lives at `frontend/nginx.conf`). The CI
workflow would have failed at the frontend build step
without the operator noticing — there was no error
visible in the workflow YAML, just a failing step.
Fixed: the Dockerfile now `COPY`s `frontend/package.json`
+ `frontend/` (instead of `.`) and `frontend/nginx.conf`
so the build works with `context: .`.

## Verified

- `go test ./...` — all 14 packages green. 7 new tests
  in `runner_test.go` (relative paths, size filter,
  include/exclude, end-to-end writeBackup, empty-archive
  fallback) and 3 new tests in `store_test.go` (sweeper
  happy path, sweeper no-touch for completed jobs,
  RecordJob ID propagation).
- Backend binary rebuilt locally as
  `phase1.7-bis-backup-fix-v3` (md5 `7c7a774160f0949e430f97667708399b`,
  11 811 232 bytes).
- Docker images rebuilt locally:
  - `webvm-backend:phase1.7-bis-backup-fix-v3` — 85.7 MB
    compressed, 359 MB on disk, sha256
    `5dcda3763388e4ae1c5c7d84b00408ca8cd35667517cda30d5aca446cea2028c`.
  - `webvm-frontend:phase1.7-bis-backup-fix-v3` — 26.1 MB
    compressed, 94.3 MB on disk, sha256
    `26e4415ae5b42be38ec4d64d5b7b163389fb023bf266dcfc138940092c1caa4f`.
- Both images also tagged
  `ghcr.io/slaker19/webvm-{backend,frontend}:{latest,phase1.7-bis-backup-fix-v3,25d0198}`
  ready to push.
- **.130** (systemd): rebuilt binary deployed, `active (running)`.
  Smoke test:
  - `default` target: 200 OK, 4233-byte tar.gz at
    `/opt/webVM/backup/default/webvm-webvm-20260625T214148Z.tar.gz`
    with the expected `webVM/...` layout.
  - `xddxdf` target: now returns the **clear** error
    `create: open /home/alvin/webvm-webvm-…tar.gz: read-only
    file system` (the user picked `/home/alvin/` which is
    a read-only mount on .130). The job is properly
    recorded with `status=error` and `ended_at` set, so
    the Jobs tab updates correctly. The UI toast shows
    the actual `err.Error()` now.
- **.163** (docker): rebuilt image deployed, container
  `Up 10 seconds (healthy)`, version
  `phase1.7-bis-backup-fix-v3` visible in the startup
  logs.

## Known issues / follow-ups (not in this phase)

- **GHCR push not done from this dev machine**: no
  `GHCR_TOKEN` in the env, `gh` CLI not installed, no
  cached credentials. The images are tagged and the
  Makefile target is in place; the user needs to run
  `make docker-push` from a machine with a `GHCR_TOKEN`
  exported (Settings → Developer settings → Personal
  access tokens → Generate new token → `write:packages`).
  Once pushed, the same tag can be deployed to .163 with
  `docker compose pull && docker compose up -d`.
- **`/home/alvin/` is a read-only mount on .130**: the
  `xddxdf` target was configured with `path=/home/alvin/`
  by the operator. Now that the 500 is fixed, the toast
  correctly shows `read-only file system`; the fix is to
  change the target path to a writable location (e.g.
  `/opt/webVM/backup/xddxdf` or the `/mnt/webvm-backup`
  NFS share). Not in scope for this phase.
- **`PUT /api/settings/backup.max_file_size_mb` returns
  404** on .130. The settings-store Update endpoint
  isn't routed for nested keys; only the top-level
  `settings` is wired. Workaround: edit
  `/opt/webVM/config.json` directly, or add the route
  in a follow-up. Not in scope for this phase.
- **Admin JWT exposed in chat earlier in the session**:
  the token the operator pasted in chat is still valid
  (expires 2026-06-29 18:38 UTC). Revoke via Account →
  API tokens → Revoke all + create a new one.
- **GitHub push still blocked**: no `GHCR_TOKEN` and no
  GitHub PAT available locally. Commits
  `493f832` (I-1/I-2/I-4/I-5/I-6) and `25d0198` (I-3) are
  local-only on the `main` branch.

## Files touched

- `backend/internal/backupstore/runner.go` (writeBackup,
  collectVMFiles, RunOnce — 185 lines changed)
- `backend/internal/backupstore/runner_test.go` (NEW,
  233 lines, 7 tests)
- `backend/internal/backupstore/store.go` (RecordJob,
  UpdateJob, sweepStuckJobs, Store.New — 45 lines changed)
- `backend/internal/backupstore/store_test.go` (3 new
  tests, 105 lines added)
- `frontend/src/routes/Backup.svelte` (1.7-bis UI fixes
  shipped in `0fcdc7f` — see Phase H for unrelated UI
  work)
- `frontend/Dockerfile` (fixed `nginx.conf` path; now
  works with `context: .` like the backend Dockerfile)
- `Makefile` (new `build`, `docker`, `docker-push`
  targets)
- `docs/CHANGELOG-PHASE-I.md` (this file)
- `README.md` (current version stamp, Phase I summary)
- `AGENTS.md` (build / push flow)
