<script>
  import { onMount } from 'svelte';
  import { api, auth, passwordStrength } from '$lib/stores/auth.svelte.js';
  import { toast } from '$lib/components/ui/toast';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import DataTable from '$lib/components/DataTable.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Spinner from '$lib/components/Spinner.svelte';
  import Alert from '$lib/components/Alert.svelte';
  import SearchInput from '$lib/components/SearchInput.svelte';

  let users = $state([]);
  let loading = $state(true);
  let error = $state('');
  let search = $state('');
  let page = $state(0);
  const PAGE_SIZE = 10;

  let showAdd = $state(false);
  let newUsername = $state('');
  let newPassword = $state('');
  let newRole = $state('operator');
  let newEmail = $state('');

  let editing = $state(null);
  let editPassword = $state('');
  let editRole = $state('');
  let editEmail = $state('');
  let editActive = $state(true);

  let confirmState = $state({
    open: false,
    title: '',
    description: '',
    confirmLabel: 'Delete',
    variant: 'destructive',
    onConfirm: () => {},
    loading: false,
  });

  const newStrength = $derived(passwordStrength(newPassword));

  const filtered = $derived.by(() => {
    const q = search.toLowerCase().trim();
    if (!q) return users;
    return users.filter((u) => u.username.toLowerCase().includes(q));
  });

  const paginated = $derived(filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE));
  const totalPages = $derived(Math.max(1, Math.ceil(filtered.length / PAGE_SIZE)));

  $effect(() => {
    void search; // track search to re-run on change
    page = 0;
  });

  onMount(() => {
    if (auth.isAdmin()) load();
  });

  async function load() {
    loading = true;
    error = '';
    try {
      users = await api.listUsers();
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function askConfirm(opts) {
    confirmState = { ...opts, open: true, loading: false };
  }

  async function addUser() {
    if (!newUsername || !newPassword || newPassword.length < 8) return;
    // Capture names BEFORE resetting (was a pre-existing toast bug —
    // the name was used after being cleared).
    const createdName = newUsername;
    const createdRole = newRole;
    try {
      await api.createUser({
        username: createdName,
        password: newPassword,
        role: createdRole,
        email: newEmail,
      });
      newUsername = '';
      newPassword = '';
      newRole = 'operator';
      newEmail = '';
      showAdd = false;
      toast.success(`User "${createdName}" created`);
      await load();
    } catch (e) {
      toast.error(e.message);
    }
  }

  function startEdit(u) {
    editing = u.username;
    editPassword = '';
    editRole = u.role;
    editEmail = u.email || '';
    editActive = u.active !== false;
  }

  async function saveEdit() {
    const data = {};
    if (editPassword) data.password = editPassword;
    if (editRole) data.role = editRole;
    if (editEmail) data.email = editEmail;
    else data.email = '';
    if (typeof editActive === 'boolean') data.active = editActive;
    try {
      await api.updateUser(editing, data);
      editing = null;
      toast.success('User updated');
      await load();
    } catch (e) {
      toast.error(e.message);
    }
  }

  function deleteUser(username) {
    if (username === auth.user) {
      toast.error('You cannot delete your own account');
      return;
    }
    askConfirm({
      title: `Delete user "${username}"?`,
      description: 'This user will lose access to WebVM immediately.',
      confirmLabel: 'Delete',
      onConfirm: async () => {
        confirmState.loading = true;
        try {
          await api.deleteUser(username);
          confirmState.open = false;
          toast.success(`User "${username}" deleted`);
          await load();
        } catch (e) {
          toast.error(e.message);
          confirmState.loading = false;
        }
      },
    });
  }
</script>

<div class="p-6 max-w-5xl">
  <PageHeader title="Users" subtitle="Manage user accounts and roles">
    {#snippet actions()}
      <SearchInput bind:value={search} placeholder="Search users..." class="w-48" />
      <Button onclick={() => (showAdd = !showAdd)}>{showAdd ? 'Cancel' : 'Add User'}</Button>
    {/snippet}
  </PageHeader>

  {#if error}
    <div class="mb-4"><Alert variant="error">{error}</Alert></div>
  {/if}

  {#if showAdd}
    <div class="border border-border rounded-lg bg-card p-4 mb-4 space-y-3">
      <div class="text-sm font-medium">New User</div>
      <div class="grid grid-cols-4 gap-2">
        <Input bind:value={newUsername} placeholder="Username" autocomplete="off" />
        <Input
          bind:value={newPassword}
          type="password"
          placeholder="Password (min 8)"
          autocomplete="new-password"
        />
        <Input
          bind:value={newEmail}
          type="email"
          placeholder="Email (optional)"
          autocomplete="off"
        />
        <select bind:value={newRole} class="input">
          <option value="viewer">Viewer</option>
          <option value="operator">Operator</option>
          <option value="admin">Admin</option>
        </select>
      </div>
      {#if newPassword}
        <div class="flex items-center gap-2 text-xs">
          <div class="flex-1 h-1.5 rounded bg-muted overflow-hidden">
            <div
              class="h-full transition-all {newStrength.color}"
              style="width: {(newStrength.score + 1) * 20}%"
            ></div>
          </div>
          <span class="text-muted-foreground w-16 text-right">{newStrength.label}</span>
        </div>
      {/if}
      <div>
        <Button onclick={addUser} disabled={!newUsername || newPassword.length < 8}>Create</Button>
      </div>
    </div>
  {/if}

  {#if loading}
    <div class="flex items-center justify-center py-24"><Spinner size="lg" /></div>
  {:else}
    <DataTable
      columns={[
        { key: 'username', label: 'Username', render: userCell },
        { key: 'role', label: 'Role', width: '110px', render: roleCell },
        { key: 'active', label: 'Active', width: '70px', render: activeCell },
        {
          key: 'created_at',
          label: 'Created',
          width: '120px',
          class: 'tnum text-muted-foreground',
          render: createdCell,
        },
        {
          key: 'last_login_at',
          label: 'Last login',
          width: '140px',
          class: 'tnum text-muted-foreground',
          render: lastLoginCell,
        },
        { key: 'actions', label: '', align: 'right', width: 'auto', render: actionsCell },
      ]}
      rows={paginated}
      rowKey="username"
      emptyMessage={search ? 'No users match your search' : 'No users yet'}
    />

    {#if totalPages > 1}
      <div class="flex items-center justify-between mt-3 text-sm">
        <span class="text-muted-foreground tnum">
          Showing {page * PAGE_SIZE + 1}–{Math.min((page + 1) * PAGE_SIZE, filtered.length)} of {filtered.length}
        </span>
        <div class="flex items-center gap-1">
          <Button variant="outline" size="sm" disabled={page === 0} onclick={() => page--}
            >Previous</Button
          >
          <span class="px-3 text-muted-foreground tnum">{page + 1} / {totalPages}</span>
          <Button
            variant="outline"
            size="sm"
            disabled={page >= totalPages - 1}
            onclick={() => page++}>Next</Button
          >
        </div>
      </div>
    {/if}
  {/if}
</div>

{#snippet createdCell(row)}
  {row.created_at?.slice(0, 10) || '—'}
{/snippet}

{#snippet lastLoginCell(row)}
  {row.last_login_at ? row.last_login_at.replace('T', ' ').slice(0, 16) : '—'}
{/snippet}

{#snippet activeCell(row)}
  {#if row.active === false}
    <span
      class="text-xs px-2 py-0.5 rounded border border-destructive/30 bg-destructive/10 text-destructive"
      >disabled</span
    >
  {:else}
    <span class="text-xs px-2 py-0.5 rounded border border-success/30 bg-success/10 text-success"
      >active</span
    >
  {/if}
{/snippet}

{#snippet userCell(row)}
  {#if editing === row.username}
    <div class="space-y-1">
      <Input
        bind:value={editEmail}
        type="email"
        placeholder="Email"
        class="!py-1 !text-xs"
        autocomplete="off"
      />
      <Input
        bind:value={editPassword}
        type="password"
        placeholder="New password (leave empty to keep)"
        class="!py-1 !text-xs"
        autocomplete="new-password"
      />
    </div>
  {:else}
    <div class="flex items-center gap-2.5 min-w-0">
      <div
        class="w-7 h-7 rounded-full bg-muted border border-border flex items-center justify-center text-xs font-semibold text-muted-foreground shrink-0"
      >
        {row.username[0].toUpperCase()}
      </div>
      <div class="min-w-0">
        <div class="font-medium truncate">{row.username}</div>
        {#if row.email}
          <div class="text-xs text-muted-foreground truncate">{row.email}</div>
        {/if}
      </div>
    </div>
  {/if}
{/snippet}

{#snippet roleCell(row)}
  {#if editing === row.username}
    <div class="space-y-1">
      <select bind:value={editRole} class="input !py-1 !text-xs w-28">
        <option value="viewer">Viewer</option>
        <option value="operator">Operator</option>
        <option value="admin">Admin</option>
      </select>
      <label class="flex items-center gap-1 text-xs text-muted-foreground">
        <input type="checkbox" bind:checked={editActive} class="rounded" />
        active
      </label>
    </div>
  {:else}
    <span
      class="text-xs px-2 py-0.5 rounded border
			{row.role === 'admin'
        ? 'border-accent/30 bg-accent/10 text-accent'
        : row.role === 'operator'
          ? 'border-info/30 bg-info/10 text-info'
          : 'border-border bg-muted text-muted-foreground'}">{row.role}</span
    >
  {/if}
{/snippet}

{#snippet actionsCell(row)}
  {#if editing === row.username}
    <div class="flex items-center gap-1 justify-end">
      <button onclick={saveEdit} class="text-xs text-success hover:bg-success/10 px-2 py-1 rounded"
        >Save</button
      >
      <button
        onclick={() => (editing = null)}
        class="text-xs text-muted-foreground hover:bg-muted px-2 py-1 rounded">Cancel</button
      >
    </div>
  {:else}
    <div class="flex items-center gap-1 justify-end">
      <button
        onclick={() => startEdit(row)}
        class="text-xs text-accent hover:bg-muted px-2 py-1 rounded"
        aria-label="Edit {row.username}">Edit</button
      >
      {#if row.username !== auth.user}
        <button
          onclick={() => deleteUser(row.username)}
          class="text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10 px-2 py-1 rounded"
          aria-label="Delete {row.username}">Delete</button
        >
      {/if}
    </div>
  {/if}
{/snippet}

<ConfirmDialog
  bind:open={confirmState.open}
  title={confirmState.title}
  description={confirmState.description}
  confirmLabel={confirmState.confirmLabel}
  variant={confirmState.variant}
  loading={confirmState.loading}
  onConfirm={confirmState.onConfirm}
/>
