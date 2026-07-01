<script>
  import { getRoute, navigate } from '../router.svelte.js';
  import { auth } from '../stores/auth.svelte.js';
  import { APP_VERSION, SITE_NAME } from '../brand.js';
  import Icon from './Icon.svelte';

  let { onNavigate = () => {} } = $props();

  const route = $derived(getRoute());

  const allItems = [
    {
      id: 'vms',
      path: '/vms',
      label: 'Virtual Machines',
      icon: 'computer',
      roles: ['admin', 'operator', 'viewer'],
    },
    {
      id: 'storage',
      path: '/storage',
      label: 'Storage',
      icon: 'hardDrive',
      roles: ['admin', 'operator', 'viewer'],
    },
    {
      id: 'networks',
      path: '/networks',
      label: 'Networks',
      icon: 'network',
      roles: ['admin', 'operator', 'viewer'],
    },
    { id: 'users', path: '/users', label: 'Users', icon: 'users', roles: ['admin'] },
    { id: 'nodes', path: '/nodes', label: 'Nodes', icon: 'server', roles: ['admin'] },
    { id: 'backup', path: '/backup', label: 'Backup', icon: 'archive', roles: ['admin'] },
    { id: 'settings', path: '/settings', label: 'Settings', icon: 'settings', roles: ['admin'] },
    {
      id: 'status',
      path: '/status',
      label: 'System',
      icon: 'activity',
      roles: ['admin', 'operator', 'viewer'],
    },
  ];

  const navItems = $derived(allItems.filter((it) => it.roles.includes(auth.role || '')));

  function isActive(id) {
    if (
      id === 'vms' &&
      (route.name === 'vms' || route.name === 'vm-detail' || route.name === 'vms-new')
    )
      return true;
    return route.name === id;
  }

  function go(path) {
    navigate(path);
    onNavigate();
  }
</script>

<aside class="w-56 border-r border-border flex flex-col shrink-0 h-screen bg-card">
  <div class="p-4 border-b border-border">
    <div class="flex items-center gap-3">
      <div class="w-8 h-8 rounded-lg bg-accent flex items-center justify-center shrink-0">
        <Icon name="computer" size={16} class="text-accent-foreground" />
      </div>
      <div class="min-w-0">
        <span class="font-semibold text-sm truncate block">{SITE_NAME}</span>
        <p class="text-xs text-muted-foreground">Manager</p>
      </div>
    </div>
  </div>

  <nav class="flex-1 p-2 space-y-0.5 overflow-y-auto">
    {#each navItems as item (item.id)}
      <button
        onclick={() => go(item.path)}
        class="w-full flex items-center gap-2.5 px-2.5 py-2 rounded-md text-sm font-medium transition-colors {isActive(
          item.id
        )
          ? 'bg-accent/10 text-accent'
          : 'text-muted-foreground hover:text-foreground hover:bg-muted'}"
      >
        <Icon name={item.icon} size={16} class="shrink-0" />
        {item.label}
      </button>
    {/each}
  </nav>

  <div class="p-2 border-t border-border space-y-0.5">
    <button
      onclick={() => go('/account')}
      class="w-full flex items-center gap-2.5 px-2.5 py-2 rounded-md text-sm transition-colors {route.name ===
      'account'
        ? 'bg-accent/10 text-accent'
        : 'text-muted-foreground hover:text-foreground hover:bg-muted'}"
      aria-current={route.name === 'account' ? 'page' : undefined}
    >
      <Icon name="users" size={16} class="shrink-0" />
      <div class="flex-1 text-left min-w-0">
        <div class="font-medium truncate">{auth.user || 'Account'}</div>
        <div class="text-xs text-muted-foreground truncate">{auth.role || '—'}</div>
      </div>
    </button>
    <div class="px-2.5 py-1 text-[11px] text-muted-foreground font-mono">v{APP_VERSION}</div>
  </div>
</aside>
