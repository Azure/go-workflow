## ADDED Requirements

### Requirement: Two-layer interceptor types

go-workflow SHALL provide two orthogonal interceptor interfaces for global, structured
observability across all Steps in a Workflow:

- `StepInterceptor` wraps the **full lifecycle** of a Step (all retry attempts, called once
  per Step).
- `AttemptInterceptor` wraps **each individual attempt** (called once per attempt, including
  retried attempts).

```go
type StepInterceptor interface {
    InterceptStep(ctx context.Context, step Steper, next func(context.Context) error) error
}
type AttemptInterceptor interface {
    InterceptAttempt(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error
}
```

Function adapters `StepInterceptorFunc` and `AttemptInterceptorFunc` are provided so callers
can pass plain functions.

The `Steper` value passed to interceptors is the canonical Step identifier — the same
pointer used as the map key inside `Workflow`. Callers needing a human-readable name SHALL
call `flow.String(step)`.

#### Scenario: StepInterceptor fires exactly once per step
- **WHEN** a Step executes (succeeds, fails, or retries any number of times)
- **THEN** each registered `StepInterceptor.InterceptStep` is invoked exactly once

#### Scenario: AttemptInterceptor fires once per attempt
- **WHEN** a Step is retried N times (i.e. N+1 attempts total)
- **THEN** each registered `AttemptInterceptor.InterceptAttempt` is invoked N+1 times,
  with `attempt` taking values `0, 1, ..., N`

#### Scenario: Attempt error is observable
- **WHEN** an `AttemptInterceptor` calls `next(ctx)` and the attempt fails
- **THEN** `next` returns the attempt's error and the interceptor MAY inspect it before
  returning

---

### Requirement: Skipped and Canceled steps bypass the interceptor chain

Steps whose `Condition` evaluates to a terminal status (`Skipped` or `Canceled`) before
execution SHALL NOT enter the `StepInterceptor` chain. The Workflow SHALL evaluate the
Condition inline in the scheduling loop (`tick()`) and settle the step's terminal
`StepResult` directly — without spawning a worker goroutine and without consuming a
`MaxConcurrency` lease. The post-run status remains queryable via
`workflow.StateOf(step).GetStatus()`.

This avoids the footgun of forcing every interceptor to check whether the step "will
actually execute" before calling `next`, and ensures terminal-by-condition steps do not
serialize behind a low concurrency limit.

#### Scenario: Skipped step does not invoke interceptors
- **WHEN** a Step's Condition returns `Skipped`
- **THEN** no `StepInterceptor` or `AttemptInterceptor` is invoked for that step
- **AND** `workflow.StateOf(step).GetStatus()` returns `Skipped`

#### Scenario: Canceled-by-condition step does not invoke interceptors
- **WHEN** a Step's Condition returns `Canceled`
- **THEN** no `StepInterceptor` or `AttemptInterceptor` is invoked for that step

#### Scenario: Skipped step does not consume a concurrency lease
- **GIVEN** a Workflow with `MaxConcurrency = 1` and a chain `a → b → c` where `b`'s
  Condition returns `Skipped`
- **WHEN** the Workflow runs
- **THEN** `b` is settled inline; no worker goroutine is spawned for `b`; `b` does not
  occupy the single available lease while `a` or `c` are running

---

### Requirement: Workflow registration of interceptors

`Workflow` SHALL expose two slice fields for global interceptor registration:

```go
type Workflow struct {
    StepInterceptors    []StepInterceptor    // [0] outermost, [len-1] innermost
    AttemptInterceptors []AttemptInterceptor // [0] outermost, [len-1] innermost
    IsolateInterceptors bool                 // if true, do not inherit from a parent workflow
}
```

Nil/empty slices mean no interceptors. Existing workflows without interceptors SHALL behave
identically to before this feature was added (zero-value safe, no allocations on the hot
path).

#### Scenario: Outer-to-inner ordering
- **WHEN** `StepInterceptors = [A, B]` are registered
- **THEN** the execution order is `A:before → B:before → step → B:after → A:after`

#### Scenario: No interceptors means no behavioural change
- **WHEN** a Workflow is constructed without `StepInterceptors` or `AttemptInterceptors`
- **THEN** all existing semantics (retries, conditions, BeforeStep/AfterStep) are unchanged

---

### Requirement: BeforeStep / AfterStep are orthogonal to interceptors

`BeforeStep` and `AfterStep` callbacks (configured per-step via `StepConfig`) execute
**inside** the `AttemptInterceptor` chain — they wrap a single `Do` call. Interceptors are
workflow-level and apply globally; `BeforeStep`/`AfterStep` are step-level and configured
per-step. Both mechanisms are preserved and complementary.

The full execution stack for a single attempt is:

```
StepInterceptor[0] → ... → StepInterceptor[N-1]
  → retry loop
    → AttemptInterceptor[0] → ... → AttemptInterceptor[M-1]
      → BeforeStep callbacks
        → step.Do(ctx)
          → AfterStep callbacks
```

#### Scenario: BeforeStep runs inside AttemptInterceptor
- **WHEN** an `AttemptInterceptor` calls `next(ctx)`
- **THEN** the chain reaches the per-step `BeforeStep` callbacks before `step.Do` runs

---

### Requirement: Interceptor propagation to nested workflows

`Workflow` SHALL implement the `InterceptorReceiver` interface so that when a `*Workflow`
(or a step embedding `SubWorkflow`) is used as a Step inside another Workflow, the parent's
interceptors are prepended to the child's interceptor stack.

```go
type InterceptorReceiver interface {
    PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}
```

`stepExecution` calls `PrependInterceptors` exactly once per step, in `executeWithRetry`
before the retry loop begins. The implementation SHALL use `make`+`copy` to construct fresh
slices, so:

- Parent backing arrays are never aliased.
- Repeated `Do()` runs (across `Reset()` cycles) do not accumulate prepended interceptors.

`SubWorkflow.PrependInterceptors` SHALL delegate to the embedded `Workflow.PrependInterceptors`.

#### Scenario: Nested *Workflow inherits parent interceptors
- **GIVEN** a parent Workflow with a `StepInterceptor` X, and a child `*Workflow` containing
  step `S` added as a step in the parent
- **WHEN** the parent runs
- **THEN** X is invoked for both the child workflow step and the inner step S

#### Scenario: SubWorkflow inherits parent interceptors
- **GIVEN** a parent Workflow with a `StepInterceptor` X, and a step embedding `SubWorkflow`
  containing step `S`
- **WHEN** the parent runs
- **THEN** X is invoked for both the outer step and the inner step S

#### Scenario: PrependInterceptors does not duplicate across retries
- **WHEN** a sub-workflow step is retried N times
- **THEN** parent interceptors are prepended exactly once, not N times

#### Scenario: PrependInterceptors does not accumulate across Reset
- **WHEN** a workflow that received prepended interceptors is reset and run again as a child
- **THEN** the parent's interceptors are present exactly once, not duplicated

---

### Requirement: Opting out of inheritance via IsolateInterceptors

A nested `Workflow` MAY set `IsolateInterceptors = true` to opt out of inheriting
interceptors from its parent. When true, `Workflow.PrependInterceptors` SHALL be a no-op
and the workflow runs only with its own registered interceptors.

This is intended for self-contained sub-workflows that define their own observability
pipeline (e.g., their own tracer or event sink) that must not be wrapped by parent
interceptors.

#### Scenario: Isolated child does not see parent interceptors
- **GIVEN** a parent Workflow with `StepInterceptor` X and a child Workflow with
  `IsolateInterceptors = true` and its own `StepInterceptor` Y, containing inner step S
- **WHEN** the parent runs the child as a step
- **THEN** X is invoked exactly once (for the child workflow step itself)
- **AND** Y is invoked for inner step S
- **AND** X is NOT invoked for inner step S

---

### Requirement: Attempt counter ownership and increment timing

The internal `stepExecution` SHALL own the attempt counter (`uint64`), exposed to
`AttemptInterceptor` as the `attempt` parameter. The counter is incremented after each
attempt completes — including attempts that are short-circuited by an
`AttemptInterceptor` (e.g., one that returns without calling `next`).

This guarantees the value passed as `attempt` is monotonically increasing and zero-indexed,
regardless of interceptor behaviour.

#### Scenario: Attempt counter starts at zero
- **WHEN** a Step's first attempt runs
- **THEN** the `attempt` argument to `AttemptInterceptor.InterceptAttempt` is `0`

#### Scenario: Attempt counter increments even when interceptor short-circuits
- **WHEN** an `AttemptInterceptor` returns without calling `next`
- **THEN** the next attempt (if retried) still receives `attempt = previous + 1`
