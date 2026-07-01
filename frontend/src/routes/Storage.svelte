<script>
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Alert from '$lib/components/Alert.svelte';
  import Spinner from '$lib/components/Spinner.svelte';
  import { onMount } from 'svelte';
  import { api, auth } from '$lib/stores/auth.svelte.js';
  import { toast } from '$lib/components/ui/toast';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import * as Dialog from '$lib/components/ui/dialog';
  import { navigate } from '$lib/router.svelte.js';

  let pools = $state([]);
  let volumes = $state([]);
  let selectedPool = $state('');
  let loading = $state(true);
  let error = $state('');
  let isos = $state([]);
  let uploading = $state(false);
  let uploadProgress = $state(0);
  let downloadProgress = $state(0);
  let downloading = $state(false);
  let downloadMessage = $state('');

  let showCreatePool = $state(false);
  let poolName = $state('');
  let poolType = $state('dir');
  let poolSourceHost = $state('');
  let poolSourceDir = $state('');
  let poolSourceFormat = $state('nfs');
  let poolPath = $state('');
  let poolPurpose = $state('disk');
  // CIFS auth fields. Only meaningful when poolSourceFormat is
  // "cifs"; for NFS the backend silently ignores them. Kept as
  // plain $state (not derived) so the user can type into them
  // even when the format selector flips to nfs — the createPool
  // guard just refuses to send the request in that case.
  let poolSourceUsername = $state('');
  let poolSourcePassword = $state('');

  // Reauth modal: when set to a pool name, opens the modal for
  // that pool. The form collects new credentials and submits a
  // PUT /api/storage/pools/{name} with cifs-needs-reauth=true.
  let reauthOpen = $state(false);
  let reauthPool = $state('');
  let reauthUsername = $state('');
  let reauthPassword = $state('');
  let reauthNeedsReauth = $state(true);
  let reauthLoading = $state(false);

  let showCreateVol = $state(false);
  let volName = $state('');
  let volSize = $state(20);
  let volFormat = $state('qcow2');

  let showResizeVol = $state(false);
  let resizeVolName = $state('');
  let resizeVolSize = $state(20);
  let resizeVolCurrent = $state(0);
  let resizeVolPool = $state('');

  let showDownloadISO = $state(false);
  let downloadURL = $state('');
  let downloadName = $state('');
  let selectedISOPool = $state('ISOS');

  let showRenameISO = $state(false);
  let renameOldName = $state('');
  let renameNewName = $state('');
  let renaming = $state(false);

  let selectedPoolInfo = $derived(pools.find((p) => p.name === selectedPool));
  let selectedPoolIsISO = $derived(selectedPoolInfo?.purpose === 'iso');

  // Confirm dialog state
  let confirmState = $state({
    open: false,
    title: '',
    description: '',
    confirmLabel: 'Delete',
    variant: 'destructive',
    onConfirm: () => {},
    loading: false,
  });

  onMount(() => load());

  async function load() {
    loading = true;
    error = '';
    try {
      pools = (await api.listPools()) || [];
      const diskPools = pools.filter((p) => p.purpose !== 'iso');
      if (!selectedPool || !pools.find((p) => p.name === selectedPool)) {
        selectedPool = diskPools[0]?.name || pools[0]?.name || '';
      }
      volumes = (selectedPool ? await api.listVolumes(selectedPool) : []) || [];
      const isoPools = pools.filter((p) => p.purpose === 'iso');
      if (!selectedISOPool || !isoPools.find((p) => p.name === selectedISOPool)) {
        selectedISOPool = isoPools[0]?.name || 'ISOS';
      }
      isos = (await api.listISOs(selectedISOPool)) || [];
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function askConfirm(opts) {
    confirmState = { ...opts, open: true, loading: false };
  }

  async function createPool() {
    if (!poolName || !poolPath) return;
    if (poolType === 'netfs' && (!poolSourceHost || !poolSourceDir)) {
      toast.error('Netfs pools require a source host and export path');
      return;
    }
    // CIFS auth: both fields must be present together. Match the
    // backend's 400 — the API rejects the request before talking
    // to libvirt, so this client-side check is just a faster UX.
    const wantAuth =
      poolType === 'netfs' &&
      poolSourceFormat === 'cifs' &&
      (poolSourceUsername !== '' || poolSourcePassword !== '');
    if (wantAuth && (poolSourceUsername === '' || poolSourcePassword === '')) {
      toast.error('CIFS auth requires both username and password');
      return;
    }
    try {
      await api.createPool({
        name: poolName,
        type: poolType,
        path: poolPath,
        source_host: poolSourceHost || undefined,
        source_dir: poolSourceDir || undefined,
        source_format: poolType === 'netfs' ? poolSourceFormat : undefined,
        source_username: wantAuth ? poolSourceUsername : undefined,
        source_password: wantAuth ? poolSourcePassword : undefined,
        purpose: poolPurpose,
      });
      const createdName = poolName;
      poolName = '';
      poolPath = '';
      poolSourceHost = '';
      poolSourceDir = '';
      poolSourceUsername = '';
      poolSourcePassword = '';
      poolType = 'dir';
      poolPurpose = 'disk';
      showCreatePool = false;
      toast.success(`Pool "${createdName}" created`);
      await load();
    } catch (e) {
      toast.error(e.message);
    }
  }

  function openReauth(pool) {
    reauthOpen = true;
    reauthPool = pool.name;
    reauthUsername = '';
    reauthPassword = '';
    reauthNeedsReauth = true;
    reauthLoading = false;
  }

  function closeReauth() {
    reauthOpen = false;
    reauthPool = '';
    reauthUsername = '';
    reauthPassword = '';
    reauthNeedsReauth = true;
    reauthLoading = false;
  }

  async function submitReauth() {
    if (reauthPool === '') return;
    if (reauthUsername === '' || reauthPassword === '') {
      toast.error('Username and password are required for reauth');
      return;
    }
    reauthLoading = true;
    try {
      await api.updatePool(reauthPool, {
        source_username: reauthUsername,
        source_password: reauthPassword,
        'cifs-needs-reauth': reauthNeedsReauth,
      });
      toast.success(`Pool "${reauthPool}" credentials rotated`);
      closeReauth();
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      reauthLoading = false;
    }
  }

  // A pool is a CIFS pool when its source path includes
  // "/...cifs..." or the libvirt pool type is "netfs" with a
  // cifs format. The backend doesn't currently expose the
  // source_format on the storage pool model, so we use a
  // heuristic: any netfs pool whose path lives at a location
  // the operator mounted via cifs. The fallback is to always
  // show the Reauth button on netfs pools — a no-op for NFS
  // (the backend returns 400 "reauth only for cifs") which is
  // a clear enough error for the operator.
  function isCifsPool(p) {
    return p.type === 'netfs';
  }

  async function deletePool(name) {
    askConfirm({
      title: `Delete pool "${name}"?`,
      description: 'The directory on disk will be kept, but the pool will be removed from libvirt.',
      confirmLabel: 'Delete',
      onConfirm: async () => {
        confirmState.loading = true;
        try {
          await api.deletePool(name);
          confirmState.open = false;
          toast.success(`Pool "${name}" deleted`);
          if (!pools.find((p) => p.name === selectedPool)) {
            selectedPool = pools[0]?.name || '';
          }
          await load();
        } catch (e) {
          toast.error(e.message);
          confirmState.loading = false;
        }
      },
    });
  }

  async function createVolume() {
    if (!volName) return;
    try {
      await api.createVolume({
        name: volName,
        pool: selectedPool,
        capacity: volSize,
        format: volFormat,
      });
      volName = '';
      volSize = 20;
      volFormat = 'qcow2';
      showCreateVol = false;
      toast.success(`Volume "${volName || ''}" created`);
      await load();
    } catch (e) {
      toast.error(e.message);
    }
  }

  async function deleteVolume(pool, name) {
    askConfirm({
      title: `Delete volume "${name}"?`,
      description: `This will DELETE the disk data in pool "${pool}". Cannot be undone.`,
      confirmLabel: 'Delete',
      onConfirm: async () => {
        confirmState.loading = true;
        try {
          await api.deleteVolume(pool, name);
          confirmState.open = false;
          toast.success(`Volume "${name}" deleted`);
          await load();
        } catch (e) {
          toast.error(e.message);
          confirmState.loading = false;
        }
      },
    });
  }

  async function resizeVolume() {
    if (!resizeVolName) return;
    try {
      await api.resizeVolume(resizeVolPool, resizeVolName, resizeVolSize);
      showResizeVol = false;
      resizeVolName = '';
      toast.success('Volume resized');
      await load();
    } catch (e) {
      toast.error(e.message);
    }
  }

  async function deleteISO(name) {
    askConfirm({
      title: `Delete ISO "${name}"?`,
      description: 'This will permanently delete the file.',
      confirmLabel: 'Delete',
      onConfirm: async () => {
        confirmState.loading = true;
        try {
          await api.deleteISO(name, selectedISOPool);
          confirmState.open = false;
          toast.success(`ISO "${name}" deleted`);
          await load();
        } catch (e) {
          toast.error(e.message);
          confirmState.loading = false;
        }
      },
    });
  }

  function openRenameISO(iso) {
    renameOldName = iso.name;
    renameNewName = iso.name;
    showRenameISO = true;
  }

  async function doRenameISO() {
    if (!renameOldName || !renameNewName || renameNewName === renameOldName) {
      showRenameISO = false;
      return;
    }
    renaming = true;
    try {
      await api.renameISO(renameOldName, renameNewName, selectedISOPool);
      showRenameISO = false;
      renameOldName = '';
      renameNewName = '';
      toast.success('ISO renamed');
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      renaming = false;
    }
  }

  async function handleDownloadISO() {
    if (!downloadURL) return;
    downloading = true;
    downloadProgress = 0;
    downloadMessage = 'Starting download...';
    let intervalId;
    try {
      const data = await api.downloadISO(downloadURL, downloadName || undefined, selectedISOPool);
      const jobId = data.job_id;
      if (!jobId) throw new Error('No job ID returned');

      await new Promise((resolve) => {
        intervalId = setInterval(async () => {
          try {
            const job = await api.getDownloadJob(jobId);
            if (!job) return;
            if (job.status === 'queued') downloadMessage = 'Waiting in queue...';
            else if (job.status === 'downloading') {
              const pct =
                job.progress > 0 && job.progress < 0.01 ? 0 : Math.round(job.progress || 0);
              downloadProgress = pct;
              downloadMessage = `Downloading ${job.name}... ${pct}%`;
            } else if (job.status === 'completed') {
              downloadProgress = 100;
              downloadMessage = 'Download complete!';
              clearInterval(intervalId);
              intervalId = null;
              downloadURL = '';
              downloadName = '';
              showDownloadISO = false;
              toast.success('Download complete');
              resolve();
            } else if (job.status === 'error') {
              toast.error(job.error || 'Download failed');
              clearInterval(intervalId);
              intervalId = null;
              downloadMessage = '';
              downloadProgress = 0;
              resolve();
            }
          } catch {
            /* ignore poll errors */
          }
        }, 500);
      });
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      if (intervalId) clearInterval(intervalId);
      downloading = false;
    }
  }

  let uploadFiles = $state(null);

  async function handleUpload() {
    const file = uploadFiles?.[0];
    if (!file) return;
    uploading = true;
    uploadProgress = 0;
    try {
      await api.uploadISO(
        file,
        (pct) => {
          uploadProgress = pct;
        },
        selectedISOPool
      );
      uploadFiles = null;
      toast.success('ISO uploaded');
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      uploading = false;
    }
  }

  function bytesToStr(b) {
    if (!b) return '0 B';
    const u = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    while (b >= 1024 && i < u.length - 1) {
      b /= 1024;
      i++;
    }
    return b.toFixed(i > 0 ? 1 : 0) + ' ' + u[i];
  }

  function selectPool(name) {
    selectedPool = name;
    load();
  }

  // Group volumes into roots (real files) and snapshot children
  // (internal qcow2 snapshot views). Snapshots are rendered as
  // sub-rows beneath their parent disk, with a "Manage in <vm>" link
  // to the VmDetail snapshot tree instead of resize/delete.
  const volumeTree = $derived.by(() => buildVolumeTree(volumes));
  const vmNameById = $derived.by(() => buildVmNameById(volumes));

  function buildVolumeTree(vols) {
    const byName = {};
    for (const v of vols) byName[v.name] = { ...v, children: [] };
    const roots = [];
    for (const k of Object.keys(byName)) {
      const node = byName[k];
      if (node.is_snapshot && node.parent_volume && byName[node.parent_volume]) {
        byName[node.parent_volume].children.push(node);
      } else {
        roots.push(node);
      }
    }
    // Stable sort by name so the UI doesn't shuffle on every refresh.
    const sortRec = (nodes) => {
      nodes.sort((a, b) => a.name.localeCompare(b.name));
      nodes.forEach((n) => sortRec(n.children));
    };
    sortRec(roots);
    return roots;
  }

  function buildVmNameById(_vols) {
    // volumes only carry snapshot_of_vm_id (UUID), not the name.
    // Fetch lazily; this is cheap because the VM list is already
    // in flight for the dashboard pages. We resolve names on demand
    // and cache the result for the lifetime of the page.
    return _vmNameByIdCache;
  }
  let _vmNameByIdCache = $state({});

  $effect(() => {
    // Whenever we see a new snapshot_of_vm_id, fetch the VM name
    // once and remember it.
    const ids = new Set();
    for (const v of volumes) {
      if (v.is_snapshot && v.snapshot_of_vm_id) ids.add(v.snapshot_of_vm_id);
    }
    for (const id of ids) {
      if (_vmNameByIdCache[id]) continue;
      api
        .getVM(id)
        .then((vm) => {
          if (vm && vm.name) _vmNameByIdCache = { ..._vmNameByIdCache, [id]: vm.name };
        })
        .catch(() => {});
    }
  });
</script>

<div class="p-6 max-w-6xl">
  <PageHeader title="Storage" subtitle="Manage pools, volumes, and ISOs" />

  {#if error}
    <Alert variant="error">{error}</Alert>
  {/if}

  {#if loading}
    <div class="flex items-center justify-center py-24"><Spinner size="lg" /></div>
  {:else}
    <!-- Storage Pools -->
    <div class="border border-border rounded-lg bg-card p-5 mb-4">
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          Storage Pools
        </h2>
        <Button size="sm" variant="outline" onclick={() => (showCreatePool = !showCreatePool)}>
          {showCreatePool ? 'Cancel' : '+ Pool'}
        </Button>
      </div>

      {#if showCreatePool}
        <div class="bg-muted/30 rounded-md p-3 mb-3 border border-border space-y-3">
          <div class="text-sm font-medium">New Pool</div>
          <div class="flex flex-wrap gap-2">
            <Input bind:value={poolName} placeholder="Pool name" class="flex-1 min-w-[150px]" />
            <select bind:value={poolType} class="input w-32">
              <option value="dir">Local dir</option>
              <option value="netfs">NFS / SMB</option>
            </select>
            <select bind:value={poolPurpose} class="input w-32">
              <option value="disk">VDI</option>
              <option value="iso">ISO</option>
            </select>
          </div>
          {#if poolType === 'netfs'}
            <div class="flex flex-wrap gap-2">
              <Input
                bind:value={poolSourceHost}
                placeholder="server (e.g. 10.0.0.5)"
                class="flex-1 min-w-[200px]"
              />
              <Input
                bind:value={poolSourceDir}
                placeholder="export path (e.g. /vms)"
                class="flex-1 min-w-[200px]"
              />
              <select bind:value={poolSourceFormat} class="input w-24">
                <option value="nfs">NFS</option>
                <option value="cifs">SMB</option>
              </select>
            </div>
            {#if poolSourceFormat === 'cifs'}
              <div class="flex flex-wrap gap-2">
                <Input
                  bind:value={poolSourceUsername}
                  placeholder="username (e.g. alice)"
                  type="text"
                  autocomplete="off"
                  class="flex-1 min-w-[180px]"
                />
                <Input
                  bind:value={poolSourcePassword}
                  placeholder="password"
                  type="password"
                  autocomplete="new-password"
                  class="flex-1 min-w-[180px]"
                />
              </div>
            {/if}
            <p class="text-xs text-muted-foreground">
              The target path will be auto-mounted via libvirt. For SMB, credentials are stored in
              libvirt's secret store (never on disk in plaintext). For NFS, authentication is
              handled at the kernel level via /etc/fstab.
            </p>
          {/if}
          <div class="flex flex-wrap gap-2">
            <Input
              bind:value={poolPath}
              placeholder={poolType === 'netfs' ? '/mnt/local-mount' : '/path/to/pool'}
              class="flex-1 min-w-[200px]"
            />
            <Button onclick={createPool}>Create</Button>
          </div>
        </div>
      {/if}

      <div class="space-y-1.5">
        {#if pools.filter((p) => p.purpose !== 'iso').length > 0}
          <div class="text-xs font-medium text-muted-foreground uppercase tracking-wider mt-2 mb-1">
            VDI Pools
          </div>
          {#each pools.filter((p) => p.purpose !== 'iso') as p (p.name)}
            <div
              class="flex items-center justify-between px-3 py-2 rounded-md border cursor-pointer transition-colors {selectedPool ===
              p.name
                ? 'border-accent/50 bg-accent/5'
                : 'border-border bg-background hover:bg-muted/30'}"
              onclick={() => selectPool(p.name)}
              onkeydown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  selectPool(p.name);
                }
              }}
              role="button"
              tabindex="0"
            >
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <span class="text-sm font-medium {selectedPool === p.name ? 'text-accent' : ''}"
                    >{p.name}</span
                  >
                  <span
                    class="text-[10px] px-1.5 py-0.5 rounded border border-accent/30 bg-accent/10 text-accent uppercase tracking-wide"
                    >VDI</span
                  >
                </div>
                {#if auth.role === 'admin'}
                  <p class="text-xs text-muted-foreground font-mono mt-0.5 truncate">{p.path}</p>
                {/if}
              </div>
              {#if auth.role === 'admin' || auth.role === 'operator'}
                <div class="flex items-center gap-1 shrink-0">
                  {#if isCifsPool(p)}
                    <button
                      onclick={(e) => {
                        e.stopPropagation();
                        openReauth(p);
                      }}
                      class="p-1.5 rounded-md text-muted-foreground hover:text-accent hover:bg-accent/10 transition-colors"
                      aria-label="Rotate credentials for pool {p.name}"
                      title="Rotate credentials"
                    >
                      <svg
                        class="w-4 h-4"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                        viewBox="0 0 24 24"
                      >
                        <path
                          d="M15 7h2a4 4 0 010 8h-2m-6 4H5a4 4 0 010-8h2m0-4l3 3-3 3m6 0l3-3-3-3"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                  {/if}
                  {#if auth.role === 'admin'}
                    <button
                      onclick={(e) => {
                        e.stopPropagation();
                        deletePool(p.name);
                      }}
                      class="p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                      aria-label="Delete pool {p.name}"
                    >
                      <svg
                        class="w-4 h-4"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                        viewBox="0 0 24 24"
                      >
                        <path
                          d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                  {/if}
                </div>
              {/if}
            </div>
          {/each}
        {/if}
        {#if pools.filter((p) => p.purpose === 'iso').length > 0}
          <div class="text-xs font-medium text-muted-foreground uppercase tracking-wider mt-3 mb-1">
            ISO Pools
          </div>
          {#each pools.filter((p) => p.purpose === 'iso') as p (p.name)}
            <div
              class="flex items-center justify-between px-3 py-2 rounded-md border border-border bg-background"
            >
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <span class="text-sm font-medium">{p.name}</span>
                  <span
                    class="text-[10px] px-1.5 py-0.5 rounded border border-warning/30 bg-warning/10 text-warning uppercase tracking-wide"
                    >ISO</span
                  >
                </div>
                {#if auth.role === 'admin'}
                  <p class="text-xs text-muted-foreground font-mono mt-0.5 truncate">{p.path}</p>
                {/if}
              </div>
              {#if auth.role === 'admin'}
                <button
                  onclick={(e) => {
                    e.stopPropagation();
                    deletePool(p.name);
                  }}
                  class="p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors shrink-0"
                  aria-label="Delete pool {p.name}"
                >
                  <svg
                    class="w-4 h-4"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    viewBox="0 0 24 24"
                  >
                    <path
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    />
                  </svg>
                </button>
              {/if}
            </div>
          {/each}
        {/if}
      </div>
    </div>

    <!-- Volumes -->
    <div class="border border-border rounded-lg bg-card p-5 mb-4">
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          Volumes — {selectedPool || 'none'}
          {#if selectedPoolIsISO}
            <span
              class="text-[10px] px-1.5 py-0.5 rounded border border-warning/30 bg-warning/10 text-warning uppercase tracking-wide ml-2"
              >ISO pool — read only</span
            >
          {/if}
        </h2>
        {#if !selectedPoolIsISO}
          <Button
            size="sm"
            variant="outline"
            onclick={() => {
              volName = '';
              volSize = 20;
              showCreateVol = true;
            }}>+ Volume</Button
          >
        {/if}
      </div>

      {#if showCreateVol && !selectedPoolIsISO}
        <div class="bg-muted/30 rounded-md p-3 mb-3 border border-border space-y-2">
          <div class="text-sm font-medium">New Volume</div>
          <div class="flex flex-wrap gap-2 items-end">
            <Input
              bind:value={volName}
              placeholder="Volume name (e.g. vm-disk)"
              class="flex-1 min-w-[200px]"
            />
            <Input type="number" bind:value={volSize} min="1" class="w-24 tnum" />
            <span class="text-xs text-muted-foreground">GB</span>
            <select bind:value={volFormat} class="input w-32">
              <option value="qcow2">qcow2</option>
              <option value="raw">raw</option>
            </select>
            <Button onclick={createVolume}>Create</Button>
          </div>
        </div>
      {/if}

      {#if showResizeVol && !selectedPoolIsISO}
        <div class="bg-muted/30 rounded-md p-3 mb-3 border border-border space-y-2">
          <div class="text-sm font-medium">
            Resize Volume: {resizeVolName} (current: {resizeVolCurrent} GB)
          </div>
          <div class="flex flex-wrap gap-2 items-end">
            <Input
              type="number"
              min={resizeVolCurrent}
              bind:value={resizeVolSize}
              class="w-24 tnum"
            />
            <span class="text-xs text-muted-foreground">GB</span>
            <Button onclick={resizeVolume}>Resize</Button>
            <Button variant="outline" onclick={() => (showResizeVol = false)}>Cancel</Button>
          </div>
        </div>
      {/if}

      {#if volumes.length === 0}
        <p class="text-sm text-muted-foreground">No volumes in this pool</p>
      {:else}
        <div class="space-y-1">
          {#each volumeTree as vol (vol.name)}
            <div class="border border-border rounded-md bg-background">
              <div class="flex items-center justify-between px-3 py-2">
                <div class="flex items-center gap-3 min-w-0">
                  <span class="text-sm truncate">{vol.name}</span>
                  <span class="text-xs text-muted-foreground tnum">{bytesToStr(vol.capacity)}</span>
                  {#if vol.allocation != null}
                    <span class="text-xs text-muted-foreground tnum"
                      >({bytesToStr(vol.allocation)} used)</span
                    >
                  {/if}
                  {#if vol.children.length > 0}
                    <span
                      class="text-[10px] px-1.5 py-0.5 rounded border border-accent/30 bg-accent/10 text-accent uppercase tracking-wider"
                    >
                      {vol.children.length} snapshot{vol.children.length !== 1 ? 's' : ''}
                    </span>
                  {/if}
                </div>
                <div class="flex items-center gap-1 shrink-0">
                  {#if !selectedPoolIsISO}
                    <button
                      onclick={() => {
                        resizeVolName = vol.name;
                        resizeVolSize = vol.capacity / (1024 * 1024 * 1024);
                        resizeVolCurrent = vol.capacity / (1024 * 1024 * 1024);
                        resizeVolPool = selectedPool;
                        showResizeVol = true;
                      }}
                      class="text-xs text-accent hover:text-accent-hover px-2 py-1 rounded hover:bg-muted"
                      >Resize</button
                    >
                  {/if}
                  <button
                    onclick={() => deleteVolume(selectedPool, vol.name)}
                    class="p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                    aria-label="Delete {vol.name}"
                  >
                    <svg
                      class="w-4 h-4"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      viewBox="0 0 24 24"
                      ><polyline points="3 6 5 6 21 6" /><path
                        d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"
                      /></svg
                    >
                  </button>
                </div>
              </div>
              {#if vol.children.length > 0}
                <div class="ml-4 mr-3 mb-2 pl-3 border-l-2 border-dashed border-border space-y-1">
                  {#each vol.children as snap (snap.name)}
                    <div
                      class="flex items-center justify-between px-2 py-1.5 rounded bg-muted/30 text-xs"
                    >
                      <div class="flex items-center gap-2 min-w-0">
                        <svg
                          class="w-3 h-3 text-muted-foreground shrink-0"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          viewBox="0 0 24 24"
                          ><circle cx="12" cy="12" r="10" /><polyline
                            points="12 6 12 12 16 14"
                          /></svg
                        >
                        <span class="font-mono truncate" title={snap.name}>{snap.name}</span>
                        <span
                          class="text-[10px] px-1.5 py-0.5 rounded border border-border bg-background text-muted-foreground uppercase tracking-wider"
                          >Internal snapshot</span
                        >
                        <span class="text-muted-foreground tnum">{bytesToStr(snap.allocation)}</span
                        >
                      </div>
                      <button
                        onclick={() =>
                          navigate(`/vms/${snap.snapshot_of_vm_id}`, {
                            query: { tab: 'snapshots' },
                          })}
                        class="text-xs text-accent hover:text-accent-hover px-2 py-1 rounded hover:bg-muted shrink-0"
                        title="Open {vmNameById[snap.snapshot_of_vm_id] ||
                          'VM'} and scroll to the snapshot tree"
                      >
                        Manage in {vmNameById[snap.snapshot_of_vm_id] || 'VM'} →
                      </button>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- ISO Library -->
    <div class="border border-border rounded-lg bg-card p-5">
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          ISO Library
        </h2>
        <div class="flex items-center gap-2">
          <select
            bind:value={selectedISOPool}
            onchange={() =>
              api
                .listISOs(selectedISOPool)
                .then((data) => (isos = data || []))
                .catch((e) => (error = e.message))}
            class="input !py-1 !text-xs !w-auto"
          >
            {#each pools.filter((p) => p.purpose === 'iso') as p}
              <option value={p.name}>{p.name}</option>
            {/each}
          </select>
          <Button size="sm" variant="outline" onclick={() => (showDownloadISO = !showDownloadISO)}>
            Download URL
          </Button>
          <Input
            type="file"
            accept=".iso"
            bind:files={uploadFiles}
            onchange={handleUpload}
            class="hidden"
            id="iso-upload"
          />
          <label for="iso-upload" class="btn btn-primary !text-xs !h-7 cursor-pointer">
            {uploading ? 'Uploading...' : 'Upload ISO'}
          </label>
        </div>
      </div>

      {#if showDownloadISO}
        <div class="bg-muted/30 rounded-md p-3 mb-3 border border-border space-y-2">
          <div class="text-sm text-muted-foreground">Download an ISO from a URL</div>
          <div class="flex flex-wrap gap-2 items-end">
            <Input
              bind:value={downloadURL}
              placeholder="https://releases.ubuntu.com/24.04/ubuntu-24.04-desktop-amd64.iso"
              class="flex-1 min-w-[300px]"
            />
            <Input bind:value={downloadName} placeholder="Filename (optional)" class="w-48" />
            <Button onclick={handleDownloadISO} disabled={!downloadURL || downloading}>
              {downloading ? 'Downloading...' : 'Download'}
            </Button>
          </div>
        </div>
      {/if}

      {#if uploading || downloading}
        <div class="mb-3 bg-muted/30 rounded-md p-3 border border-border space-y-1">
          {#if uploading}
            <div class="flex justify-between text-sm text-muted-foreground">
              <span>Uploading...</span>
              <span class="tnum">{Math.round(uploadProgress)}%</span>
            </div>
            <div class="w-full h-1.5 bg-muted rounded-full overflow-hidden">
              <div class="h-full bg-accent transition-all" style="width: {uploadProgress}%"></div>
            </div>
          {/if}
          {#if downloading}
            <div class="flex justify-between text-sm text-muted-foreground">
              <span>{downloadMessage || 'Downloading...'}</span>
              <span class="tnum">{Math.round(downloadProgress)}%</span>
            </div>
            <div class="w-full h-1.5 bg-muted rounded-full overflow-hidden">
              <div
                class="h-full bg-success transition-all"
                style="width: {downloadProgress}%"
              ></div>
            </div>
          {/if}
        </div>
      {/if}

      {#if isos.length === 0}
        <p class="text-sm text-muted-foreground">No ISOs in this pool</p>
      {:else}
        <div class="space-y-1 max-h-72 overflow-y-auto">
          {#each isos as iso (iso.name)}
            <div
              class="flex items-center justify-between px-3 py-2 rounded-md border border-border bg-background"
            >
              <div class="flex items-center gap-3 min-w-0">
                <svg
                  class="w-4 h-4 text-warning shrink-0"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  viewBox="0 0 24 24"
                >
                  <circle cx="12" cy="12" r="10" /><circle cx="12" cy="12" r="6" /><circle
                    cx="12"
                    cy="12"
                    r="2"
                  />
                </svg>
                <div class="min-w-0">
                  <span class="text-sm truncate">{iso.name}</span>
                  <span class="text-xs text-muted-foreground ml-2 tnum">{bytesToStr(iso.size)}</span
                  >
                </div>
              </div>
              <div class="flex items-center gap-1">
                <button
                  onclick={() => openRenameISO(iso)}
                  class="p-1.5 rounded-md text-muted-foreground hover:text-accent hover:bg-muted"
                  aria-label="Rename {iso.name}"
                >
                  <svg
                    class="w-4 h-4"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    viewBox="0 0 24 24"
                  >
                    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
                    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
                  </svg>
                </button>
                <button
                  onclick={() => deleteISO(iso.name)}
                  class="p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                  aria-label="Delete {iso.name}"
                >
                  <svg
                    class="w-4 h-4"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    viewBox="0 0 24 24"
                    ><polyline points="3 6 5 6 21 6" /><path
                      d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"
                    /></svg
                  >
                </button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<!-- Reauth Modal -->
<Dialog.Root bind:open={reauthOpen}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Rotate Credentials</Dialog.Title>
      <Dialog.Description
        >Pool: <span class="font-mono">{reauthPool}</span>. Enter new CIFS credentials. The pool
        will be redefined with the new secret; running VMs using this pool must be restarted for the
        change to take effect.</Dialog.Description
      >
    </Dialog.Header>
    <div class="space-y-3">
      <div>
        <label for="reauth-username" class="block text-sm font-medium">Username</label>
        <Input
          id="reauth-username"
          bind:value={reauthUsername}
          placeholder="alice"
          type="text"
          autocomplete="off"
        />
      </div>
      <div>
        <label for="reauth-password" class="block text-sm font-medium">Password</label>
        <Input
          id="reauth-password"
          bind:value={reauthPassword}
          placeholder="••••••••"
          type="password"
          autocomplete="new-password"
        />
      </div>
      <div class="flex items-center gap-2">
        <input
          type="checkbox"
          bind:checked={reauthNeedsReauth}
          id="reauth-reauth"
          class="rounded border-input"
        />
        <label for="reauth-reauth" class="text-sm text-muted-foreground">
          <code>cifs-needs-reauth</code> — tick if recovering from a libvirtd reinstall (replaces the
          secret even if one exists). Untick for a routine password rotation.
        </label>
      </div>
      <p class="text-xs text-muted-foreground">
        The pool must be stopped before rotating credentials. If it is running, the backend will
        reject the request.
      </p>
    </div>
    <Dialog.Footer class="gap-2">
      <Button variant="outline" onclick={closeReauth} disabled={reauthLoading}>Cancel</Button>
      <Button
        onclick={submitReauth}
        disabled={reauthLoading || reauthUsername === '' || reauthPassword === ''}
      >
        {reauthLoading ? 'Rotating…' : 'Rotate Credentials'}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Rename ISO Dialog -->
<Dialog.Root bind:open={showRenameISO}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Rename ISO</Dialog.Title>
      <Dialog.Description
        >Current: <span class="font-mono">{renameOldName}</span></Dialog.Description
      >
    </Dialog.Header>
    <div class="space-y-2">
      <label for="rename-iso-input" class="block text-sm font-medium">New name</label>
      <Input id="rename-iso-input" bind:value={renameNewName} placeholder="new-name.iso" />
      <p class="text-xs text-muted-foreground">Must end with <code>.iso</code></p>
    </div>
    <Dialog.Footer class="gap-2">
      <Button
        variant="outline"
        onclick={() => {
          showRenameISO = false;
          renameOldName = '';
          renameNewName = '';
        }}
        disabled={renaming}>Cancel</Button
      >
      <Button
        onclick={doRenameISO}
        disabled={renaming || !renameNewName || renameNewName === renameOldName}
      >
        {renaming ? 'Renaming…' : 'Rename'}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Confirm Dialog -->
<ConfirmDialog
  bind:open={confirmState.open}
  title={confirmState.title}
  description={confirmState.description}
  confirmLabel={confirmState.confirmLabel}
  variant={confirmState.variant}
  loading={confirmState.loading}
  onConfirm={confirmState.onConfirm}
/>
