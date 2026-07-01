<script>
  import Alert from '$lib/components/Alert.svelte';
  import Spinner from '$lib/components/Spinner.svelte';
  import { onMount } from 'svelte';
  import { api } from '$lib/stores/auth.svelte.js';
  import { navigate } from '$lib/router.svelte.js';
  import { toast } from '$lib/components/ui/toast';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import SettingRow from '$lib/components/SettingRow.svelte';

  let name = $state('');
  let vcpus = $state(2);
  let ramMB = $state(2048);
  let storagePool = $state('');
  let cpuMode = $state('host-passthrough');
  let cpuModel = $state('');
  let videoModel = $state('virtio');
  let network = $state('default');
  let iso = $state('');
  let loading = $state(false);
  let error = $state('');
  let pools = $state([]);
  let networks = $state([]);
  let isos = $state([]);
  let loadingData = $state(true);

  let osType = $state('linux');
  let osVersion = $state('arch');
  let chipset = $state('q35');
  let firmware = $state('uefi');
  let secureBoot = $state(false);
  let tpmEnabled = $state(false);
  let networkModel = $state('virtio');

  // Disk options
  let diskSize = $state(30);
  let diskBus = $state('virtio');
  let diskFormat = $state('qcow2');
  let virtioISO = $state('');
  let useExistingDisk = $state(false);
  let existingDiskPool = $state('');
  let existingDiskName = $state('');
  let existingVolumes = $state([]);
  let loadingVolumes = $state(false);

  // Validation
  let touched = $state({ name: false, vcpus: false, ramMB: false, diskSize: false });
  const nameError = $derived(
    !name.trim() ? 'Name is required' : name.length > 64 ? 'Name too long' : ''
  );
  const vcpusError = $derived(vcpus < 1 || vcpus > 64 ? 'Must be 1-64' : '');
  const ramError = $derived(ramMB < 512 ? 'Minimum 512 MB' : '');
  const diskSizeError = $derived(
    !useExistingDisk && (diskSize < 1 || diskSize > 1024) ? 'Must be 1-1024 GB' : ''
  );
  const isValid = $derived(!nameError && !vcpusError && !ramError && !diskSizeError);

  const cpuModes = [
    { value: 'host-passthrough', label: 'host-passthrough (recommended)' },
    { value: 'host-model', label: 'host-model' },
    { value: 'max', label: 'max' },
    { value: 'custom', label: 'custom' },
  ];

  const videoModels = [
    { value: 'virtio', label: 'virtio' },
    { value: 'qxl', label: 'qxl' },
    { value: 'vga', label: 'VGA' },
    { value: 'cirrus', label: 'cirrus' },
    { value: 'vmvga', label: 'vmvga (VMware)' },
    { value: 'bochs', label: 'bochs' },
    { value: 'none', label: 'none' },
  ];

  const networkModels = [
    { value: 'virtio', label: 'virtio (recommended)' },
    { value: 'e1000e', label: 'e1000e (Intel, ideal for Windows)' },
    { value: 'e1000', label: 'e1000 (legacy Intel)' },
    { value: 'rtl8139', label: 'rtl8139 (Realtek, very compatible)' },
    { value: 'pcnet', label: 'pcnet (AMD, legacy)' },
  ];

  const windowsVersions = [
    { value: 'win11', label: 'Windows 11' },
    { value: 'win10', label: 'Windows 10' },
    { value: 'win2k22', label: 'Windows Server 2022' },
    { value: 'win2k19', label: 'Windows Server 2019' },
    { value: 'win2k16', label: 'Windows Server 2016' },
  ];

  const linuxVersions = [
    { value: 'arch', label: 'Arch Linux' },
    { value: 'ubuntu24', label: 'Ubuntu 24.04' },
    { value: 'ubuntu22', label: 'Ubuntu 22.04' },
    { value: 'debian12', label: 'Debian 12' },
    { value: 'debian11', label: 'Debian 11' },
    { value: 'fedora40', label: 'Fedora 40' },
    { value: 'centos9', label: 'CentOS Stream 9' },
    { value: 'rhel9', label: 'RHEL 9' },
    { value: 'rocky9', label: 'Rocky Linux 9' },
    { value: 'opensuse', label: 'openSUSE Leap' },
    { value: 'alpine', label: 'Alpine Linux' },
    { value: 'gentoo', label: 'Gentoo' },
    { value: 'void', label: 'Void Linux' },
    { value: 'other', label: 'Other Linux' },
  ];

  let osVersions = $derived(osType === 'windows' ? windowsVersions : linuxVersions);

  const diskBusOptions = [
    { value: 'virtio', label: 'virtio (recommended)' },
    { value: 'sata', label: 'SATA' },
    { value: 'scsi', label: 'SCSI' },
    { value: 'ide', label: 'IDE' },
  ];

  const diskFormatOptions = [
    { value: 'qcow2', label: 'qcow2 (recommended)' },
    { value: 'raw', label: 'raw' },
  ];

  $effect(() => {
    if (osType === 'windows') osVersion = 'win11';
    else osVersion = 'arch';
  });

  onMount(async () => {
    try {
      const [p, n, i] = await Promise.all([api.listPools(), api.listNetworks(), api.listISOs()]);
      pools = p;
      networks = n;
      isos = i;
      const diskPools = pools.filter((p) => p.purpose !== 'iso');
      const preferredDiskPool = diskPools.find((p) => p.name === 'vmmanager-disks');
      storagePool = preferredDiskPool ? preferredDiskPool.name : diskPools[0]?.name || '';
      existingDiskPool = storagePool;
      loadExistingVolumes(existingDiskPool);
    } catch (e) {
      error = 'Error loading data: ' + e.message;
    } finally {
      loadingData = false;
    }
  });

  async function loadExistingVolumes(pool) {
    if (!pool) {
      existingVolumes = [];
      return;
    }
    loadingVolumes = true;
    try {
      const all = (await api.listVolumes(pool)) || [];
      // Internal qcow2 snapshots (e.g. "ubuntu-1.gnome") are
      // valid StorageVolume entries but must never be selected
      // as a primary disk for a new VM — they share the
      // overlay chain of their parent and would corrupt the
      // backing file. Filter them out by the is_snapshot flag
      // set by the backend's H3a classifier.
      existingVolumes = all.filter((v) => !v.is_snapshot);
      if (!existingVolumes.find((v) => v.name === existingDiskName)) {
        existingDiskName = existingVolumes[0]?.name || '';
      }
    } catch {
      existingVolumes = [];
    } finally {
      loadingVolumes = false;
    }
  }

  async function create() {
    touched = { name: true, vcpus: true, ramMB: true, diskSize: true };
    if (!isValid) {
      error = 'Please fix the errors above';
      return;
    }
    loading = true;
    error = '';
    try {
      const payload = {
        name,
        vcpus,
        ram_mb: ramMB,
        storage_pool: useExistingDisk ? undefined : storagePool,
        cpu_mode: cpuMode === 'custom' ? 'custom' : cpuMode,
        cpu_model: cpuMode === 'custom' ? cpuModel : undefined,
        video_model: videoModel,
        network,
        network_model: networkModel,
        iso: iso || undefined,
        os_type: osType,
        os_version: osVersion,
        chipset,
        firmware,
        secure_boot: chipset === 'q35' ? secureBoot : false,
        tpm_enabled: chipset === 'q35' ? tpmEnabled : false,
        disk_gb: useExistingDisk ? undefined : diskSize,
        disk_bus: diskBus,
        disk_format: useExistingDisk ? undefined : diskFormat,
        virtio_iso: virtioISO || undefined,
      };
      if (useExistingDisk) {
        payload.existing_disk_pool = existingDiskPool;
        payload.existing_disk_name = existingDiskName;
      }
      await api.createVM(payload);
      toast.success(`VM "${name}" created`);
      navigate('/vms');
    } catch (e) {
      error = e.message;
      toast.error(e.message);
    } finally {
      loading = false;
    }
  }

  function onBack() {
    navigate('/vms');
  }
</script>

<div class="p-6 max-w-3xl">
  <div class="flex items-center gap-3 mb-6">
    <button
      onclick={onBack}
      class="p-1.5 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
      aria-label="Back"
    >
      <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
        <polyline points="15 18 9 12 15 6" />
      </svg>
    </button>
    <div>
      <h1 class="text-xl font-semibold tracking-tight">Create Virtual Machine</h1>
      <p class="text-sm text-muted-foreground mt-0.5">Configure a new VM</p>
    </div>
  </div>

  {#if error}
    <Alert variant="error">{error}</Alert>
  {/if}

  {#if loadingData}
    <div class="flex items-center justify-center py-20"><Spinner size="lg" /></div>
  {:else}
    <form
      onsubmit={(e) => {
        e.preventDefault();
        create();
      }}
      class="space-y-5"
    >
      <!-- General + OS -->
      <div class="border border-border rounded-lg bg-card p-5">
        <div class="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
          General
        </div>
        <SettingRow
          label="Name"
          helper="Identifies this VM in lists and libvirt"
          error={touched.name ? nameError : ''}
        >
          <Input
            bind:value={name}
            type="text"
            placeholder="my-vm"
            class="max-w-sm tnum"
            aria-invalid={touched.name && nameError ? 'true' : undefined}
            onblur={() => (touched.name = true)}
          />
        </SettingRow>
        <SettingRow label="Operating System" helper="Used to pick drivers and defaults">
          <div class="grid grid-cols-[8rem_minmax(0,1fr)] gap-3 w-full">
            <select bind:value={osType} class="input">
              <option value="linux">Linux</option>
              <option value="windows">Windows</option>
            </select>
            <select bind:value={osVersion} class="input">
              {#each osVersions as v}
                <option value={v.value}>{v.label}</option>
              {/each}
            </select>
          </div>
        </SettingRow>
        <SettingRow label="ISO (optional)" helper="Attach an installation ISO to the first CDROM">
          <select bind:value={iso} class="input max-w-xs">
            <option value="">None (install later)</option>
            {#each isos as isoFile}
              <option value={isoFile.path}>{isoFile.name}</option>
            {/each}
          </select>
        </SettingRow>
      </div>

      <!-- System -->
      <div class="border border-border rounded-lg bg-card p-5">
        <div class="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
          System
        </div>
        <SettingRow label="Chipset" helper="Q35 supports modern features; i440fx is legacy">
          <select
            bind:value={chipset}
            onchange={() => {
              if (chipset === 'i440fx') {
                firmware = 'seabios';
                secureBoot = false;
                tpmEnabled = false;
              }
            }}
            class="input w-40"
          >
            <option value="q35">Q35 (modern)</option>
            <option value="i440fx">i440fx (legacy)</option>
          </select>
        </SettingRow>
        <SettingRow label="BIOS" helper="UEFI required for Secure Boot and TPM">
          <select
            bind:value={firmware}
            disabled={chipset === 'i440fx'}
            class="input w-40 {chipset === 'i440fx' ? 'opacity-50' : ''}"
          >
            <option value="seabios">SeaBIOS</option>
            <option value="uefi">UEFI</option>
          </select>
        </SettingRow>
        {#if chipset === 'q35' && firmware === 'uefi'}
          <SettingRow label="Secure Boot" helper="Requires OVMF UEFI with Secure Boot support">
            <button
              type="button"
              onclick={() => (secureBoot = !secureBoot)}
              class="relative w-9 h-5 rounded-full transition-colors {secureBoot
                ? 'bg-accent'
                : 'bg-muted'}"
              aria-label="Toggle Secure Boot"
            >
              <span
                class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform {secureBoot
                  ? 'translate-x-4'
                  : ''}"
              ></span>
            </button>
          </SettingRow>
          <SettingRow label="TPM 2.0" helper="Emulated TPM (requires swtpm on the host)">
            <button
              type="button"
              onclick={() => (tpmEnabled = !tpmEnabled)}
              class="relative w-9 h-5 rounded-full transition-colors {tpmEnabled
                ? 'bg-accent'
                : 'bg-muted'}"
              aria-label="Toggle TPM"
            >
              <span
                class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform {tpmEnabled
                  ? 'translate-x-4'
                  : ''}"
              ></span>
            </button>
          </SettingRow>
        {/if}
      </div>

      <!-- Hardware -->
      <div class="border border-border rounded-lg bg-card p-5">
        <div class="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
          Hardware
        </div>
        <SettingRow
          label="vCPUs"
          helper="Virtual CPU cores"
          error={touched.vcpus ? vcpusError : ''}
        >
          <Input
            type="number"
            bind:value={vcpus}
            min="1"
            max="64"
            class="w-24 tnum"
            onblur={() => (touched.vcpus = true)}
          />
        </SettingRow>
        <SettingRow
          label="RAM (MB)"
          helper="Allocated memory"
          error={touched.ramMB ? ramError : ''}
        >
          <Input
            type="number"
            bind:value={ramMB}
            min="512"
            step="512"
            class="w-28 tnum"
            onblur={() => (touched.ramMB = true)}
          />
        </SettingRow>
        <SettingRow
          label="Use existing disk"
          helper="Attach an existing volume instead of creating a new one"
        >
          <button
            type="button"
            onclick={() => (useExistingDisk = !useExistingDisk)}
            class="relative w-9 h-5 rounded-full transition-colors {useExistingDisk
              ? 'bg-accent'
              : 'bg-muted'}"
            aria-label="Toggle existing disk"
          >
            <span
              class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform {useExistingDisk
                ? 'translate-x-4'
                : ''}"
            ></span>
          </button>
        </SettingRow>
        {#if useExistingDisk}
          <SettingRow label="Disk pool" helper="Pool containing the volume">
            <select
              bind:value={existingDiskPool}
              onchange={() => loadExistingVolumes(existingDiskPool)}
              class="input max-w-xs"
            >
              {#each pools.filter((p) => p.purpose !== 'iso') as p}
                <option value={p.name}>{p.name}</option>
              {/each}
            </select>
          </SettingRow>
          <SettingRow label="Existing volume" helper="Will be attached as the primary disk">
            <select
              bind:value={existingDiskName}
              class="input max-w-xs"
              disabled={loadingVolumes || existingVolumes.length === 0}
            >
              {#if loadingVolumes}
                <option value="">Loading...</option>
              {:else if existingVolumes.length === 0}
                <option value="">No volumes</option>
              {:else}
                {#each existingVolumes as v}
                  <option value={v.name}
                    >{v.name} ({(v.capacity / 1024 / 1024 / 1024).toFixed(1)} GB)</option
                  >
                {/each}
              {/if}
            </select>
          </SettingRow>
        {:else}
          <SettingRow label="Storage pool" helper="Pool for the new disk">
            <select bind:value={storagePool} class="input max-w-xs">
              {#each pools.filter((p) => p.purpose !== 'iso') as p}
                <option value={p.name}>{p.name}</option>
              {/each}
            </select>
          </SettingRow>
          <SettingRow
            label="Disk size (GB)"
            helper="Maximum virtual disk size"
            error={touched.diskSize ? diskSizeError : ''}
          >
            <Input
              type="number"
              bind:value={diskSize}
              min="1"
              max="1024"
              class="w-24 tnum"
              onblur={() => (touched.diskSize = true)}
            />
          </SettingRow>
          <SettingRow label="Disk format" helper="qcow2 supports snapshots; raw is faster">
            <select bind:value={diskFormat} class="input max-w-xs">
              {#each diskFormatOptions as o}
                <option value={o.value}>{o.label}</option>
              {/each}
            </select>
          </SettingRow>
        {/if}
        <SettingRow label="Disk bus" helper="VirtIO is fastest; SATA is most compatible">
          <select bind:value={diskBus} class="input max-w-xs">
            {#each diskBusOptions as o}
              <option value={o.value}>{o.label}</option>
            {/each}
          </select>
        </SettingRow>
        {#if osType === 'windows'}
          <SettingRow
            label="VirtIO drivers ISO"
            helper="e.g. virtio-win.iso, needed for Windows install"
          >
            <select bind:value={virtioISO} class="input max-w-xs">
              <option value="">None</option>
              {#each isos as isoFile}
                <option value={isoFile.path}>{isoFile.name}</option>
              {/each}
            </select>
          </SettingRow>
        {/if}
      </div>

      <!-- CPU + Video + Network -->
      <div class="border border-border rounded-lg bg-card p-5">
        <div class="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
          Advanced
        </div>
        <SettingRow
          label="CPU mode"
          helper="Passthrough exposes host CPU features; custom lets you pick a model"
        >
          <select bind:value={cpuMode} class="input max-w-xs">
            {#each cpuModes as m}
              <option value={m.value}>{m.label}</option>
            {/each}
          </select>
        </SettingRow>
        {#if cpuMode === 'custom'}
          <SettingRow label="CPU model" helper="e.g. EPYC, Skylake-Server, qemu64">
            <Input bind:value={cpuModel} type="text" placeholder="EPYC" class="max-w-xs" />
          </SettingRow>
        {/if}
        <SettingRow label="Video model" helper="virtio for modern guests; qxl for Spice">
          <select bind:value={videoModel} class="input max-w-xs">
            {#each videoModels as m}
              <option value={m.value}>{m.label}</option>
            {/each}
          </select>
        </SettingRow>
        <SettingRow label="Network" helper="Default is libvirt NAT">
          <select bind:value={network} class="input max-w-xs">
            {#each networks as net}
              <option value={net.name}>{net.name} ({net.forward || 'isolated'})</option>
            {/each}
          </select>
        </SettingRow>
        <SettingRow label="Adapter" helper="virtio is recommended">
          <select bind:value={networkModel} class="input max-w-xs">
            {#each networkModels as m}
              <option value={m.value}>{m.label}</option>
            {/each}
          </select>
        </SettingRow>
      </div>

      <div class="flex items-center gap-2 pt-2">
        <Button type="submit" disabled={loading || !isValid}>
          {#if loading}
            <Spinner size="sm" color="text-white" />
            Creating...
          {:else}
            Create VM
          {/if}
        </Button>
        <Button type="button" variant="outline" onclick={onBack}>Cancel</Button>
      </div>
    </form>
  {/if}
</div>
