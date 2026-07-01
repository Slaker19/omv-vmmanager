<script>
  import PageHeader from '$lib/components/PageHeader.svelte';
</script>

<div class="p-6 max-w-5xl">
  <PageHeader
    title="Nodes"
    subtitle="Libvirt hosts the backend can manage. The local node is auto-created from the LIBVIRT_URI env var. Remote nodes are SSH/TCP libvirt URIs the backend can reach."
  />

  <div class="flex flex-col items-center justify-center py-24 text-center">
    <p class="text-lg font-medium text-muted-foreground">Coming Soon</p>
    <p class="text-sm text-muted-foreground mt-1">
      Multi-host support is planned for a future release.
    </p>
  </div>
</div>
