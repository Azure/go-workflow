# workflow-options — Spec Delta

## MODIFIED Requirements

### Requirement: MaxConcurrency limits simultaneous running Steps

When `Workflow.Option.MaxConcurrency` is non-nil and points to a positive integer
`N`, the Workflow SHALL run at most `N` Steps concurrently. Additional runnable
Steps wait until a running Step terminates before starting. A nil pointer (the
default) or a value of `0` means unlimited concurrency.

The limit is implemented via a buffered channel used as a lease bucket.

#### Scenario: MaxConcurrency=2 allows exactly 2 concurrent Steps
- **WHEN** `Option.MaxConcurrency` points to `2` and 3 independent Steps are added
- **THEN** at most 2 Steps run simultaneously; the third starts only after one finishes

#### Scenario: MaxConcurrency nil imposes no limit
- **WHEN** `Option.MaxConcurrency` is nil (the default)
- **THEN** all runnable Steps start concurrently without any concurrency bound

---

### Requirement: DontPanic converts panics to errors

When `Workflow.Option.DontPanic` is non-nil and dereferences to `true`, any
`panic` in a Step's `Do`, `Input`, or `BeforeStep`/`AfterStep` callbacks is
recovered and returned as a `ErrPanic`-wrapped error, setting the Step to
`Failed`. Stack trace information is captured and included in the error.

When `Option.DontPanic` is nil (the default) or dereferences to `false`,
panics propagate normally and crash the process.

#### Scenario: Panic converted to Failed with DontPanic=true
- **WHEN** `Option.DontPanic` points to `true` and a Step panics during `Do`
- **THEN** the Step status is `Failed`; the returned `ErrWorkflow` entry wraps the panic
  value as an `ErrPanic`

#### Scenario: Panic propagates with DontPanic nil
- **WHEN** `Option.DontPanic` is nil and a Step panics
- **THEN** the panic propagates out of the goroutine (process crash or test failure)

---

### Requirement: SkipAsError controls whether Skipped counts as failure

When `Workflow.Option.SkipAsError` is nil or dereferences to `false` (the
default), Steps that are `Skipped` are considered acceptable outcomes.
`workflow.Do` returns `nil` if all root Steps are `Succeeded` or `Skipped`.

When `Option.SkipAsError` dereferences to `true`, any `Skipped` Step causes
`workflow.Do` to return an `ErrWorkflow` even if no Step actually failed.

#### Scenario: Skipped is acceptable by default
- **WHEN** `Option.SkipAsError` is nil and all root Steps are either `Succeeded` or `Skipped`
- **THEN** `workflow.Do` returns `nil`

#### Scenario: Skipped counts as error when SkipAsError=true
- **WHEN** `Option.SkipAsError` points to `true` and at least one root Step is `Skipped`
- **THEN** `workflow.Do` returns an `ErrWorkflow` containing the skipped Step

---

### Requirement: DefaultStepOption applies a baseline StepOption to all Steps

`Workflow.Option.DefaultStepOption` is a `*StepOption` that the Workflow
prepends to the Option slice of every Step added to it. This lets callers set
a universal default for all Steps (e.g., a global timeout) without modifying
each Step individually.

Step-level options that are set after the default take precedence because the
Option slice is evaluated left-to-right and later values overwrite earlier
ones on the same `StepOption` struct.

The renaming from `DefaultOption` to `DefaultStepOption` clarifies that the
field configures step-level options, not workflow-level options
(`WorkflowOption`).

#### Scenario: DefaultStepOption sets a global timeout
- **WHEN** `Option.DefaultStepOption` has `Timeout` set to 10 minutes
  and a Step is added without its own `Timeout`
- **THEN** the effective timeout for that Step is 10 minutes

#### Scenario: Step-level option overrides the default
- **WHEN** `Option.DefaultStepOption` has `Timeout` of 10 minutes and a Step declares
  `.Timeout(5 * time.Minute)`
- **THEN** the effective timeout for that Step is 5 minutes

---

### Requirement: Clock enables time injection for testing

`Workflow.Option.Clock` is a `clock.Clock` interface (from
`github.com/benbjohnson/clock`). The Workflow uses the Clock for all
time-related operations: Step-level timeouts, per-try timeouts in the retry
loop, and backoff waits.

When `Option.Clock` is `nil`, the Workflow uses the real wall clock
(`clock.New()`). Providing a mock clock allows unit tests to control time
without real delays.

#### Scenario: Nil Clock uses wall clock
- **WHEN** `Option.Clock` is not set
- **THEN** the Workflow automatically uses `clock.New()` for all time operations

#### Scenario: Mock clock controls timeout behavior in tests
- **WHEN** a `clock.Mock` is injected as `Option.Clock`
  and the mock is advanced past a Step's `Timeout` duration
- **THEN** the Step's context is canceled and the Step is set to `Canceled`

---

## ADDED Requirements

### Requirement: WorkflowOption groups workflow-level configuration

`flow.Workflow` SHALL expose all configuration fields under a single named
field `Option WorkflowOption`. The previous nine top-level configuration
fields (`MaxConcurrency`, `DontPanic`, `SkipAsError`, `Clock`,
`DefaultOption`, `Mutators`, `StepInterceptors`, `AttemptInterceptors`,
`IsolateInterceptors`) SHALL NOT exist as direct fields on `Workflow`.

`WorkflowOption` SHALL declare:

```go
type WorkflowOption struct {
    MaxConcurrency    *int
    DontPanic         *bool
    SkipAsError       *bool
    Clock             clock.Clock
    DefaultStepOption *StepOption

    Mutators            []Mutator
    StepInterceptors    []StepInterceptor
    AttemptInterceptors []AttemptInterceptor

    DontInherit bool
}
```

Scalar configuration fields are pointer-typed so that "unset" (nil pointer)
and "explicit zero value" are distinguishable. This distinction is required
for parent → child Option inheritance: a nil pointer on the child means
"inherit from parent"; a non-nil pointer means "child wins".

#### Scenario: Workflow exposes Option field
- **WHEN** a user constructs `&flow.Workflow{Option: flow.WorkflowOption{...}}`
- **THEN** the Workflow accepts the configuration and applies it

#### Scenario: Explicit zero is distinguishable from unset
- **GIVEN** a parent with `Option.MaxConcurrency = &four` (where `four = 4`)
- **AND** a child with `Option.MaxConcurrency = &zero` (where `zero = 0`)
- **WHEN** parent runs the child as a sub-workflow
- **THEN** the child observes `MaxConcurrency = 0` (unlimited), NOT inherited 4

---

### Requirement: WorkflowOptionReceiver propagates Option to sub-workflows

`flow.Workflow` SHALL implement `WorkflowOptionReceiver`:

```go
type WorkflowOptionReceiver interface {
    InheritOption(parent WorkflowOption)
}
```

The parent Workflow SHALL invoke `InheritOption(parent.Option)` exactly once
per sub-workflow root step, in the parent's `Do()` prologue (after `init()`,
before the scheduling tick begins). The parent SHALL locate the receiver by
walking each root step's `Unwrap()` chain via `findOptionReceiver`, returning
the first `WorkflowOptionReceiver` found in pre-order. This means a
sub-workflow MAY be wrapped in any Steper-only wrapper (notably `flow.Name` /
`NamedStep`) without losing inheritance.

`Workflow.InheritOption` SHALL apply the following merge rules:

1. If `w.Option.DontInherit` is `true`, return immediately without modifying
   any field.
2. For each scalar pointer field (`MaxConcurrency`, `DontPanic`,
   `SkipAsError`) and each interface/pointer field (`Clock`,
   `DefaultStepOption`): if the child's field is nil, set it to the parent's
   value. Non-nil child fields SHALL NOT be modified.
3. For each slice field (`Mutators`, `StepInterceptors`,
   `AttemptInterceptors`): allocate a fresh slice equal to
   `parent_slice ++ child_slice` and assign it to the child's field. The
   parent's and child's input slices SHALL NOT be mutated.

The parent's user-supplied `WorkflowOption` SHALL NOT be mutated by
`InheritOption`.

#### Scenario: Scalar nil inherits parent's value
- **GIVEN** parent `Option.DontPanic = &true`, child `Option.DontPanic = nil`
- **WHEN** parent invokes `child.InheritOption(parent.Option)`
- **THEN** child observes `DontPanic = true` for the duration of the run

#### Scenario: Scalar non-nil child wins
- **GIVEN** parent `Option.MaxConcurrency = &four`, child `Option.MaxConcurrency = &eight`
- **WHEN** parent invokes `child.InheritOption(parent.Option)`
- **THEN** child observes `MaxConcurrency = 8`

#### Scenario: Slices are parent-prepended
- **GIVEN** parent `Option.Mutators = [A]`, child `Option.Mutators = [B]`
- **WHEN** parent invokes `child.InheritOption(parent.Option)`
- **THEN** child's effective `Mutators` is `[A, B]` for the duration of the run

#### Scenario: Multi-level nesting prepends in order
- **GIVEN** grandparent `Option.Mutators = [A]`, parent `Option.Mutators = [B]`, child `Option.Mutators = [C]`
- **WHEN** grandparent runs, propagating to parent then to child
- **THEN** child's effective `Mutators` is `[A, B, C]`

#### Scenario: Inheritance survives Steper-only wrappers
- **GIVEN** a parent with a `Mutator` registered, and a child `*Workflow` added to the parent via `flow.Name(child, "name")`
- **WHEN** the parent runs
- **THEN** the parent's `Mutator` reaches the inner steps via `InheritOption` propagation through `Unwrap`

---

### Requirement: DontInherit opts out of all parent Option inheritance

When a sub-workflow's `Option.DontInherit` is `true`, the parent's
`InheritOption(parent.Option)` call SHALL be a no-op. The sub-workflow runs
with exactly its own configured Option, with no scalars filled in from the
parent and no slices prepended.

This replaces the previous `IsolateInterceptors` flag and widens its
semantics from interceptor-only to whole-Option opt-out. The naming aligns
with `DontPanic`.

#### Scenario: DontInherit blocks scalar inheritance
- **GIVEN** parent `Option.DontPanic = &true`, child `Option.DontInherit = true, DontPanic = nil`
- **WHEN** parent invokes `child.InheritOption(parent.Option)`
- **THEN** child observes `DontPanic = false` (nil dereferenced as default)

#### Scenario: DontInherit blocks slice prepending
- **GIVEN** parent `Option.Mutators = [A]`, child `Option.DontInherit = true, Mutators = [B]`
- **WHEN** parent invokes `child.InheritOption(parent.Option)`
- **THEN** child's effective `Mutators` is `[B]`, not `[A, B]`

---

### Requirement: Do() snapshots and restores Option to prevent accumulation

`Workflow.Do()` SHALL snapshot `w.Option` immediately after acquiring the
`isRunning` lock and SHALL restore `w.Option` from the snapshot via `defer`.
This ensures that `InheritOption` writes performed by a parent during one
`Do()` run do not accumulate into subsequent runs.

The snapshot is a shallow copy. This is correct because:

- Pointer overwrites on nil scalar fields point to the parent's existing
  pointer values without mutating them.
- Slice fields are always written via fresh `make`-and-append in
  `InheritOption`; the snapshot's slice header still references the
  pre-inheritance backing array.

The internal `reset()` SHALL NOT clear `w.Option` (that is the snapshot
restore's job, and reset runs at the top of `Do()` before scheduling).

#### Scenario: Repeated Do() runs do not accumulate inherited contributions
- **GIVEN** a parent with `Option.Mutators = [A]` and a child with `Option.Mutators = [B]`
- **WHEN** `parent.Do()` is invoked N times
- **THEN** each invocation results in the child's effective `Mutators` being `[A, B]` during the run
- **AND** the child's `Option.Mutators` field is `[B]` after each run completes

#### Scenario: Snapshot/restore covers all exit paths
- **WHEN** `Do()` returns successfully, returns an error, or panics (when not recovered)
- **THEN** `w.Option` is restored to its pre-`Do()` state via `defer`

---

## REMOVED Requirements

### Requirement: ~~Workflow exposes Mutators / StepInterceptors / AttemptInterceptors as top-level fields~~

**Reason:** Replaced by `Workflow.Option.Mutators`, `Workflow.Option.StepInterceptors`,
`Workflow.Option.AttemptInterceptors` under the new `WorkflowOption` grouping.

### Requirement: ~~MutatorReceiver interface~~

**Reason:** Replaced by `WorkflowOptionReceiver.InheritOption`, which carries
`Mutators` as one component of the inherited `WorkflowOption`.

### Requirement: ~~InterceptorReceiver interface~~

**Reason:** Replaced by `WorkflowOptionReceiver.InheritOption`, which carries
`StepInterceptors` and `AttemptInterceptors` as components of the inherited
`WorkflowOption`.

### Requirement: ~~IsolateInterceptors~~

**Reason:** Replaced by `Option.DontInherit`, which generalises from
interceptor-only opt-out to whole-`WorkflowOption` opt-out.

### Requirement: ~~inheritedStep / inheritedAttempt side fields~~

**Reason:** The accumulation-prevention invariant is now satisfied by
snapshot-and-restore of `w.Option` in `Do()`; the side fields and their
special-cased `reset()` behavior are no longer needed.
