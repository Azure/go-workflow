## Why

go-workflow's root module is intentionally minimal: it provides the orchestration framework
and nothing more. But users repeatedly solve the same problems on top of it — waiting for a
signal, rate-limiting against an external API, sending a notification when something fails,
implementing a human-approval gate, etc.

Today each user reinvents these patterns from scratch. A `contrib/` subtree of submodules
can provide battle-tested, reusable step implementations while keeping the root module lean
and dependency-free.

This mirrors the Go ecosystem pattern (e.g., `golang.org/x/...`, OpenTelemetry's
`go.opentelemetry.io/contrib/...`).

## What Changes

- A new `contrib/` directory at the repo root, containing independent Go modules.
- Each submodule has its own `go.mod` and can pull in external dependencies without
  affecting users of the root module.
- The root module has zero new dependencies.

## Capabilities

### New Capabilities

**Initial candidate submodules** (exact list TBD, these are ideas):

- **`contrib/steps`** — general-purpose reusable steps:
  - `WaitSignal[T]` — a step that blocks until a value is sent on a channel, useful for
    human-approval gates or inter-workflow coordination.
  - `Delay` — a step that waits for a fixed duration (respects context cancellation).
  - `Gate` — a step that waits until a predicate becomes true (polls with interval + timeout).

- **`contrib/notify`** — notification steps (Slack, PagerDuty, email, webhook) to use in
  `AnyFailed` / cleanup branches.

- **`contrib/ratelimit`** — a `BeforeStep` callback factory that applies token-bucket or
  leaky-bucket rate limiting across a group of steps (useful when steps call the same
  external API).

- **`contrib/otel`** — OpenTelemetry integration: an `EventSink` (once that feature lands)
  plus a `BeforeStep`/`AfterStep` pair that creates trace spans and records metrics.

### Design Principles for Contrib Steps

1. Every contrib step implements `flow.Steper` — fully composable with the core framework.
2. Steps are configurable via struct fields, not functional options (keeps it simple).
3. Each submodule depends only on the root `go-workflow` module plus minimal external deps.
4. Steps are tested independently; no contribution required in the root module's test suite.

### Open Questions

- Flat `contrib/steps` vs. per-concern submodules (`contrib/signal`, `contrib/ratelimit`)?
  Single `contrib/steps` is simpler initially; can split later if dependency sets diverge.

- Should contrib steps live in this repo or a separate `go-workflow-contrib` repo?
  Same repo is easier to keep in sync with API changes; separate repo avoids repo bloat.
  Leaning toward same repo with clear directory boundary.

## Impact

- New `contrib/` directory with one or more `go.mod` files.
- No changes to root module code or go.mod.
- CI: add build/test jobs for each contrib submodule.
