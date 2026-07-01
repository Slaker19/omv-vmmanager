<script>
  import SearchInput from '$lib/components/SearchInput.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Alert from '$lib/components/Alert.svelte';
  import Spinner from '$lib/components/Spinner.svelte';
  import { onMount } from 'svelte';
  import { api } from '$lib/stores/auth.svelte.js';
  import { events } from '$lib/stores/events.svelte.js';
  import { getRoute, navigate } from '$lib/router.svelte.js';
  import { auth } from '$lib/stores/auth.svelte.js';
  import { toast } from '$lib/components/ui/toast';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import * as Dialog from '$lib/components/ui/dialog';
  import Chart from '$lib/components/Chart.svelte';

  let vms = $state([]);
  let loading = $state(true);
  let error = $state('');

  // Read initial filter state from URL query.
  const route = $derived(getRoute());
  function readQuery(key) {
    return route.query?.[key] ?? '';
  }
  let search = $state(readQuery('q'));
  let groupFilter = $state(readQuery('group') || 'all');
  let stateFilter = $state(readQuery('state') || 'all');

  // Sync filters → URL.
  $effect(() => {
    const q = new URLSearchParams();
    if (search) q.set('q', search);
    if (groupFilter && groupFilter !== 'all') q.set('group', groupFilter);
    if (stateFilter && stateFilter !== 'all') q.set('state', stateFilter);
    if (selectMode) q.set('select', '1');
    const target = '/vms' + (q.toString() ? '?' + q.toString() : '');
    if (typeof location !== 'undefined' && location.hash !== '#' + target) {
      history.replaceState(null, '', '#' + target);
    }
  });

  // Bulk selection (Phase D, reworked in Phase H: grid-only with
  // explicit select-mode toggle). When selectMode is false, clicking
  // a card navigates to the VM; when true, clicking toggles its
  // membership in selectedKeys. The toggle lives in the PageHeader
  // and is the only way to enter select mode (no mouse-only affordance).
  let selectMode = $state(readQuery('select') === '1');
  let selectedKeys = $state(new Set());

  // Auto-exit select mode when the selection is cleared, so the UI
  // doesn't stay in "bulk" mode after the user is done.
  $effect(() => {
    if (!selectMode && selectedKeys.size === 0) return;
    if (selectedKeys.size === 0) selectMode = false;
  });

  let groups = $state([]);
  let showManageGroups = $state(false);
  let newGroupName = $state('');
  let newGroupColor = $state('#7c3aed');
  let mgSaving = $state(false);
  let mgError = $state('');

  const palette = [
    '#7c3aed',
    '#3b82f6',
    '#10b981',
    '#f59e0b',
    '#ef4444',
    '#ec4899',
    '#06b6d4',
    '#84cc16',
  ];

  // Confirm dialog state
  let confirmDeleteOpen = $state(false);
  let confirmDeleteVm = $state(null);
  let confirmDeleteLoading = $state(false);

  // Bulk confirm dialog
  let confirmBulkOpen = $state(false);
  let confirmBulkAction = $state(''); // 'start' | 'shutdown' | 'forceoff' | 'delete'
  let confirmBulkLoading = $state(false);

  // Bulk tag dialog
  let showBulkTag = $state(false);
  let bulkTagName = $state('');

  // Import modal state
  let showImport = $state(false);
  let importName = $state('');
  let importPool = $state('vmmanager-disks');
  let importFile = $state(null);
  let importing = $state(false);
  let importProgress = $state(0);
  let importPhase = $state('');
  let importError = $state('');
  let pools = $state([]);

  // Derived filtered list (search AND group filter AND state filter).
  const filteredVms = $derived.by(() => {
    const q = search.toLowerCase().trim();
    let out = vms;
    if (groupFilter !== 'all') {
      out = out.filter((v) => Array.isArray(v.groups) && v.groups.includes(groupFilter));
    }
    if (stateFilter !== 'all') {
      out = out.filter((v) => v.state === stateFilter);
    }
    if (q) {
      out = out.filter(
        (v) =>
          v.name.toLowerCase().includes(q) ||
          (v.alias && v.alias.toLowerCase().includes(q)) ||
          (v.ip && v.ip.includes(q))
      );
    }
    return out;
  });

  // Selection should clear when the filtered list changes.
  $effect(() => {
    // Re-derive when filtered set changes.
    void filteredVms;
    const valid = new Set(filteredVms.map((v) => v.id));
    let changed = false;
    const next = new Set();
    for (const k of selectedKeys) {
      if (valid.has(k)) next.add(k);
      else changed = true;
    }
    if (changed) selectedKeys = next;
  });

  onMount(() => {
    loadVMs();
    loadGroups();
    // Subscribe to VM state events for realtime updates
    const off = events.onVmState((e) => {
      const idx = vms.findIndex((v) => v.id === e.vm_id);
      if (idx >= 0) {
        const prev = vms[idx];
        if (prev.state !== e.state) {
          vms = vms.map((v) =>
            v.id === e.vm_id ? { ...v, state: e.state, name: e.name || v.name } : v
          );
          if (e.state === 'running') loadSparklines();
        }
      }
    });
    // Subscribe to metrics for live sparkline updates.
    const offMetrics = events.onVmMetrics((e) => {
      metricsByVm = { ...metricsByVm, [e.vm_id]: e.data };
    });
    return () => {
      off();
      offMetrics();
    };
  });

  async function loadVMs() {
    loading = true;
    error = '';
    try {
      vms = await api.listVMs();
      // Fire-and-forget sparkline load; don't block the table render.
      loadSparklines();
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function loadGroups() {
    try {
      const res = await api.listGroups();
      groups = res.groups || [];
    } catch {
      groups = [];
    }
  }

  // Per-VM metric series for sparklines (Phase 21). Keyed by VM id.
  let metricsByVm = $state({});

  async function loadSparklines() {
    // Only request for VMs that are running; others stay empty (no chart).
    const running = vms.filter((v) => v.state === 'running');
    const updates = {};
    await Promise.all(
      running.map(async (v) => {
        try {
          const m = await api.getVMMetrics(v.id);
          updates[v.id] = m;
        } catch {
          // Don't fail the whole load on one VM.
        }
      })
    );
    metricsByVm = { ...metricsByVm, ...updates };
  }

  const last30 = (arr) => (Array.isArray(arr) ? arr.slice(-30) : []);

  async function openManageGroups() {
    mgError = '';
    newGroupName = '';
    newGroupColor = palette[0];
    await loadGroups();
    showManageGroups = true;
  }

  async function createGroup() {
    if (!newGroupName.trim()) {
      mgError = 'Name is required';
      return;
    }
    mgSaving = true;
    mgError = '';
    try {
      await api.createGroup({ name: newGroupName.trim(), color: newGroupColor });
      newGroupName = '';
      await loadGroups();
    } catch (e) {
      mgError = e.message;
    } finally {
      mgSaving = false;
    }
  }

  async function updateGroupColor(g) {
    try {
      await api.updateGroup(g.name, { name: g.name, color: g.color });
      await loadGroups();
    } catch (e) {
      toast.error(e.message);
    }
  }

  async function deleteGroup(g) {
    try {
      await api.deleteGroup(g.name);
      if (groupFilter === g.name) groupFilter = 'all';
      await loadGroups();
      await loadVMs();
      toast.success(`Group "${g.name}" removed`);
    } catch (e) {
      toast.error(e.message);
    }
  }

  async function doDelete() {
    if (!confirmDeleteVm) return;
    confirmDeleteLoading = true;
    try {
      await api.deleteVM(confirmDeleteVm.id);
      toast.success(`VM "${confirmDeleteVm.name}" deleted`);
      confirmDeleteOpen = false;
      confirmDeleteVm = null;
      await loadVMs();
    } catch (e) {
      toast.error(e.message);
    } finally {
      confirmDeleteLoading = false;
    }
  }

  // ---- bulk actions ----
  function askBulk(action) {
    confirmBulkAction = action;
    confirmBulkOpen = true;
  }

  async function doBulk() {
    const ids = Array.from(selectedKeys);
    if (ids.length === 0) return;
    confirmBulkLoading = true;
    let succeeded = 0,
      failed = 0;
    try {
      for (const id of ids) {
        try {
          if (confirmBulkAction === 'start') await api.startVM(id);
          else if (confirmBulkAction === 'shutdown') await api.shutdownVM(id);
          else if (confirmBulkAction === 'forceoff') await api.forceOffVM(id);
          else if (confirmBulkAction === 'delete') await api.deleteVM(id);
          succeeded++;
        } catch (_) {
          failed++;
        }
      }
      const label = {
        start: 'started',
        shutdown: 'shut down',
        forceoff: 'force-offed',
        delete: 'deleted',
      }[confirmBulkAction];
      if (succeeded) toast.success(`${succeeded} VM${succeeded !== 1 ? 's' : ''} ${label}`);
      if (failed) toast.error(`${failed} failed`);
      confirmBulkOpen = false;
      selectedKeys = new Set();
      await loadVMs();
    } finally {
      confirmBulkLoading = false;
    }
  }

  async function doBulkTag() {
    if (!bulkTagName.trim() || selectedKeys.size === 0) return;
    const ids = Array.from(selectedKeys);
    let ok = 0,
      fail = 0;
    for (const id of ids) {
      try {
        const m = await api.getVMMeta(id);
        const groups = new Set(Array.isArray(m.groups) ? m.groups : []);
        groups.add(bulkTagName.trim());
        await api.updateVMMeta(id, { groups: Array.from(groups) });
        ok++;
      } catch (_) {
        fail++;
      }
    }
    if (ok) toast.success(`Tagged ${ok} VM${ok !== 1 ? 's' : ''} with "${bulkTagName}"`);
    if (fail) toast.error(`${fail} failed`);
    showBulkTag = false;
    bulkTagName = '';
    selectedKeys = new Set();
    await loadVMs();
  }

  function formatRAM(mb) {
    if (!mb) return '—';
    if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
    return `${mb} MB`;
  }

  const stateColors = {
    running: 'bg-status-running',
    shutoff: 'bg-status-shutoff',
    paused: 'bg-status-paused',
    crashed: 'bg-status-crashed',
  };

  // Import modal
  async function openImport() {
    showImport = true;
    importError = '';
    try {
      pools = ((await api.listPools()) || []).filter((p) => p.purpose !== 'iso');
      if (pools.length > 0 && !pools.find((p) => p.name === importPool)) {
        importPool = pools[0].name;
      }
    } catch (e) {
      importError = 'Could not load storage pools: ' + e.message;
    }
  }

  async function doImport() {
    if (!importFile) {
      importError = 'Pick a .tar.gz, .tar.zst or .ova file';
      return;
    }
    importing = true;
    importError = '';
    importProgress = 0;
    importPhase = 'Uploading...';
    try {
      const res = await api.importVM(importFile, importName, importPool, (pct) => {
        importProgress = pct;
        if (pct >= 100) importPhase = 'Processing on server...';
      });
      if (res && res.name) {
        if (res.requested_name && res.requested_name !== res.name) {
          toast.warning(`"${res.requested_name}" already existed — imported as "${res.name}"`);
        } else if (importName && importName !== res.name) {
          toast.warning(`Name conflict — imported as "${res.name}"`);
        } else {
          toast.success(`Imported as "${res.name}"`);
        }
        // Surface non-fatal server warnings (typically a
        // CDROM ISO that was not bundled with the archive,
        // so the VM is defined but cannot start until the
        // user uploads the ISO). These are sticky so the
        // operator actually notices them.
        if (Array.isArray(res.warnings) && res.warnings.length > 0) {
          for (const w of res.warnings) {
            toast.warning(w, { duration: 0 });
          }
        }
      } else {
        toast.success('VM imported');
      }
      showImport = false;
      importFile = null;
      importName = '';
      importProgress = 0;
      importPhase = '';
      await loadVMs();
    } catch (e) {
      importError = e.message;
    } finally {
      importing = false;
    }
  }
</script>

<div class="p-6 max-w-6xl">
  <PageHeader title="Virtual Machines" subtitle="{vms.length} machine{vms.length !== 1 ? 's' : ''}">
    {#snippet actions()}
      <button
        onclick={() => (selectMode = !selectMode)}
        class="px-3 h-8 inline-flex items-center gap-1.5 border rounded-md text-xs font-medium transition-colors {selectMode
          ? 'border-accent bg-accent/15 text-accent'
          : 'border-border text-muted-foreground hover:text-foreground hover:bg-muted'}"
        aria-pressed={selectMode}
        title="Toggle multi-select for bulk actions"
      >
        <svg
          class="w-3.5 h-3.5"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          viewBox="0 0 24 24"
        >
          <rect x="3" y="3" width="18" height="18" rx="2" />
          {#if selectMode}
            <polyline points="9 12 11 14 15 10" stroke-linecap="round" stroke-linejoin="round" />
          {/if}
        </svg>
        Select
      </button>
      <SearchInput bind:value={search} placeholder="Search by name, alias or IP..." class="w-64" />
      <Button variant="outline" onclick={openManageGroups}>Manage Groups</Button>
      <Button variant="outline" onclick={openImport}>Import VM</Button>
      <Button onclick={() => navigate('/vms/new')}>Create VM</Button>
    {/snippet}
  </PageHeader>

  {#if selectedKeys.size > 0}
    <div
      class="flex items-center justify-between gap-2 mb-3 px-3 py-2 border border-accent/30 bg-accent/10 rounded-md"
    >
      <span class="text-sm font-medium text-accent">
        {selectedKeys.size} selected
      </span>
      <div class="flex items-center gap-1.5">
        {#if auth.canMutate()}
          <Button size="sm" variant="outline" onclick={() => askBulk('start')}>Start</Button>
          <Button size="sm" variant="outline" onclick={() => askBulk('shutdown')}>Shutdown</Button>
          <Button size="sm" variant="outline" onclick={() => askBulk('forceoff')}>Force off</Button>
        {/if}
        <Button
          size="sm"
          variant="outline"
          onclick={() => (
            (showBulkTag = true),
            (bulkTagName = groupFilter !== 'all' ? groupFilter : '')
          )}>Tag with group</Button
        >
        {#if auth.isAdmin()}
          <Button size="sm" variant="destructive" onclick={() => askBulk('delete')}>Delete</Button>
        {/if}
        <button
          onclick={() => (selectedKeys = new Set())}
          class="text-xs text-muted-foreground hover:text-foreground px-2 py-1">Clear</button
        >
      </div>
    </div>
  {/if}

  {#if groups.length > 0 || stateFilter !== 'all'}
    <div class="flex items-center gap-1.5 flex-wrap mb-4">
      <button
        onclick={() => ((groupFilter = 'all'), (stateFilter = 'all'))}
        class="text-xs px-2.5 py-1 rounded-full border transition-colors {groupFilter === 'all' &&
        stateFilter === 'all'
          ? 'border-accent bg-accent/15 text-accent'
          : 'border-border text-muted-foreground hover:text-foreground hover:border-border-hover'}"
      >
        All <span class="text-[10px] opacity-60">({vms.length})</span>
      </button>
      {#each [{ v: 'running', c: 'bg-status-running', l: 'running' }, { v: 'shutoff', c: 'bg-status-shutoff', l: 'shutoff' }, { v: 'paused', c: 'bg-status-paused', l: 'paused' }, { v: 'crashed', c: 'bg-status-crashed', l: 'crashed' }] as s}
        <button
          onclick={() => (stateFilter = stateFilter === s.v ? 'all' : s.v)}
          class="text-xs px-2.5 py-1 rounded-full border transition-colors {stateFilter === s.v
            ? 'border-foreground text-foreground bg-muted'
            : 'border-border text-muted-foreground hover:text-foreground'}"
        >
          <span class="inline-block w-1.5 h-1.5 rounded-full mr-1.5 {s.c}"></span>
          {s.l}
          <span class="text-[10px] opacity-60">({vms.filter((v) => v.state === s.v).length})</span>
        </button>
      {/each}
      {#if groups.length > 0}
        <span class="text-xs text-muted-foreground mx-1">|</span>
        {#each groups as g}
          <button
            onclick={() => (groupFilter = groupFilter === g.name ? 'all' : g.name)}
            class="text-xs px-2.5 py-1 rounded-full border transition-colors {groupFilter === g.name
              ? 'border-foreground text-foreground'
              : 'border-border text-muted-foreground hover:text-foreground'}"
            style={groupFilter === g.name
              ? `background-color: ${g.color}25; border-color: ${g.color};`
              : ''}
          >
            <span
              class="inline-block w-1.5 h-1.5 rounded-full mr-1.5"
              style="background-color: {g.color}"
            ></span>
            {g.name} <span class="text-[10px] opacity-60">({g.member_count})</span>
          </button>
        {/each}
      {/if}
    </div>
  {/if}

  {#if error}
    <Alert variant="error">{error}</Alert>
  {/if}

  {#if loading}
    <div class="flex items-center justify-center py-24"><Spinner size="lg" /></div>
  {:else if filteredVms.length === 0}
    <div class="border border-border rounded-lg bg-card p-12 text-center">
      <svg
        class="w-12 h-12 mx-auto mb-3 text-muted-foreground/40"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        viewBox="0 0 24 24"
      >
        <rect x="3" y="4" width="18" height="12" rx="2" /><path d="M8 20h8M12 16v4" />
      </svg>
      <p class="text-muted-foreground text-sm">
        No virtual machines yet. Click 'Create VM' to get started.
      </p>
    </div>
  {:else}
    <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-4">
      {#each filteredVms as vm (vm.id)}
        {@const isSelected = selectedKeys.has(vm.id)}
        {@const metrics = metricsByVm[vm.id]}
        {@const cpuPts = last30(metrics?.cpu?.points)}
        {@const ramPts = last30(metrics?.ram?.points)}
        <button
          onclick={() => {
            if (selectMode) {
              const next = new Set(selectedKeys);
              if (next.has(vm.id)) next.delete(vm.id);
              else next.add(vm.id);
              selectedKeys = next;
            } else {
              navigate(`/vms/${vm.id}`);
            }
          }}
          aria-pressed={selectMode ? isSelected : undefined}
          class="group relative text-left border rounded-lg overflow-hidden bg-card transition-colors {(
            selectMode ? isSelected : false
          )
            ? 'border-accent ring-1 ring-accent/40'
            : 'border-border hover:border-border-hover'}"
        >
          <div
            class="aspect-video w-full bg-gradient-to-br from-muted to-background relative overflow-hidden"
          >
            {#if vm.cover}
              <img src={vm.cover} alt="" class="w-full h-full object-cover" />
            {:else}
              <div
                class="absolute inset-0 flex items-center justify-center text-5xl font-bold text-muted-foreground/30 select-none"
              >
                {(vm.alias || vm.name).charAt(0).toUpperCase()}
              </div>
            {/if}
            <div
              class="absolute top-2 left-2 inline-flex items-center gap-1.5 px-1.5 py-0.5 rounded bg-black/50 text-white text-[10px] uppercase tracking-wider backdrop-blur"
            >
              <span class="w-1.5 h-1.5 rounded-full {stateColors[vm.state] || stateColors.crashed}"
              ></span>
              {vm.state}
            </div>
            {#if selectMode}
              <div
                class="absolute top-2 right-2 w-5 h-5 rounded border-2 flex items-center justify-center transition-colors {isSelected
                  ? 'bg-accent border-accent'
                  : 'bg-black/40 border-white/70'}"
              >
                {#if isSelected}
                  <svg
                    class="w-3 h-3 text-white"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="3"
                    viewBox="0 0 24 24"
                    ><polyline
                      points="5 12 10 17 19 7"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    /></svg
                  >
                {/if}
              </div>
            {/if}
          </div>
          <div class="p-3 space-y-2">
            <div class="flex items-center justify-between gap-2">
              <div class="font-medium text-sm truncate min-w-0">{vm.alias || vm.name}</div>
              {#if vm.state === 'running' && vm.ip}
                <span class="font-mono text-[10px] text-accent shrink-0">{vm.ip}</span>
              {/if}
            </div>
            <div class="flex items-center justify-between text-xs text-muted-foreground tnum">
              <span>{vm.vcpus} vCPU · {formatRAM(vm.ram_mb)}</span>
            </div>
            {#if vm.state === 'running' && (cpuPts.length > 0 || ramPts.length > 0)}
              <div class="grid grid-cols-2 gap-2 pt-1.5 border-t border-border/50">
                <div>
                  <div
                    class="flex items-center justify-between text-[10px] text-muted-foreground tnum mb-0.5"
                  >
                    <span>CPU</span>
                    <span>{cpuPts.length ? cpuPts[cpuPts.length - 1].v.toFixed(0) : 0}%</span>
                  </div>
                  <Chart
                    points={cpuPts}
                    yMax={100}
                    width={80}
                    height={22}
                    strokeWidth={1}
                    fillOpacity={0.2}
                  />
                </div>
                <div>
                  <div
                    class="flex items-center justify-between text-[10px] text-muted-foreground tnum mb-0.5"
                  >
                    <span>RAM</span>
                    <span>{ramPts.length ? ramPts[ramPts.length - 1].v.toFixed(0) : 0}%</span>
                  </div>
                  <Chart
                    points={ramPts}
                    yMax={100}
                    width={80}
                    height={22}
                    strokeWidth={1}
                    fillOpacity={0.2}
                    color="var(--success)"
                  />
                </div>
              </div>
            {/if}
          </div>
        </button>
      {/each}
    </div>
  {/if}
</div>

<!-- Delete confirmation -->
<ConfirmDialog
  bind:open={confirmDeleteOpen}
  title="Delete VM?"
  description="This will permanently delete the VM configuration. Disks will NOT be removed."
  confirmLabel="Delete"
  variant="destructive"
  loading={confirmDeleteLoading}
  onConfirm={doDelete}
/>

<!-- Bulk action confirmation -->
<ConfirmDialog
  bind:open={confirmBulkOpen}
  title={confirmBulkAction === 'delete'
    ? `Delete ${selectedKeys.size} VMs?`
    : {
        start: `Start ${selectedKeys.size} VMs?`,
        shutdown: `Shutdown ${selectedKeys.size} VMs?`,
        forceoff: `Force off ${selectedKeys.size} VMs?`,
      }[confirmBulkAction] || 'Bulk action'}
  description={confirmBulkAction === 'delete'
    ? 'This will permanently delete the selected VM configurations. Disks will NOT be removed.'
    : {
        start: 'Only VMs that are currently shut off will start.',
        shutdown: 'Sends an ACPI shutdown request. Running VMs will stop gracefully.',
        forceoff: 'Pulls the power plug. Use when shutdown hangs.',
      }[confirmBulkAction] || ''}
  confirmLabel={confirmBulkAction === 'delete' ? 'Delete all' : 'Apply'}
  variant={confirmBulkAction === 'delete' ? 'destructive' : 'default'}
  loading={confirmBulkLoading}
  onConfirm={doBulk}
/>

<!-- Bulk tag dialog -->
<Dialog.Root open={showBulkTag} onOpenChange={(v) => (showBulkTag = v)}>
  <Dialog.Content>
    <Dialog.Header>
      <Dialog.Title>Tag {selectedKeys.size} VMs</Dialog.Title>
      <Dialog.Description>Add these VMs to a group. Existing tags are preserved.</Dialog.Description
      >
    </Dialog.Header>
    <div class="py-2 space-y-2">
      <label for="bulk-tag-name" class="text-sm font-medium">Group name</label>
      <Input id="bulk-tag-name" bind:value={bulkTagName} placeholder="e.g. production" />
      {#if groups.length > 0}
        <div class="flex flex-wrap gap-1">
          {#each groups as g}
            <button
              onclick={() => (bulkTagName = g.name)}
              type="button"
              class="text-xs px-2 py-0.5 rounded border"
              style="border-color: {g.color}40; color: {g.color}"
            >
              {g.name}
            </button>
          {/each}
        </div>
      {/if}
    </div>
    <Dialog.Footer>
      <Button variant="outline" onclick={() => (showBulkTag = false)}>Cancel</Button>
      <Button disabled={!bulkTagName.trim()} onclick={doBulkTag}>Tag</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Import dialog -->
<Dialog.Root bind:open={showImport}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Import VM from Backup</Dialog.Title>
      <Dialog.Description
        >Upload a .tar.gz, .tar.zst, or .ova file. The VM will be created in the selected storage
        pool.</Dialog.Description
      >
    </Dialog.Header>
    <div class="space-y-3">
      <div>
        <label for="import-file" class="block text-sm font-medium mb-1.5">Backup or OVA file</label>
        <Input
          id="import-file"
          type="file"
          accept=".tar.gz,.tgz,.tar.zst,.zst,.ova"
          onchange={(e) => (importFile = e.target.files?.[0] || null)}
        />
        {#if importFile}
          <p class="text-xs text-muted-foreground mt-1.5">
            {importFile.name} ({(importFile.size / 1024 / 1024).toFixed(1)} MB)
          </p>
        {/if}
      </div>
      <div>
        <label for="import-name" class="block text-sm font-medium mb-1.5"
          >New VM name (optional)</label
        >
        <Input
          id="import-name"
          bind:value={importName}
          placeholder="leave empty to keep original"
        />
      </div>
      <div>
        <label for="import-pool" class="block text-sm font-medium mb-1.5">Storage pool</label>
        <select
          id="import-pool"
          bind:value={importPool}
          class="input"
          disabled={pools.length === 0}
        >
          {#if pools.length === 0}<option value="vmmanager-disks">vmmanager-disks</option>{/if}
          {#each pools as p}<option value={p.name}>{p.name}</option>{/each}
        </select>
      </div>
      {#if importError}
        <p class="text-sm text-destructive">{importError}</p>
      {/if}
      {#if importing}
        <div class="bg-muted/30 rounded-md p-3 border border-border">
          <div class="flex justify-between text-sm text-muted-foreground mb-1.5">
            <span>{importPhase || 'Uploading...'}</span>
            <span class="tnum">{Math.round(importProgress)}%</span>
          </div>
          <div class="w-full h-1.5 bg-muted rounded-full overflow-hidden">
            <div
              class="h-full bg-accent transition-all duration-300"
              style="width: {importProgress}%"
            ></div>
          </div>
        </div>
      {/if}
    </div>
    <Dialog.Footer class="gap-2">
      <Button variant="outline" onclick={() => (showImport = false)} disabled={importing}
        >Cancel</Button
      >
      <Button onclick={doImport} disabled={importing || !importFile}>
        {#if importing}<Spinner size="sm" color="text-white" />{:else}Import{/if}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Manage Groups dialog -->
<Dialog.Root bind:open={showManageGroups}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Manage Groups</Dialog.Title>
      <Dialog.Description
        >Create and color groups. Assign them to VMs in the Identity & Notes dialog.</Dialog.Description
      >
    </Dialog.Header>

    <div class="space-y-3">
      <div class="border border-border rounded-md bg-background p-3 space-y-2">
        <div class="flex gap-2">
          <Input bind:value={newGroupName} placeholder="group-name" class="flex-1" />
          <Button onclick={createGroup} disabled={mgSaving || !newGroupName.trim()}>
            {#if mgSaving}<Spinner size="xs" color="text-white" />{:else}Add{/if}
          </Button>
        </div>
        <div class="flex items-center gap-1.5">
          <span class="text-xs text-muted-foreground mr-1">Color</span>
          {#each palette as c}
            <button
              type="button"
              onclick={() => (newGroupColor = c)}
              class="w-5 h-5 rounded-full border-2 transition-all {newGroupColor === c
                ? 'border-foreground scale-110'
                : 'border-transparent'}"
              style="background-color: {c}"
              aria-label={c}
            ></button>
          {/each}
        </div>
        {#if mgError}<p class="text-xs text-destructive">{mgError}</p>{/if}
      </div>

      <div>
        <h3 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
          Existing groups
        </h3>
        {#if groups.length === 0}
          <p class="text-sm text-muted-foreground">No groups defined yet.</p>
        {:else}
          <div class="space-y-1.5">
            {#each groups as g (g.name)}
              <div
                class="flex items-center justify-between border border-border rounded-md bg-background px-2.5 py-1.5"
              >
                <div class="flex items-center gap-2">
                  <span
                    class="inline-block w-2.5 h-2.5 rounded-full"
                    style="background-color: {g.color}"
                  ></span>
                  <span class="text-sm font-medium">{g.name}</span>
                  <span class="text-xs text-muted-foreground tnum"
                    >{g.member_count} VM{g.member_count !== 1 ? 's' : ''}</span
                  >
                </div>
                <div class="flex items-center gap-1.5">
                  {#each palette as c}
                    <button
                      type="button"
                      onclick={() => {
                        g.color = c;
                        updateGroupColor(g);
                      }}
                      class="w-3.5 h-3.5 rounded-full border {g.color === c
                        ? 'border-foreground'
                        : 'border-transparent'}"
                      style="background-color: {c}"
                      aria-label="color {c}"
                    ></button>
                  {/each}
                  <button
                    type="button"
                    onclick={() => deleteGroup(g)}
                    class="p-1 text-muted-foreground hover:text-destructive hover:bg-destructive/10 rounded transition-colors"
                    aria-label="Delete group"
                    title="Delete group"
                  >
                    <svg
                      class="w-3.5 h-3.5"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      viewBox="0 0 24 24"
                      ><polyline points="3 6 5 6 21 6" /><path
                        d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"
                      /></svg
                    >
                  </button>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>

    <Dialog.Footer>
      <Button variant="outline" onclick={() => (showManageGroups = false)}>Close</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
