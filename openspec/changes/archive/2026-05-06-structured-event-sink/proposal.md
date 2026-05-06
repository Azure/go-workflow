## Why

Currently, observability in go-workflow relies on users wiring up `BeforeStep`/`AfterStep`
callbacks manually on every step they care about. There is no structured way to observe
all steps globally — no lifecycle events, no attempt count, no timing, no retry visibility.

In production, you need to answer: which step is running right now? How many retries has step X
done? How long did step Y take? None of these are answerable today without bespoke instrumentation.

Temporal exposes a full Event History for every workflow execution. go-workflow should offer a
lightweight equivalent: a global `EventSink` that receives structured events for every step
lifecycle transition.

## What Changes

- A `WorkflowEvent` struct capturing step identity, event type, attempt, error, and timestamp.
- An `EventSink` interface (or a simple `func`) that the `Workflow` calls on every transition.
- A field on `Workflow` to register the sink.

## Capabilities

### New Capabilities

- **Structured events**: every meaningful transition emits a `WorkflowEvent`:
  - `Scheduled` — step is ready to run (all upstreams terminated, condition evaluated to Running)
  - `Started` — goroutine launched, `Do()` about to be called
  - `Retrying` — `Do()` returned an error, backoff is sleeping before next attempt
  - `Succeeded` / `Failed` / `Canceled` / `Skipped` — terminal transitions
  - `HeartbeatReceived` — if heartbeat feature lands (see heartbeat-and-liveness change)

- **EventSink integration**: `Workflow.EventSink` is a function `func(WorkflowEvent)` (or an
  interface). Simple function type avoids an extra abstraction layer and is trivially composable
  (fan-out = call multiple funcs).

- **Zero-cost when unset**: if `EventSink` is nil, no allocations occur on the hot path.

- **Out-of-box adapters (separate package or examples)**:
  - `slog` adapter — logs each event as a structured log line
  - OpenTelemetry span adapter — wraps each step attempt in a trace span
  - Prometheus metrics adapter — increments counters and records histograms

### Example sketch

```go
w := &flow.Workflow{
    EventSink: func(e flow.WorkflowEvent) {
        slog.Info("step event",
            "step", flow.String(e.Step),
            "event", e.Type,
            "attempt", e.Attempt,
            "err", e.Err,
            "duration", e.Duration,
        )
    },
}
```

### Open Questions

- `WorkflowEvent.Step` is a `Steper` (interface/pointer). For logging we need a stable string
  name. Should `WorkflowEvent` also carry a pre-computed `StepName string` (from `flow.String()`)?
  Probably yes, to avoid callers doing it themselves.

- Should `Retrying` carry the backoff duration so callers can log "retrying in 2s"?

- Should the sink be called synchronously on the step goroutine, or dispatched async?
  Synchronous is simpler and predictable; async risks hiding slow sinks.

## Impact

- New `WorkflowEvent` struct and `EventType` constants.
- `Workflow` struct — add `EventSink func(WorkflowEvent)` field.
- `workflow.go` — call sink at each status transition (in `tick` and `runStep`).
- New spec: `openspec/specs/event-sink/spec.md`.
- No breaking changes.
