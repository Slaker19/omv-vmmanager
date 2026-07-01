<script>
  import Alert from '$lib/components/Alert.svelte';
  import Spinner from '$lib/components/Spinner.svelte';
  import { onMount } from 'svelte';
  import { api } from '$lib/stores/auth.svelte.js';
  import { events } from '$lib/stores/events.svelte.js';
  import { navigate, getRoute } from '$lib/router.svelte.js';
  import { toast } from '$lib/components/ui/toast';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import * as Dialog from '$lib/components/ui/dialog';
  import Chart from '$lib/components/Chart.svelte';
  import Switch from '$lib/components/Switch.svelte';

  let { vmId } = $props();

  let vm = $state(null);
  let snapshots = $state([]);
  let bootDevice = $state('hd');
  let loading = $state(true);
  let error = $state('');
  let actionLoading = $state('');
  // Derived: `actionLoading` is a string ('' when idle). Without this
  // coercion, `disabled={actionLoading}` becomes `disabled=""` in
  // the rendered HTML, which is truthy and dims every button even
  // when no action is in flight.
  const busy = $derived(!!actionLoading);

  // Autostart: separate flag from actionLoading because the
  // autostart toggle is a fast PATCH-style operation that
  // shouldn't dim the Start/Shutdown buttons while in flight.
  // The visual state lives on `vm.autostart` (the Switch
  // mirrors it via its `checked` prop); on failure we restore
  // the previous value.
  let autostartSaving = $state(false);

  let snapName = $state('');
  let snapDesc = $state('');

  // Metrics (Phase 21): in-memory time series per metric, updated via
  // SSE and on mount via a single REST fetch.
  let metrics = $state(null);
  const last60 = (arr) => (Array.isArray(arr) ? arr.slice(-60) : []);
  const cpuPoints = $derived(last60(metrics?.cpu?.points));
  const ramPoints = $derived(last60(metrics?.ram?.points));
  const diskRPoints = $derived(last60(metrics?.disk_read?.points));
  const diskWPoints = $derived(last60(metrics?.disk_write?.points));
  const netRxPoints = $derived(last60(metrics?.net_rx?.points));
  const netTxPoints = $derived(last60(metrics?.net_tx?.points));

  // Edit state
  let showEdit = $state(false);
  let eName = $state('');
  let eVcpus = $state(2);
  let eRamMB = $state(2048);
  let eCPUMode = $state('host-passthrough');
  let eVideoModel = $state('virtio');
  let eNetwork = $state('default');
  let eNetworkModel = $state('virtio');
  let eChipset = $state('q35');
  let eSecureBoot = $state(false);
  let eTPM = $state(false);
  let eFirmware = $state('uefi');
  let eOSType = $state('');
  let eOSVersion = $state('');
  let editSaving = $state(false);

  const networkModels = [
    { value: 'virtio', label: 'virtio (recommended)' },
    { value: 'e1000e', label: 'e1000e (Intel, ideal for Windows)' },
    { value: 'e1000', label: 'e1000 (legacy Intel)' },
    { value: 'rtl8139', label: 'rtl8139 (Realtek, very compatible)' },
    { value: 'pcnet', label: 'pcnet (AMD, legacy)' },
  ];

  // Add Disk state
  let showAddDisk = $state(false);
  let aDiskDevice = $state('disk');
  let aDiskBus = $state('virtio');
  let aDiskSize = $state(10);
  let aDiskPool = $state('vmmanager-disks');
  let aDiskISO = $state('');
  let aDiskFormat = $state('qcow2');

  // Change ISO state
  let showChangeISO = $state(false);
  let cISOTarget = $state('');
  let cISOSource = $state('');

  // Resize Disk state
  let showResizeDisk = $state(false);
  let resizeDiskTarget = $state('');
  let resizeDiskSize = $state(10);
  let resizeDiskCurrent = $state(0);

  // Add Net state
  let showAddNet = $state(false);
  let aNetNetwork = $state('default');
  let aNetModel = $state('virtio');

  // Clone state
  let showClone = $state(false);
  let cName = $state('');
  let cPool = $state('vmmanager-disks');

  // Export state
  let showExport = $state(false);
  let exportTarget = $state('vmware');
  let exportProgress = $state(null);
  let exportAbort = $state(null);

  // Identity / metadata state (Phase 16)
  let showIdentity = $state(false);
  let identityTab = $state('alias'); // 'alias' | 'cover' | 'network' | 'notes' | 'groups'
  let eAlias = $state('');
  let eNotes = $state('');
  let eNotesOriginal = $state(''); // tracks the last-saved value for blur autosave
  let eGroupsText = $state('');
  let eGroupsList = $state([]); // groups available to assign
  let coverFile = $state(null);
  let coverPreview = $state(null);
  let uploadingCover = $state(false);
  let vlanSupportByNetwork = $state({}); // networkName -> { supported, reason }
  let ifaceEdits = $state({}); // mac -> { mac, network, vlan, busy, error }
  let savingIdentity = $state(false);
  let notesStatus = $state(''); // '' | 'saving' | 'saved' | 'error'
  let notesError = $state('');

  // Confirm dialogs
  let confirmState = $state({
    open: false,
    title: '',
    description: '',
    confirmLabel: 'Confirm',
    variant: 'default',
    onConfirm: () => {},
    loading: false,
  });

  let pools = $state([]);
  let networks = $state([]);
  let isos = $state([]);

  const stateColors = {
    running: 'bg-status-running',
    shutoff: 'bg-status-shutoff',
    paused: 'bg-status-paused',
    crashed: 'bg-status-crashed',
  };

  onMount(() => {
    load();
    loadMetrics();
    const offMetrics = events.onVmMetrics((e) => {
      if (e.vm_id !== vmId) return;
      metrics = e.data;
    });

    // Subscribe to VM state events for this VM
    const off = events.onVmState((e) => {
      if (e.vm_id !== vmId) return;
      if (vm && vm.state !== e.state) {
        vm = { ...vm, state: e.state, name: e.name || vm.name };
        // Light refetch to update uptime/ip
        load(true);
      }
    });
    return () => {
      off();
      offMetrics();
    };
  });

  async function loadMetrics() {
    try {
      metrics = await api.getVMMetrics(vmId);
    } catch {
      metrics = null;
    }
  }

  async function load(silent = false) {
    if (!vmId) return;
    if (!silent) loading = true;
    error = '';
    try {
      const [vmData, snapData, bootData] = await Promise.all([
        api.getVM(vmId),
        api.listSnapshots(vmId),
        api.getBootDevice(vmId).catch(() => ({ boot_device: 'hd' })),
      ]);
      vm = vmData;
      snapshots = snapData;
      if (bootData) bootDevice = bootData.boot_device;
      Promise.all([
        api
          .listNetworks()
          .then((n) => (networks = n))
          .catch(() => {}),
        api
          .listPools()
          .then((p) => (pools = p))
          .catch(() => {}),
        api
          .listISOs()
          .then((i) => (isos = i))
          .catch(() => {}),
        api
          .getVMMeta(vmId)
          .then((m) => {
            // Cover changes are picked up by re-render, alias/groups
            // in vm are already populated from ListVMs. We only need
            // to keep them in sync after detail reloads.
            if (vm && m) {
              vm = { ...vm, alias: m.alias || '', cover: m.cover || '', groups: m.groups || [] };
            }
          })
          .catch(() => {}),
      ]);
    } catch (e) {
      error = e.message;
    } finally {
      if (!silent) loading = false;
    }
  }

  async function openIdentity() {
    identityTab = 'alias';
    coverFile = null;
    coverPreview = null;
    notesStatus = '';
    notesError = '';
    try {
      const meta = await api.getVMMeta(vmId);
      eAlias = meta.alias || '';
      eNotes = meta.notes || '';
      eNotesOriginal = meta.notes || '';
      eGroupsText = (meta.groups || []).join(', ');
    } catch {
      eAlias = vm?.alias || '';
      eNotes = '';
      eNotesOriginal = '';
      eGroupsText = (vm?.groups || []).join(', ');
    }
    // Initialize iface edit state for each network interface.
    const edits = {};
    for (const iface of vm.networks || []) {
      edits[iface.mac] = {
        mac: iface.mac,
        network: iface.network,
        vlan: '',
        busy: false,
        error: '',
      };
    }
    ifaceEdits = edits;
    vlanSupportByNetwork = {};
    // Pre-fetch VLAN support for each network this VM uses.
    const uniqueNets = [...new Set((vm.networks || []).map((n) => n.network).filter(Boolean))];
    await Promise.all(
      uniqueNets.map(async (net) => {
        try {
          vlanSupportByNetwork[net] = await api.checkVLANSupport(net);
        } catch (e) {
          vlanSupportByNetwork[net] = { supported: false, reason: e.message };
        }
      })
    );
    // Load available groups (for the alias/cover tab also has a groups list).
    try {
      const grp = await api.listGroups();
      eGroupsList = grp.groups || [];
    } catch {
      eGroupsList = [];
    }
    showIdentity = true;
  }

  async function saveIdentityBasics() {
    // Save alias + notes + groups in a single PUT.
    savingIdentity = true;
    try {
      const groups = eGroupsText
        .split(/[\s,;]+/)
        .map((s) => s.trim())
        .filter(Boolean);
      await api.updateVMMeta(vmId, {
        alias: eAlias,
        notes: eNotes,
        groups: groups,
      });
      eNotesOriginal = eNotes;
      vm = { ...vm, alias: eAlias, groups };
      toast.success('Identity updated');
    } catch (e) {
      toast.error(e.message);
    } finally {
      savingIdentity = false;
    }
  }

  // Notes blur-autosave: only fires if the value actually changed since
  // the last save (openIdentity / successful save updates eNotesOriginal).
  async function saveNotesIfChanged() {
    if (eNotes === eNotesOriginal) return;
    notesStatus = 'saving';
    notesError = '';
    try {
      await api.updateVMMeta(vmId, { notes: eNotes });
      eNotesOriginal = eNotes;
      notesStatus = 'saved';
      setTimeout(() => {
        if (notesStatus === 'saved') notesStatus = '';
      }, 2000);
    } catch (e) {
      notesStatus = 'error';
      notesError = e.message;
    }
  }

  function onCoverPicked(e) {
    const f = e.target.files?.[0];
    if (!f) return;
    coverFile = f;
    const reader = new FileReader();
    reader.onload = () => (coverPreview = reader.result);
    reader.readAsDataURL(f);
  }

  async function uploadCover() {
    if (!coverFile) return;
    uploadingCover = true;
    try {
      const res = await api.uploadCover(vmId, coverFile);
      vm = { ...vm, cover: res.url };
      toast.success('Cover updated');
      coverFile = null;
      coverPreview = null;
    } catch (e) {
      toast.error(e.message);
    } finally {
      uploadingCover = false;
    }
  }

  async function removeCover() {
    try {
      await api.deleteCover(vmId);
      vm = { ...vm, cover: '' };
      toast.success('Cover removed');
    } catch (e) {
      toast.error(e.message);
    }
  }

  async function saveIface(mac) {
    const cur = ifaceEdits[mac];
    if (!cur) return;
    cur.error = '';
    cur.busy = true;
    ifaceEdits = { ...ifaceEdits };
    const newMac = cur.mac.trim();
    const vlanRaw = cur.vlan.trim();
    let vlanTag = null;
    if (vlanRaw !== '') {
      const n = parseInt(vlanRaw, 10);
      if (isNaN(n) || n < 0 || n > 4094) {
        cur.error = 'VLAN tag must be 0–4094 (0 = remove VLAN)';
        cur.busy = false;
        ifaceEdits = { ...ifaceEdits };
        return;
      }
      vlanTag = n;
    }
    const payload = {};
    if (newMac && newMac !== mac) payload.mac = newMac;
    if (cur.network && cur.network !== (vm.networks.find((i) => i.mac === mac)?.network || '')) {
      payload.network = cur.network;
    }
    if (vlanTag !== null) payload.vlan_tag = vlanTag;
    if (Object.keys(payload).length === 0) {
      cur.busy = false;
      ifaceEdits = { ...ifaceEdits };
      toast.info('Nothing to save');
      return;
    }
    try {
      await api.updateNetIface(vmId, mac, payload);
      toast.success('Network interface updated');
      await load();
      // Re-seed the edit state for the (possibly new) MAC.
      const updatedIface = (vm.networks || []).find(
        (i) => i.mac === newMac || i.mac === cur.network
      );
      if (newMac && newMac !== mac) {
        delete ifaceEdits[mac];
      }
      if (updatedIface) {
        ifaceEdits[updatedIface.mac] = {
          mac: updatedIface.mac,
          network: updatedIface.network,
          vlan: '',
          busy: false,
          error: '',
        };
      }
    } catch (e) {
      // Revert: keep the form value the user typed, but show the error.
      cur.error = e.message;
    } finally {
      cur.busy = false;
      ifaceEdits = { ...ifaceEdits };
    }
  }

  function openEdit() {
    eName = vm.name;
    eVcpus = vm.vcpus;
    eRamMB = vm.ram_mb;
    eCPUMode = vm.cpu_mode || 'host-passthrough';
    eVideoModel = vm.video_model || 'virtio';
    eNetwork = vm.networks?.[0]?.network || networks[0]?.name || 'default';
    eNetworkModel = vm.networks?.[0]?.model || 'virtio';
    eChipset = vm.chipset || 'q35';
    eSecureBoot = vm.secure_boot;
    eTPM = vm.tpm_enabled;
    eFirmware = vm.chipset === 'i440fx' ? 'seabios' : vm.firmware || 'seabios';
    if (eChipset === 'i440fx') {
      eSecureBoot = false;
      eTPM = false;
    }
    eOSType = vm.os_type || '';
    eOSVersion = vm.os_version || '';
    showEdit = true;
  }

  async function saveEdit() {
    editSaving = true;
    try {
      const data = {};
      if (eName !== vm.name) data.name = eName;
      if (eVcpus !== vm.vcpus) data.vcpus = eVcpus;
      if (eRamMB !== vm.ram_mb) data.ram_mb = eRamMB;
      if (eCPUMode !== (vm.cpu_mode || 'host-passthrough')) data.cpu_mode = eCPUMode;
      if (eVideoModel !== (vm.video_model || 'virtio')) data.video_model = eVideoModel;
      if (eNetwork !== (vm.networks?.[0]?.network || networks[0]?.name || 'default'))
        data.network = eNetwork;
      if (eNetworkModel !== (vm.networks?.[0]?.model || 'virtio'))
        data.network_model = eNetworkModel;
      if (eOSType !== (vm.os_type || '')) data.os_type = eOSType;
      if (eOSVersion !== (vm.os_version || '')) data.os_version = eOSVersion;
      const effSecureBoot = eFirmware === 'uefi' ? eSecureBoot : false;
      const effTPM = eFirmware === 'uefi' ? eTPM : false;
      if (effSecureBoot !== vm.secure_boot) data.secure_boot = effSecureBoot;
      if (effTPM !== vm.tpm_enabled) data.tpm_enabled = effTPM;
      if (eFirmware !== (vm.firmware || 'uefi')) data.firmware = eFirmware;
      await api.updateVM(vmId, data);
      showEdit = false;
      toast.success('Settings updated');
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      editSaving = false;
    }
  }

  function askConfirm(opts) {
    confirmState = { ...opts, open: true, loading: false };
  }

  async function doAction(action) {
    if (action === 'forceOffVM') {
      askConfirm({
        title: 'Force off VM?',
        description: 'May cause data loss. Use only if the VM is unresponsive.',
        confirmLabel: 'Force Off',
        variant: 'destructive',
        onConfirm: async () => {
          await doActionRun(action);
        },
      });
      return;
    }
    if (action === 'forceRebootVM') {
      askConfirm({
        title: 'Force reboot VM?',
        description: 'Forces the VM off and starts it again. May cause data loss.',
        confirmLabel: 'Force Reboot',
        variant: 'destructive',
        onConfirm: async () => {
          await doActionRun(action);
        },
      });
      return;
    }
    if (action === 'deleteVM') {
      askConfirm({
        title: 'Delete VM?',
        description:
          'This will permanently delete the VM configuration. Disks will NOT be removed.',
        confirmLabel: 'Delete',
        variant: 'destructive',
        onConfirm: async () => {
          confirmState.loading = true;
          try {
            await api.deleteVM(vmId);
            confirmState.open = false;
            toast.success(`VM "${vm.name}" deleted`);
            navigate('/vms');
          } catch (e) {
            toast.error(e.message);
            confirmState.loading = false;
          }
        },
      });
      return;
    }
    await doActionRun(action);
  }

  // toggleAutostart flips libvirtd's per-VM autostart flag. It
  // is intentionally separate from doActionRun (which dims the
  // whole Actions card) — the round-trip is fast and the user
  // reported that accidentally toggling autostart was the
  // annoyance that motivated this control, so a quick, narrow
  // visual confirmation is the right feedback.
  //
  // On failure we restore the previous value (the Switch's
  // local `checked` already flipped optimistically via the
  // Switch's onclick handler; we re-set it from the still-
  // unchanged vm.autostart). The toast is sticky so the user
  // doesn't miss the libvirt error.
  async function toggleAutostart(next) {
    if (!vm) return;
    const previous = vm.autostart;
    autostartSaving = true;
    try {
      await api.setVMAutostart(vmId, next);
      // Reflect the new value on the VM object so any other
      // UI reading vm.autostart (e.g. a future list view
      // badge) stays in sync.
      vm = { ...vm, autostart: next };
      toast.success(next ? 'Autostart enabled' : 'Autostart disabled', { duration: 3000 });
    } catch (e) {
      // Roll the Switch back to the previous value.
      vm = { ...vm, autostart: previous };
      toast.error(`Failed to set autostart: ${e.message}`, { duration: 0 });
    } finally {
      autostartSaving = false;
    }
  }

  async function doActionRun(action) {
    actionLoading = action;
    try {
      if (action === 'forceRebootVM') {
        await api.forceOffVM(vmId);
        await waitForState('shutoff', 30000);
        await api.startVM(vmId);
      } else {
        await api[action](vmId);
      }
      const labels = {
        startVM: 'started',
        shutdownVM: 'shut down',
        rebootVM: 'rebooted',
        forceRebootVM: 'force-rebooted',
        suspendVM: 'suspended',
        resumeVM: 'resumed',
        forceOffVM: 'force-off',
      };
      toast.success(`VM ${labels[action] || 'updated'}`);
      confirmState.open = false;
      await load();
    } catch (e) {
      toast.error(e.message, { duration: 0 });
    } finally {
      actionLoading = '';
    }
  }

  async function waitForState(targetState, timeoutMs = 30000) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const cur = await api.getVM(vmId);
      if (cur.state === targetState) return;
      await new Promise(r => setTimeout(r, 500));
    }
    throw new Error(`Timeout waiting for VM state: ${targetState}`);
  }

  async function createSnapshot() {
    if (!snapName) return;
    actionLoading = 'snapshot';
    try {
      await api.createSnapshot(vmId, { name: snapName, description: snapDesc });
      snapName = '';
      snapDesc = '';
      toast.success('Snapshot created');
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      actionLoading = '';
    }
  }

  async function revertSnapshot(sid) {
    askConfirm({
      title: 'Revert to snapshot?',
      description: 'Current VM state will be lost. The VM will be reverted to this snapshot.',
      confirmLabel: 'Revert',
      variant: 'destructive',
      onConfirm: async () => {
        confirmState.loading = true;
        try {
          await api.revertSnapshot(vmId, sid);
          confirmState.open = false;
          toast.success('Reverted to snapshot');
          await load();
        } catch (e) {
          toast.error(e.message);
          confirmState.loading = false;
        }
      },
    });
  }

  function deleteSnapshot(sid) {
    askConfirm({
      title: 'Delete snapshot?',
      description: 'This will permanently delete the snapshot. Cannot be undone.',
      confirmLabel: 'Delete',
      variant: 'destructive',
      onConfirm: async () => {
        confirmState.loading = true;
        try {
          await api.deleteSnapshot(vmId, sid);
          confirmState.open = false;
          toast.success('Snapshot deleted');
          await load();
        } catch (e) {
          toast.error(e.message);
          confirmState.loading = false;
        }
      },
    });
  }

  async function addDisk() {
    actionLoading = 'adddisk';
    try {
      const data = { device: aDiskDevice, bus: aDiskBus, format: aDiskFormat };
      if (aDiskDevice === 'cdrom') data.source = aDiskISO;
      else {
        data.size_gb = aDiskSize;
        data.pool = aDiskPool;
      }
      await api.createDisk(vmId, data);
      showAddDisk = false;
      toast.success('Disk added');
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      actionLoading = '';
    }
  }

  async function changeISO() {
    if (!cISOTarget) return;
    actionLoading = 'changeiso';
    try {
      await api.updateDiskSource(vmId, cISOTarget, cISOSource);
      showChangeISO = false;
      toast.success('ISO changed');
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      actionLoading = '';
    }
  }

  async function resizeDisk() {
    if (!resizeDiskTarget) return;
    actionLoading = 'resizedisk';
    try {
      const disk = vm.disks.find((d) => d.target === resizeDiskTarget);
      if (!disk) throw new Error('Disk not found');
      if (!disk.pool) throw new Error('Disk storage pool unknown');
      const volName = disk.source.split('/').pop();
      await api.resizeVolume(disk.pool, volName, resizeDiskSize);
      showResizeDisk = false;
      toast.success('Disk resized');
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      actionLoading = '';
    }
  }

  function removeDisk(target) {
    askConfirm({
      title: `Remove disk ${target}?`,
      description:
        'The disk will be detached from the VM. The underlying file will NOT be deleted.',
      confirmLabel: 'Remove',
      variant: 'destructive',
      onConfirm: async () => {
        confirmState.loading = true;
        try {
          await api.deleteDisk(vmId, target);
          confirmState.open = false;
          toast.success('Disk removed');
          await load();
        } catch (e) {
          toast.error(e.message);
          confirmState.loading = false;
        }
      },
    });
  }

  async function addNet() {
    actionLoading = 'addnet';
    try {
      await api.createNetIface(vmId, { network: aNetNetwork, model: aNetModel });
      showAddNet = false;
      toast.success('Network interface added');
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      actionLoading = '';
    }
  }

  function removeNet(mac) {
    askConfirm({
      title: `Remove network interface?`,
      description: `MAC ${mac} will be detached from the VM.`,
      confirmLabel: 'Remove',
      variant: 'destructive',
      onConfirm: async () => {
        confirmState.loading = true;
        try {
          await api.deleteNetIface(vmId, mac);
          confirmState.open = false;
          toast.success('Network interface removed');
          await load();
        } catch (e) {
          toast.error(e.message);
          confirmState.loading = false;
        }
      },
    });
  }

  async function cloneVM() {
    if (!cName) return;
    actionLoading = 'clone';
    try {
      await api.cloneVM(vmId, { name: cName, pool: cPool });
      showClone = false;
      toast.success(`VM cloned as "${cName}"`);
      await load();
    } catch (e) {
      toast.error(e.message);
    } finally {
      actionLoading = '';
    }
  }

  // Export
  function exportVM() {
    if (vm.state === 'running') {
      toast.error('Shut off the VM before exporting');
      return;
    }
    exportTarget = 'vmware';
    exportProgress = null;
    showExport = true;
  }

  async function startExport() {
    if (vm.state === 'running') {
      toast.error('Shut off the VM before exporting');
      showExport = false;
      return;
    }
    const label =
      exportTarget === 'backup' ? 'Building WebVM backup' : `Building OVA (${exportTarget})`;
    exportProgress = { received: 0, total: 0, percent: 0, label };
    const ac = new AbortController();
    exportAbort = ac;
    try {
      const opts = {
        signal: ac.signal,
        onProgress: (p) => {
          exportProgress = { received: p.received, total: p.total, percent: p.percent, label };
        },
      };
      if (exportTarget === 'backup') {
        opts.format = 'backup';
        opts.compress = true;
      } else {
        opts.format = 'ova';
        opts.target = exportTarget;
      }
      const result = await api.exportVM(vm.name, opts);
      exportProgress = { received: result.size, total: result.size, percent: 100, label: 'Done' };
      toast.success('Export complete');
      setTimeout(() => {
        showExport = false;
        exportProgress = null;
        exportAbort = null;
      }, 800);
    } catch (e) {
      if (e.name === 'AbortError') {
        exportProgress = { received: 0, total: 0, percent: 0, label: 'Cancelled' };
        setTimeout(() => {
          showExport = false;
          exportProgress = null;
          exportAbort = null;
        }, 600);
      } else {
        toast.error(e.message);
        showExport = false;
        exportProgress = null;
        exportAbort = null;
      }
    }
  }

  function cancelExport() {
    if (exportAbort) exportAbort.abort();
  }

  function onBack() {
    navigate('/vms');
  }

  function formatUptime(s) {
    if (!s) return '—';
    const d = Math.floor(s / 86400),
      h = Math.floor((s % 86400) / 3600),
      m = Math.floor((s % 3600) / 60);
    const parts = [];
    if (d) parts.push(d + 'd');
    if (h) parts.push(h + 'h');
    if (m) parts.push(m + 'm');
    return parts.join(' ') || '<1m';
  }

  function formatRate(b) {
    if (b == null) return '0 B/s';
    if (b < 1024) return b.toFixed(0) + ' B/s';
    if (b < 1024 * 1024) return (b / 1024).toFixed(1) + ' KB/s';
    if (b < 1024 * 1024 * 1024) return (b / 1024 / 1024).toFixed(2) + ' MB/s';
    return (b / 1024 / 1024 / 1024).toFixed(2) + ' GB/s';
  }

  function bytesToStr(b) {
    if (!b) return '0 B';
    const u = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let n = b;
    while (n >= 1024 && i < u.length - 1) {
      n /= 1024;
      i++;
    }
    return n.toFixed(i > 0 ? 1 : 0) + ' ' + u[i];
  }

  function diskLabel(d) {
    if (d.device === 'cdrom') return d.name || (d.source ? d.source.split('/').pop() : '(empty)');
    return d.name || (d.source ? d.source.split('/').pop() : d.target);
  }

  // Build a snapshot tree from the flat list. Roots have parent_name == "".
  const snapshotTree = $derived.by(() => buildSnapshotTree(snapshots));

  function buildSnapshotTree(flat) {
    if (!Array.isArray(flat) || flat.length === 0) return { roots: [], byId: {} };
    const byId = {};
    for (const s of flat) byId[s.name] = { ...s, children: [] };
    const roots = [];
    for (const k of Object.keys(byId)) {
      const node = byId[k];
      if (node.parent_name && byId[node.parent_name]) {
        byId[node.parent_name].children.push(node);
      } else {
        roots.push(node);
      }
    }
    // Sort by creation time ascending so children appear below parents.
    const sortRec = (nodes) => {
      nodes.sort((a, b) => (a.creation_time || 0) - (b.creation_time || 0));
      nodes.forEach((n) => sortRec(n.children));
    };
    sortRec(roots);
    return { roots, byId };
  }

  function formatSnapshotDate(epoch) {
    if (!epoch) return '—';
    const d = new Date(epoch * 1000);
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    return `${y}-${m}-${day} ${hh}:${mm}`;
  }

  // Deep-link from Storage: ?tab=snapshots scrolls the snapshot
  // section into view. Re-runs whenever the route query changes.
  $effect(() => {
    const r = getRoute();
    if (r.query?.tab === 'snapshots') {
      // Defer to next tick so the section is rendered first.
      queueMicrotask(() => {
        const el = document.getElementById('snapshots');
        if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      });
    }
  });
</script>

<div class="p-6 max-w-6xl">
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
    {#if vm}
      <div class="flex items-center gap-3 flex-1 flex-wrap min-w-0">
        <span class="status-dot {stateColors[vm.state] || stateColors.crashed}"></span>
        <h1 class="text-xl font-semibold tracking-tight truncate">{vm.alias || vm.name}</h1>
        {#if vm.alias && vm.alias !== vm.name}
          <span class="text-xs text-muted-foreground font-mono">({vm.name})</span>
        {/if}
        <span class="text-xs text-muted-foreground capitalize">{vm.state}</span>
        {#if vm.state === 'running' && vm.ip}
          <span
            class="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded bg-accent/10 border border-accent/20 text-accent font-mono"
          >
            {vm.ip}
          </span>
        {/if}
      </div>
    {/if}
  </div>

  {#if error}
    <Alert variant="error">{error}</Alert>
  {/if}

  {#if loading}
    <div class="flex items-center justify-center py-24"><Spinner size="lg" /></div>
  {:else if vm}
    <div class="grid grid-cols-1 lg:grid-cols-[1fr_280px] gap-5">
      <!-- Main column -->
      <div class="space-y-5">
        <!-- Overview -->
        <div class="border border-border rounded-lg bg-card p-5">
          <h2 class="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">
            Overview
          </h2>
          <div class="grid grid-cols-3 gap-3 mb-4">
            <div class="border border-border rounded-md p-3 bg-background">
              <p class="text-2xl font-semibold tnum">{vm.vcpus}</p>
              <p class="text-xs text-muted-foreground mt-0.5">vCPUs</p>
              {#if vm.cpu_usage != null}<p class="text-xs text-accent mt-0.5 tnum">
                  {vm.cpu_usage.toFixed(1)}% used
                </p>{/if}
            </div>
            <div class="border border-border rounded-md p-3 bg-background">
              <p class="text-2xl font-semibold tnum">{vm.ram_mb}</p>
              <p class="text-xs text-muted-foreground mt-0.5">RAM (MB)</p>
              {#if vm.ram_used_mb != null}<p class="text-xs text-accent mt-0.5 tnum">
                  {vm.ram_used_mb} MB used
                </p>{/if}
            </div>
            <div class="border border-border rounded-md p-3 bg-background">
              <p class="text-lg font-semibold tnum">{formatUptime(vm.uptime_sec)}</p>
              <p class="text-xs text-muted-foreground mt-0.5">Uptime</p>
            </div>
          </div>
          <div class="grid grid-cols-2 gap-x-6 gap-y-1.5 text-sm">
            {#if vm.os_type}
              <div class="flex gap-2">
                <span class="text-muted-foreground shrink-0">OS:</span><span class="truncate"
                  >{vm.os_type}{vm.os_version ? ' ' + vm.os_version : ''}</span
                >
              </div>
            {/if}
            <div class="flex gap-2">
              <span class="text-muted-foreground shrink-0">Chipset:</span><span>{vm.chipset}</span>
            </div>
            <div class="flex gap-2">
              <span class="text-muted-foreground shrink-0">Secure Boot:</span><span
                >{vm.secure_boot ? 'Yes' : 'No'}</span
              >
            </div>
            <div class="flex gap-2">
              <span class="text-muted-foreground shrink-0">TPM:</span><span
                >{vm.tpm_enabled ? 'Yes' : 'No'}</span
              >
            </div>
            <div class="flex gap-2">
              <span class="text-muted-foreground shrink-0">BIOS:</span><span class="capitalize"
                >{vm.firmware || '?'}</span
              >
            </div>
            <div class="flex gap-2">
              <span class="text-muted-foreground shrink-0">CPU Mode:</span><span
                >{vm.cpu_mode || 'host-passthrough'}</span
              >
            </div>
            <div class="flex gap-2">
              <span class="text-muted-foreground shrink-0">Video:</span><span
                >{vm.video_model || 'virtio'}</span
              >
            </div>
            <div class="flex items-center gap-2">
              <span class="text-muted-foreground shrink-0">Boot:</span>
              <select
                bind:value={bootDevice}
                onchange={() =>
                  api
                    .setBootDevice(vmId, bootDevice)
                    .then(() => load())
                    .catch((e) => toast.error(e.message))}
                class="input !py-1 !text-xs w-auto"
              >
                <option value="hd">Hard Disk</option>
                <option value="cdrom">CDROM</option>
                <option value="network">Network</option>
              </select>
            </div>
          </div>
        </div>

        <!-- Metrics -->
        <div class="border border-border rounded-lg bg-card p-5">
          <div class="flex items-center justify-between mb-3">
            <h2 class="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
              Metrics <span class="text-xs normal-case font-normal">(last hour)</span>
            </h2>
            <span class="text-xs text-muted-foreground tnum"
              >updated {metrics?.sampled_at
                ? new Date(metrics.sampled_at * 1000).toLocaleTimeString()
                : '—'}</span
            >
          </div>
          {#if vm?.state !== 'running'}
            <p class="text-sm text-muted-foreground">
              Metrics are only collected while the VM is running. Start the VM to begin sampling.
            </p>
          {:else if !metrics || (cpuPoints.length === 0 && ramPoints.length === 0)}
            <p class="text-sm text-muted-foreground">Collecting first samples… (every 5 s)</p>
          {:else}
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <div class="flex items-baseline justify-between mb-1.5">
                  <span class="text-xs font-medium text-muted-foreground uppercase tracking-wider"
                    >CPU</span
                  >
                  <span class="text-sm tnum"
                    >{cpuPoints.length
                      ? cpuPoints[cpuPoints.length - 1].v.toFixed(1)
                      : '0.0'}%</span
                  >
                </div>
                <Chart points={cpuPoints} yMax={100} height={70} />
              </div>
              <div>
                <div class="flex items-baseline justify-between mb-1.5">
                  <span class="text-xs font-medium text-muted-foreground uppercase tracking-wider"
                    >RAM</span
                  >
                  <span class="text-sm tnum"
                    >{ramPoints.length
                      ? ramPoints[ramPoints.length - 1].v.toFixed(1)
                      : '0.0'}%</span
                  >
                </div>
                <Chart points={ramPoints} yMax={100} height={70} />
              </div>
              <div>
                <div class="flex items-baseline justify-between mb-1.5">
                  <span class="text-xs font-medium text-muted-foreground uppercase tracking-wider"
                    >Disk read</span
                  >
                  <span class="text-sm tnum"
                    >{diskRPoints.length
                      ? formatRate(diskRPoints[diskRPoints.length - 1].v)
                      : '0 B/s'}</span
                  >
                </div>
                <Chart points={diskRPoints} height={70} color="var(--success)" />
              </div>
              <div>
                <div class="flex items-baseline justify-between mb-1.5">
                  <span class="text-xs font-medium text-muted-foreground uppercase tracking-wider"
                    >Disk write</span
                  >
                  <span class="text-sm tnum"
                    >{diskWPoints.length
                      ? formatRate(diskWPoints[diskWPoints.length - 1].v)
                      : '0 B/s'}</span
                  >
                </div>
                <Chart points={diskWPoints} height={70} color="var(--warning)" />
              </div>
              <div>
                <div class="flex items-baseline justify-between mb-1.5">
                  <span class="text-xs font-medium text-muted-foreground uppercase tracking-wider"
                    >Net RX</span
                  >
                  <span class="text-sm tnum"
                    >{netRxPoints.length
                      ? formatRate(netRxPoints[netRxPoints.length - 1].v)
                      : '0 B/s'}</span
                  >
                </div>
                <Chart points={netRxPoints} height={70} color="var(--info, var(--accent))" />
              </div>
              <div>
                <div class="flex items-baseline justify-between mb-1.5">
                  <span class="text-xs font-medium text-muted-foreground uppercase tracking-wider"
                    >Net TX</span
                  >
                  <span class="text-sm tnum"
                    >{netTxPoints.length
                      ? formatRate(netTxPoints[netTxPoints.length - 1].v)
                      : '0 B/s'}</span
                  >
                </div>
                <Chart points={netTxPoints} height={70} color="var(--info, var(--accent))" />
              </div>
            </div>
          {/if}
        </div>

        <!-- Disks -->
        <div class="border border-border rounded-lg bg-card p-5">
          <div class="flex items-center justify-between mb-3">
            <h2 class="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
              Disks
            </h2>
            <Button
              size="xs"
              variant="outline"
              onclick={() => {
                aDiskDevice = 'disk';
                aDiskBus = 'virtio';
                aDiskSize = 10;
                aDiskPool = pools.find((p) => p.purpose !== 'iso')?.name || 'vmmanager-disks';
                showAddDisk = true;
              }}>+ Add Disk</Button
            >
          </div>
          {#if !vm.disks || vm.disks.length === 0}
            <p class="text-sm text-muted-foreground">No disks</p>
          {:else}
            <div class="space-y-1.5">
              {#each vm.disks as disk}
                <div
                  class="flex items-center justify-between px-3 py-2 rounded-md border border-border bg-background"
                >
                  <div class="flex items-center gap-2 min-w-0">
                    <span class="text-xs font-mono text-muted-foreground w-8">{disk.target}</span>
                    <span
                      class="text-xs px-1.5 py-0.5 rounded border {disk.device === 'cdrom'
                        ? 'border-accent/30 bg-accent/10 text-accent'
                        : 'border-border bg-muted text-muted-foreground'}"
                      >{disk.device === 'cdrom' ? 'CDROM' : 'DISK'}</span
                    >
                    <span class="text-xs text-muted-foreground">{disk.bus}</span>
                    <span class="text-sm truncate">{diskLabel(disk)}</span>
                  </div>
                  <div class="flex items-center gap-1 shrink-0">
                    {#if disk.device === 'cdrom'}
                      <button
                        onclick={() => {
                          cISOTarget = disk.target;
                          cISOSource = disk.source || '';
                          showChangeISO = true;
                        }}
                        class="text-xs text-accent hover:text-accent-hover px-2 py-1 rounded hover:bg-muted"
                        >Change ISO</button
                      >
                    {:else if disk.pool}
                      <span class="text-xs text-muted-foreground tnum"
                        >{disk.size_gb ? disk.size_gb + ' GB' : ''}</span
                      >
                      <button
                        onclick={() => {
                          resizeDiskTarget = disk.target;
                          resizeDiskSize = disk.size_gb || 10;
                          resizeDiskCurrent = disk.size_gb || 0;
                          showResizeDisk = true;
                        }}
                        class="text-xs text-accent hover:text-accent-hover px-2 py-1 rounded hover:bg-muted"
                        >Resize</button
                      >
                    {/if}
                    <button
                      onclick={() => removeDisk(disk.target)}
                      disabled={vm.state === 'running'}
                      class="text-xs text-muted-foreground hover:text-destructive px-2 py-1 rounded hover:bg-destructive/10 disabled:opacity-50 disabled:cursor-not-allowed"
                      >Remove</button
                    >
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Network Interfaces -->
        <div class="border border-border rounded-lg bg-card p-5">
          <div class="flex items-center justify-between mb-3">
            <h2 class="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
              Network Interfaces
            </h2>
            <Button
              size="xs"
              variant="outline"
              onclick={() => {
                aNetNetwork = networks[0]?.name || 'default';
                aNetModel = 'virtio';
                showAddNet = true;
              }}>+ Add Interface</Button
            >
          </div>
          {#if !vm.networks || vm.networks.length === 0}
            <p class="text-sm text-muted-foreground">No network interfaces</p>
          {:else}
            <div class="space-y-1.5">
              {#each vm.networks as iface, idx}
                <div
                  class="flex items-center justify-between px-3 py-2 rounded-md border border-border bg-background"
                >
                  <div class="flex items-center gap-2 flex-wrap min-w-0">
                    <span class="text-xs font-mono text-muted-foreground">{iface.mac}</span>
                    <span
                      class="text-xs px-1.5 py-0.5 rounded border border-border bg-muted text-muted-foreground"
                      >{iface.model}</span
                    >
                    <span class="text-sm">{iface.network}</span>
                    {#if vm.state === 'running' && idx === 0 && vm.ip}
                      <span
                        class="text-xs px-1.5 py-0.5 rounded bg-accent/10 border border-accent/20 text-accent font-mono"
                        >IP: {vm.ip}</span
                      >
                    {/if}
                  </div>
                  <button
                    onclick={() => removeNet(iface.mac)}
                    class="text-xs text-muted-foreground hover:text-destructive px-2 py-1 rounded hover:bg-destructive/10 disabled:opacity-50 disabled:cursor-not-allowed"
                    >Remove</button
                  >
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Snapshots -->
        <div id="snapshots" class="border border-border rounded-lg bg-card p-5 scroll-mt-6">
          <h2 class="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">
            Snapshots
          </h2>
          <div class="flex gap-2 mb-3">
            <Input bind:value={snapName} placeholder="Snapshot name" class="flex-1" />
            <Button onclick={createSnapshot} disabled={!snapName || actionLoading === 'snapshot'}>
              {#if actionLoading === 'snapshot'}<Spinner
                  size="xs"
                  color="text-white"
                />{:else}Create{/if}
            </Button>
          </div>
          {#if snapshots.length === 0}
            <p class="text-sm text-muted-foreground">No snapshots yet</p>
          {:else}
            <div class="space-y-0.5">
              {#each snapshotTree.roots as root}
                {@render snapshotNode(root, 0)}
              {/each}
            </div>
          {/if}
        </div>
      </div>

      <!-- Sidebar -->
      <div class="space-y-4">
        <div class="border border-border rounded-lg bg-card p-4">
          <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
            Power
          </h2>
          <div class="space-y-2">
            {#if vm.state === 'shutoff'}
              <Button onclick={() => doAction('startVM')} disabled={busy} class="w-full">
                {#if actionLoading === 'startVM'}<Spinner
                    size="sm"
                    color="text-white"
                  />{:else}Start{/if}
              </Button>
            {:else if vm.state === 'running'}
              <Button
                variant="outline"
                onclick={() => doAction('shutdownVM')}
                disabled={busy}
                class="w-full"
              >
                Shutdown
              </Button>
              <Button
                variant="destructive"
                onclick={() => doAction('forceOffVM')}
                disabled={busy}
                class="w-full"
              >
                {#if actionLoading === 'forceOffVM'}<Spinner
                    size="sm"
                    color="text-white"
                  />{:else}Force Off{/if}
              </Button>
              <Button
                variant="destructive"
                onclick={() => doAction('forceRebootVM')}
                disabled={busy}
                class="w-full"
              >
                {#if actionLoading === 'forceRebootVM'}<Spinner
                    size="sm"
                    color="text-white"
                  />{:else}Force Reboot{/if}
              </Button>
              <Button
                variant="outline"
                onclick={() => doAction('suspendVM')}
                disabled={busy}
                class="w-full"
              >
                {#if actionLoading === 'suspendVM'}<Spinner
                    size="sm"
                    color="text-white"
                  />{:else}Suspend{/if}
              </Button>
            {:else if vm.state === 'paused'}
              <Button onclick={() => doAction('resumeVM')} disabled={busy} class="w-full">
                {#if actionLoading === 'resumeVM'}<Spinner
                    size="sm"
                    color="text-white"
                  />{:else}Resume{/if}
              </Button>
            {/if}
          </div>
        </div>

        <div class="border border-border rounded-lg bg-card p-4">
          <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
            Actions
          </h2>
          <div class="space-y-2">
            <Button
              onclick={() => window.open(`/console/${vm.id}`, '_blank', 'noopener,noreferrer')}
              class="w-full"
            >
              <svg
                class="w-4 h-4"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                viewBox="0 0 24 24"
                ><rect x="2" y="3" width="20" height="14" rx="2" /><line
                  x1="8"
                  y1="21"
                  x2="16"
                  y2="21"
                /><line x1="12" y1="17" x2="12" y2="21" /></svg
              >
              Open Console
            </Button>
            <Button
              variant="destructive"
              onclick={() => doAction('deleteVM')}
              disabled={busy}
              class="w-full"
            >
              Delete VM
            </Button>
            <Button
              variant="outline"
              onclick={() => {
                cName = vm.name + '-clone';
                cPool = pools.find((p) => p.purpose !== 'iso')?.name || 'vmmanager-disks';
                showClone = true;
              }}
              class="w-full"
            >
              Clone VM
            </Button>
            <Button variant="outline" onclick={openEdit} class="w-full">Edit Settings</Button>
            <Button variant="outline" onclick={openIdentity} class="w-full">
              Identity & Notes
            </Button>
            <Button
              variant="outline"
              onclick={exportVM}
              disabled={actionLoading === 'export' || vm.state === 'running'}
              class="w-full"
            >
              {#if actionLoading === 'export'}<Spinner size="sm" color="text-white" /> Exporting...{:else}Export
                Backup{/if}
            </Button>
            <div class="grid grid-cols-2 gap-2">
              <a
                href={api.getRDPUrl(vmId)}
                download
                class="btn btn-ghost flex-1 justify-center text-center">RDP</a
              >
              <a
                href={api.getSPICEUrl(vmId)}
                download
                class="btn btn-ghost flex-1 justify-center text-center">SPICE</a
              >
            </div>
            <!-- Autostart toggle: lives at the bottom of the
						     Actions card so it doesn't compete with the
						     primary action (Open Console). The Switch's
						     `checked` is bound to vm.autostart; onchange
						     fires toggleAutostart() which PATCHes the
						     server and rolls back on failure. -->
            <div class="pt-3 mt-2 border-t border-border">
              <Switch
                checked={!!vm.autostart}
                disabled={autostartSaving}
                onchange={toggleAutostart}
                label="Start on host boot"
                description={autostartSaving
                  ? 'Saving…'
                  : vm.autostart
                    ? 'libvirtd starts this VM automatically when the host boots'
                    : 'Off — the VM stays shut off when the host boots'}
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  {/if}
</div>

<!-- Shared confirm dialog (replaces all window.confirm) -->
<ConfirmDialog
  bind:open={confirmState.open}
  title={confirmState.title}
  description={confirmState.description}
  confirmLabel={confirmState.confirmLabel}
  variant={confirmState.variant}
  loading={confirmState.loading}
  onConfirm={confirmState.onConfirm}
/>

<!-- Edit Settings Dialog -->
<Dialog.Root bind:open={showEdit}>
  <Dialog.Content class="sm:max-w-lg max-h-[90vh] overflow-y-auto">
    <Dialog.Header>
      <Dialog.Title>Edit VM Settings</Dialog.Title>
      <Dialog.Description
        >Some changes require shutdown and restart to take effect.</Dialog.Description
      >
    </Dialog.Header>
    <div class="space-y-3">
      <div>
        <label for="edit-name" class="block text-sm font-medium mb-1.5">Name</label>
        <Input id="edit-name" bind:value={eName} type="text" />
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label for="edit-vcpus" class="block text-sm font-medium mb-1.5">vCPUs</label>
          <Input id="edit-vcpus" type="number" min="1" max="64" bind:value={eVcpus} class="tnum" />
        </div>
        <div>
          <label for="edit-ram" class="block text-sm font-medium mb-1.5">RAM (MB)</label>
          <Input
            id="edit-ram"
            type="number"
            min="512"
            step="512"
            bind:value={eRamMB}
            class="tnum"
          />
        </div>
      </div>
      <div>
        <label for="edit-cpu" class="block text-sm font-medium mb-1.5">CPU Mode</label>
        <select id="edit-cpu" bind:value={eCPUMode} class="input">
          <option value="host-passthrough">host-passthrough</option>
          <option value="host-model">host-model</option>
          <option value="max">max</option>
          <option value="custom">custom</option>
        </select>
      </div>
      <div>
        <label for="edit-video" class="block text-sm font-medium mb-1.5">Video Model</label>
        <select id="edit-video" bind:value={eVideoModel} class="input">
          <option value="virtio">virtio</option>
          <option value="qxl">qxl</option>
          <option value="vga">VGA</option>
          <option value="cirrus">cirrus</option>
          <option value="vmvga">vmvga</option>
          <option value="bochs">bochs</option>
          <option value="none">none</option>
        </select>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label for="edit-net" class="block text-sm font-medium mb-1.5">Network</label>
          <select id="edit-net" bind:value={eNetwork} class="input">
            {#each networks as net}<option value={net.name}>{net.name}</option>{/each}
          </select>
        </div>
        <div>
          <label for="edit-netmodel" class="block text-sm font-medium mb-1.5">Adapter</label>
          <select id="edit-netmodel" bind:value={eNetworkModel} class="input">
            {#each networkModels as m}<option value={m.value}>{m.label}</option>{/each}
          </select>
        </div>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label for="edit-chipset" class="block text-sm font-medium mb-1.5"
            >Chipset <span class="text-xs text-muted-foreground">(locked)</span></label
          >
          <select id="edit-chipset" bind:value={eChipset} disabled class="input opacity-50">
            <option value="q35">Q35</option>
            <option value="i440fx">i440fx</option>
          </select>
        </div>
        <div>
          <label for="edit-firmware" class="block text-sm font-medium mb-1.5">BIOS</label>
          <select
            id="edit-firmware"
            bind:value={eFirmware}
            disabled={eChipset === 'i440fx'}
            class="input {eChipset === 'i440fx' ? 'opacity-50' : ''}"
          >
            <option value="uefi">UEFI</option>
            <option value="seabios">SeaBIOS</option>
          </select>
          {#if eChipset === 'i440fx'}<p class="text-xs text-muted-foreground mt-1">
              i440fx requires SeaBIOS
            </p>{/if}
        </div>
      </div>
      {#if eFirmware === 'uefi'}
        <div class="flex gap-4">
          <label class="flex items-center gap-2 text-sm cursor-pointer">
            <input
              type="checkbox"
              bind:checked={eSecureBoot}
              class="rounded border-border bg-background text-accent focus:ring-accent"
            />
            Secure Boot
          </label>
          <label class="flex items-center gap-2 text-sm cursor-pointer">
            <input
              type="checkbox"
              bind:checked={eTPM}
              class="rounded border-border bg-background text-accent focus:ring-accent"
            />
            TPM 2.0
          </label>
        </div>
      {/if}
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label for="edit-ostype" class="block text-sm font-medium mb-1.5">OS Type</label>
          <select id="edit-ostype" bind:value={eOSType} class="input">
            <option value="">Auto</option>
            <option value="linux">Linux</option>
            <option value="windows">Windows</option>
            <option value="freebsd">FreeBSD</option>
            <option value="other">Other</option>
          </select>
        </div>
        <div>
          <label for="edit-osver" class="block text-sm font-medium mb-1.5">OS Version</label>
          <Input id="edit-osver" bind:value={eOSVersion} placeholder="e.g. ubuntu24.04" />
        </div>
      </div>
    </div>
    <Dialog.Footer class="gap-2">
      <Button variant="outline" onclick={() => (showEdit = false)} disabled={editSaving}
        >Cancel</Button
      >
      <Button onclick={saveEdit} disabled={editSaving}>
        {#if editSaving}<Spinner size="sm" color="text-white" /> Saving...{:else}Save{/if}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Add Disk Dialog -->
<Dialog.Root bind:open={showAddDisk}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>{aDiskDevice === 'cdrom' ? 'Attach ISO' : 'Add Disk'}</Dialog.Title>
    </Dialog.Header>
    <div class="space-y-3">
      <div>
        <label for="adisk-type" class="block text-sm font-medium mb-1.5">Type</label>
        <select
          id="adisk-type"
          bind:value={aDiskDevice}
          onchange={() => {
            aDiskBus = aDiskDevice === 'cdrom' ? 'scsi' : 'virtio';
          }}
          class="input"
        >
          <option value="disk">Disk</option>
          <option value="cdrom">CDROM (ISO)</option>
        </select>
      </div>
      <div>
        <label for="adisk-bus" class="block text-sm font-medium mb-1.5">Bus</label>
        <select id="adisk-bus" bind:value={aDiskBus} class="input">
          {#if aDiskDevice === 'cdrom'}
            <option value="scsi">SCSI (recommended)</option>
            <option value="sata">SATA</option>
          {:else}
            <option value="virtio">VirtIO (recommended)</option>
            <option value="sata">SATA</option>
            <option value="scsi">SCSI</option>
            <option value="ide">IDE</option>
          {/if}
        </select>
      </div>
      {#if aDiskDevice === 'disk'}
        <div>
          <label for="adisk-size" class="block text-sm font-medium mb-1.5">Size (GB)</label>
          <Input id="adisk-size" type="number" min="1" bind:value={aDiskSize} class="tnum" />
        </div>
        <div>
          <label for="adisk-pool" class="block text-sm font-medium mb-1.5">Storage Pool</label>
          <select id="adisk-pool" bind:value={aDiskPool} class="input">
            {#each pools.filter((p) => p.purpose !== 'iso') as p}<option value={p.name}
                >{p.name}</option
              >{/each}
          </select>
        </div>
        <div>
          <label for="adisk-fmt" class="block text-sm font-medium mb-1.5">Format</label>
          <select id="adisk-fmt" bind:value={aDiskFormat} class="input">
            <option value="qcow2">qcow2</option>
            <option value="raw">raw</option>
          </select>
        </div>
      {:else}
        <div>
          <label for="adisk-iso" class="block text-sm font-medium mb-1.5">ISO</label>
          <select id="adisk-iso" bind:value={aDiskISO} class="input">
            <option value="">(empty)</option>
            {#each isos as iso}<option value={iso.path}>{iso.name}</option>{/each}
          </select>
        </div>
      {/if}
    </div>
    <Dialog.Footer class="gap-2">
      <Button
        variant="outline"
        onclick={() => (showAddDisk = false)}
        disabled={actionLoading === 'adddisk'}>Cancel</Button
      >
      <Button onclick={addDisk} disabled={actionLoading === 'adddisk'}>
        {#if actionLoading === 'adddisk'}<Spinner size="sm" color="text-white" />{:else}Add{/if}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Change ISO Dialog -->
<Dialog.Root bind:open={showChangeISO}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Change ISO — {cISOTarget}</Dialog.Title>
    </Dialog.Header>
    <div>
      <label for="ciso-src" class="block text-sm font-medium mb-1.5">ISO</label>
      <select id="ciso-src" bind:value={cISOSource} class="input">
        <option value="">(eject — no ISO)</option>
        {#each isos as iso}<option value={iso.path}>{iso.name}</option>{/each}
      </select>
    </div>
    <Dialog.Footer class="gap-2">
      <Button
        variant="outline"
        onclick={() => (showChangeISO = false)}
        disabled={actionLoading === 'changeiso'}>Cancel</Button
      >
      <Button onclick={changeISO} disabled={actionLoading === 'changeiso'}>
        {#if actionLoading === 'changeiso'}<Spinner
            size="sm"
            color="text-white"
          />{:else}Change{/if}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Resize Disk Dialog -->
<Dialog.Root bind:open={showResizeDisk}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Resize Disk — {resizeDiskTarget}</Dialog.Title>
      <Dialog.Description
        >Current: {resizeDiskCurrent} GB. Growing is safe; shrinking below the current size is not allowed.</Dialog.Description
      >
    </Dialog.Header>
    <div>
      <label for="rdisk-size" class="block text-sm font-medium mb-1.5">New size (GB)</label>
      <Input
        id="rdisk-size"
        type="number"
        min={resizeDiskCurrent}
        bind:value={resizeDiskSize}
        class="tnum"
      />
    </div>
    <Dialog.Footer class="gap-2">
      <Button
        variant="outline"
        onclick={() => (showResizeDisk = false)}
        disabled={actionLoading === 'resizedisk'}>Cancel</Button
      >
      <Button onclick={resizeDisk} disabled={actionLoading === 'resizedisk' || !resizeDiskSize}>
        {#if actionLoading === 'resizedisk'}<Spinner
            size="sm"
            color="text-white"
          />{:else}Resize{/if}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Add Net Dialog -->
<Dialog.Root bind:open={showAddNet}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Add Network Interface</Dialog.Title>
    </Dialog.Header>
    <div class="space-y-3">
      <div>
        <label for="anet-net" class="block text-sm font-medium mb-1.5">Network</label>
        <select id="anet-net" bind:value={aNetNetwork} class="input">
          {#each networks as net}<option value={net.name}>{net.name}</option>{/each}
        </select>
      </div>
      <div>
        <label for="anet-model" class="block text-sm font-medium mb-1.5">Model</label>
        <select id="anet-model" bind:value={aNetModel} class="input">
          {#each networkModels as m}<option value={m.value}>{m.label}</option>{/each}
        </select>
      </div>
    </div>
    <Dialog.Footer class="gap-2">
      <Button
        variant="outline"
        onclick={() => (showAddNet = false)}
        disabled={actionLoading === 'addnet'}>Cancel</Button
      >
      <Button onclick={addNet} disabled={actionLoading === 'addnet'}>
        {#if actionLoading === 'addnet'}<Spinner size="sm" color="text-white" />{:else}Add{/if}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Clone Dialog -->
<Dialog.Root bind:open={showClone}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Clone VM</Dialog.Title>
      <Dialog.Description
        >Creates a new VM with the same configuration and a new empty disk.</Dialog.Description
      >
    </Dialog.Header>
    <div class="space-y-3">
      <div>
        <label for="clone-name" class="block text-sm font-medium mb-1.5">New Name</label>
        <Input id="clone-name" bind:value={cName} type="text" />
      </div>
      <div>
        <label for="clone-pool" class="block text-sm font-medium mb-1.5">Storage Pool</label>
        <select id="clone-pool" bind:value={cPool} class="input">
          {#each pools.filter((p) => p.purpose !== 'iso') as p}<option value={p.name}
              >{p.name}</option
            >{/each}
        </select>
      </div>
    </div>
    <Dialog.Footer class="gap-2">
      <Button
        variant="outline"
        onclick={() => (showClone = false)}
        disabled={actionLoading === 'clone'}>Cancel</Button
      >
      <Button onclick={cloneVM} disabled={actionLoading === 'clone' || !cName}>
        {#if actionLoading === 'clone'}<Spinner size="sm" color="text-white" />{:else}Clone{/if}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Export Dialog -->
<Dialog.Root bind:open={showExport}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>Export VM</Dialog.Title>
    </Dialog.Header>
    {#if !exportProgress}
      <p class="text-sm text-muted-foreground">
        Pick a destination. The OVA is portable; the WebVM backup round-trips with full fidelity
        (UEFI, TPM, custom networks, etc.).
      </p>
      <div class="space-y-2 mt-2">
        <label
          class="flex items-start gap-3 p-3 rounded border border-border cursor-pointer hover:border-border-hover"
        >
          <input type="radio" bind:group={exportTarget} value="vmware" class="mt-1" />
          <div>
            <div class="text-sm font-medium">VirtualBox / VMware</div>
            <div class="text-xs text-muted-foreground">
              OVA with VMDK (streamOptimized). Maximum portability.
            </div>
          </div>
        </label>
        <label
          class="flex items-start gap-3 p-3 rounded border border-border cursor-pointer hover:border-border-hover"
        >
          <input type="radio" bind:group={exportTarget} value="libvirt" class="mt-1" />
          <div>
            <div class="text-sm font-medium">Proxmox / libvirt / GNOME Boxes / WebVM</div>
            <div class="text-xs text-muted-foreground">
              OVA with qcow2. Preserves most features.
            </div>
          </div>
        </label>
        <label
          class="flex items-start gap-3 p-3 rounded border border-border cursor-pointer hover:border-border-hover"
        >
          <input type="radio" bind:group={exportTarget} value="backup" class="mt-1" />
          <div>
            <div class="text-sm font-medium">WebVM backup (zstd, compressed)</div>
            <div class="text-xs text-muted-foreground">
              Full fidelity (UEFI, TPM, all features). Re-import via /import.
            </div>
          </div>
        </label>
      </div>
      <Dialog.Footer class="gap-2">
        <Button variant="outline" onclick={() => (showExport = false)}>Cancel</Button>
        <Button onclick={startExport}>Export</Button>
      </Dialog.Footer>
    {:else}
      <div class="space-y-2">
        <div class="text-sm">{exportProgress.label}</div>
        {#if exportProgress.total > 0}
          <div class="w-full h-2 bg-muted rounded overflow-hidden">
            <div
              class="h-full bg-accent transition-all"
              style="width: {exportProgress.percent}%"
            ></div>
          </div>
          <div class="flex justify-between text-xs text-muted-foreground tnum">
            <span
              >{(exportProgress.received / 1e9).toFixed(2)} GB / {(
                exportProgress.total / 1e9
              ).toFixed(2)} GB</span
            >
            <span>{exportProgress.percent.toFixed(1)}%</span>
          </div>
        {:else}
          <div class="w-full h-2 bg-muted rounded overflow-hidden">
            <div class="h-full bg-accent w-1/3 animate-progress-indeterminate"></div>
          </div>
          <div class="flex justify-between text-xs text-muted-foreground tnum">
            <span>{(exportProgress.received / 1e9).toFixed(2)} GB downloaded</span>
            <span>—</span>
          </div>
        {/if}
      </div>
      <Dialog.Footer>
        <Button variant="outline" onclick={cancelExport}>Cancel</Button>
      </Dialog.Footer>
    {/if}
  </Dialog.Content>
</Dialog.Root>

<!-- Identity & Notes Dialog -->
<Dialog.Root bind:open={showIdentity}>
  <Dialog.Content class="sm:max-w-2xl max-h-[90vh] overflow-y-auto">
    <Dialog.Header>
      <Dialog.Title>Identity & Notes</Dialog.Title>
      <Dialog.Description
        >Alias, cover image, network interfaces, groups and notes for this VM.</Dialog.Description
      >
    </Dialog.Header>

    <div class="flex gap-1 border-b border-border mb-4">
      {#each [['alias', 'Alias'], ['cover', 'Cover'], ['network', 'Network'], ['notes', 'Notes'], ['groups', 'Groups']] as [k, label]}
        <button
          onclick={() => (identityTab = k)}
          class="px-3 py-2 text-sm border-b-2 -mb-px transition-colors {identityTab === k
            ? 'border-accent text-foreground'
            : 'border-transparent text-muted-foreground hover:text-foreground'}">{label}</button
        >
      {/each}
    </div>

    {#if identityTab === 'alias'}
      <div class="space-y-3">
        <div>
          <label for="ident-alias" class="block text-sm font-medium mb-1.5">Alias</label>
          <Input id="ident-alias" bind:value={eAlias} placeholder={vm?.name} />
          <p class="text-xs text-muted-foreground mt-1">
            Friendly display name. Leave blank to use the domain name.
          </p>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" onclick={() => (showIdentity = false)}>Close</Button>
          <Button onclick={saveIdentityBasics} disabled={savingIdentity}
            >{savingIdentity ? 'Saving…' : 'Save'}</Button
          >
        </div>
      </div>
    {:else if identityTab === 'cover'}
      <div class="space-y-3">
        <div
          class="aspect-video w-full border border-border rounded-md bg-muted overflow-hidden flex items-center justify-center"
        >
          {#if coverPreview}
            <img src={coverPreview} alt="cover preview" class="w-full h-full object-cover" />
          {:else if vm?.cover}
            <img src={vm.cover} alt="cover" class="w-full h-full object-cover" />
          {:else}
            <div class="text-center text-muted-foreground p-6">
              <svg
                class="w-10 h-10 mx-auto mb-2 opacity-40"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                viewBox="0 0 24 24"
                ><rect x="3" y="3" width="18" height="18" rx="2" /><circle
                  cx="8.5"
                  cy="8.5"
                  r="1.5"
                /><path d="m21 15-5-5L5 21" /></svg
              >
              <p class="text-xs">No cover image</p>
            </div>
          {/if}
        </div>
        <div class="flex items-center gap-2">
          <label class="btn btn-outline cursor-pointer text-sm">
            {coverFile ? 'Change' : 'Choose image…'}
            <input
              type="file"
              accept="image/png,image/jpeg,image/webp"
              class="hidden"
              onchange={onCoverPicked}
            />
          </label>
          {#if coverFile}
            <span class="text-xs text-muted-foreground truncate"
              >{coverFile.name} ({(coverFile.size / 1024).toFixed(0)} KB)</span
            >
            <Button size="sm" onclick={uploadCover} disabled={uploadingCover}
              >{uploadingCover ? 'Uploading…' : 'Upload'}</Button
            >
          {/if}
          {#if vm?.cover}
            <Button size="sm" variant="outline" onclick={removeCover}>Remove current</Button>
          {/if}
        </div>
        <p class="text-xs text-muted-foreground">PNG, JPEG or WebP. 8 MB max.</p>
      </div>
    {:else if identityTab === 'network'}
      <div class="space-y-3">
        {#if vm?.state !== 'shutoff'}
          <div
            class="p-3 border border-warning/30 bg-warning/10 rounded-md text-warning text-xs flex items-start gap-2"
          >
            <svg
              class="w-4 h-4 shrink-0 mt-0.5"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              viewBox="0 0 24 24"
              ><path
                d="M12 9v2m0 4h.01M5 19h14a2 2 0 0 0 1.84-2.75L13.74 4a2 2 0 0 0-3.48 0L3.16 16.25A2 2 0 0 0 5 19z"
              /></svg
            >
            <span
              >VM must be shut off to edit network interfaces. The MAC and network columns will be
              locked.</span
            >
          </div>
        {/if}
        {#if !vm?.networks || vm.networks.length === 0}
          <p class="text-sm text-muted-foreground">No network interfaces to edit.</p>
        {:else}
          <div class="space-y-2">
            {#each vm.networks as iface}
              {@const edit = ifaceEdits[iface.mac] || {
                mac: iface.mac,
                network: iface.network,
                vlan: '',
                busy: false,
                error: '',
              }}
              {@const support = vlanSupportByNetwork[iface.network]}
              <div class="border border-border rounded-md bg-background p-3 space-y-2">
                <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
                  <div>
                    <label class="block text-xs font-medium text-muted-foreground mb-1">MAC</label>
                    <Input
                      bind:value={edit.mac}
                      disabled={vm.state !== 'shutoff'}
                      class="font-mono text-xs"
                    />
                  </div>
                  <div>
                    <label class="block text-xs font-medium text-muted-foreground mb-1"
                      >Network</label
                    >
                    <select
                      bind:value={edit.network}
                      disabled={vm.state !== 'shutoff'}
                      class="input !text-xs"
                    >
                      {#each networks as n}
                        <option value={n.name}>{n.name}</option>
                      {/each}
                    </select>
                  </div>
                  <div>
                    <label class="block text-xs font-medium text-muted-foreground mb-1"
                      >VLAN tag <span class="text-muted-foreground font-normal"
                        >(0–4094, blank = leave)</span
                      ></label
                    >
                    <Input
                      bind:value={edit.vlan}
                      disabled={vm.state !== 'shutoff' || (support && !support.supported)}
                      placeholder="—"
                      class="tnum text-xs"
                    />
                  </div>
                </div>
                {#if support && !support.supported}
                  <p class="text-xs text-warning">VLAN unavailable: {support.reason}</p>
                {/if}
                {#if edit.error}
                  <p class="text-xs text-destructive">{edit.error}</p>
                {/if}
                <div class="flex justify-end">
                  <Button
                    size="xs"
                    onclick={() => saveIface(iface.mac)}
                    disabled={edit.busy || vm.state !== 'shutoff'}
                  >
                    {#if edit.busy}<Spinner size="xs" color="text-white" />{:else}Save interface{/if}
                  </Button>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {:else if identityTab === 'notes'}
      <div class="space-y-3">
        <div>
          <label for="ident-notes" class="block text-sm font-medium mb-1.5">Notes</label>
          <textarea
            id="ident-notes"
            bind:value={eNotes}
            onblur={() => saveNotesIfChanged()}
            rows="8"
            class="input !text-sm font-mono"
            placeholder="Free-form notes. Auto-saved when you click away from this field."
          ></textarea>
          <p class="text-xs text-muted-foreground mt-1 flex items-center gap-1.5">
            {#if notesStatus === 'saving'}
              <Spinner size="xs" />
              <span>Saving…</span>
            {:else if notesStatus === 'saved'}
              <svg
                class="w-3 h-3 text-success"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"
                viewBox="0 0 24 24"><polyline points="20 6 9 17 4 12" /></svg
              >
              <span class="text-success">Saved</span>
            {:else if notesStatus === 'error'}
              <svg
                class="w-3 h-3 text-destructive"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                viewBox="0 0 24 24"
                ><circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" /><line
                  x1="12"
                  y1="16"
                  x2="12.01"
                  y2="16"
                /></svg
              >
              <span class="text-destructive">{notesError}</span>
            {:else}
              <span>Auto-saves on blur</span>
            {/if}
          </p>
        </div>
      </div>
    {:else if identityTab === 'groups'}
      <div class="space-y-3">
        <div>
          <label for="ident-groups" class="block text-sm font-medium mb-1.5">Groups</label>
          <Input
            id="ident-groups"
            bind:value={eGroupsText}
            placeholder="production, staging, team-a"
          />
          <p class="text-xs text-muted-foreground mt-1">
            Comma- or space-separated. Use the <em>Manage Groups</em> dialog from the VM list to rename
            or recolor.
          </p>
        </div>
        {#if eGroupsList.length > 0}
          <div>
            <p class="text-xs font-medium text-muted-foreground mb-1.5">Available groups</p>
            <div class="flex flex-wrap gap-1.5">
              {#each eGroupsList as g}
                <button
                  type="button"
                  onclick={() => {
                    const current = eGroupsText
                      .split(/[\s,;]+/)
                      .map((s) => s.trim())
                      .filter(Boolean);
                    if (!current.includes(g.name)) {
                      eGroupsText = [...current, g.name].join(', ');
                    } else {
                      eGroupsText = current.filter((n) => n !== g.name).join(', ');
                    }
                  }}
                  class="text-xs px-2 py-0.5 rounded border"
                  style="border-color: {g.color}40; background-color: {g.color}15; color: {g.color}"
                  >{g.name}</button
                >
              {/each}
            </div>
          </div>
        {/if}
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" onclick={() => (showIdentity = false)}>Close</Button>
          <Button onclick={saveIdentityBasics} disabled={savingIdentity}
            >{savingIdentity ? 'Saving…' : 'Save'}</Button
          >
        </div>
      </div>
    {/if}
  </Dialog.Content>
</Dialog.Root>

{#snippet snapshotNode(node, depth)}
  <div
    class="flex items-center justify-between py-1.5 border-b border-border last:border-0"
    style="padding-left: {depth * 20}px"
  >
    <div class="flex items-center gap-2 min-w-0">
      {#if depth > 0}
        <svg
          class="w-3 h-3 text-muted-foreground/40 shrink-0"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          viewBox="0 0 24 24"><polyline points="9 6 15 12 9 18" /></svg
        >
      {/if}
      <div class="min-w-0">
        <p class="text-sm font-medium truncate">
          {node.name}
          {#if node.current}
            <span
              class="text-[10px] text-success ml-1.5 px-1.5 py-0.5 rounded border border-success/30 bg-success/10 uppercase tracking-wider"
              >current</span
            >
          {/if}
        </p>
        <p class="text-xs text-muted-foreground tnum">
          {formatSnapshotDate(node.creation_time)}
          {#if node.size_at_snap_bytes}
            <span class="mx-1.5 text-border">·</span>
            <span class="text-accent">{bytesToStr(node.size_at_snap_bytes)}</span>
            <span class="text-muted-foreground/70"> at creation</span>
          {/if}
        </p>
      </div>
    </div>
    <div class="flex gap-1 shrink-0">
      {#if !node.current}
        <button
          onclick={() => revertSnapshot(node.id)}
          class="text-xs text-warning hover:text-warning px-2 py-1 rounded hover:bg-warning/10"
          >Revert</button
        >
      {/if}
      <button
        onclick={() => deleteSnapshot(node.id)}
        class="text-xs text-muted-foreground hover:text-destructive px-2 py-1 rounded hover:bg-destructive/10"
        >Delete</button
      >
    </div>
  </div>
  {#each node.children as child}
    {@render snapshotNode(child, depth + 1)}
  {/each}
{/snippet}
