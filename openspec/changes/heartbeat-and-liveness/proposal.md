## Why

Long-running steps (large file processing, external API polling, batch jobs) can silently stall
inside `Do()` — the goroutine is alive but making no progress. Currently go-workflow has no way
to detect this. Temporal solves it with an Activity Heartbeat: the step periodically signals
liveness, and the runtime kills and retries it if heartbeats stop.

go-workflow should provide an equivalent mechanism that fits its single-process, in-memory model.

## What Changes

- A `Heartbeat` function that a step calls periodically during `Do()` to signal it is alive.
- A `HeartbeatTimeout` field in `RetryOption` (or `StepOption`) — if no heartbeat arrives within
  this duration, the step's context is canceled and the step is treated as failed (and retried
  if retry is configured).
- Optionally, a heartbeat payload so a retried step can recover its last known progress
  (similar to Temporal's `activity.GetHeartbeatDetails`).

## Capabilities

### New Capabilities

- **Heartbeat signaling**: step calls `flow.Heartbeat(ctx)` or `flow.HeartbeatWith(ctx, payload)`
  inside its `Do()` loop. The call is a no-op if no heartbeat timeout is configured, so it is
  safe to add unconditionally.

- **Liveness enforcement**: when `HeartbeatTimeout` is set on a step, the workflow starts a
  watchdog that cancels the step's context if `HeartbeatTimeout` elapses without a heartbeat.
  The step's `ctx.Done()` fires, `Do()` returns `context.Canceled`, and normal retry logic applies.

- **Progress recovery**: `flow.LastHeartbeat[T](ctx)` returns the payload from the most recent
  heartbeat of the previous attempt, so a retried step can resume from where it left off instead
  of restarting from zero.

### Open Questions

- Should `HeartbeatTimeout` live in `RetryOption` (only meaningful with retry) or `StepOption`
  (independent — you might want liveness detection even without retry)?
  Leaning toward `StepOption` so liveness and retry are orthogonal concerns.

- Payload type: generic `any` with the caller doing type assertion, or a typed generic helper
  `flow.HeartbeatWithTyped[T]`? Generic is cleaner but requires Go 1.21+ type inference.

- Should the watchdog goroutine be started per-step or shared across steps?
  Per-step is simpler and avoids coordination overhead.

## Impact

- `StepOption` or `RetryOption` — add `HeartbeatTimeout *time.Duration`.
- New exported functions: `Heartbeat(ctx)`, `HeartbeatWith(ctx, payload)`,
  `LastHeartbeat[T](ctx)`.
- `workflow.go` / `runStep` — start watchdog goroutine when `HeartbeatTimeout` is set.
- New spec: `openspec/specs/heartbeat/spec.md`.
- No breaking changes; steps that do not call `Heartbeat` are unaffected.
