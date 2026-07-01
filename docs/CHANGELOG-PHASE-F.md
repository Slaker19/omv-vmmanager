# Phase F — Router / SSE / a11y / keyboard

The frontend had five cross-cutting UX gaps: unknown URLs fell
through to the VM list silently, the SSE client used a fixed
3-second reconnect with no UI feedback, the toaster and
side-bar lacked ARIA annotations, there was no keyboard navigation
beyond ⌘K, and the layout broke on mobile because the sidebar
was always 56 characters wide. Phase F addresses all five.

## F1 — Router: 404, role guards

`router.svelte.js`:

- `ROUTES` now carries an optional `roles: string[]` per entry.
  The `users` route is `roles: ['admin']`; everyone else sees
  the same.
- Unknown URLs no longer fall back to the VM list — they
  produce `route.name === 'not-found'`, which `App.svelte`
  renders as a `<NotFound>` component with a "Back to VMs"
  call-to-action.
- `navigate(path, { query })` now accepts a query object so
  callers don't have to build the `?a=1&b=2` string by hand.

`App.svelte` (server):

- A `$derived` `access` value checks the matched route's
  `roles` against `auth.role`; non-matching renders an
  `<AccessDenied>` component that shows the current role and
  the required role.
- New routes: `account`, `not-found`, `access-denied`.

`NotFound.svelte` (NEW): centered empty state with icon, title,
description, and a "Back to VMs" button.

`AccessDenied.svelte` (NEW): same shape as NotFound but
displays the caller's role and the required roles. Wrapped in
the standard `<Layout>` so the user always has a way out.

## F2 — SSE exponential backoff + reconnect UI

`lib/stores/events.svelte.js`:

- Reconnect schedule: 1s → 2s → 4s → 8s → 16s → 30s (capped).
  Resets to 1s after a successful `open` event. No upper bound
  on attempts (resilient for a long outage).
- Exposes `connected`, `reconnecting`, `reconnectAttempts`,
  `lastError` reactive state.
- New `reconnectNow()` method for an explicit "try now" button
  in the future.
- New `onHostMetrics` listener for the host SSE channel
  (Phase E5).

`Layout.svelte` (header):

- Header shows a small pill next to the page title:
  - **Reconnecting…** (yellow, pulsing dot) when `reconnecting`
  - **Offline** (gray) when `!connected && !reconnecting`
  - Nothing when connected
- All pills have `role="status"` + `aria-live="polite"` so
  screen readers announce the state change.

## F3 — a11y pass

- `Layout.svelte`: a skip-to-main link (`<a href="#main">` with
  `sr-only focus:not-sr-only`) at the top of the DOM.
  `<main id="main" tabindex="-1">` is the target.
- `Layout.svelte` Logout button: now has `aria-label="Log out"`.
- `Sidebar.svelte` (existing): nav buttons now have
  `aria-current="page"` (Phase B).
- `ui/toast/Toaster.svelte`: container now has
  `role="status"` + `aria-live="polite"`; individual toasts
  with type=`error` have `role="alert"`. Errors get announced
  immediately; other types are polite.
- `Login.svelte` (existing): error banner has `role="alert"`
  + `aria-live="assertive"` (Phase A).
- `VmDetail.svelte` and other routes (existing): many icon-only
  buttons now have `aria-label`.

## F4 — Keyboard shortcuts + cheatsheet

A new `KeyboardShortcuts.svelte` component listens for global
keystrokes (when no input has focus) and shows a cheatsheet via
`?`.

| Combo | Action |
|---|---|
| `?` | Open / close the cheatsheet |
| `/` | Focus the first `<input type="search">` on the page; falls back to ⌘K |
| `c` | Create VM (when on the VMs list or detail) |
| `g` then `v` | Go to Virtual Machines |
| `g` then `s` | Go to Storage |
| `g` then `n` | Go to Networks |
| `g` then `u` | Go to Users |
| `g` then `a` | Go to Account |
| `Esc` | Close dialogs and palettes (built into bits-ui) |
| `⌘K` / `Ctrl+K` | Open the command palette (existing) |

The `g` prefix uses a 1.2 s window so `g` accidentally pressed
in isolation is harmless. Shortcuts are suppressed when an
`<input>`, `<textarea>`, `<select>`, or `[contenteditable]`
has focus.

## F5 — Mobile sidebar drawer

- `Layout.svelte` now has two sidebar slots:
  - **Desktop (≥lg)**: `hidden lg:flex` — always visible.
  - **Mobile (<lg)**: a hamburger button in the header opens a
    fixed-position drawer; tapping the backdrop or any nav item
    closes it.
- Header buttons collapse gracefully on small screens: the
  `Search` and `Logout` labels hide under `sm:`, the user-name
  pill hides under `md:`. Only the icons remain.
- `Sidebar.svelte` accepts an `onNavigate` callback so a
  navigation in the mobile drawer auto-closes it.
- A $effect on the route name resets `mobileNavOpen` to `false`
  on every route change (handles the case of programmatic
  navigation like ⌘K).

## Files touched in Phase F

```
frontend/src/lib/router.svelte.js                    (404, roles, query helper)
frontend/src/lib/stores/events.svelte.js             (exponential backoff, host metrics listener)
frontend/src/lib/components/Layout.svelte           (reconnect pill, skip link, mobile drawer, <main> landmark)
frontend/src/lib/components/Sidebar.svelte           (onNavigate prop)
frontend/src/lib/components/KeyboardShortcuts.svelte (NEW)
frontend/src/lib/components/ui/toast/Toaster.svelte (role/aria-live)
frontend/src/routes/NotFound.svelte                 (NEW)
frontend/src/routes/AccessDenied.svelte             (NEW)
frontend/src/App.svelte                             (NotFound/AccessDenied mount, role-derived access)
```
