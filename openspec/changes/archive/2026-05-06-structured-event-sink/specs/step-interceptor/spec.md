# Step Interceptor Spec

## Overview

Two-layer interceptor system for global structured observability in go-workflow.
Registered on `Workflow`; applies to all steps automatically.

## Types

### StepInterceptor

Wraps the full lifecycle of one step execution (all retry attempts).

```go
type StepInterceptor interface {
    InterceptStep(ctx context.Context, info StepInfo, next func(context.Context) error) error
}
type StepInterceptorFunc func(ctx context.Context, info StepInfo, next func(context.Context) error) error
```

- Called once per step regardless of retry count
- `info.TerminalReason != Pending` means step is Skipped/Canceled; **must not** call `next`
- `next` calls into the retry loop → AttemptInterceptors → BeforeStep → Do → AfterStep

### AttemptInterceptor

Wraps each individual attempt (`BeforeStep → Do → AfterStep`).

```go
type AttemptInterceptor interface {
    InterceptAttempt(ctx context.Context, info AttemptInfo, next func(context.Context) error) error
}
type AttemptInterceptorFunc func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error
```

- Called once per attempt (including retried attempts)
- `info.Attempt` is 0-indexed; increments after each attempt

### StepInfo / AttemptInfo

```go
type StepInfo struct {
    Step           Steper     // canonical identifier (same pointer as Workflow map key)
    TerminalReason StepStatus // Pending = will execute; Skipped/Canceled = will not
}
type AttemptInfo struct {
    StepInfo
    Attempt uint64
}
```

Callers wanting a human-readable name call `flow.String(info.Step)`.

### EventType / WorkflowEvent

```go
type EventType string
const (
    Scheduled      EventType = "Scheduled"
    Started        EventType = "Started"
    Retrying       EventType = "Retrying"
    EventSucceeded EventType = "Succeeded"
    EventFailed    EventType = "Failed"
    EventCanceled  EventType = "Canceled"
    EventSkipped   EventType = "Skipped"
)

type WorkflowEvent struct {
    Step            Steper
    Type            EventType
    Attempt         uint64
    Err             error
    Duration        time.Duration
    BackoffDuration time.Duration // non-zero only for Retrying
}
```

`EventType` is a distinct named type from `StepStatus`. Terminal `EventType` constants are
prefixed with `Event` to avoid redeclaration conflicts with `StepStatus` constants.

### InterceptorReceiver

```go
type InterceptorReceiver interface {
    PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}
```

Steps embedding `SubWorkflow` implement this interface. `stepExecution` calls it in
`executeWithRetry` (once per step, before the retry loop) to propagate parent interceptors.

## Workflow Integration

```go
type Workflow struct {
    // ... existing fields ...
    StepInterceptors    []StepInterceptor    // [0] outermost, [len-1] innermost
    AttemptInterceptors []AttemptInterceptor // [0] outermost; BeforeStep/AfterStep always innermost
}
```

Zero-value safe: nil slices mean no interceptors; existing behaviour is unchanged.

## Built-in Adapters

```go
func NewStepEventSink(sink func(WorkflowEvent)) StepInterceptor
func NewAttemptEventSink(sink func(WorkflowEvent)) AttemptInterceptor
```

`NewStepEventSink` emits: `Scheduled` → (Retrying events via side-channel) → terminal event.
`NewAttemptEventSink` emits: `Started` per attempt.

`NewStepEventSink` also implements the package-private `retryNotifier` interface so `wireNotify`
can deliver `Retrying` events (which bypass the chain) to the sink.

## Execution Stack

```
StepInterceptor[0]
  └── StepInterceptor[1]
        └── [retry loop — Notify wired to onRetry]
              └── AttemptInterceptor[0]
                    └── AttemptInterceptor[1]
                          └── BeforeStep callbacks (from StepConfig)
                                └── step.Do(ctx)
                                      └── AfterStep callbacks
```

## Invariants

- `StepInterceptor` fires exactly once per step execution
- `AttemptInterceptor` fires exactly once per attempt
- `Retrying` event `Attempt` field matches the attempt that just failed (0-indexed)
- `SubWorkflow` parent interceptors execute outside child interceptors
- `PrependInterceptors` called once per step (in `executeWithRetry`), not per attempt
- `State.Option()` allocates a fresh `*StepOption` + `*RetryOption` each call — `wireNotify` mutations are safe and do not persist across `Reset()`+`Do()` runs
