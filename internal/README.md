# Backend layout (modular monolith / DDD direction)

## Layers

| Package | Role |
|---------|------|
| `domain` | Pure types + business rules (no I/O, stdlib only) |
| `app` | Use cases / orchestration (DB + HTTP clients + jobs) |
| `store` | Persistence only — load/save, no secret-merge policy long-term |
| `monitor` / `epay` | Anti-corruption layers for external APIs |
| `httpapi` | HTTP adapters — call `app`, not `store` |

## Rules of thumb

1. **Domain stays pure.** Thresholds, mute/restore, secret merge, formulas live here.
2. **App orchestrates.** Load rules → call domain → persist / call remote.
3. **Store does not decide.** Prefer receiving already-merged entities from app.
4. **httpapi stays thin.** Handlers call `s.App.*` use cases only — no `s.App.Store`.

## Evolution

See session DDD plan: enrich domain (probe/merge/profit) → split app services → ports + remove Store bypass.

### Domain pure-rule surface (so far)

| Area | Package files | Notes |
|------|---------------|--------|
| Probe mute / auto-disable | `probe.go` | thresholds, restore eligibility |
| Merge / secret keep | `merge.go`, `secrets.go` | `MergeUpdate` on aggregates |
| Profit math | `profit.go` | `UsageUnits`, `LineProfit`, cost helpers |
| Notify mapping | `notify.go` | `ShouldNotify`, `AlertEventType` |
| Revenue card rules | `revenue.go` | normalize/validate source types |
| Scheduler group matching | `scheduler_groups.go` | `GroupsForPrice`, `TargetGroups`, `SplitGroups` |

### App facades (Phase 2)

`app.Service` remains the composition root for `httpapi`. Bounded contexts are extracted as same-package facades:

| Facade | Field | Owns |
|--------|-------|------|
| `SchedulerService` | `Service.Scheduler` | config, channels/groups apply, cost snapshots, auto disable/restore |
| `ProfitService` | `Service.ProfitSvc` | pool profit aggregation from scheduler logs |
| `ProbeService` | `Service.Probe` | model cards CRUD, probe runs, upstream check, monitor status |
| `CLIProxyService` | `Service.CLIProxy` | CLIProxyAPI config, auth files, quota reset/snapshot |
| `TGService` | `Service.TG` | Telegram session, channels, message sync/media cache |

Public methods stay on `*Service` via thin forwarders in `facade_forwarders.go` (no HTTP handler churn).

### App use-cases for thin httpapi (Phase 3)

`use_cases.go` exposes store-backed operations that handlers used to call via `s.App.Store`:

- upstream get/delete + browser token capture
- card / revenue-card get/delete
- ops mark/ack (single + bulk)
- audit record, public site settings
- export/import data (`app.ExportData` DTO — httpapi does not import store for this)
- TG channel/message list/get/delete

**httpapi no longer touches `App.Store`.** Auth still uses `*store.Store` only in `AuthenticatedRequest` (session lookup at the edge).

### Minimal ports (Phase 3 / PR7)

Defined in `ports.go` and injected on `Service` (defaults in `New`):

| Port | Field | Default | Used by |
|------|-------|---------|---------|
| `CardRepository` | `Cards` | `*store.Store` | ProbeService, SchedulerService, `GetCard` |
| `Notifier` | `Notify` | Telegram via `sendTelegram` | `alert`, `TestNotification` |
| `ProbeRunner` | `Prober` | live `monitor.Client.Probe` | card probe path |

Not a full hexagonal rewrite: only hard-to-test boundaries. Remaining contexts still use concrete `*store.Store` / `monitor.Client`.

### Remaining (optional later)

- More ports where tests need isolation (upstream repo, balance runner)
- Split large facades into subpackages if package size becomes painful
- `ModelCard` conceptual split (probe vs pool binding) — needs migration design
