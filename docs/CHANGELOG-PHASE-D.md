# Phase D — VmList power features

The primary screen of the app was missing several features that
are table-stakes for a VM manager: sorting, bulk actions,
pagination, and shareable filter URLs. Phase D adds them.

## D1 — DataTable upgrades

`frontend/src/lib/components/DataTable.svelte` gains three new
optional feature sets:

- **Sorting**: `sortable` (global flag) + per-column `sortable`/
  `sortAccessor` overrides. `bind:sortKey` + `bind:sortDir` are
  two-way bindings. Click column header to toggle. Indicator
  arrow shows current sort column + direction.
- **Selection**: `selectable` flag + `bind:selectedKeys`. A
  leading checkbox column is added; clicking the header checkbox
  selects all *on the current page*. The indeterminate state is
  used when only some rows are selected.
- **Pagination**: `pageSize` + `bind:page`. A new
  `Pagination.svelte` component renders the "Showing X–Y of N"
  indicator and prev/next buttons.

A new `Pagination.svelte` standalone component is exported from
`frontend/src/lib/components/Pagination.svelte` and is reused by
VmList and Users.

## D2 — VmList changes

### URL-persisted filters
Every filter now lives in the URL hash so it's shareable and
survives a page refresh:

| Query key | Effect |
|---|---|
| `?q=foo` | Search query |
| `?group=web` | Group filter |
| `?state=running` | State filter (running / shutoff / paused / crashed) |
| `?sort=ram_mb&dir=desc` | Default sort |
| `?view=table` | Default view mode |

`history.replaceState` is used so the back button isn't
polluted. Initial state is read from `route.query` on mount.

### State filter
Four new chip buttons (running / shutoff / paused / crashed) sit
next to the group chips. State + group + search are AND-combined.

### Bulk actions
A new "N selected" action bar appears when one or more checkboxes
are ticked. Actions:
- **Start / Shutdown / Force off** — visible to operator+admin
- **Tag with group** — opens a dialog with group-name input +
  existing-group quick-pick chips
- **Delete** — admin only, with a confirmation dialog that
  spells out the effect
- **Clear** — drops the selection

Per-VM operations run sequentially; partial failures are reported
(0 success, 0 fail, or mixed). Selection is auto-cleared after
each action and on filter changes.

### Pagination
25 rows per page in table view (the grid view keeps all tiles).
The new `Pagination` component renders below the table.

### Sortable columns
Every data column (state, name, vCPU, RAM, CPU, MEM, IP, uptime)
is sortable with a click. Custom `sortAccessor` lets sparkline
columns sort by their *latest* value rather than the column key.

### Grid view actions (deferred)
Hover-to-reveal Start/Stop/Delete on grid cards is **not** part
of this phase — the click target is the entire card so the
nested buttons are noisy. Will be added in a follow-up.

## D3 — Filter chip consolidation

Group chips and state chips share a row and styling. The
"clear all" behaviour on the "All" chip resets state + group +
search.

## Files touched in Phase D

```
frontend/src/lib/components/DataTable.svelte            (sortable, selectable, pagination, indeterminate checkbox)
frontend/src/lib/components/Pagination.svelte           (NEW)
frontend/src/routes/VmList.svelte                       (URL filters, state filter, bulk actions, sortable, pagination, selection)
```
