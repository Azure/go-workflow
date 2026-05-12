# step-interceptor — Spec Delta

## MODIFIED Requirements

### Requirement: Workflow registration of interceptors

`Workflow` SHALL expose two slice fields for global interceptor registration
under `Workflow.Option` (the new `WorkflowOption` grouping introduced by the
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

The parent SHALL invoke `recv.InheritOption(parent.Option)` exactly once
per root sub-workflow step, before the parent's tick begins. Inheritance
is **per-run scoped**:

- The parent's user-supplied `Option.StepInterceptors` /
  `Option.AttemptInterceptors` slices SHALL NOT be mutated by
  `InheritOption`; the implementation prepends to a fresh slice.
- The child's `Option` SHALL be snapshotted at the start of its own `Do()`
  and restored via `defer` at exit, so prepended parent contributions do
  not accumulate across repeated `Do()` runs.

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
When true, the `InheritOption` call from the parent SHALL be a no-op and
the workflow runs only with its own configured `Option`.

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

---

## REMOVED Requirements

### Requirement: ~~InterceptorReceiver interface~~

**Reason:** Replaced by `WorkflowOptionReceiver.InheritOption`, which
carries `StepInterceptors` and `AttemptInterceptors` as components of the
inherited `WorkflowOption`.

### Requirement: ~~IsolateInterceptors as a separate flag~~

**Reason:** Replaced by `Option.DontInherit`, which generalises from
interceptor-only opt-out to whole-`WorkflowOption` opt-out.

### Requirement: ~~inheritedStep / inheritedAttempt side fields~~

**Reason:** The accumulation-prevention invariant is now satisfied by
snapshot-and-restore of `w.Option` in `Do()`; the side fields and their
special-cased `reset()` behavior are no longer needed.
