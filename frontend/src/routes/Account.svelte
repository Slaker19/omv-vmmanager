<script>
  import Spinner from '$lib/components/Spinner.svelte';
  import { auth, api, passwordStrength } from '../lib/stores/auth.svelte.js';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { navigate } from '../lib/router.svelte.js';
  import { toast } from '$lib/components/ui/toast';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';

  let me = $state(null);
  let loading = $state(true);
  let saving = $state(false);

  let oldPassword = $state('');
  let newPassword = $state('');
  let confirmPassword = $state('');
  let showOld = $state(false);
  let showNew = $state(false);
  let showConfirm = $state(false);

  // --- API tokens ---
  let tokens = $state([]);
  let showTokenDialog = $state(false);
  let newTokenName = $state('');
  let newTokenTtl = $state(720); // 30 days in hours
  let newTokenPlain = $state(''); // shown ONCE after creation
  let newTokenSaving = $state(false);
  let confirmRevoke = $state(null);
  let confirmDelete = $state(null);

  const strength = $derived(passwordStrength(newPassword));
  const passwordsMatch = $derived(
    newPassword === '' || confirmPassword === '' || newPassword === confirmPassword
  );
  const canSubmit = $derived(
    oldPassword.length > 0 && newPassword.length >= 8 && newPassword === confirmPassword
  );

  async function load() {
    loading = true;
    try {
      me = await api.me();
      if (!me.must_change_password) {
        auth.setMustChange(false);
      }
      await loadTokens();
    } catch (e) {
      toast.error(e.message || 'Failed to load account');
    } finally {
      loading = false;
    }
  }

  async function loadTokens() {
    try {
      const r = await api.listTokens(false);
      tokens = r.tokens || [];
    } catch {
      // tokens may be disabled — silently ignore
      tokens = [];
    }
  }

  async function createToken() {
    if (!newTokenName.trim()) {
      toast.error('Name is required');
      return;
    }
    newTokenSaving = true;
    try {
      const r = await api.createToken(newTokenName.trim(), newTokenTtl);
      newTokenPlain = r.plain;
      newTokenName = '';
      await loadTokens();
    } catch (e) {
      toast.error(e.message || 'Failed to create token');
    } finally {
      newTokenSaving = false;
    }
  }

  async function revokeToken(t) {
    confirmRevoke = t;
  }

  async function doRevoke() {
    const t = confirmRevoke;
    confirmRevoke = null;
    try {
      await api.revokeToken(t.id);
      toast.success('Token revoked');
      await loadTokens();
    } catch (e) {
      toast.error(e.message || 'Failed to revoke');
    }
  }

  async function deleteToken(t) {
    confirmDelete = t;
  }

  async function doDelete() {
    const t = confirmDelete;
    confirmDelete = null;
    try {
      await api.deleteToken(t.id);
      toast.success('Token deleted');
      await loadTokens();
    } catch (e) {
      toast.error(e.message || 'Failed to delete');
    }
  }

  function copyToken() {
    navigator.clipboard.writeText(newTokenPlain).then(
      () => toast.success('Token copied to clipboard'),
      () => toast.error('Copy failed — please select and copy manually')
    );
  }

  function fmtDate(iso) {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  async function changePassword(e) {
    e.preventDefault();
    if (!canSubmit) return;
    saving = true;
    try {
      await api.changeMyPassword(oldPassword, newPassword);
      toast.success('Password changed');
      oldPassword = '';
      newPassword = '';
      confirmPassword = '';
      auth.setMustChange(false);
      await load();
    } catch (e) {
      toast.error(e.message || 'Failed to change password');
    } finally {
      saving = false;
    }
  }

  async function doLogout() {
    try {
      await api.logoutApi();
    } catch (_) {
      // ignore network errors; we still want to clear locally
    }
    auth.logout();
    navigate('/vms'); // re-evaluated by App.svelte to render Login
  }

  $effect(() => {
    if (auth.token) load();
  });
</script>

<div class="p-6 max-w-2xl">
  <div class="mb-6">
    <h1 class="text-xl font-semibold tracking-tight">Account</h1>
    <p class="text-sm text-muted-foreground mt-1">Manage your profile and password</p>
  </div>

  {#if loading}
    <div class="flex items-center gap-2 text-sm text-muted-foreground">
      <Spinner size="sm" />
      Loading account…
    </div>
  {:else if me}
    {#if me.must_change_password}
      <div role="alert" class="mb-6 p-4 border border-warning/40 bg-warning/10 rounded-lg text-sm">
        <div class="flex items-start gap-3">
          <svg
            class="w-5 h-5 mt-0.5 text-warning shrink-0"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            viewBox="0 0 24 24"
          >
            <path
              d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
            />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
          <div>
            <strong class="font-semibold">You must change your password.</strong>
            <p class="text-muted-foreground mt-1">
              Set a new password below before using the rest of the app.
            </p>
          </div>
        </div>
      </div>
    {/if}

    <!-- Profile -->
    <section class="mb-8 border border-border rounded-lg bg-card p-5">
      <h2 class="text-sm font-semibold mb-4">Profile</h2>
      <dl class="grid grid-cols-[160px_1fr] gap-y-2 text-sm">
        <dt class="text-muted-foreground">Username</dt>
        <dd class="font-mono">{me.username}</dd>

        <dt class="text-muted-foreground">Role</dt>
        <dd>
          <span
            class="px-2 py-0.5 rounded text-xs font-medium
						{me.role === 'admin'
              ? 'bg-accent/20 text-accent'
              : me.role === 'operator'
                ? 'bg-info/20 text-info'
                : 'bg-muted text-muted-foreground'}"
          >
            {me.role}
          </span>
        </dd>

        <dt class="text-muted-foreground">Email</dt>
        <dd class="text-muted-foreground">{me.email || '—'}</dd>

        <dt class="text-muted-foreground">Account created</dt>
        <dd class="text-muted-foreground">{me.created_at || '—'}</dd>

        <dt class="text-muted-foreground">Last login</dt>
        <dd class="text-muted-foreground">{me.last_login_at || '—'}</dd>
      </dl>
    </section>

    <!-- Change password -->
    <section class="mb-8 border border-border rounded-lg bg-card p-5">
      <h2 class="text-sm font-semibold mb-4">Change password</h2>
      <form onsubmit={changePassword} class="space-y-4">
        <div>
          <label for="old-pw" class="block text-sm font-medium mb-1.5">Current password</label>
          <div class="relative">
            <input
              id="old-pw"
              bind:value={oldPassword}
              type={showOld ? 'text' : 'password'}
              required
              autocomplete="current-password"
              class="input pr-10"
            />
            <button
              type="button"
              onclick={() => (showOld = !showOld)}
              class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-1"
              aria-label={showOld ? 'Hide password' : 'Show password'}
            >
              {#if showOld}
                <svg
                  class="w-4 h-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  ><path
                    d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"
                  /><line x1="1" y1="1" x2="23" y2="23" /></svg
                >
              {:else}
                <svg
                  class="w-4 h-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  ><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle
                    cx="12"
                    cy="12"
                    r="3"
                  /></svg
                >
              {/if}
            </button>
          </div>
        </div>

        <div>
          <label for="new-pw" class="block text-sm font-medium mb-1.5">New password</label>
          <div class="relative">
            <input
              id="new-pw"
              bind:value={newPassword}
              type={showNew ? 'text' : 'password'}
              required
              minlength="8"
              autocomplete="new-password"
              class="input pr-10"
            />
            <button
              type="button"
              onclick={() => (showNew = !showNew)}
              class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-1"
              aria-label={showNew ? 'Hide password' : 'Show password'}
            >
              {#if showNew}
                <svg
                  class="w-4 h-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  ><path
                    d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"
                  /><line x1="1" y1="1" x2="23" y2="23" /></svg
                >
              {:else}
                <svg
                  class="w-4 h-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  ><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle
                    cx="12"
                    cy="12"
                    r="3"
                  /></svg
                >
              {/if}
            </button>
          </div>
          {#if newPassword}
            <div class="mt-2 flex items-center gap-2 text-xs">
              <div class="flex-1 h-1.5 rounded bg-muted overflow-hidden">
                <div
                  class="h-full transition-all {strength.color}"
                  style="width: {(strength.score + 1) * 20}%"
                ></div>
              </div>
              <span class="text-muted-foreground w-16 text-right">{strength.label}</span>
            </div>
          {/if}
        </div>

        <div>
          <label for="confirm-pw" class="block text-sm font-medium mb-1.5"
            >Confirm new password</label
          >
          <div class="relative">
            <input
              id="confirm-pw"
              bind:value={confirmPassword}
              type={showConfirm ? 'text' : 'password'}
              required
              minlength="8"
              autocomplete="new-password"
              class="input pr-10"
            />
            <button
              type="button"
              onclick={() => (showConfirm = !showConfirm)}
              class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-1"
              aria-label={showConfirm ? 'Hide password' : 'Show password'}
            >
              {#if showConfirm}
                <svg
                  class="w-4 h-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  ><path
                    d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"
                  /><line x1="1" y1="1" x2="23" y2="23" /></svg
                >
              {:else}
                <svg
                  class="w-4 h-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  ><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle
                    cx="12"
                    cy="12"
                    r="3"
                  /></svg
                >
              {/if}
            </button>
          </div>
          {#if confirmPassword && !passwordsMatch}
            <p class="mt-1 text-xs text-destructive">Passwords do not match</p>
          {/if}
        </div>

        <Button type="submit" disabled={!canSubmit || saving}>
          {saving ? 'Changing…' : 'Change password'}
        </Button>
      </form>
    </section>

    <!-- API tokens -->
    <section class="mb-8 border border-border rounded-lg bg-card p-5">
      <div class="flex items-center justify-between mb-2">
        <h2 class="text-sm font-semibold">API tokens</h2>
        <Button size="sm" onclick={() => (showTokenDialog = true)}>New token</Button>
      </div>
      <p class="text-sm text-muted-foreground mb-4">
        Long-lived tokens for scripting. Use the Bearer header: <code
          class="text-xs bg-muted px-1 py-0.5 rounded">Authorization: Bearer wvmb_…</code
        >
      </p>
      {#if tokens.length === 0}
        <p class="text-sm text-muted-foreground">No tokens yet.</p>
      {:else}
        <div class="divide-y divide-border">
          {#each tokens as t (t.id)}
            <div class="flex items-center gap-3 py-2.5">
              <div class="flex-1 min-w-0">
                <div class="flex items-center gap-2">
                  <span class="font-medium text-sm truncate">{t.name}</span>
                  {#if t.revoked}
                    <span
                      class="text-[10px] px-1.5 py-0.5 rounded bg-destructive/10 text-destructive"
                      >revoked</span
                    >
                  {:else if new Date(t.expires_at) < new Date()}
                    <span class="text-[10px] px-1.5 py-0.5 rounded bg-warning/10 text-warning"
                      >expired</span
                    >
                  {/if}
                </div>
                <div class="text-xs text-muted-foreground font-mono mt-0.5">
                  {t.prefix}…
                </div>
                <div class="text-xs text-muted-foreground mt-0.5">
                  Created {fmtDate(t.created_at)} · Expires {fmtDate(t.expires_at)}
                  {#if t.last_used_at}· Last used {fmtDate(t.last_used_at)}{/if}
                </div>
              </div>
              <div class="flex gap-1">
                {#if !t.revoked}
                  <Button size="xs" variant="outline" onclick={() => revokeToken(t)}>Revoke</Button>
                {/if}
                <Button size="xs" variant="destructive" onclick={() => deleteToken(t)}>
                  Delete
                </Button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <!-- Logout -->
    <section class="mb-8 border border-border rounded-lg bg-card p-5">
      <h2 class="text-sm font-semibold mb-2">Session</h2>
      <p class="text-sm text-muted-foreground mb-4">
        Logging out will invalidate the current token on this device.
      </p>
      <Button variant="outline" onclick={doLogout}>Log out</Button>
    </section>
  {:else}
    <p class="text-sm text-muted-foreground">Could not load account information.</p>
  {/if}
</div>

<!-- Create token dialog -->
<ConfirmDialog
  open={showTokenDialog}
  title="Create API token"
  message=""
  confirmLabel={newTokenPlain ? 'Done' : 'Create'}
  hideCancel={!!newTokenPlain}
  onConfirm={() => {
    if (newTokenPlain) {
      showTokenDialog = false;
      newTokenPlain = '';
    } else {
      createToken();
    }
  }}
  onCancel={() => {
    showTokenDialog = false;
    newTokenPlain = '';
  }}
>
  {#if newTokenPlain}
    <div class="space-y-3">
      <p class="text-sm text-muted-foreground">
        Copy this token now. You won't be able to see it again.
      </p>
      <div class="flex items-center gap-2">
        <Input value={newTokenPlain} readonly class="font-mono text-xs" />
        <Button onclick={copyToken} size="sm">Copy</Button>
      </div>
    </div>
  {:else}
    <div class="space-y-3">
      <div>
        <label class="text-sm font-medium block mb-1">Name</label>
        <Input bind:value={newTokenName} placeholder="e.g. ci-deploy, monitoring" />
      </div>
      <div>
        <label class="text-sm font-medium block mb-1">Expires in (hours)</label>
        <Input type="number" bind:value={newTokenTtl} min="1" max="8760" />
        <p class="text-xs text-muted-foreground mt-1">Default 720 (30 days). Max 8760 (1 year).</p>
      </div>
      {#if newTokenSaving}
        <p class="text-sm text-muted-foreground">Creating…</p>
      {/if}
    </div>
  {/if}
</ConfirmDialog>

<ConfirmDialog
  open={!!confirmRevoke}
  title="Revoke token?"
  message={confirmRevoke
    ? `"${confirmRevoke.name}" will be marked revoked. The token will no longer authenticate. You can still see it in the list, but it can't be re-enabled — delete it and create a new one instead.`
    : ''}
  confirmLabel="Revoke"
  onConfirm={doRevoke}
  onCancel={() => (confirmRevoke = null)}
/>

<ConfirmDialog
  open={!!confirmDelete}
  title="Delete token?"
  message={confirmDelete
    ? `"${confirmDelete.name}" will be permanently removed. Any script using it will need to be updated.`
    : ''}
  confirmLabel="Delete"
  onConfirm={doDelete}
  onCancel={() => (confirmDelete = null)}
/>
/>
