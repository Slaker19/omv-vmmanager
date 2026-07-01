<script>
  /**
   * Backup — multi-target / per-VM / schedule / manual cleanup.
   *
   * The Phase 1.7-bis-backup rewrite drops the global retention
   * policy. Backups accumulate on disk; the operator deletes them
   * by hand from the Files tab. Schedules are configured via a
   * visual cron picker instead of a raw 5-field string. Each
   * target also gets a VM selector so you can back up "all VMs",
   * "only these N VMs", or "every VM except these".
   */
  import { onMount } from 'svelte';
  import { api } from '$lib/stores/auth.svelte.js';
  import { navigate } from '$lib/router.svelte.js';
  import { toast } from '$lib/components/ui/toast';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import * as Dialog from '$lib/components/ui/dialog';
  import { Loader2 } from '@lucide/svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import CronPicker from '$lib/components/CronPicker.svelte';
  import Switch from '$lib/components/Switch.svelte';

  let targets = $state([]);
  let schedules = $state([]);
  let jobs = $state([]);
  // filesByTarget keeps the per-target backup archive list so the
  // Files tab can render without re-hitting the API on every tab
  // switch. Keyed by target ID; an entry of `null` means "not yet
  // loaded" (we lazy-load on first visit).
  let filesByTarget = $state({});
  let vms = $state([]);
  let loading = $state(true);
  let activeTab = $state('targets');

  // Add target form (also reused for Edit, driven by editingTarget)
  let showAddTarget = $state(false);
  let editingTarget = $state(null); // null = add mode, otherwise the target being edited
  let newTargetName = $state('');
  let newTargetType = $state('local');
  let newTargetPath = $state('');
  let newTargetVMFilter = $state('all');
  let newTargetVMIDs = $state([]);
  let newTargetEnabled = $state(true);
  // editSaving is true while a create/update request is in flight;
  // disables the confirm button to avoid double-submits on slow links.
  let editSaving = $state(false);

  // Add schedule form
  let showAddSchedule = $state(false);
  let newScheduleName = $state('');
  let newScheduleCron = $state('0 2 * * *');
  let newScheduleTarget = $state('');

  // Search box for the VM selector. Filtering the full list client-
  // side is fine — we don't expect more than a few dozen VMs on a
  // single host.
  let vmSearch = $state('');

  let confirmDeleteTarget = $state(null);
  let confirmDeleteSchedule = $state(null);
  let confirmDeleteFile = $state(null); // { targetId, filename }
  let runningBackups = $state({});
  let filesLoading = $state({}); // targetId -> bool

  onMount(async () => {
    await load();
  });

  async function load() {
    try {
      const [t, s, j, v] = await Promise.all([
        api.listBackupTargets(),
        api.listBackupSchedules(),
        api.listBackupJobs(),
        api.listVMs(),
      ]);
      targets = t.targets || [];
      schedules = s.schedules || [];
      jobs = j.jobs || [];
      vms = v || [];
    } catch (err) {
      toast.error('Failed to load backup state: ' + err.message);
    } finally {
      loading = false;
    }
  }

  // filteredVMs is the visible list inside the Add Target dialog
  // after applying the search box. Items are sorted by name so the
  // checkboxes are stable as the user types.
  let filteredVMs = $derived.by(() => {
    const q = vmSearch.trim().toLowerCase();
    const list = q ? vms.filter((vm) => (vm.name || '').toLowerCase().includes(q)) : vms;
    return [...list].sort((a, b) => (a.name || '').localeCompare(b.name || ''));
  });

  // addTarget handles both the Add and the Edit submit. The mode
  // is dictated by editingTarget: null = add, otherwise = update.
  async function addTarget() {
    if (!newTargetName.trim() || !newTargetPath.trim()) {
      toast.error('Name and path are required');
      return;
    }
    if (newTargetVMFilter === 'include' && newTargetVMIDs.length === 0) {
      toast.error('Pick at least one VM, or switch the selector to "All VMs"');
      return;
    }
    if (editSaving) return;
    editSaving = true;
    try {
      const payload = {
        name: newTargetName.trim(),
        type: newTargetType,
        path: newTargetPath.trim(),
        vm_filter: newTargetVMFilter,
        vm_ids: newTargetVMIDs,
        enabled: newTargetEnabled,
      };
      if (editingTarget) {
        // The "default" target's path is pinned server-side; strip
        // it from the payload to avoid a 400 "cannot change the
        // path of the default target". The Input itself is also
        // disabled, but defence in depth.
        if (editingTarget.id === 'default') delete payload.path;
        await api.updateBackupTarget(editingTarget.id, payload);
        resetAddTarget();
        await load();
        toast.success('Target updated');
      } else {
        // New targets are always enabled server-side; drop the
        // flag so the create request stays minimal.
        delete payload.enabled;
        await api.createBackupTarget(payload);
        resetAddTarget();
        await load();
        toast.success('Target added');
      }
    } catch (err) {
      toast.error(err.message);
    } finally {
      editSaving = false;
    }
  }

  // editTarget pre-fills the form with the values of the chosen
  // target and reopens the same dialog. Called by the per-card
  // "Edit" button; not visible on the default target's card.
  function editTarget(t) {
    editingTarget = t;
    newTargetName = t.name || '';
    newTargetType = t.type || 'local';
    newTargetPath = t.path || '';
    newTargetVMFilter = t.vm_filter || 'all';
    newTargetVMIDs = Array.isArray(t.vm_ids) ? [...t.vm_ids] : [];
    newTargetEnabled = t.enabled !== false; // default to enabled
    vmSearch = '';
    showAddTarget = true;
  }

  function resetAddTarget() {
    showAddTarget = false;
    editingTarget = null;
    newTargetName = '';
    newTargetPath = '';
    newTargetType = 'local';
    newTargetVMFilter = 'all';
    newTargetVMIDs = [];
    newTargetEnabled = true;
    vmSearch = '';
  }

  // resetAddTarget covers the Confirm + Cancel paths, but the X
  // button and ESC key close the dialog through the underlying
  // bits-ui Dialog without firing onCancel. Watch the open flag
  // and clear the form on the open→closed transition so the next
  // "Add target" click doesn't inherit the previous values.
  let prevAddTargetOpen = false;
  $effect(() => {
    if (prevAddTargetOpen && !showAddTarget) {
      editingTarget = null;
      newTargetName = '';
      newTargetPath = '';
      newTargetType = 'local';
      newTargetVMFilter = 'all';
      newTargetVMIDs = [];
      newTargetEnabled = true;
      vmSearch = '';
    }
    prevAddTargetOpen = showAddTarget;
  });

  async function deleteTarget(t) {
    confirmDeleteTarget = t;
  }

  async function doDeleteTarget() {
    const t = confirmDeleteTarget;
    confirmDeleteTarget = null;
    try {
      await api.deleteBackupTarget(t.id);
      await load();
      toast.success('Target removed');
    } catch (err) {
      toast.error(err.message);
    }
  }

  async function addSchedule() {
    if (!newScheduleName.trim() || !newScheduleCron.trim() || !newScheduleTarget) {
      toast.error('Name, cron, and target are required');
      return;
    }
    try {
      await api.createBackupSchedule({
        name: newScheduleName.trim(),
        cron: newScheduleCron.trim(),
        target_id: newScheduleTarget,
      });
      showAddSchedule = false;
      newScheduleName = '';
      newScheduleCron = '0 2 * * *';
      newScheduleTarget = '';
      await load();
      toast.success('Schedule added');
    } catch (err) {
      toast.error(err.message);
    }
  }

  async function deleteSchedule(s) {
    confirmDeleteSchedule = s;
  }

  async function doDeleteSchedule() {
    const s = confirmDeleteSchedule;
    confirmDeleteSchedule = null;
    try {
      await api.deleteBackupSchedule(s.id);
      await load();
      toast.success('Schedule removed');
    } catch (err) {
      toast.error(err.message);
    }
  }

  async function toggleSchedule(s) {
    try {
      await api.updateBackupSchedule(s.id, { enabled: !s.enabled });
      await load();
    } catch (err) {
      toast.error(err.message);
    }
  }

  async function runBackup(t) {
    runningBackups = { ...runningBackups, [t.id]: true };
    try {
      await api.backupNow(t.id);
      toast.success('Backup started');
      await load();
      // If the user is on the Files tab, refresh that target's
      // list so the new archive shows up without a manual reload.
      if (activeTab === 'files') {
        await loadFiles(t.id);
      }
    } catch (err) {
      toast.error(err.message);
    } finally {
      runningBackups = { ...runningBackups, [t.id]: false };
    }
  }

  // loadFiles fetches the per-target archive list. Called lazily
  // on first visit to the Files tab and again after a backup.
  async function loadFiles(targetId) {
    filesLoading = { ...filesLoading, [targetId]: true };
    try {
      const r = await api.listBackupsOnTarget(targetId);
      filesByTarget = { ...filesByTarget, [targetId]: r.backups || [] };
    } catch (err) {
      toast.error(`Failed to list files for target: ${err.message}`);
    } finally {
      filesLoading = { ...filesLoading, [targetId]: false };
    }
  }

  // verifyFile is a one-shot sha256 read; we don't keep the result
  // around, just toast it. If the user wants to inspect the hash
  // they can re-click or use the (future) dedicated detail view.
  let verifying = $state({}); // `${targetId}/${filename}` -> bool

  async function verifyFile(targetId, filename) {
    const key = `${targetId}/${filename}`;
    verifying = { ...verifying, [key]: true };
    try {
      const r = await api.verifyBackup(targetId, filename);
      toast.success(`OK · ${filename} · sha256 ${r.sha256.slice(0, 16)}…`);
    } catch (err) {
      toast.error(err.message);
    } finally {
      verifying = { ...verifying, [key]: false };
    }
  }

  function askDeleteFile(targetId, filename) {
    confirmDeleteFile = { targetId, filename };
  }

  async function doDeleteFile() {
    const { targetId, filename } = confirmDeleteFile;
    confirmDeleteFile = null;
    try {
      await api.deleteBackupFile(targetId, filename);
      await loadFiles(targetId);
      toast.success(`Deleted ${filename}`);
    } catch (err) {
      toast.error(err.message);
    }
  }

  // restoreForm is the per-file Restore-as-VM dialog state.
  // - target/filename: which archive
  // - name: what to call the new VM (default = filename stem)
  // - pool: which storage pool to put the disk in (default =
  //   vmmanager-disks or the first disk-purpose pool available)
  // - loading: disable the submit button while the API call
  //   is in flight (multi-GB imports can take a while)
  let restoreForm = $state(null);
  let restoreLoading = $state(false);
  let diskPools = $state([]);

  // Load disk pools once so the dialog's pool picker has options
  // without hitting the API every time the operator opens it.
  onMount(async () => {
    try {
      const all = (await api.listPools()) || [];
      diskPools = all.filter((p) => p.purpose !== 'iso');
      if (diskPools.length > 0 && !restoreForm?.pool) {
        const def = diskPools.find((p) => p.name === 'vmmanager-disks') || diskPools[0];
        if (restoreForm) restoreForm.pool = def.name;
      }
    } catch {
      // If the pool list fails the dialog will show an empty
      // picker; the backend will reject with a 500 if the
      // operator picks a non-existent one.
    }
  });

  function openRestoreDialog(target, filename) {
    // Derive a sensible default name from the filename.
    // Per-VM tar: vmmanager-...-<uuid>.tar.zst → take the uuid
    // prefix up to ".tar.zst".
    // Config tar: vmmanager-...-config.tar.zst → "config-restored"
    let defaultName = filename.replace(/\.tar\.(gz|zst)$/, '');
    const m = defaultName.match(/-([0-9a-f-]{36})$/i);
    if (m) {
      defaultName = `restored-${m[1].slice(0, 8)}`;
    } else if (defaultName.endsWith('-config')) {
      defaultName = defaultName.replace(/-config$/, '') + '-restored';
    }
    const defPool = diskPools.find((p) => p.name === 'vmmanager-disks') || diskPools[0];
    restoreForm = {
      target,
      filename,
      name: defaultName,
      pool: defPool?.name || 'vmmanager-disks',
    };
  }

  function closeRestoreDialog() {
    if (restoreLoading) return;
    restoreForm = null;
  }

  async function submitRestore() {
    if (!restoreForm || restoreLoading) return;
    if (!restoreForm.name.trim()) {
      toast.error('VM name is required');
      return;
    }
    restoreLoading = true;
    try {
      const r = await api.restoreAsVM(restoreForm.target.id, {
        filename: restoreForm.filename,
        name: restoreForm.name.trim(),
        pool: restoreForm.pool,
      });
      toast.success(`VM '${r.name}' restored, id ${r.id.slice(0, 8)}…`);
      restoreForm = null;
      // Reload the VMs list and jump to the new VM's detail
      // page so the operator can see it / start it.
      navigate('/vms/' + r.id);
    } catch (err) {
      toast.error(err.message || 'Restore failed');
    } finally {
      restoreLoading = false;
    }
  }

  // --- Formatters -----------------------------------------------------
  function fmtBytes(n) {
    if (!n) return '0 B';
    const k = 1024;
    const units = ['B', 'KB', 'MB', 'GB'];
    const i = Math.min(Math.floor(Math.log(n) / Math.log(k)), units.length - 1);
    return `${(n / Math.pow(k, i)).toFixed(1)} ${units[i]}`;
  }

  function fmtDate(iso) {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  // vmFilterSummary returns the human description of a target's
  // VM filter. Used both in the card and in the dialog label.
  function vmFilterSummary(t) {
    const filter = t.vm_filter || 'all';
    if (filter === 'all' || !t.vm_ids || t.vm_ids.length === 0) {
      return 'All VMs';
    }
    const names = t.vm_ids.map((id) => vms.find((v) => v.id === id)?.name || id).join(', ');
    if (filter === 'include') {
      return `${t.vm_ids.length} VM${t.vm_ids.length === 1 ? '' : 's'}: ${names}`;
    }
    return `All except ${t.vm_ids.length}: ${names}`;
  }

  // --- Tab-change handler --------------------------------------------
  // The Files tab lazily loads the per-target archive list the
  // first time the user visits it. We don't fire this from
  // activeTab= change because reactive blocks shouldn't make HTTP
  // calls; instead we hook into the onMount of the Files view
  // section.
  let filesTabVisited = $state(false);
  $effect(() => {
    if (activeTab === 'files' && !filesTabVisited) {
      filesTabVisited = true;
      for (const t of targets) {
        if (!filesByTarget[t.id]) loadFiles(t.id);
      }
    }
  });
</script>

<div class="p-6 max-w-5xl">
  <PageHeader
    title="Backup"
    subtitle="Manual cleanup only. Archives accumulate on disk; delete them from the Files tab whenever you need the space back. Schedules use a visual cron picker; each target picks which VMs to back up."
  >
    {#snippet actions()}
      {#if activeTab === 'targets'}
        <Button onclick={() => (showAddTarget = true)}>Add target</Button>
      {:else if activeTab === 'schedules'}
        <Button onclick={() => (showAddSchedule = true)}>Add schedule</Button>
      {/if}
    {/snippet}
  </PageHeader>

  <div class="flex gap-1 mb-4 border-b border-border">
    <button
      class="px-3 py-2 text-sm font-medium border-b-2 transition-colors {activeTab === 'targets'
        ? 'border-accent text-foreground'
        : 'border-transparent text-muted-foreground hover:text-foreground'}"
      onclick={() => (activeTab = 'targets')}
    >
      Targets ({targets.length})
    </button>
    <button
      class="px-3 py-2 text-sm font-medium border-b-2 transition-colors {activeTab === 'files'
        ? 'border-accent text-foreground'
        : 'border-transparent text-muted-foreground hover:text-foreground'}"
      onclick={() => (activeTab = 'files')}
    >
      Files
    </button>
    <button
      class="px-3 py-2 text-sm font-medium border-b-2 transition-colors {activeTab === 'schedules'
        ? 'border-accent text-foreground'
        : 'border-transparent text-muted-foreground hover:text-foreground'}"
      onclick={() => (activeTab = 'schedules')}
    >
      Schedules ({schedules.length})
    </button>
    <button
      class="px-3 py-2 text-sm font-medium border-b-2 transition-colors {activeTab === 'jobs'
        ? 'border-accent text-foreground'
        : 'border-transparent text-muted-foreground hover:text-foreground'}"
      onclick={() => (activeTab = 'jobs')}
    >
      Jobs ({jobs.length})
    </button>
  </div>

  {#if loading}
    <p class="text-sm text-muted-foreground">Loading…</p>
  {:else if activeTab === 'targets'}
    <div class="space-y-3">
      {#each targets as t (t.id)}
        <div class="border border-border rounded-lg bg-card p-4">
          <div class="flex items-center justify-between mb-2">
            <div class="flex items-center gap-2">
              <span class="font-medium">{t.name}</span>
              <span
                class="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground uppercase"
                >{t.type}</span
              >
              {#if !t.enabled}
                <span class="text-[10px] px-1.5 py-0.5 rounded bg-destructive/10 text-destructive"
                  >disabled</span
                >
              {/if}
            </div>
            <div class="flex gap-1">
              <Button size="xs" onclick={() => runBackup(t)} disabled={runningBackups[t.id]}>
                {runningBackups[t.id] ? 'Running…' : 'Backup now'}
              </Button>
              {#if t.id !== 'default'}
                <Button size="xs" variant="outline" onclick={() => editTarget(t)}>Edit</Button>
                <Button size="xs" variant="destructive" onclick={() => deleteTarget(t)}>
                  Remove
                </Button>
              {/if}
            </div>
          </div>
          <p class="text-xs text-muted-foreground font-mono mb-2">{t.path}</p>
          <p class="text-xs text-muted-foreground">
            <span class="font-medium text-foreground/70">Backs up:</span>
            {vmFilterSummary(t)}
          </p>
        </div>
      {/each}
    </div>
  {:else if activeTab === 'files'}
    {#if targets.length === 0}
      <p class="text-sm text-muted-foreground">No targets yet. Add one from the Targets tab.</p>
    {:else}
      <div class="space-y-4">
        {#each targets as t (t.id)}
          {@const files = filesByTarget[t.id]}
          <div class="border border-border rounded-lg bg-card p-4">
            <div class="flex items-center justify-between mb-3">
              <div>
                <div class="font-medium">{t.name}</div>
                <p class="text-xs text-muted-foreground font-mono">{t.path}</p>
              </div>
              <Button
                size="xs"
                variant="outline"
                onclick={() => loadFiles(t.id)}
                disabled={filesLoading[t.id]}
              >
                {filesLoading[t.id] ? 'Refreshing…' : 'Refresh'}
              </Button>
            </div>
            {#if filesLoading[t.id] && !files}
              <p class="text-sm text-muted-foreground">Loading…</p>
            {:else if !files || files.length === 0}
              <p class="text-sm text-muted-foreground">No backup files yet.</p>
            {:else}
              <div class="space-y-1.5">
                {#each files as f (f.filename)}
                  <div
                    class="flex items-center justify-between gap-2 py-1.5 px-2 rounded bg-muted/30"
                  >
                    <div class="min-w-0 flex-1">
                      <div class="font-mono text-sm truncate">{f.filename}</div>
                      <div class="text-xs text-muted-foreground">
                        {fmtBytes(f.size)} · {fmtDate(f.modified)}
                      </div>
                    </div>
                    <div class="flex gap-1 shrink-0">
                      <Button
                        size="xs"
                        variant="outline"
                        onclick={() => verifyFile(t.id, f.filename)}
                        disabled={verifying[`${t.id}/${f.filename}`]}
                      >
                        {verifying[`${t.id}/${f.filename}`] ? '…' : 'Verify'}
                      </Button>
                      <Button
                        size="xs"
                        variant="outline"
                        onclick={() => openRestoreDialog(t, f.filename)}
                      >
                        Restore
                      </Button>
                      <Button
                        size="xs"
                        variant="outline"
                        onclick={() => askDeleteFile(t.id, f.filename)}
                      >
                        Delete
                      </Button>
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  {:else if activeTab === 'schedules'}
    <div class="space-y-3">
      {#each schedules as s (s.id)}
        <div class="border border-border rounded-lg bg-card p-4 flex items-center justify-between">
          <div>
            <div class="flex items-center gap-2">
              <span class="font-medium">{s.name}</span>
              {#if !s.enabled}
                <span class="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground"
                  >disabled</span
                >
              {/if}
              {#if s.last_status === 'success'}
                <span class="text-[10px] px-1.5 py-0.5 rounded bg-success/10 text-success"
                  >last ok</span
                >
              {:else if s.last_status === 'error'}
                <span class="text-[10px] px-1.5 py-0.5 rounded bg-destructive/10 text-destructive"
                  >last failed</span
                >
              {/if}
            </div>
            <p class="text-xs text-muted-foreground mt-1">
              <span class="font-mono">{s.cron}</span> · Target:
              <span class="font-mono">{s.target_id}</span> · Last run: {fmtDate(s.last_run_at)}
            </p>
            {#if s.last_error}
              <p class="text-xs text-destructive mt-1">{s.last_error}</p>
            {/if}
          </div>
          <div class="flex gap-1">
            <Button size="xs" variant="outline" onclick={() => toggleSchedule(s)}>
              {s.enabled ? 'Disable' : 'Enable'}
            </Button>
            <Button size="xs" variant="destructive" onclick={() => deleteSchedule(s)}>Remove</Button
            >
          </div>
        </div>
      {/each}
    </div>
  {:else if activeTab === 'jobs'}
    <div class="space-y-1">
      {#each jobs as j (j.id)}
        <div
          class="border border-border rounded-lg bg-card p-3 flex items-center justify-between text-sm"
        >
          <div class="flex items-center gap-3">
            <span
              class="w-2 h-2 rounded-full {j.status === 'success'
                ? 'bg-success'
                : j.status === 'error'
                  ? 'bg-destructive'
                  : 'bg-warning'}"
            ></span>
            <div>
              <div class="font-medium">
                {j.schedule_id ? `Schedule: ${j.schedule_id}` : 'Manual'}
                · target <span class="font-mono text-xs">{j.target_id}</span>
              </div>
              <div class="text-xs text-muted-foreground">
                {fmtDate(j.started_at)}{j.ended_at ? ` → ${fmtDate(j.ended_at)}` : ''}
                {#if j.filename}· {j.filename} ({fmtBytes(j.size)}){/if}
              </div>
              {#if j.error}
                <div class="text-xs text-destructive mt-1">{j.error}</div>
              {/if}
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<ConfirmDialog
  open={showAddTarget}
  title={editingTarget ? 'Edit backup target' : 'Add backup target'}
  message={editingTarget
    ? 'Update the target name, VM filter, or disabled state. The default target has its path pinned by the backend.'
    : 'Targets are directories (local or mounted NFS/SMB) the backend writes archives to. No retention is applied — you delete archives by hand from the Files tab.'}
  confirmLabel={editingTarget ? 'Save' : 'Add'}
  onConfirm={addTarget}
  onCancel={resetAddTarget}
>
  <div class="space-y-3">
    <div>
      <label class="text-sm font-medium block mb-1" for="add-tgt-name">Name</label>
      <Input id="add-tgt-name" bind:value={newTargetName} placeholder="e.g. nightly-nfs" />
    </div>
    <div>
      <label class="text-sm font-medium block mb-1" for="add-tgt-type">Type</label>
      <select
        id="add-tgt-type"
        bind:value={newTargetType}
        class="w-full h-8 rounded-lg border border-border bg-background px-2 text-sm"
      >
        <option value="local">Local directory</option>
        <option value="nfs">NFS (mounted path)</option>
        <option value="smb">SMB / CIFS (mounted path)</option>
      </select>
    </div>
    <div>
      <label class="text-sm font-medium block mb-1" for="add-tgt-path">
        Path
        {#if editingTarget && editingTarget.id === 'default'}
          <span class="text-xs text-muted-foreground font-normal">— pinned by backend</span>
        {/if}
      </label>
      <Input
        id="add-tgt-path"
        bind:value={newTargetPath}
        placeholder="/mnt/backups"
        class="font-mono"
        disabled={!!(editingTarget && editingTarget.id === 'default')}
      />
    </div>

    <!--
      VM selector: three radios, plus a conditional checkbox list
      with a search box. The list is bound to newTargetVMIDs as a
      Set-like array; toggling a checkbox adds or removes its VM ID.

      The checkboxes are native <input type=checkbox> rather than
      the bespoke Checkbox.svelte wrapper because that component
      only exposes bind:checked — it has no `onchange` prop, so
      imperative mutations on newTargetVMIDs wouldn't propagate.
    -->
    <div>
      <div class="text-sm font-medium mb-1">VMs</div>
      <div class="flex flex-wrap gap-3 mb-2">
        <label class="flex items-center gap-1.5 text-sm cursor-pointer">
          <input
            type="radio"
            name="vm-filter"
            value="all"
            checked={newTargetVMFilter === 'all'}
            onchange={() => {
              newTargetVMFilter = 'all';
              newTargetVMIDs = [];
            }}
            class="accent-accent"
          />
          All VMs
        </label>
        <label class="flex items-center gap-1.5 text-sm cursor-pointer">
          <input
            type="radio"
            name="vm-filter"
            value="include"
            checked={newTargetVMFilter === 'include'}
            onchange={() => (newTargetVMFilter = 'include')}
            class="accent-accent"
          />
          Selected only
        </label>
        <label class="flex items-center gap-1.5 text-sm cursor-pointer">
          <input
            type="radio"
            name="vm-filter"
            value="exclude"
            checked={newTargetVMFilter === 'exclude'}
            onchange={() => (newTargetVMFilter = 'exclude')}
            class="accent-accent"
          />
          All except
        </label>
      </div>
      {#if newTargetVMFilter !== 'all'}
        <Input bind:value={vmSearch} placeholder="Search VMs…" class="mb-2" />
        <div
          class="border border-border rounded-md bg-background max-h-48 overflow-y-auto p-1 space-y-0.5"
        >
          {#each filteredVMs as vm (vm.id)}
            <label
              class="flex items-start gap-2 px-2 py-1.5 rounded cursor-pointer hover:bg-muted/40"
            >
              <input
                type="checkbox"
                checked={newTargetVMIDs.includes(vm.id)}
                onchange={(e) => {
                  const c = e.currentTarget.checked;
                  if (c) {
                    if (!newTargetVMIDs.includes(vm.id)) {
                      newTargetVMIDs = [...newTargetVMIDs, vm.id];
                    }
                  } else {
                    newTargetVMIDs = newTargetVMIDs.filter((id) => id !== vm.id);
                  }
                }}
                class="mt-0.5 h-4 w-4 rounded border-border bg-background text-accent focus:ring-2 focus:ring-accent/40 accent-accent"
              />
              <span class="flex-1 min-w-0">
                <span class="text-sm font-medium block leading-tight">{vm.name}</span>
                <span class="text-xs text-muted-foreground block">{vm.state}</span>
              </span>
            </label>
          {:else}
            <p class="text-sm text-muted-foreground px-2 py-3 text-center">
              {vmSearch ? 'No VMs match' : 'No VMs found'}
            </p>
          {/each}
        </div>
        <p class="text-xs text-muted-foreground mt-1">
          {newTargetVMIDs.length} selected
        </p>
      {/if}
    </div>

    {#if editingTarget}
      <div class="pt-1 border-t border-border">
        <Switch
          bind:checked={newTargetEnabled}
          label="Enabled"
          description="Disabled targets stay registered but no backups run against them — schedules skip them and Backup now is blocked."
        />
      </div>
    {/if}
  </div>
</ConfirmDialog>

<ConfirmDialog
  open={showAddSchedule}
  title="Add backup schedule"
  message="Schedules fire a backup against the chosen target. Pick a preset or open Custom for fine-grained control."
  confirmLabel="Add"
  onConfirm={addSchedule}
  onCancel={() => (showAddSchedule = false)}
>
  <div class="space-y-3">
    <div>
      <label class="text-sm font-medium block mb-1" for="add-sched-name">Name</label>
      <Input id="add-sched-name" bind:value={newScheduleName} placeholder="e.g. nightly" />
    </div>
    <div>
      <label class="text-sm font-medium block mb-1" for="add-sched-cron">Schedule</label>
      <CronPicker bind:expression={newScheduleCron} />
    </div>
    <div>
      <label class="text-sm font-medium block mb-1" for="add-sched-target">Target</label>
      <select
        id="add-sched-target"
        bind:value={newScheduleTarget}
        class="w-full h-8 rounded-lg border border-border bg-background px-2 text-sm"
      >
        <option value="">— pick a target —</option>
        {#each targets as t}
          <option value={t.id}>{t.name} ({t.type})</option>
        {/each}
      </select>
    </div>
  </div>
</ConfirmDialog>

<ConfirmDialog
  open={!!confirmDeleteTarget}
  title="Remove target?"
  message={confirmDeleteTarget
    ? `"${confirmDeleteTarget.name}" will be unregistered. Existing backup files on disk are NOT deleted. Schedules pointing at it will fail.`
    : ''}
  confirmLabel="Remove"
  onConfirm={doDeleteTarget}
  onCancel={() => (confirmDeleteTarget = null)}
/>

<ConfirmDialog
  open={!!confirmDeleteSchedule}
  title="Remove schedule?"
  message={confirmDeleteSchedule
    ? `"${confirmDeleteSchedule.name}" will be removed. No further backups will fire on its cron.`
    : ''}
  confirmLabel="Remove"
  onConfirm={doDeleteSchedule}
  onCancel={() => (confirmDeleteSchedule = null)}
/>

<ConfirmDialog
  open={!!confirmDeleteFile}
  title="Delete backup file?"
  message={confirmDeleteFile
    ? `"${confirmDeleteFile.filename}" will be permanently deleted from disk. This cannot be undone.`
    : ''}
  confirmLabel="Delete"
  onConfirm={doDeleteFile}
  onCancel={() => (confirmDeleteFile = null)}
/>

<!--
  Restore-as-VM dialog. Replaces the old extract-only Restore
  flow: the operator picks a name + pool, the backend imports
  the archive as a new libvirt domain, and we navigate to the
  new VM's detail page on success.
-->
<Dialog.Root open={!!restoreForm} onOpenChange={(v) => !v && closeRestoreDialog()}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Restore as VM</Dialog.Title>
      <Dialog.Description>
        Create a new VM from this backup archive. The source archive is left in place; the new VM
        gets its own UUID.
      </Dialog.Description>
    </Dialog.Header>
    {#if restoreForm}
      <div class="space-y-3">
        <div class="text-xs text-muted-foreground break-all">
          Source: <span class="font-mono">{restoreForm.filename}</span>
        </div>
        <div class="space-y-1.5">
          <Label for="restore-name">VM name</Label>
          <Input
            id="restore-name"
            bind:value={restoreForm.name}
            placeholder="ubuntu-1-restored"
            disabled={restoreLoading}
            onkeydown={(e) => e.key === 'Enter' && submitRestore()}
          />
          <p class="text-xs text-muted-foreground">
            Letters, digits, dashes, underscores. Must be unique on this host.
          </p>
        </div>
        <div class="space-y-1.5">
          <Label for="restore-pool">Storage pool</Label>
          <select
            id="restore-pool"
            bind:value={restoreForm.pool}
            disabled={restoreLoading}
            class="input w-full"
          >
            {#each diskPools as p (p.name)}
              <option value={p.name}>{p.name} ({p.purpose || 'disk'})</option>
            {/each}
            {#if diskPools.length === 0}
              <option value="vmmanager-disks">vmmanager-disks</option>
            {/if}
          </select>
        </div>
      </div>
    {/if}
    <Dialog.Footer class="gap-2">
      <Button variant="outline" onclick={closeRestoreDialog} disabled={restoreLoading}>
        Cancel
      </Button>
      <Button onclick={submitRestore} disabled={restoreLoading || !restoreForm}>
        {#if restoreLoading}
          <Loader2 class="h-3.5 w-3.5 mr-1.5 animate-spin" />
          Restoring…
        {:else}
          Restore as VM
        {/if}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
