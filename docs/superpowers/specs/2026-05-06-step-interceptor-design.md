# Step Interceptor Design

**Date:** 2026-05-06
**Status:** Draft
**Scope:** go-workflow structured observability via two-layer interceptor system

---

## Why

Currently, observability in go-workflow requires users to wire `BeforeStep`/`AfterStep` callbacks
manually on individual steps. There is no structured way to observe all steps globally — no
lifecycle hooks, no attempt count, no timing.

In production, you need to answer: which step is running right now? How many retries has step X
done? How long did step Y take? None of these are answerable today without bespoke instrumentation.

This design introduces a two-layer interceptor system that:
- Provides global, structured observability across all steps
- Is orthogonal to `BeforeStep`/`AfterStep` — they serve different scopes and both are preserved
- Propagates automatically into nested `SubWorkflow`s

---

## Concepts

### StepStatus vs the interceptor layers

**`StepStatus`** is the state machine used by the orchestration engine. It is persistent and
queryable. The `Condition` system reads it to decide whether to run downstream steps.

The interceptors are a separate, orthogonal observability mechanism. They do not replace or alter
`StepStatus` — they wrap execution to give users structured hooks.

The key difference: `Running` is a single `StepStatus` that spans the entire retry loop. Within
it, `AttemptInterceptor` fires multiple times (once per attempt). These cannot be merged without
breaking the `Condition` system.

```
StepStatus:  Pending ──────────────────────────────► Running ──────────────► Succeeded
                                                                         └──► Failed
                                                                         └──► Canceled
             └─────────────────────────────────────────────────────────────► Skipped
             └─────────────────────────────────────────────────────────────► Canceled

Interceptors:         StepInterceptor.entry
                                       AttemptInterceptor[attempt=0]
                                                          AttemptInterceptor[attempt=1]
                                                                             AttemptInterceptor[attempt=2]
                      StepInterceptor.exit (err=nil → Succeeded)
```

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

**StepInterceptor** wraps the entire lifecycle of a step including all retry attempts. It is
called exactly once per step: on entry `info.TerminalReason` tells you whether the step will
execute (`Pending`) or has already been determined terminal (`Skipped`/`Canceled`). On exit the
returned error reflects the final outcome. Right place for OTel spans (one span per step) and
step-level metrics.

**AttemptInterceptor** wraps each individual attempt (`Before → Do → After`). It fires once per
attempt, including retried ones. The error returned by `next` is the attempt's failure (if any)
— the interceptor can inspect it before returning. Right place for per-attempt logging and
attempt-level tracing.

**BeforeStep/AfterStep** (existing) are step-level callbacks configured per-step via `StepConfig`.
Interceptors are workflow-level and apply globally. They are orthogonal — interceptors execute on
the outside, BeforeStep/AfterStep execute on the inside.

### stepExecution (internal)

The current anonymous goroutine in `tick()` is replaced by a `stepExecution` struct:

```go
type stepExecution struct {
    w       *Workflow
    step    Steper
    state   *State
    attempt uint64  // single source of truth; incremented in buildAttemptChain wrapper
}
```

### tick() simplification

`tick()` is reduced to atomically claiming a step (private `scheduled` sentinel) to prevent
double-spawning. All other logic moves into `stepExecution.run()`.

---

## API

### New Types

```go
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

// StepInterceptor intercepts the full lifecycle of a step (all retry attempts).
// If info.TerminalReason != Pending, next must not be called — the step will not execute.
// Return nil in that case after observing the event.
type StepInterceptor interface {
    InterceptStep(ctx context.Context, info StepInfo, next func(context.Context) error) error
}

// AttemptInterceptor intercepts each individual attempt (Before → Do → After).
// The error returned by next (if any) is the attempt's failure.
type AttemptInterceptor interface {
    InterceptAttempt(ctx context.Context, info AttemptInfo, next func(context.Context) error) error
}

// StepInterceptorFunc is a function adapter for StepInterceptor.
type StepInterceptorFunc func(ctx context.Context, info StepInfo, next func(context.Context) error) error

// AttemptInterceptorFunc is a function adapter for AttemptInterceptor.
type AttemptInterceptorFunc func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error

// InterceptorReceiver is implemented by steps that contain a sub-workflow.
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
    AttemptInterceptors []AttemptInterceptor
}
```

### Usage examples

```go
// OTel: one span per step
w := &flow.Workflow{
    StepInterceptors: []flow.StepInterceptor{
        flow.StepInterceptorFunc(func(ctx context.Context, info flow.StepInfo, next func(context.Context) error) error {
            ctx, span := tracer.Start(ctx, flow.String(info.Step))
            defer span.End()
            if info.TerminalReason != flow.Pending {
                return nil // step will not execute
            }
            err := next(ctx)
            if err != nil {
                span.RecordError(err)
            }
            return err
        }),
    },
}

// Per-attempt logging with attempt number and error
w := &flow.Workflow{
    AttemptInterceptors: []flow.AttemptInterceptor{
        flow.AttemptInterceptorFunc(func(ctx context.Context, info flow.AttemptInfo, next func(context.Context) error) error {
            err := next(ctx)
            slog.Info("attempt", "step", flow.String(info.Step), "attempt", info.Attempt, "err", err)
            return err
        }),
    },
}
```

---

## SubWorkflow Propagation

`SubWorkflow` implements `InterceptorReceiver`. Once in `executeWithRetry` (before the retry loop),
`stepExecution` injects the parent's interceptors into the child workflow:

```go
if recv, ok := ex.step.(InterceptorReceiver); ok {
    recv.PrependInterceptors(ex.w.StepInterceptors, ex.w.AttemptInterceptors)
}
```

`PrependInterceptors` uses `make`+`copy` to build fresh slices, so parent interceptors are
prepended without aliasing the parent's backing array and without accumulating across `Reset()`
cycles.

Execution stack for inner steps:

```
[parent StepInterceptors] → [child StepInterceptors] → retry → [parent AttemptInterceptors] → [child AttemptInterceptors] → Before → Do → After
```

---

## Skipped / Canceled in StepInterceptor

Steps that are Skipped or Canceled by their `Condition` still enter the `StepInterceptor` chain.
`StepInfo.TerminalReason` carries the reason. The contract is:

- If `TerminalReason != Pending`, the interceptor **must not** call `next`.
- Return nil after observing the terminal reason.

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
| `interceptor.go` | New file: interceptor interfaces, info types, func adapters, `InterceptorReceiver` |
| `wrap.go` | `SubWorkflow` implements `InterceptorReceiver` |

---

## Open Questions

None. All questions from the brainstorm have been resolved:

| Question | Resolution |
|----------|------------|
| EventSink vs Interceptor | Pure interceptor; no built-in EventSink adapter — users bring their own event types |
| Per-step vs per-attempt | Both layers; different use cases |
| Skipped/Canceled visibility | Enter StepInterceptor chain via TerminalReason |
| SubWorkflow propagation | PrependInterceptors on InterceptorReceiver; once per step, make+copy |
| Retrying / BackoffDuration event | Removed; not worth the side-channel complexity; failure error available from InterceptAttempt |
| attempt counter ownership | stepExecution owns it; incremented in buildAttemptChain wrapper |
| BeforeStep/AfterStep fate | Unchanged; orthogonal to Interceptors |
| Step identifier / name | No precomputed name; Step pointer is the identifier; callers call flow.String() |
| EventType / WorkflowEvent | Removed; users define their own event types |
| Breaking changes | None |
