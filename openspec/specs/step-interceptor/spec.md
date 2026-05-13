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

`Workflow` SHALL expose two slice fields for global interceptor registration
under `Workflow.Option` (the `WorkflowOption` grouping defined by the
`workflow-options` capability):

```go
type WorkflowOption struct {
    // ... other fields ...
    StepInterceptors    []StepInterceptor    // [0] outermost, [len-1] innermost
    AttemptInterceptors []AttemptInterceptor // [0] outermost, [len-1] innermost
    DontInherit         bool                 // if true, do not inherit from a parent workflow
    // ... other fields ...
}
```

Nil/empty slices mean no interceptors. Existing workflows without
interceptors SHALL behave identically to before this feature was added
(zero-value safe, no allocations on the hot path).

The previous top-level `Workflow.StepInterceptors` /
`Workflow.AttemptInterceptors` / `Workflow.IsolateInterceptors` fields
SHALL NOT exist. `IsolateInterceptors` is replaced by `Option.DontInherit`,
whose semantics extend from interceptor-only to whole-`WorkflowOption`
opt-out.

#### Scenario: Outer-to-inner ordering
- **WHEN** `Option.StepInterceptors = [A, B]` are registered
- **THEN** the execution order is `A:before → B:before → step → B:after → A:after`

#### Scenario: No interceptors means no behavioural change
- **WHEN** a Workflow is constructed without `Option.StepInterceptors` or `Option.AttemptInterceptors`
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

`Workflow` SHALL implement `WorkflowOptionReceiver` (defined in the
`workflow-options` capability spec) so that when a `*Workflow` (or a user
struct embedding `flow.Workflow`) is used as a Step inside another
Workflow, the parent's interceptors are prepended to the child's
interceptor slices via the parent's prologue-pass `InheritOption` call.

The parent's `Do()` prologue locates the receiver for each root step by
walking `Unwrap()` via `findOptionReceiver` (the same protocol used by
`As[T]` / `Has[T]`) and selecting the first receiver found in pre-order.
This means a sub-workflow MAY be wrapped in any Steper-only wrapper
(notably `flow.Name` / `NamedStep`) without losing inheritance.

The parent SHALL invoke `restore := recv.InheritOption(parent.Option)`
exactly once per root sub-workflow step before the parent's tick begins,
and SHALL `defer restore()`. Inheritance is **per-run scoped**:

- The parent's user-supplied `Option.StepInterceptors` /
  `Option.AttemptInterceptors` slices SHALL NOT be mutated by
  `InheritOption`; the implementation prepends to a fresh slice.
- Each `InheritOption` call snapshots the receiver's `Option` and returns
  a `restore func()` that the parent defers, so prepended parent
  contributions do not accumulate across repeated `Do()` runs.

The previous `InterceptorReceiver` interface, `PrependInterceptors` method,
and `inheritedStep` / `inheritedAttempt` side fields SHALL NOT exist.

#### Scenario: Nested *Workflow inherits parent interceptors
- **GIVEN** a parent Workflow with `Option.StepInterceptors = [X]`, and a child `*Workflow` containing
  step `S` added as a step in the parent
- **WHEN** the parent runs
- **THEN** X is invoked for both the child workflow step and the inner step S

#### Scenario: Embedded Workflow inherits parent interceptors
- **GIVEN** a parent Workflow with `Option.StepInterceptors = [X]`, and a step embedding `flow.Workflow`
  containing step `S`
- **WHEN** the parent runs
- **THEN** X is invoked for both the outer step and the inner step S

#### Scenario: Inheritance survives Steper-only wrappers (NamedStep / flow.Name)
- **GIVEN** a parent Workflow with `Option.StepInterceptors = [X]`, and a child `*Workflow`
  containing step `S` that is added to the parent via `flow.Name(child, "name")`
- **WHEN** the parent runs
- **THEN** X is invoked for both the wrapping `NamedStep` and the inner step S
- **AND** inheritance works because the parent looks up `WorkflowOptionReceiver` via
  `Unwrap`, not via a direct type assertion on the registered Step

#### Scenario: InheritOption does not duplicate across retries
- **WHEN** a sub-workflow step is retried N times within one parent run
- **THEN** parent interceptors are prepended to the child's slice exactly once, not N times

#### Scenario: InheritOption does not accumulate across repeated Do() runs
- **GIVEN** a parent containing a child sub-workflow
- **WHEN** the parent's `Do()` is invoked N times in succession
- **THEN** each invocation results in the parent's interceptors firing exactly once per
  step (no compounding across runs)
- **AND** the child's `Option.StepInterceptors` field after each run is its original
  pre-inheritance value

---

### Requirement: Opting out of inheritance via DontInherit

A nested `Workflow` MAY set `Option.DontInherit = true` to opt out of
inheriting any of the parent's `WorkflowOption`, including interceptors.
When true, the merge step performed by `InheritOption` SHALL be a no-op
and the workflow runs only with its own configured `Option`. A (possibly
trivial) restore func is still returned so the parent's `defer restore()`
remains uniform.

This is intended for self-contained sub-workflows that define their own
observability pipeline (e.g., their own tracer or event sink) that must not
be wrapped by parent interceptors, or that more generally want isolation
from the parent's whole `WorkflowOption`.

The previous `IsolateInterceptors` flag SHALL NOT exist; its semantics are
subsumed and widened by `Option.DontInherit`.

#### Scenario: DontInherit blocks interceptor inheritance
- **GIVEN** parent `Option.StepInterceptors = [X]`, child `Option.DontInherit = true, StepInterceptors = [Y]`
- **WHEN** parent runs the child
- **THEN** the child runs with only `[Y]` as its effective StepInterceptors

#### Scenario: Isolated child does not see parent interceptors
- **GIVEN** a parent Workflow with `Option.StepInterceptors = [X]` and a child Workflow with
  `Option.DontInherit = true` and its own `Option.StepInterceptors = [Y]`, containing inner step S
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

---

### Requirement: DontPanic protects interceptor panics

When `Workflow.Option.DontPanic` is non-nil and dereferences to `true`, panics raised
inside user-provided `StepInterceptor` or `AttemptInterceptor` implementations SHALL be
caught and converted to errors using the same `catchPanicAsError` mechanism already
applied to `Before` / `Do` / `After`. This prevents:

- Process crashes from a faulty user interceptor.
- `MaxConcurrency` lease leaks (an unrecovered panic skips the deferred `unlease`).
- Loss of `signalStatusChange`, which would otherwise hang the main `Do()` loop.

When `Option.DontPanic` is nil or dereferences to `false` (the default), interceptor
panics propagate as in normal Go semantics.

#### Scenario: Panicking StepInterceptor under DontPanic
- **GIVEN** a Workflow with `Option.DontPanic = &true` and a `StepInterceptor` that panics
- **WHEN** the Workflow runs
- **THEN** `Do()` returns an error within a bounded time
- **AND** the step's `StepResult.Err` carries the panic value
- **AND** the workflow does not hang waiting for a status signal

#### Scenario: Panicking AttemptInterceptor under DontPanic
- **GIVEN** a Workflow with `Option.DontPanic = &true` and an `AttemptInterceptor` that panics
- **WHEN** the Workflow runs
- **THEN** `Do()` returns an error within a bounded time
- **AND** the step's `StepResult.Err` carries the panic value
