# Step Interceptor Implementation Plan

> ⚠️ **OUTDATED — DO NOT USE AS A REFERENCE FOR THE SHIPPED IMPLEMENTATION.**
>
> This plan was written before the design was simplified. The shipped PR drops the
> `EventSink` / `WorkflowEvent` / `EventType` vocabulary entirely (users plug their
> own event types into the interceptors), removes the `StepInfo` / `AttemptInfo`
> wrappers (interceptors receive `Steper` and `uint64` directly), removes
> `TerminalReason` (Skipped/Canceled steps bypass the interceptor chain entirely),
> and removes the `scheduled` `StepStatus` sentinel (`tick()` evaluates Condition
> inline and sets `Running` directly). Files referenced here that do not exist in
> the final tree (`event.go`, `event_test.go`) were never created.
>
> The current design and rationale live in
> [`docs/superpowers/specs/2026-05-06-step-interceptor-design.md`](../specs/2026-05-06-step-interceptor-design.md).
> The synced main spec lives in
> [`openspec/specs/step-interceptor/spec.md`](../../../openspec/specs/step-interceptor/spec.md).
> This plan is kept only as a record of the original direction.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a two-layer interceptor system (`StepInterceptor` + `AttemptInterceptor`) to go-workflow, enabling structured global observability with built-in `EventSink` adapters.

**Architecture:** Introduce `event.go` for public types, refactor `workflow.go` to extract `stepExecution` (replacing the anonymous goroutine in `tick()`), and add `InterceptorReceiver` to `SubWorkflow` for nested propagation. `BeforeStep`/`AfterStep` remain unchanged as step-level configuration; interceptors are workflow-level and orthogonal.

**Tech Stack:** Go 1.23, `github.com/stretchr/testify`, `github.com/benbjohnson/clock`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `event.go` | **Create** | `EventType`, `WorkflowEvent`, `StepInterceptor`, `AttemptInterceptor`, `StepInterceptorFunc`, `AttemptInterceptorFunc`, `StepInfo`, `AttemptInfo`, `InterceptorReceiver`, `NewStepEventSink`, `NewAttemptEventSink`, private `retryNotifier` |
| `event_test.go` | **Create** | Tests for `NewStepEventSink` and `NewAttemptEventSink` |
| `workflow.go` | **Modify** | Add `StepInterceptors`/`AttemptInterceptors` fields; introduce `stepExecution`; simplify `tick()`; add `wireNotify` |
| `workflow_test.go` | **Modify** | Integration tests for interceptor ordering, SubWorkflow propagation, Retrying events |
| `wrap.go` | **Modify** | `SubWorkflow` implements `InterceptorReceiver` |
| `wrap_test.go` | **Modify** | Tests for interceptor propagation through SubWorkflow |

---

## Task 1: Define public types in `event.go`

**Files:**
- Create: `event.go`
- Create: `event_test.go`

- [ ] **Step 1: Write the failing test**

```go
// event_test.go
package flow

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestEventTypeConstants(t *testing.T) {
    // Verify all constants exist and are distinct
    types := []EventType{Scheduled, Started, Retrying, Succeeded, Failed, Canceled, Skipped}
    seen := map[EventType]bool{}
    for _, et := range types {
        assert.False(t, seen[et], "duplicate EventType: %q", et)
        seen[et] = true
    }
}

func TestStepInterceptorFunc(t *testing.T) {
    called := false
    var ic StepInterceptor = StepInterceptorFunc(func(ctx context.Context, info StepInfo, next func(context.Context) error) error {
        called = true
        return next(ctx)
    })
    _ = ic.InterceptStep(context.Background(), StepInfo{}, func(ctx context.Context) error { return nil })
    assert.True(t, called)
}

func TestAttemptInterceptorFunc(t *testing.T) {
    called := false
    var ic AttemptInterceptor = AttemptInterceptorFunc(func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
        called = true
        return next(ctx)
    })
    _ = ic.InterceptAttempt(context.Background(), AttemptInfo{}, func(ctx context.Context) error { return nil })
    assert.True(t, called)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./... -run "TestEventType|TestStepInterceptorFunc|TestAttemptInterceptorFunc" -v
```

Expected: FAIL — types not defined.

- [ ] **Step 3: Write `event.go`**

```go
package flow

import (
    "context"
    "time"
)

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

// StepInfo is passed to StepInterceptor.
// Step is the canonical identifier — same pointer used as map key in Workflow.
// Callers that need a human-readable name can call flow.String(info.Step).
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
type AttemptInterceptor interface {
    InterceptAttempt(ctx context.Context, info AttemptInfo, next func(context.Context) error) error
}

// StepInterceptorFunc is a function adapter for StepInterceptor.
type StepInterceptorFunc func(ctx context.Context, info StepInfo, next func(context.Context) error) error

func (f StepInterceptorFunc) InterceptStep(ctx context.Context, info StepInfo, next func(context.Context) error) error {
    return f(ctx, info, next)
}

// AttemptInterceptorFunc is a function adapter for AttemptInterceptor.
type AttemptInterceptorFunc func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error

func (f AttemptInterceptorFunc) InterceptAttempt(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
    return f(ctx, info, next)
}

// InterceptorReceiver is implemented by steps that contain a sub-workflow.
// stepExecution calls PrependInterceptors before each attempt so that
// parent interceptors wrap child interceptors.
type InterceptorReceiver interface {
    PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}

// retryNotifier is a package-private interface implemented by the concrete
// type returned by NewStepEventSink. stepExecution uses it to deliver
// Retrying events (which bypass the interceptor chain) to the sink.
type retryNotifier interface {
    onRetry(WorkflowEvent)
}
```

Note: `event.go` also needs `"time"` in imports — add it.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./... -run "TestEventType|TestStepInterceptorFunc|TestAttemptInterceptorFunc" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add event.go event_test.go
git commit -m "feat: add interceptor public types and EventType constants"
```

---

## Task 2: Built-in `NewStepEventSink` and `NewAttemptEventSink`

**Files:**
- Modify: `event.go`
- Modify: `event_test.go`

- [ ] **Step 1: Write failing tests**

```go
// event_test.go — add these tests

func TestNewStepEventSink_SucceededStep(t *testing.T) {
    var events []WorkflowEvent
    sink := NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) })

    step := NoOp("a")
    info := StepInfo{Step: step, TerminalReason: Pending}
    err := sink.InterceptStep(context.Background(), info, func(ctx context.Context) error {
        return nil
    })

    assert.NoError(t, err)
    assert.Len(t, events, 2)
    assert.Equal(t, Scheduled, events[0].Type)
    assert.Equal(t, step, events[0].Step)
    assert.Equal(t, Succeeded, events[1].Type)
    assert.NotZero(t, events[1].Duration)
}

func TestNewStepEventSink_FailedStep(t *testing.T) {
    var events []WorkflowEvent
    sink := NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) })

    step := NoOp("a")
    boom := errors.New("boom")
    info := StepInfo{Step: step, TerminalReason: Pending}
    err := sink.InterceptStep(context.Background(), info, func(ctx context.Context) error {
        return boom
    })

    assert.Equal(t, boom, err)
    assert.Len(t, events, 2)
    assert.Equal(t, Scheduled, events[0].Type)
    assert.Equal(t, Failed, events[1].Type)
    assert.Equal(t, boom, events[1].Err)
}

func TestNewStepEventSink_SkippedStep(t *testing.T) {
    var events []WorkflowEvent
    sink := NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) })

    step := NoOp("a")
    info := StepInfo{Step: step, TerminalReason: Skipped}
    nextCalled := false
    err := sink.InterceptStep(context.Background(), info, func(ctx context.Context) error {
        nextCalled = true
        return nil
    })

    assert.NoError(t, err)
    assert.False(t, nextCalled, "next must not be called for Skipped")
    assert.Len(t, events, 2)
    assert.Equal(t, Scheduled, events[0].Type)
    assert.Equal(t, Skipped, events[1].Type)
}

func TestNewStepEventSink_OnRetry(t *testing.T) {
    var events []WorkflowEvent
    sink := NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) })

    rn, ok := sink.(retryNotifier)
    assert.True(t, ok, "NewStepEventSink should implement retryNotifier")

    boom := errors.New("boom")
    rn.onRetry(WorkflowEvent{Type: Retrying, Attempt: 0, Err: boom, BackoffDuration: time.Second})

    assert.Len(t, events, 1)
    assert.Equal(t, Retrying, events[0].Type)
    assert.Equal(t, boom, events[0].Err)
}

func TestNewAttemptEventSink_EmitsStarted(t *testing.T) {
    var events []WorkflowEvent
    sink := NewAttemptEventSink(func(e WorkflowEvent) { events = append(events, e) })

    step := NoOp("a")
    info := AttemptInfo{StepInfo: StepInfo{Step: step}, Attempt: 2}
    err := sink.InterceptAttempt(context.Background(), info, func(ctx context.Context) error {
        return nil
    })

    assert.NoError(t, err)
    assert.Len(t, events, 1)
    assert.Equal(t, Started, events[0].Type)
    assert.Equal(t, uint64(2), events[0].Attempt)
    assert.Equal(t, step, events[0].Step)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./... -run "TestNewStepEventSink|TestNewAttemptEventSink" -v
```

Expected: FAIL — functions not defined.

- [ ] **Step 3: Implement `NewStepEventSink` and `NewAttemptEventSink` in `event.go`**

```go
// Add to event.go:

// terminalEventType maps an error to the corresponding terminal EventType.
func terminalEventType(err error) EventType {
    if err == nil {
        return Succeeded
    }
    switch StatusFromError(err) {
    case Canceled:
        return Canceled
    case Skipped:
        return Skipped
    default:
        return Failed
    }
}

// stepEventSink is the concrete type returned by NewStepEventSink.
// It implements both StepInterceptor and the package-private retryNotifier.
type stepEventSink struct {
    sink func(WorkflowEvent)
}

// NewStepEventSink returns a StepInterceptor that emits Scheduled, then a terminal
// event (Succeeded/Failed/Canceled/Skipped) for every step. It also receives
// Retrying events via the package-private retryNotifier interface.
func NewStepEventSink(sink func(WorkflowEvent)) StepInterceptor {
    return &stepEventSink{sink: sink}
}

func (s *stepEventSink) InterceptStep(ctx context.Context, info StepInfo, next func(context.Context) error) error {
    s.sink(WorkflowEvent{Step: info.Step, Type: Scheduled})

    if info.TerminalReason != Pending {
        s.sink(WorkflowEvent{Step: info.Step, Type: EventType(info.TerminalReason)})
        return nil
    }

    start := time.Now()
    err := next(ctx)
    s.sink(WorkflowEvent{
        Step:     info.Step,
        Type:     terminalEventType(err),
        Err:      err,
        Duration: time.Since(start),
    })
    return err
}

func (s *stepEventSink) onRetry(e WorkflowEvent) { s.sink(e) }

// NewAttemptEventSink returns an AttemptInterceptor that emits a Started event
// for each attempt.
func NewAttemptEventSink(sink func(WorkflowEvent)) AttemptInterceptor {
    return AttemptInterceptorFunc(func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
        sink(WorkflowEvent{
            Step:    info.Step,
            Type:    Started,
            Attempt: info.Attempt,
        })
        return next(ctx)
    })
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./... -run "TestNewStepEventSink|TestNewAttemptEventSink" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add event.go event_test.go
git commit -m "feat: add NewStepEventSink and NewAttemptEventSink"
```

---

## Task 3: Introduce `stepExecution` and refactor `tick()`

This is the largest refactor. We replace the anonymous goroutine in `tick()` with a
`stepExecution` struct. `makeDoForStep` is deleted; its logic moves into
`stepExecution.runAttempt`.

**Files:**
- Modify: `workflow.go`
- Modify: `workflow_test.go`

- [ ] **Step 1: Write failing tests for `stepExecution` behavior**

```go
// workflow_test.go — add these tests

func TestStepExecution_BasicSuccess(t *testing.T) {
    t.Parallel()
    var events []WorkflowEvent
    step := NoOp("a")
    w := &Workflow{
        StepInterceptors: []StepInterceptor{
            NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) }),
        },
    }
    w.Add(Step(step))
    err := w.Do(context.Background())
    assert.NoError(t, err)
    assert.Equal(t, []EventType{Scheduled, Succeeded}, eventTypes(events))
}

func TestStepExecution_StepInterceptorOrder(t *testing.T) {
    t.Parallel()
    var order []string
    makeIC := func(name string) StepInterceptor {
        return StepInterceptorFunc(func(ctx context.Context, info StepInfo, next func(context.Context) error) error {
            order = append(order, name+":before")
            err := next(ctx)
            order = append(order, name+":after")
            return err
        })
    }
    w := &Workflow{
        StepInterceptors: []StepInterceptor{makeIC("A"), makeIC("B")},
    }
    w.Add(Step(NoOp("s")))
    assert.NoError(t, w.Do(context.Background()))
    assert.Equal(t, []string{"A:before", "B:before", "B:after", "A:after"}, order)
}

func TestStepExecution_AttemptInterceptorOrder(t *testing.T) {
    t.Parallel()
    var order []string
    makeIC := func(name string) AttemptInterceptor {
        return AttemptInterceptorFunc(func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
            order = append(order, name+":before")
            err := next(ctx)
            order = append(order, name+":after")
            return err
        })
    }
    w := &Workflow{
        AttemptInterceptors: []AttemptInterceptor{makeIC("X"), makeIC("Y")},
    }
    w.Add(Step(NoOp("s")))
    assert.NoError(t, w.Do(context.Background()))
    assert.Equal(t, []string{"X:before", "Y:before", "Y:after", "X:after"}, order)
}

func TestStepExecution_SkippedStep(t *testing.T) {
    t.Parallel()
    var events []WorkflowEvent
    step := NoOp("a")
    w := &Workflow{
        StepInterceptors: []StepInterceptor{
            NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) }),
        },
    }
    w.Add(Step(step).When(func(_ context.Context, _ map[Steper]StepResult) StepStatus {
        return Skipped
    }))
    assert.NoError(t, w.Do(context.Background()))
    assert.Equal(t, []EventType{Scheduled, Skipped}, eventTypes(events))
}

func TestStepExecution_RetryingEvent(t *testing.T) {
    t.Parallel()
    var events []WorkflowEvent
    boom := errors.New("boom")
    attempts := 0
    step := Func("s", func(ctx context.Context) error {
        attempts++
        if attempts < 3 {
            return boom
        }
        return nil
    })
    w := &Workflow{
        StepInterceptors: []StepInterceptor{
            NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) }),
        },
        AttemptInterceptors: []AttemptInterceptor{
            NewAttemptEventSink(func(e WorkflowEvent) { events = append(events, e) }),
        },
    }
    w.Add(Step(step).Retry(func(o *RetryOption) {
        o.Attempts = 3
        o.Backoff = &backoff.ZeroBackOff{}
    }))
    assert.NoError(t, w.Do(context.Background()))
    types := eventTypes(events)
    // Scheduled, Started(0), Retrying(0), Started(1), Retrying(1), Started(2), Succeeded
    assert.Equal(t, []EventType{
        Scheduled,
        Started, Retrying,
        Started, Retrying,
        Started, Succeeded,
    }, types)
    assert.Equal(t, []EventType{
        Scheduled,
        Started, Retrying,
        Started, Retrying,
        Started, Succeeded,
    }, types)
}

// helper
func eventTypes(events []WorkflowEvent) []EventType {
    types := make([]EventType, len(events))
    for i, e := range events {
        types[i] = e.Type
    }
    return types
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./... -run "TestStepExecution" -v
```

Expected: FAIL — `StepInterceptors` field not defined.

- [ ] **Step 3: Add fields to `Workflow` struct**

In `workflow.go`, add two fields to the `Workflow` struct:

```go
type Workflow struct {
    MaxConcurrency      int
    DontPanic           bool
    SkipAsError         bool
    Clock               clock.Clock
    DefaultOption       *StepOption
    StepInterceptors    []StepInterceptor    // per-step global interceptors
    AttemptInterceptors []AttemptInterceptor // per-attempt global interceptors

    StepBuilder
    steps        map[Steper]*State
    statusChange *sync.Cond
    leaseBucket  chan struct{}
    waitGroup    sync.WaitGroup
    isRunning    sync.Mutex
}
```

- [ ] **Step 4: Add `stepExecution` struct and `scheduled` sentinel**

Add after the `Workflow` struct in `workflow.go`:

```go
// scheduled is a private StepStatus sentinel used by tick() to atomically
// claim a step and prevent double-spawning. It is never exposed to users.
const scheduled StepStatus = "scheduled"

// stepExecution owns the full lifecycle of a single step run.
type stepExecution struct {
    w       *Workflow
    step    Steper
    state   *State
    attempt uint64         // single source of truth shared by AttemptInfo and wireNotify
    onRetry func(WorkflowEvent) // assembled from StepInterceptors that implement retryNotifier
}
```

- [ ] **Step 5: Implement `stepExecution.run()`**

Add to `workflow.go`:

```go
func (ex *stepExecution) run(ctx context.Context) {
    defer ex.w.waitGroup.Done()

    // Evaluate condition now (safe: all upstreams are terminated at this point).
    ups := ex.w.UpstreamOf(ex.step)
    option := ex.state.Option()
    cond := DefaultCondition
    if option != nil && option.Condition != nil {
        cond = option.Condition
    }

    terminalReason := Pending
    if nextStatus := cond(ctx, ups); nextStatus.IsTerminated() {
        terminalReason = nextStatus
    }

    info := StepInfo{Step: ex.step, TerminalReason: terminalReason}

    // Build StepInterceptor chain; also collect retryNotifiers for wireNotify.
    var retrySinks []func(WorkflowEvent)
    stepNext := ex.executeWithRetry
    for i := len(ex.w.StepInterceptors) - 1; i >= 0; i-- {
        ic := ex.w.StepInterceptors[i]
        if rn, ok := ic.(retryNotifier); ok {
            retrySinks = append(retrySinks, rn.onRetry)
        }
        next := stepNext
        icLocal := ic
        stepNext = func(ctx context.Context) error {
            return icLocal.InterceptStep(ctx, info, next)
        }
    }
    ex.onRetry = func(e WorkflowEvent) {
        for _, s := range retrySinks {
            s(e)
        }
    }

    var status StepStatus
    var err error

    if terminalReason != Pending {
        // Skipped or Canceled: run the chain (interceptors observe it), but
        // executeWithRetry will never be called because chain was built with
        // terminalReason set. The chain returns nil.
        err = stepNext(ctx)
        status = terminalReason
    } else {
        ex.state.SetStatus(Running)
        err = stepNext(ctx)
        status = statusFromError(err)
        if status == Failed {
            switch {
            case DefaultIsCanceled(err),
                errors.Is(err, context.Canceled),
                errors.Is(err, context.DeadlineExceeded):
                status = Canceled
            }
        }
    }

    ex.state.SetStatus(status)
    ex.state.SetError(err)
    ex.w.unlease()
    ex.w.signalStatusChange()
}
```

- [ ] **Step 6: Implement `stepExecution.executeWithRetry` and `stepExecution.runAttempt`**

```go
// executeWithRetry is the bottom of the StepInterceptor chain.
// It wires Retrying events and drives the retry loop.
func (ex *stepExecution) executeWithRetry(ctx context.Context) error {
    option := ex.state.Option()
    ex.wireNotify(option)

    // Build AttemptInterceptor chain; innermost is runAttempt (Before→Do→After).
    attemptChain := ex.buildAttemptChain()

    var notAfter time.Time
    if option != nil && option.Timeout != nil {
        notAfter = ex.w.Clock.Now().Add(*option.Timeout)
        var cancel func()
        ctx, cancel = ex.w.Clock.WithDeadline(ctx, notAfter)
        defer cancel()
    }

    return ex.w.retry(option.RetryOption)(ctx, attemptChain, notAfter)
}

func (ex *stepExecution) buildAttemptChain() func(context.Context) error {
    // Innermost: per-step Before callbacks → Do → After callbacks.
    chain := func(ctx context.Context) error {
        return ex.runAttempt(ctx)
    }
    for i := len(ex.w.AttemptInterceptors) - 1; i >= 0; i-- {
        ic := ex.w.AttemptInterceptors[i]
        next := chain
        icLocal := ic
        chain = func(ctx context.Context) error {
            info := AttemptInfo{
                StepInfo: StepInfo{Step: ex.step},
                Attempt:  ex.attempt,
            }
            return icLocal.InterceptAttempt(ctx, info, next)
        }
    }
    return chain
}

func (ex *stepExecution) runAttempt(ctx context.Context) error {
    defer func() { ex.attempt++ }()

    // Propagate interceptors to SubWorkflow if applicable.
    if recv, ok := ex.step.(InterceptorReceiver); ok {
        recv.PrependInterceptors(ex.w.StepInterceptors, ex.w.AttemptInterceptors)
    }

    do := func(fn func() error) error { return fn() }
    if ex.w.DontPanic {
        do = catchPanicAsError
    }

    var ctxStep context.Context
    err := do(func() error {
        ctxBefore, errBefore := ex.state.Before(ctx, ex.step, do)
        ctxStep = ctxBefore
        return errBefore
    })
    if err != nil {
        return ErrBeforeStep{err}
    }
    err = do(func() error { return ex.step.Do(ctxStep) })
    return do(func() error { return ex.state.After(ctxStep, ex.step, err) })
}

func (ex *stepExecution) wireNotify(option *StepOption) {
    if option == nil || option.RetryOption == nil {
        return
    }
    userNotify := option.RetryOption.Notify
    option.RetryOption.Notify = func(err error, d time.Duration) {
        e := WorkflowEvent{
            Step:            ex.step,
            Type:            Retrying,
            Attempt:         ex.attempt,
            Err:             err,
            BackoffDuration: d,
        }
        ex.attempt++
        if ex.onRetry != nil {
            ex.onRetry(e)
        }
        if userNotify != nil {
            userNotify(err, d)
        }
    }
}
```

Note: add a `statusFromError` helper in `workflow.go` (replaces the inline `StatusFromError` call):

```go
func statusFromError(err error) StepStatus {
    if err == nil {
        return Succeeded
    }
    if s := StatusFromError(err); s != Failed {
        return s
    }
    return Failed
}
```

- [ ] **Step 7: Simplify `tick()`**

Replace the entire goroutine block in `tick()`:

```go
// Before (remove this block):
state.SetStatus(Running)
w.waitGroup.Add(1)
go func(ctx context.Context, step Steper, state *State) {
    // ... entire anonymous goroutine body ...
}(ctx, step, state)

// After:
state.SetStatus(scheduled)
w.waitGroup.Add(1)
ex := &stepExecution{w: w, step: step, state: state}
go ex.run(ctx)
```

Also remove the `makeDoForStep` method from `workflow.go` entirely (its logic is now in `stepExecution.runAttempt`).

And update `runStep` — it is now unused; remove it. Its timeout and retry logic moved into `executeWithRetry`.

- [ ] **Step 8: Run all tests**

```bash
go test ./... -v 2>&1 | tail -30
```

Expected: All existing tests PASS, new `TestStepExecution_*` tests PASS.

- [ ] **Step 9: Commit**

```bash
git add workflow.go workflow_test.go event.go event_test.go
git commit -m "feat: introduce stepExecution, add StepInterceptors/AttemptInterceptors to Workflow"
```

---

## Task 4: `SubWorkflow` implements `InterceptorReceiver`

**Files:**
- Modify: `wrap.go`
- Modify: `wrap_test.go`

- [ ] **Step 1: Write failing test**

```go
// wrap_test.go — add this test

func TestSubWorkflow_InterceptorPropagation(t *testing.T) {
    t.Parallel()

    var events []WorkflowEvent
    sink := NewStepEventSink(func(e WorkflowEvent) {
        events = append(events, e)
    })

    innerStep := NoOp("inner")
    type mySubStep struct{ SubWorkflow }
    sub := &mySubStep{}
    sub.Add(Step(innerStep))

    w := &Workflow{
        StepInterceptors: []StepInterceptor{sink},
    }
    w.Add(Step(sub))

    assert.NoError(t, w.Do(context.Background()))

    // Expect events for both outer step (sub) and inner step (innerStep)
    types := eventTypes(events)
    assert.Contains(t, types, Scheduled)
    assert.Contains(t, types, Succeeded)
    // There should be at least 4 events: Scheduled+Succeeded for sub, Scheduled+Succeeded for innerStep
    assert.GreaterOrEqual(t, len(events), 4)
    // All events should have a non-nil Step
    for _, e := range events {
        assert.NotNil(t, e.Step)
    }
}

func TestSubWorkflow_ChildInterceptorPreserved(t *testing.T) {
    t.Parallel()

    var parentEvents []WorkflowEvent
    var childEvents []WorkflowEvent

    parentSink := NewStepEventSink(func(e WorkflowEvent) { parentEvents = append(parentEvents, e) })
    childSink := NewStepEventSink(func(e WorkflowEvent) { childEvents = append(childEvents, e) })

    innerStep := NoOp("inner")
    type mySubStep struct{ SubWorkflow }
    sub := &mySubStep{}
    sub.Add(Step(innerStep))
    // child-only interceptor
    sub.w.StepInterceptors = []StepInterceptor{childSink}

    w := &Workflow{
        StepInterceptors: []StepInterceptor{parentSink},
    }
    w.Add(Step(sub))

    assert.NoError(t, w.Do(context.Background()))

    // Parent sees outer step + inner step (propagated)
    assert.GreaterOrEqual(t, len(parentEvents), 4)
    // Child sees inner step only
    assert.GreaterOrEqual(t, len(childEvents), 2)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./... -run "TestSubWorkflow_Interceptor" -v
```

Expected: FAIL — `SubWorkflow` does not implement `InterceptorReceiver`.

- [ ] **Step 3: Implement `PrependInterceptors` on `SubWorkflow`**

Add to `wrap.go`:

```go
// PrependInterceptors implements InterceptorReceiver.
// Parent interceptors are prepended so they execute outside child interceptors.
func (s *SubWorkflow) PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor) {
    s.w.StepInterceptors    = append(step,    s.w.StepInterceptors...)
    s.w.AttemptInterceptors = append(attempt, s.w.AttemptInterceptors...)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./... -run "TestSubWorkflow_Interceptor" -v
```

Expected: PASS.

- [ ] **Step 5: Run full test suite**

```bash
go test ./...
```

Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add wrap.go wrap_test.go
git commit -m "feat: SubWorkflow implements InterceptorReceiver for interceptor propagation"
```

---

## Task 5: Verify `retry()` integration with `wireNotify`

This task tests the full retry + Retrying event pipeline end-to-end with real backoff.

**Files:**
- Modify: `workflow_test.go`

- [ ] **Step 1: Write failing test**

```go
// workflow_test.go — add this test

func TestStepExecution_RetryingEventAttemptNumbers(t *testing.T) {
    t.Parallel()

    var events []WorkflowEvent
    mu := sync.Mutex{}
    record := func(e WorkflowEvent) {
        mu.Lock()
        events = append(events, e)
        mu.Unlock()
    }

    callCount := 0
    step := Func("flaky", func(ctx context.Context) error {
        callCount++
        if callCount < 3 {
            return errors.New("not yet")
        }
        return nil
    })

    w := &Workflow{
        StepInterceptors: []StepInterceptor{NewStepEventSink(record)},
        AttemptInterceptors: []AttemptInterceptor{NewAttemptEventSink(record)},
    }
    w.Add(Step(step).Retry(func(o *RetryOption) {
        o.Attempts = 5
        o.Backoff = &backoff.ZeroBackOff{}
    }))

    assert.NoError(t, w.Do(context.Background()))

    types := eventTypes(events)
    assert.Equal(t, []EventType{
        Scheduled,             // StepInterceptor entry
        Started,               // attempt 0
        Retrying,              // attempt 0 failed
        Started,               // attempt 1
        Retrying,              // attempt 1 failed
        Started,               // attempt 2 succeeds
        Succeeded,             // StepInterceptor exit
    }, types)

    // Verify attempt numbers in Retrying events
    retryingEvents := filterEvents(events, Retrying)
    assert.Equal(t, uint64(0), retryingEvents[0].Attempt)
    assert.Equal(t, uint64(1), retryingEvents[1].Attempt)

    // Verify attempt numbers in Started events
    startedEvents := filterEvents(events, Started)
    assert.Equal(t, uint64(0), startedEvents[0].Attempt)
    assert.Equal(t, uint64(1), startedEvents[1].Attempt)
    assert.Equal(t, uint64(2), startedEvents[2].Attempt)
}

// helpers (add once, reuse across tests)
func filterEvents(events []WorkflowEvent, t EventType) []WorkflowEvent {
    var rv []WorkflowEvent
    for _, e := range events {
        if e.Type == t {
            rv = append(rv, e)
        }
    }
    return rv
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./... -run "TestStepExecution_RetryingEventAttemptNumbers" -v
```

Expected: FAIL.

- [ ] **Step 3: Run test to verify it passes (no code change needed)**

This test should pass once Task 3 is complete. If it fails, there is a bug in `wireNotify` or attempt counter ordering — debug `stepExecution.wireNotify`.

```bash
go test ./... -run "TestStepExecution_RetryingEventAttemptNumbers" -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add workflow_test.go
git commit -m "test: verify Retrying event attempt numbers are correctly sequenced"
```

---

## Task 6: Final integration and cleanup

**Files:**
- Modify: `workflow_test.go`
- Modify: `event_test.go`

- [ ] **Step 1: Run full test suite including example package**

```bash
go test ./...
```

Expected: All PASS with no race conditions.

- [ ] **Step 2: Run with race detector**

```bash
go test -race ./...
```

Expected: All PASS, no data race detected.

- [ ] **Step 3: Verify zero-cost when no interceptors are set**

```go
// workflow_test.go — add this test
func TestWorkflow_NoInterceptors_NoAlloc(t *testing.T) {
    t.Parallel()
    // Workflows without interceptors must not regress existing behavior.
    step := NoOp("a")
    w := &Workflow{}
    w.Add(Step(step))
    assert.NoError(t, w.Do(context.Background()))
    assert.Equal(t, Succeeded, w.StateOf(step).GetStatus())
}
```

```bash
go test ./... -run "TestWorkflow_NoInterceptors_NoAlloc" -v
```

Expected: PASS.

- [ ] **Step 4: Final commit**

```bash
git add -u
git commit -m "test: final integration tests and race detector clean"
```
