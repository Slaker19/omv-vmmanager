<script>
  import { auth, api } from '../lib/stores/auth.svelte.js';
  import { navigate, getRoute } from '../lib/router.svelte.js';
  import { SITE_NAME, SITE_TAGLINE } from '../lib/brand.js';
  import Icon from '$lib/components/Icon.svelte';
  import { Button } from '$lib/components/ui/button';

  let username = $state('');
  let password = $state('');
  let showPassword = $state(false);
  let error = $state('');
  let loading = $state(false);

  // If the auth store redirected us here because a
  // request came back 401 (session expired), show a
  // banner above the form so the operator knows what
  // happened. The previous behaviour was a silent
  // "Unauthorized" toast that left the user on a page
  // that did nothing.
  let sessionExpired = $derived(getRoute()?.query?.reason === 'session_expired');

  async function handleLogin(e) {
    e?.preventDefault();
    if (loading) return;
    error = '';
    loading = true;
    try {
      const res = await api.login(username, password);
      auth.setToken(res.token, res.username, res.role, res.must_change_password);
      if (res.must_change_password) {
        navigate('/account');
      } else {
        navigate('/vms');
      }
    } catch (e) {
      if (e.status === 429) {
        error = e.retryAfter
          ? `Too many failed attempts. Try again in ${e.retryAfter}s.`
          : 'Too many failed attempts. Try again later.';
      } else {
        error = e.message || 'Login failed';
      }
    } finally {
      loading = false;
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center bg-background p-4">
  <div class="w-full max-w-sm">
    <div class="border border-border rounded-lg p-8 bg-card shadow-lg shadow-black/20">
      <div class="text-center mb-8">
        <div class="w-12 h-12 rounded-lg bg-accent flex items-center justify-center mx-auto mb-4">
          <Icon name="computer" size={24} class="text-accent-foreground" />
        </div>
        <h1 class="text-xl font-semibold tracking-tight">{SITE_NAME}</h1>
        <p class="text-muted-foreground text-sm mt-1">{SITE_TAGLINE}</p>
      </div>

      {#if error}
        <div
          role="alert"
          aria-live="assertive"
          class="mb-4 p-3 border border-destructive/30 bg-destructive/10 rounded-md text-destructive text-sm"
        >
          <div class="flex items-center gap-2">
            <Icon name="error" size={16} class="shrink-0" />
            {error}
          </div>
        </div>
      {/if}
      {#if sessionExpired}
        <div
          class="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300"
        >
          Tu sesión expiró. Volvé a iniciar sesión para continuar.
        </div>
      {/if}

      <form onsubmit={handleLogin} class="space-y-3">
        <div>
          <label for="login-user" class="block text-sm font-medium mb-1.5">Username</label>
          <input
            id="login-user"
            bind:value={username}
            type="text"
            required
            placeholder="admin"
            class="input"
            autocomplete="username"
            autocapitalize="off"
            autocorrect="off"
            spellcheck="false"
          />
        </div>

        <div>
          <label for="login-pass" class="block text-sm font-medium mb-1.5">Password</label>
          <div class="relative">
            <input
              id="login-pass"
              bind:value={password}
              type={showPassword ? 'text' : 'password'}
              required
              placeholder="••••••••"
              class="input pr-10"
              autocomplete="current-password"
            />
            <button
              type="button"
              onclick={() => (showPassword = !showPassword)}
              class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-1 rounded"
              aria-label={showPassword ? 'Hide password' : 'Show password'}
              aria-pressed={showPassword}
            >
              <Icon name={showPassword ? 'eyeOff' : 'eye'} size={16} />
            </button>
          </div>
        </div>

        <Button type="submit" disabled={loading} class="w-full mt-2">
          {#if loading}
            <Icon name="spinner" size={16} class="animate-spin" />
            Signing in...
          {:else}
            Sign in
          {/if}
        </Button>
      </form>

      <p class="mt-6 text-center text-xs text-muted-foreground">
        Default credentials: <span class="font-mono">admin / admin</span>
      </p>
    </div>
  </div>
</div>
