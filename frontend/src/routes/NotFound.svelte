<script>
  import { navigate } from '../lib/router.svelte.js';
  import { Button } from '$lib/components/ui/button';
  import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<div class="p-6 max-w-2xl">
  <EmptyState
    icon="empty"
    title="Page not found"
    description="The URL you followed doesn't match any route. It may be a typo, or this page was removed in a recent update."
  >
    {#snippet action()}
      <Button onclick={() => navigate('/vms')}>Back to VMs</Button>
    {/snippet}
  </EmptyState>
</div>
