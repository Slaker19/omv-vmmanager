# Phase C — Design system fix

The frontend had two pre-existing bugs plus 30+ instances of
duplicated UI primitives. Phase C fixes the bugs and consolidates
the primitives into reusable components.

## C1 — Tokens (was a user-visible bug)

**Problem**: shadcn's `Button default` variant uses
`bg-primary text-primary-foreground` (button.svelte:10), but
`--primary` was never defined in `app.css`. The `.dark` class
was also never applied to `<html>`, so all `dark:` utilities
were inert.

**Fix**:
- `app.css`: added `--primary` and `--primary-foreground` in
  `:root`, mapped `--color-primary` + `--color-primary-foreground`
  in `@theme inline`, and added `--info`/`--info-foreground`/
  `--info-subtle` for parity with `success`/`warning`/`destructive`.
- `main.js`: `document.documentElement.classList.add('dark')` on
  startup.
- Firefox `scrollbar-color` added for non-WebKit browsers.

Effect: primary buttons (Create VM, Save, Sign in, OK, Toaster)
now render with a real indigo background. shadcn `dark:`
utilities now activate.

## C2 — Reusable components

All new components live in `frontend/src/lib/components/`:

| Component | Replaces |
|---|---|
| `Spinner.svelte` | ~30 instances of `<div class="spinner !w-X !h-Y !border-…">` |
| `Alert.svelte` | Bespoke `<div class="border border-destructive/30 bg-destructive/10 ...">` red/green/blue banners (role="alert" + aria-live on error/warning variants) |
| `PageHeader.svelte` | Repeated `<div class="flex items-center justify-between mb-6"><h1>...</h1><p>...</p></div>` triple on every route |
| `EmptyState.svelte` | Bespoke empty illustrations (VmList, Storage, Networks, Users) |
| `Switch.svelte` | Indigo pill toggles in VmCreate + VmDetail Identity dialog |
| `Checkbox.svelte` | Raw `<input type="checkbox">` in VmDetail, Networks, Users |
| `Tabs.svelte` | Hand-rolled tab buttons in VmDetail Identity dialog |
| `ProgressBar.svelte` | Bespoke progress UI in VmList (import), Storage (upload/download), VmDetail (export) |
| `SearchInput.svelte` | Bespoke search inputs in VmList, Users, with optional debounce |

### Replacements applied this phase

- All 30+ `<div class="spinner ...">` instances → `<Spinner size color/>`
  (script-driven across Login, Networks, Storage, Users, VmCreate,
  VmDetail, VmList, Account)
- Top-level error banners in Networks, Status, Storage, VmCreate,
  VmDetail, VmList, Users → `<Alert variant="error">{error}</Alert>`
- `Users`, `VmList`, `Status`, `Networks`, `Storage` now use
  `PageHeader` with `actions` snippet
- `VmList` search field is now a `SearchInput` (clearable)

The remaining hand-rolled patterns (Identity dialog tabs, switch
toggles, empty states, progress UI inside dialogs) are good
candidates for the next cleanup pass — the components exist and
can be adopted incrementally.

## C3 — Tooltip (deferred)

The shadcn `tooltip` package was not installed; rather than add
a new dependency mid-phase, this is left for Phase F (a11y pass)
which will install the shadcn Tooltip + audit all icon-only
buttons for `aria-label`.

## Files touched in Phase C

```
frontend/src/app.css                                  (--primary, --info, Firefox scrollbar)
frontend/src/main.js                                  (.dark class on <html>)
frontend/src/lib/components/Spinner.svelte            (NEW)
frontend/src/lib/components/Alert.svelte              (NEW)
frontend/src/lib/components/PageHeader.svelte         (NEW)
frontend/src/lib/components/EmptyState.svelte         (NEW)
frontend/src/lib/components/Switch.svelte             (NEW)
frontend/src/lib/components/Checkbox.svelte           (NEW)
frontend/src/lib/components/Tabs.svelte               (NEW)
frontend/src/lib/components/ProgressBar.svelte        (NEW)
frontend/src/lib/components/SearchInput.svelte        (NEW)
frontend/src/routes/Login.svelte                      (Spinner)
frontend/src/routes/Networks.svelte                   (Spinner, Alert, PageHeader)
frontend/src/routes/Status.svelte                     (Spinner, Alert, PageHeader)
frontend/src/routes/Storage.svelte                    (Spinner, Alert, PageHeader)
frontend/src/routes/Users.svelte                      (Spinner, Alert, PageHeader, SearchInput)
frontend/src/routes/VmCreate.svelte                   (Spinner, Alert)
frontend/src/routes/VmDetail.svelte                   (Spinner, Alert)
frontend/src/routes/VmList.svelte                     (Spinner, Alert, PageHeader, SearchInput)
frontend/src/routes/Account.svelte                    (Spinner)
```
