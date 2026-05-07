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

`tick()` evaluates each runnable Step's `Condition` inline:

- If the Condition returns a terminal status (`Skipped` / `Canceled`), the Step's
  `StepResult` is set directly and execution moves on. No goroutine is spawned, no
  `MaxConcurrency` lease is consumed, no interceptor runs.
- Otherwise, `tick()` takes a lease, sets the status to `Running`, and spawns a worker
  that runs the interceptor chain.

Because the worker's status flip to `Running` happens under `statusChange.L` *before* the
goroutine is spawned, a subsequent `tick()` cannot see the Step as `Pending` and double-
spawn it. No `scheduled` sentinel is needed, and `StateOf(step).GetStatus()` only ever
returns documented public `StepStatus` values.

When a Step is settled inline, `tick()` re-iterates within the same call so newly-
unblocked downstream Steps are picked up immediately (no signal would otherwise wake the
main loop).

---

## API

### New Types

```go
// StepInterceptor intercepts the full lifecycle of a step (all retry attempts).
// Skipped and Canceled steps do not enter the interceptor chain.
type StepInterceptor interface {
    InterceptStep(ctx context.Context, step Steper, next func(context.Context) error) error
}

// AttemptInterceptor intercepts each individual attempt (Before → Do → After).
// The error returned by next (if any) is the attempt's failure.
type AttemptInterceptor interface {
    InterceptAttempt(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error
}

// StepInterceptorFunc is a function adapter for StepInterceptor.
type StepInterceptorFunc func(ctx context.Context, step Steper, next func(context.Context) error) error

// AttemptInterceptorFunc is a function adapter for AttemptInterceptor.
type AttemptInterceptorFunc func(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error

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

    // IsolateInterceptors disables inheriting interceptors from a parent workflow.
    // When true, PrependInterceptors is a no-op for this workflow.
    IsolateInterceptors bool
}
```

### Usage examples

```go
// OTel: one span per step
w := &flow.Workflow{
    StepInterceptors: []flow.StepInterceptor{
        flow.StepInterceptorFunc(func(ctx context.Context, step flow.Steper, next func(context.Context) error) error {
            ctx, span := tracer.Start(ctx, flow.String(step))
            defer span.End()
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
        flow.AttemptInterceptorFunc(func(ctx context.Context, step flow.Steper, attempt uint64, next func(context.Context) error) error {
            err := next(ctx)
            slog.Info("attempt", "step", flow.String(step), "attempt", attempt, "err", err)
            return err
        }),
    },
}
```

---

## SubWorkflow Propagation

`Workflow` itself implements `InterceptorReceiver` via `PrependInterceptors`, so any nested
workflow — whether embedded via `SubWorkflow` or used directly as a step — inherits its parent's
interceptors. `SubWorkflow.PrependInterceptors` simply delegates to the inner `Workflow`.

Once in `executeWithRetry` (before the retry loop), `stepExecution` injects the parent's
interceptors into the child:

```go
if recv, ok := ex.step.(InterceptorReceiver); ok {
    recv.PrependInterceptors(ex.w.StepInterceptors, ex.w.AttemptInterceptors)
}
```

`PrependInterceptors` uses `make`+`copy` to build fresh slices, so parent interceptors are
prepended without aliasing the parent's backing array and without accumulating across `Reset()`
cycles.

### Opting out: `IsolateInterceptors`

Set `Workflow.IsolateInterceptors = true` on a child to disable inheritance. `PrependInterceptors`
becomes a no-op and the child runs with only its own interceptor stack. Useful when the child
defines a self-contained observability pipeline (e.g., its own tracer / event sink) that must
not be wrapped by the parent.

Execution stack for inner steps (default, inheritance enabled):

```
[parent StepInterceptors] → [child StepInterceptors] → retry → [parent AttemptInterceptors] → [child AttemptInterceptors] → Before → Do → After
```

With `IsolateInterceptors = true`:

```
[child StepInterceptors] → retry → [child AttemptInterceptors] → Before → Do → After
```

---

## Skipped / Canceled steps

Steps that are Skipped or Canceled by their `Condition` do **not** enter the interceptor chain.
Their final status is set directly and the interceptors are never invoked. Post-run status is
queryable via `workflow.StateOf(step).GetStatus()`.

---

## What Does Not Change

- `BeforeStep` / `AfterStep` / `Input` / `Output` — API and behavior unchanged
- `StepConfig`, `StepOption`, `RetryOption` — unchanged
- `StepStatus` — no new values; only documented public statuses are ever observable
- `Condition` system — unchanged
- `SubWorkflow` embedding pattern — unchanged, just gains `PrependInterceptors`
- No breaking changes to existing workflow definitions

---

## Files Affected

| File | Change |
|------|--------|
| `workflow.go` | Add `StepInterceptors`, `AttemptInterceptors` fields; simplify `tick()`; add `stepExecution` |
| `interceptor.go` | New file: interceptor interfaces, info types, func adapters, `InterceptorReceiver` |
| `wrap.go` | `SubWorkflow.PrependInterceptors` delegates to embedded `Workflow.PrependInterceptors` |

---

## Open Questions

None. All questions from the brainstorm have been resolved:

| Question | Resolution |
|----------|------------|
| EventSink vs Interceptor | Pure interceptor; no built-in EventSink adapter — users bring their own event types |
| Per-step vs per-attempt | Both layers; different use cases |
| Skipped/Canceled visibility | Skipped/Canceled steps bypass interceptor chain entirely; query post-run via StateOf |
| StepInfo / AttemptInfo wrappers | Removed; step passed as Steper directly; attempt as uint64 directly |
| SubWorkflow propagation | PrependInterceptors on InterceptorReceiver; once per step, make+copy |
| Retrying / BackoffDuration event | Removed; not worth the side-channel complexity; failure error available from InterceptAttempt |
| attempt counter ownership | stepExecution owns it; incremented in buildAttemptChain wrapper |
| BeforeStep/AfterStep fate | Unchanged; orthogonal to Interceptors |
| Step identifier / name | No precomputed name; Step pointer is the identifier; callers call flow.String() |
| EventType / WorkflowEvent | Removed; users define their own event types |
| Breaking changes | None |
