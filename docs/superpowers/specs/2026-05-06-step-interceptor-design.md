# Step Interceptor Design

**Date:** 2026-05-06
**Status:** Draft
**Scope:** go-workflow structured observability via two-layer interceptor system

---

## Why

Currently, observability in go-workflow requires users to wire `BeforeStep`/`AfterStep` callbacks
manually on individual steps. There is no structured way to observe all steps globally — no
lifecycle events, no attempt count, no timing.

In production, you need to answer: which step is running right now? How many retries has step X
done? How long did step Y take? None of these are answerable today without bespoke instrumentation.

This design introduces a two-layer interceptor system that:
- Provides global, structured observability across all steps
- Is orthogonal to `BeforeStep`/`AfterStep` — they serve different scopes and both are preserved
- Propagates automatically into nested `SubWorkflow`s
- Ships with built-in `EventSink` adapters for slog, OTel, Prometheus

---

## Concepts

### StepStatus vs EventType

These are deliberately separate types serving different consumers.

**`StepStatus`** is the state machine used by the orchestration engine. It is persistent and
queryable. The `Condition` system reads it to decide whether to run downstream steps.

**`EventType`** is a stream of instantaneous observations for external consumers (logs, traces,
metrics). It is fire-and-forget.

The key difference: `Running` is a single `StepStatus` that spans the entire retry loop, but
within it multiple `EventStarted` events occur. They cannot be merged without breaking the
`Condition` system.

```
StepStatus:  Pending ──────────────────────────────► Running ──────────────► Succeeded
                                                                         └──► Failed
                                                                         └──► Canceled
             └─────────────────────────────────────────────────────────────► Skipped
             └─────────────────────────────────────────────────────────────► Canceled

EventType:            EventScheduled  EventStarted  EventStarted  EventStarted  EventSucceeded/EventFailed/EventCanceled
                                      [attempt 0]   [attempt 1]   [attempt 2]
```

Mapping of EventType to where it is emitted:

| EventType        | StepStatus transition          | Emitted in               |
|------------------|-------------------------------|--------------------------|
| `EventScheduled` | `Pending → scheduled`         | StepInterceptor entry    |
| `EventStarted`   | status stays `Running`        | AttemptInterceptor entry |
| `EventSucceeded` | `Running → Succeeded`         | StepInterceptor exit     |
| `EventFailed`    | `Running → Failed`            | StepInterceptor exit     |
| `EventCanceled`  | `Running/Pending → Canceled`  | StepInterceptor exit     |
| `EventSkipped`   | `Pending → Skipped`           | StepInterceptor exit     |

**Ownership of events by layer:**

- `StepInterceptor` sees only: `EventScheduled` + one terminal event.
  It is not aware of how many retries occurred.
- `AttemptInterceptor` sees: `EventStarted` per attempt. The failure error for each
  attempt is available when `InterceptAttempt` returns.

`EventFailed` is **only** a terminal event. Individual attempt failures within a retry loop
are not separately named — they are observable via the error returned from `InterceptAttempt`.

---

## Architecture

### Two-Layer Interceptor Stack

```
StepInterceptor[0]
  └── StepInterceptor[1]
        └── [retry loop]
              └── AttemptInterceptor[0]
                    └── AttemptInterceptor[1]
                          └── [per-step BeforeStep callbacks]   ← from StepConfig
                                └── step.Do(ctx)
                                      └── [per-step AfterStep callbacks]
```

**StepInterceptor** wraps the entire lifecycle of a step including all retry attempts. It sees
the step exactly once: entry on `EventScheduled`, exit on terminal status. It has no visibility
into individual retry attempts. It is the right place for OTel spans (one span per step, not per
attempt) and step-level metrics.

**AttemptInterceptor** wraps each individual attempt (`Before → Do → After`). It sees every
attempt, including retried ones. The failure error for each attempt is available on return.
It is the right place for per-attempt logging, attempt-level tracing, and retry observability.

**BeforeStep/AfterStep** (existing) are a different mechanism from Interceptors. Interceptors are
workflow-level and apply globally to all steps. BeforeStep/AfterStep are step-level and are
configured per-step via `StepConfig`. They are orthogonal: in the execution stack, Interceptors
execute on the outside, BeforeStep/AfterStep execute on the inside — but conceptually they belong
to different layers of the system and serve different purposes. Users configure them independently.

### stepExecution (internal)

The current anonymous goroutine in `tick()` is replaced by a `stepExecution` struct that owns
the full step lifecycle:

```go
type stepExecution struct {
    w       *Workflow
    step    Steper
    state   *State
    attempt uint64  // single source of truth for attempt count
}
```

`attempt` is incremented in a wrapper inside `buildAttemptChain` that surrounds the full
interceptor chain, so it always advances regardless of whether interceptors short-circuit.

### tick() simplification

`tick()` is reduced to a single responsibility: **atomically claiming a step** to prevent
double-spawning. All other logic moves into `stepExecution.run()`.

```go
// tick() — before
if w.lease() {
    state.SetStatus(Running)          // claim + status in one
    go func() { ... runStep ... }()
}

// tick() — after
if w.lease() {
    state.SetStatus(scheduled)        // claim only (private sentinel)
    w.waitGroup.Add(1)
    go (&stepExecution{...}).run(ctx)
}
```

`scheduled` is a private `StepStatus` sentinel. It is never exposed to users or visible in
`Condition` evaluation. Its only purpose is to prevent `tick()` from spawning the same step
twice.

Condition evaluation moves into `stepExecution.run()`. This is safe because by the time a step
is eligible to run, all its upstreams are terminated — their status cannot change.

---

## API

### New Types

```go
// StepInterceptor intercepts the full lifecycle of a step (all retry attempts).
// info.TerminalReason is Pending for steps that will execute normally.
// For Skipped or Canceled steps, TerminalReason is set and next must not be called.
type StepInterceptor interface {
    InterceptStep(ctx context.Context, info StepInfo, next func(ctx context.Context) error) error
}

// AttemptInterceptor intercepts each individual attempt (Before → Do → After).
type AttemptInterceptor interface {
    InterceptAttempt(ctx context.Context, info AttemptInfo, next func(ctx context.Context) error) error
}

// StepInterceptorFunc is a function adapter for StepInterceptor.
type StepInterceptorFunc func(ctx context.Context, info StepInfo, next func(ctx context.Context) error) error

// AttemptInterceptorFunc is a function adapter for AttemptInterceptor.
type AttemptInterceptorFunc func(ctx context.Context, info AttemptInfo, next func(ctx context.Context) error) error

// StepInfo is passed to StepInterceptor.
type StepInfo struct {
    Step           Steper
    TerminalReason StepStatus // Pending = will execute; Skipped/Canceled = will not execute
}

// AttemptInfo is passed to AttemptInterceptor.
// Interceptors that need timing should record time.Now() at the top of InterceptAttempt.
type AttemptInfo struct {
    StepInfo
    Attempt uint64
}

// EventType identifies a step lifecycle event.
type EventType string

const (
    EventScheduled EventType = "Scheduled"
    EventStarted   EventType = "Started"
    EventSucceeded EventType = "Succeeded"
    EventFailed    EventType = "Failed"
    EventCanceled  EventType = "Canceled"
    EventSkipped   EventType = "Skipped"
)

// WorkflowEvent carries information about a step lifecycle event.
type WorkflowEvent struct {
    Step     Steper
    Type     EventType
    Attempt  uint64
    Err      error
    Duration time.Duration
}

// InterceptorReceiver is implemented by steps that contain a sub-workflow
// and need to receive interceptors from the parent workflow.
type InterceptorReceiver interface {
    PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}
```

### Workflow struct additions

```go
type Workflow struct {
    // ... existing fields unchanged ...

    // StepInterceptors are called once per step, wrapping the full retry lifecycle.
    // Executed in order: [0] is outermost, [len-1] is innermost.
    StepInterceptors []StepInterceptor

    // AttemptInterceptors are called once per attempt, inside the retry loop.
    // Executed in order: [0] is outermost, [len-1] is innermost.
    // BeforeStep/AfterStep callbacks are always the innermost layer.
    AttemptInterceptors []AttemptInterceptor
}
```

### Built-in EventSink adapters

```go
// NewStepEventSink returns a StepInterceptor that emits EventScheduled and a terminal
// event (EventSucceeded/EventFailed/EventCanceled/EventSkipped) for every step.
// It is not aware of individual retry attempts.
func NewStepEventSink(sink func(WorkflowEvent)) StepInterceptor

// NewAttemptEventSink returns an AttemptInterceptor that emits an EventStarted event
// for each attempt. The failure error (if any) is available when InterceptAttempt returns.
func NewAttemptEventSink(sink func(WorkflowEvent)) AttemptInterceptor
```

Usage examples:

```go
// Step-level only
w := &flow.Workflow{
    StepInterceptors: []flow.StepInterceptor{
        flow.NewStepEventSink(func(e flow.WorkflowEvent) {
            slog.Info("step event",
                "step", flow.String(e.Step), "type", e.Type,
                "err", e.Err, "duration", e.Duration,
            )
        }),
    },
}

// Full observability: step-level spans + per-attempt detail
w := &flow.Workflow{
    StepInterceptors:    []flow.StepInterceptor{myOtelStepInterceptor},
    AttemptInterceptors: []flow.AttemptInterceptor{flow.NewAttemptEventSink(mySink)},
}
```

---

## SubWorkflow Propagation

`SubWorkflow` implements `InterceptorReceiver`. Once in `executeWithRetry` (before the retry loop
starts), `stepExecution` checks whether the step implements this interface and injects the parent's
interceptors:

```go
// in stepExecution.executeWithRetry(), once before the retry loop
if recv, ok := ex.step.(InterceptorReceiver); ok {
    recv.PrependInterceptors(ex.w.StepInterceptors, ex.w.AttemptInterceptors)
}
```

`SubWorkflow.PrependInterceptors` prepends parent interceptors before its own, so the execution
stack for inner steps is:

```
[parent StepInterceptors] → [child StepInterceptors] → retry → [parent AttemptInterceptors] → [child AttemptInterceptors] → Before → Do → After
```

This is injected once per step execution (not per attempt) because `executeWithRetry` runs once
per step, outside the retry loop.

---

## Skipped / Canceled in StepInterceptor

Steps that are Skipped or Canceled by their `Condition` still enter the `StepInterceptor` chain.
`StepInfo.TerminalReason` carries the reason. The contract is:

- If `TerminalReason != Pending`, the interceptor **must not** call `next`.
- The interceptor should emit `EventScheduled` then `EventSkipped`/`EventCanceled` and return nil.
- The built-in `NewStepEventSink` handles this correctly.

---

## What Does Not Change

- `BeforeStep` / `AfterStep` / `Input` / `Output` — API and behavior unchanged
- `StepConfig`, `StepOption`, `RetryOption` — unchanged
- `StepStatus` — no new exported values; `scheduled` is private
- `Condition` system — unchanged
- `SubWorkflow` embedding pattern — unchanged, just gains `PrependInterceptors`
- No breaking changes to existing workflow definitions

---

## Files Affected

| File | Change |
|------|--------|
| `workflow.go` | Add `StepInterceptors`, `AttemptInterceptors` fields; simplify `tick()`; add `stepExecution` |
| `event.go` | New file: `EventType`, `WorkflowEvent`, interceptor interfaces, `NewStepEventSink`, `NewAttemptEventSink` |
| `wrap.go` | `SubWorkflow` implements `InterceptorReceiver` |

---

## Open Questions

None. All questions from the brainstorm have been resolved:

| Question | Resolution |
|----------|------------|
| EventSink vs Interceptor | Interceptor; EventSink becomes a built-in adapter |
| Per-step vs per-attempt | Both layers; different use cases |
| Skipped/Canceled visibility | Enter StepInterceptor chain via TerminalReason |
| SubWorkflow propagation | PrependInterceptors on InterceptorReceiver |
| Retrying event | Removed; individual attempt failures observable via error returned from InterceptAttempt |
| attempt counter ownership | stepExecution owns it; incremented in buildAttemptChain wrapper |
| BeforeStep/AfterStep fate | Unchanged; orthogonal to Interceptors (step-level vs workflow-level) |
| Step identifier / name | No precomputed name; Step pointer is the identifier; callers call flow.String() |
| EventType naming | All constants prefixed with `Event` for consistency |
| retry.go changes | None needed |
| Breaking changes | None |
