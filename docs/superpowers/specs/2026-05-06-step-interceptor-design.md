# Step Interceptor Design

**Date:** 2026-05-06
**Status:** Draft
**Scope:** go-workflow structured observability via two-layer interceptor system

---

## Why

Currently, observability in go-workflow requires users to wire `BeforeStep`/`AfterStep` callbacks
manually on individual steps. There is no structured way to observe all steps globally — no
lifecycle events, no attempt count, no timing, no retry visibility.

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
within it multiple `Started` and `Retrying` events occur. They cannot be merged without breaking
the `Condition` system.

```
StepStatus:  Pending ──────────────────────────────► Running ──────────────► Succeeded
                                                                         └──► Failed
                                                                         └──► Canceled
             └─────────────────────────────────────────────────────────────► Skipped
             └─────────────────────────────────────────────────────────────► Canceled

EventType:              Scheduled  Started Retrying Started Retrying Started   Succeeded/Failed/Canceled
                                   [attempt 0]      [attempt 1]     [attempt 2]
```

Mapping of EventType to where it is emitted:

| EventType   | StepStatus transition          | Emitted in                          |
|-------------|-------------------------------|-------------------------------------|
| `Scheduled` | `Pending → scheduled`         | StepInterceptor entry               |
| `Started`   | status stays `Running`        | AttemptInterceptor entry            |
| `Retrying`  | status stays `Running`        | `RetryOption.Notify` side-channel → AttemptInterceptor sink |
| `Succeeded` | `Running → Succeeded`         | StepInterceptor exit                |
| `Failed`    | `Running → Failed`            | StepInterceptor exit                |
| `Canceled`  | `Running/Pending → Canceled`  | StepInterceptor exit                |
| `Skipped`   | `Pending → Skipped`           | StepInterceptor exit                |

**Ownership of events by layer:**

- `StepInterceptor` sees only: `Scheduled` + one terminal (`Succeeded`/`Failed`/`Canceled`/`Skipped`).
  It is not aware of how many retries occurred.
- `AttemptInterceptor` sees: `Started` (per attempt) + `Retrying` (per failed attempt, via side-channel).
  It owns the full picture of attempt-level activity.

`Failed` is **only** a terminal event. It is never emitted for a single failed attempt inside a
retry loop — that is covered by `Retrying`, which belongs to the attempt layer.

---

## Architecture

### Two-Layer Interceptor Stack

```
StepInterceptor[0]
  └── StepInterceptor[1]
        └── [retry loop — Notify wired here]
              └── AttemptInterceptor[0]
                    └── AttemptInterceptor[1]
                          └── [per-step BeforeStep callbacks]   ← from StepConfig
                                └── step.Do(ctx)
                                      └── [per-step AfterStep callbacks]
```

**StepInterceptor** wraps the entire lifecycle of a step including all retry attempts. It sees
the step exactly once: entry on `Scheduled`, exit on terminal status. It has no visibility into
individual retry attempts — it does not receive `Retrying` or `Started` events. It is the right
place for OTel spans (one span per step, not per attempt) and step-level metrics.

**AttemptInterceptor** wraps each individual attempt (`Before → Do → After`). It sees every
attempt, including retried ones. It also receives `Retrying` events (via the `retryNotifier`
side-channel) so it has the complete picture of attempt-level activity: when each attempt starts
(`Started`), why it failed and how long to wait (`Retrying`), and the final outcome (via the
enclosing `StepInterceptor`). It is the right place for per-attempt logging, attempt-level
tracing, and retry observability.

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
    w        *Workflow
    step     Steper
    state    *State
    attempt  uint64        // single source of truth for attempt count
    onRetry  func(WorkflowEvent) // assembled during chain build
}
```

`attempt` is the single source of truth shared between `AttemptInfo` and `RetryOption.Notify`.
It is incremented inside `wireNotify` after each failed attempt, before `Retrying` is emitted.

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
// Step is the canonical identifier — it is the same pointer used as the map key
// in Workflow, stable for the lifetime of the workflow definition.
// Callers that need a human-readable name can call flow.String(info.Step).
// No name is precomputed by the framework; different sinks may have different
// naming preferences (short name, fully-qualified type, etc.).
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
    Scheduled EventType = "Scheduled"
    Started   EventType = "Started"
    Retrying  EventType = "Retrying"
    Succeeded EventType = "Succeeded"
    Failed    EventType = "Failed"
    Canceled  EventType = "Canceled"
    Skipped   EventType = "Skipped"
)

// WorkflowEvent carries information about a step lifecycle event.
type WorkflowEvent struct {
    Step            Steper
    Type            EventType
    Attempt         uint64
    Err             error
    Duration        time.Duration
    BackoffDuration time.Duration // non-zero only for Retrying
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
// NewStepEventSink returns a StepInterceptor that emits Scheduled and a terminal
// event (Succeeded/Failed/Canceled/Skipped) for every step.
// It is not aware of individual retry attempts.
func NewStepEventSink(sink func(WorkflowEvent)) StepInterceptor

// NewAttemptEventSink returns an AttemptInterceptor that emits Started events per
// attempt and Retrying events after each failed attempt (before backoff).
// The returned value also implements a package-private retryNotifier interface
// so that stepExecution can deliver Retrying events (which bypass the chain) to sink.
// This implementation detail is not visible to callers.
func NewAttemptEventSink(sink func(WorkflowEvent)) AttemptInterceptor
```

Usage examples:

```go
// Step-level only (no retry detail)
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

// Full observability: step-level spans + attempt-level detail (Started + Retrying)
w := &flow.Workflow{
    StepInterceptors:    []flow.StepInterceptor{myOtelStepInterceptor},
    AttemptInterceptors: []flow.AttemptInterceptor{flow.NewAttemptEventSink(mySink)},
}

// Fan-out: multiple sinks via closure
sink := flow.NewAttemptEventSink(func(e flow.WorkflowEvent) {
    promSink(e)
    slogSink(e)
})
w := &flow.Workflow{
    StepInterceptors:    []flow.StepInterceptor{flow.NewStepEventSink(mySink)},
    AttemptInterceptors: []flow.AttemptInterceptor{sink},
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

## Retrying Event: Why It Bypasses the Interceptor Chain

`Retrying` fires inside `backoff.RetryNotifyWithTimer`'s Notify callback, which sits between two
consecutive `next()` calls. At that point the interceptor chain's call stack has unwound (the
previous `next()` returned an error) and the next `next()` hasn't been called yet. There is no
natural place to insert it into the chain.

The solution: `stepExecution.wireNotify()` wraps `RetryOption.Notify` and calls `ex.onRetry`
directly. `ex.onRetry` is assembled during chain construction by collecting the package-private
`retryNotifier` interface from any interceptor in `AttemptInterceptors` that implements it. The
concrete type returned by `NewAttemptEventSink` implements this interface; custom interceptors do
not need to.

```
attempt N fails → buildAttemptChain wrapper: ex.attempt++
               → backoff.Notify fires → ex.onRetry(Retrying{attempt=N})
               → AttemptInterceptor sink receives Retrying
```

This keeps `Retrying` semantically co-located with `Started` — both belong to the attempt layer
and reach the same sink.

---

## Skipped / Canceled in StepInterceptor

Steps that are Skipped or Canceled by their `Condition` still enter the `StepInterceptor` chain.
`StepInfo.TerminalReason` carries the reason. The contract is:

- If `TerminalReason != Pending`, the interceptor **must not** call `next`.
- The interceptor should emit `Scheduled` then `Skipped`/`Canceled` and return nil.
- The built-in `NewStepEventSink` handles this correctly.

Custom interceptors that call `next` when `TerminalReason != Pending` will cause a panic (the
`next` function asserts this precondition).

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
| `step.go` | Add interceptor interfaces, info types, `InterceptorReceiver` |
| `event.go` | New file: `EventType`, `WorkflowEvent`, `NewStepEventSink`, `NewAttemptEventSink` |
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
| Retrying event ownership | Belongs to AttemptInterceptor layer (not StepInterceptor); delivered via wireNotify + retryNotifier side-channel |
| attempt counter ownership | stepExecution owns it; single source of truth; incremented in buildAttemptChain wrapper |
| BeforeStep/AfterStep fate | Unchanged; orthogonal to Interceptors (step-level vs workflow-level) |
| Step identifier / name | No precomputed name; Step pointer is the identifier; callers call flow.String() |
| NewAttemptEventSink return type | Returns AttemptInterceptor (interface); retryNotifier is package-private |
| NewStepEventSink return type | Returns StepInterceptor (interface); does not implement retryNotifier |
| retry.go changes | None needed; stepExecution.attempt is independent |
| Breaking changes | None |
