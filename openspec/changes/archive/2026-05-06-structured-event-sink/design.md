# Step Interceptor Design

## Summary

The original proposal called for a simple `EventSink func(WorkflowEvent)` field on `Workflow`.
During design exploration, this evolved into a two-layer interceptor system that is strictly more
powerful: EventSink becomes a built-in adapter on top of the interceptor API.

The key insight from studying Temporal's Go SDK: a global observability hook is most useful when
it wraps the full execution lifecycle (like a `WorkerInterceptor`), not just fires events. This
allows users to implement OTel spans, Prometheus histograms, and structured logging with a single
consistent API.

## Design Decisions

### Interceptor vs EventSink

`StepInterceptor` and `AttemptInterceptor` replace the proposed `EventSink func(WorkflowEvent)`.
`NewStepEventSink` and `NewAttemptEventSink` are built-in adapters that implement these interfaces
and emit `WorkflowEvent`s — users who only want structured events use these adapters and never
interact with the interceptor interfaces directly.

### Two Layers

- **`StepInterceptor`**: wraps the full step lifecycle (all retry attempts). One invocation per step.
  Right place for OTel spans, step-level metrics.
- **`AttemptInterceptor`**: wraps each individual attempt (`Before → Do → After`). Right place
  for per-attempt logging, attempt-level tracing.

### BeforeStep/AfterStep are orthogonal

Interceptors are workflow-level; `BeforeStep`/`AfterStep` are step-level (per-step `StepConfig`).
They execute in different layers of the stack and are configured independently. No changes to the
existing `BeforeStep`/`AfterStep` API.

### StepStatus vs EventType

These are deliberately separate types:
- `StepStatus` is the orchestration engine's state machine, used by `Condition` evaluation.
- `EventType` is an observation stream for external consumers. `Running` has no `EventType`
  equivalent — within it, multiple `Started` and `Retrying` events fire.

### Retrying event delivery

`Retrying` fires inside `backoff.RetryNotifyWithTimer`'s Notify callback — between two consecutive
`next()` calls, outside the interceptor chain. It is delivered via `wireNotify`, a side-channel
that assembles `ex.onRetry` from interceptors implementing the package-private `retryNotifier`
interface. The concrete type returned by `NewStepEventSink` implements this interface.

### SubWorkflow propagation

`SubWorkflow` implements `InterceptorReceiver`. `stepExecution` calls `PrependInterceptors` in
`executeWithRetry` (once per step, not per attempt) so parent interceptors wrap child interceptors.

### stepExecution refactor

The anonymous goroutine in `tick()` is extracted into a `stepExecution` struct. `tick()` becomes
a single-responsibility function: atomically claim a step with a private `scheduled` sentinel.
All lifecycle logic (condition evaluation, interceptor chain assembly, retry, event delivery)
moves into `stepExecution.run()`.

## API Surface

```go
// New on Workflow struct
StepInterceptors    []StepInterceptor
AttemptInterceptors []AttemptInterceptor

// New interfaces
type StepInterceptor interface {
    InterceptStep(ctx context.Context, info StepInfo, next func(context.Context) error) error
}
type AttemptInterceptor interface {
    InterceptAttempt(ctx context.Context, info AttemptInfo, next func(context.Context) error) error
}

// Function adapters
type StepInterceptorFunc  func(ctx context.Context, info StepInfo,    next func(context.Context) error) error
type AttemptInterceptorFunc func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error

// Info types
type StepInfo struct {
    Step           Steper
    TerminalReason StepStatus // Pending = will execute normally
}
type AttemptInfo struct {
    StepInfo
    Attempt uint64
}

// Event types
type EventType    string  // Scheduled / Started / Retrying / EventSucceeded / EventFailed / EventCanceled / EventSkipped
type WorkflowEvent struct { Step Steper; Type EventType; Attempt uint64; Err error; Duration, BackoffDuration time.Duration }

// Built-in adapters
func NewStepEventSink(sink func(WorkflowEvent)) StepInterceptor
func NewAttemptEventSink(sink func(WorkflowEvent)) AttemptInterceptor

// SubWorkflow propagation
type InterceptorReceiver interface {
    PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}
```

## No Breaking Changes

All existing APIs (`BeforeStep`, `AfterStep`, `Condition`, `RetryOption`, `SubWorkflow` embedding)
are unchanged. The new fields on `Workflow` are zero-value safe — workflows without interceptors
behave identically to before.
