<script>
  import { navigate } from '../lib/router.svelte.js';
  import { auth } from '../lib/stores/auth.svelte.js';
  import { Button } from '$lib/components/ui/button';
  import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<div class="p-6 max-w-2xl">
  <EmptyState
    icon="empty"
    title="Access denied"
    description="Your role ({auth.role ||
      'unknown'}) doesn't have permission to view this page. Ask an admin to grant you access, or go back to a page you can view."
  >
    {#snippet action()}
      <Button onclick={() => navigate('/vms')}>Back to VMs</Button>
    {/snippet}
  </EmptyState>
</div>
