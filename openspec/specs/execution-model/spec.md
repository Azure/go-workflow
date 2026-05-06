## ADDED Requirements

### Requirement: Step status lifecycle

Every Step in a Workflow progresses through a well-defined set of statuses.
The Workflow SHALL assign each Step exactly one status at any point in time.

Statuses and their meanings:

| Status | Meaning |
|--------|---------|
| `Pending` (empty string) | Step has not started; waiting for upstream steps to terminate |
| `Running` | Step goroutine is executing `Do` |
| `Succeeded` | Step terminated successfully (`Do` returned `nil`) |
| `Failed` | Step terminated with an unrecoverable error |
| `Canceled` | Step terminated because the context was canceled or deadline exceeded |
| `Skipped` | Step was deliberately not executed (condition evaluated to skip) |

`IsTerminated()` returns `true` for `Succeeded`, `Failed`, `Canceled`, and `Skipped`.

#### Scenario: Step starts as Pending
- **WHEN** a Step is added to a Workflow before `Do` is called
- **THEN** its status is `Pending`

#### Scenario: Step transitions to Running
- **WHEN** all upstream Steps are terminated and the Step's Condition evaluates to `Running`
- **THEN** the Workflow starts a goroutine for the Step and its status becomes `Running`

#### Scenario: Step succeeds
- **WHEN** `step.Do(ctx)` returns `nil`
- **THEN** the Step status becomes `Succeeded`

#### Scenario: Step fails
- **WHEN** `step.Do(ctx)` returns a non-nil error that is not `ErrCancel` or `ErrSkip`
- **THEN** the Step status becomes `Failed`

#### Scenario: Step is canceled via context
- **WHEN** `step.Do(ctx)` returns `context.Canceled` or `context.DeadlineExceeded`
  or an error for which `DefaultIsCanceled` returns `true`
- **THEN** the Step status becomes `Canceled`

#### Scenario: Step is skipped via ErrSkip
- **WHEN** `step.Do(ctx)` returns `flow.Skip(err)`
- **THEN** the Step status becomes `Skipped` (the wrapped error is preserved)

#### Scenario: Step is marked succeeded despite error
- **WHEN** `step.Do(ctx)` returns `flow.Succeed(err)`
- **THEN** the Step status becomes `Succeeded` (the wrapped error is preserved)

---

### Requirement: DAG topology and topological execution order

The Workflow SHALL execute Steps in topological order respecting all declared
`DependsOn` relationships. A downstream Step SHALL NOT start until all its upstream
Steps are terminated.

The set of Steps and their dependency edges form a Directed Acyclic Graph (DAG).
Cycles are detected before execution begins (see "Preflight cycle detection").

#### Scenario: Upstream executes before downstream
- **WHEN** Step B depends on Step A
- **THEN** Step A terminates before Step B's goroutine is started

#### Scenario: Independent Steps execute concurrently
- **WHEN** two Steps have no dependency relationship (direct or transitive)
- **THEN** the Workflow MAY execute them concurrently in separate goroutines

#### Scenario: Diamond dependency
- **WHEN** Steps B and C both depend on A, and D depends on B and C
- **THEN** A executes first, then B and C in parallel, then D after both B and C terminate

---

### Requirement: Preflight cycle detection

Before starting execution, the Workflow SHALL detect any cycle in the dependency graph
and return an `ErrCycleDependency` error without executing any Steps.

#### Scenario: Cycle detected
- **WHEN** a Workflow is constructed with a cycle (A depends on B, B depends on A)
- **THEN** `workflow.Do` returns an `ErrCycleDependency` before any Step executes

#### Scenario: No cycle — execution proceeds
- **WHEN** the dependency graph is acyclic
- **THEN** preflight succeeds and execution begins normally

---

### Requirement: Concurrent execution via goroutines

Each runnable Step SHALL be executed in its own goroutine. The Workflow uses a
condition variable (`statusChange`) to drive a tick-based scheduler that starts
new goroutines whenever upstream Steps terminate.

#### Scenario: Each Step runs in its own goroutine
- **WHEN** multiple Steps become runnable at the same time
- **THEN** each is started in a separate goroutine without waiting for the others

#### Scenario: Workflow blocks until all Steps terminate
- **WHEN** `workflow.Do(ctx)` is called
- **THEN** it blocks the calling goroutine until every Step in the Workflow is terminated

---

### Requirement: Error aggregation via ErrWorkflow

When one or more Steps fail, the Workflow SHALL return an `ErrWorkflow` that maps each
root Step to its final `StepResult` (status + error). When all root Steps succeed (or
succeed/skip depending on `SkipAsError`), `Do` returns `nil`.

#### Scenario: All Steps succeed
- **WHEN** every root Step in the Workflow terminates with status `Succeeded`
- **THEN** `workflow.Do` returns `nil`

#### Scenario: One Step fails
- **WHEN** at least one root Step terminates with status `Failed`
- **THEN** `workflow.Do` returns an `ErrWorkflow` containing entries for all root Steps

#### Scenario: ErrWorkflow contains status for every root Step
- **WHEN** `workflow.Do` returns an `ErrWorkflow`
- **THEN** every root Step that was added to the Workflow appears as a key in the map

---

### Requirement: ErrWorkflow error output ordering

`ErrWorkflow.Error()` SHALL output steps sorted by `StepResult.FinishedAt` in ascending
order (earliest-finishing step first), so that the error message reflects the execution
timeline.

Steps with a zero `FinishedAt` (i.e., steps that never executed) SHALL appear last.

When two or more steps share an identical `FinishedAt` value, they SHALL be sorted
by their string name (`flow.String(step)`) in ascending lexicographic order to produce
a stable, deterministic output.

`ErrWorkflow.Unwrap()` SHALL return errors in the same sorted order.

#### Scenario: Single-step workflow failure output
- **WHEN** a workflow with one failed step produces `ErrWorkflow`
- **THEN** `ErrWorkflow.Error()` contains exactly that step's output

#### Scenario: Multi-step output is sorted by finish time
- **WHEN** steps A, B, C finish in that order (A earliest, C latest)
- **THEN** `ErrWorkflow.Error()` lists them A, B, C regardless of map iteration order

#### Scenario: Never-executed steps appear last
- **WHEN** some steps have zero `FinishedAt` (never ran) and others have non-zero timestamps
- **THEN** `ErrWorkflow.Error()` lists all non-zero-timestamp steps first, zero-timestamp steps last

#### Scenario: Tie-breaking by name
- **WHEN** two steps have identical `FinishedAt` values
- **THEN** `ErrWorkflow.Error()` lists them in ascending lexicographic order by step name

#### Scenario: Unwrap order matches Error order
- **WHEN** `ErrWorkflow.Unwrap()` is called
- **THEN** the returned error slice is in the same order as `ErrWorkflow.Error()` output

---

### Requirement: Empty Workflow is a no-op

A Workflow with no Steps SHALL return `nil` from `Do` immediately without blocking.

#### Scenario: Empty workflow
- **WHEN** `workflow.Do` is called on a Workflow with no Steps added
- **THEN** it returns `nil` immediately

---

### Requirement: Workflow cannot be run concurrently

A Workflow SHALL prevent concurrent calls to `Do`. If `Do` is called while it is
already running, it returns `ErrWorkflowIsRunning`.

#### Scenario: Concurrent Do calls rejected
- **WHEN** `workflow.Do` is called a second time before the first call returns
- **THEN** the second call returns `ErrWorkflowIsRunning` immediately

---

### Requirement: Reset prepares a Workflow for re-execution

After a Workflow has finished, `Reset()` SHALL set all Step statuses back to `Pending`
so the Workflow can be executed again. `Reset()` fails if the Workflow is currently running.

#### Scenario: Reset allows re-run
- **WHEN** `workflow.Reset()` is called after a completed run
- **THEN** all Steps return to `Pending` status and `workflow.Do` can be called again

#### Scenario: Reset rejected while running
- **WHEN** `workflow.Reset()` is called while `Do` is executing
- **THEN** `Reset()` returns `ErrWorkflowIsRunning`

---

### Requirement: StateOf nil-safety

`workflow.StateOf(nil)` SHALL return `nil`. `workflow.StateOf(unknownStep)` for a Step
that was never added to the Workflow SHALL also return `nil`.

#### Scenario: StateOf nil returns nil
- **WHEN** `workflow.StateOf(nil)` is called
- **THEN** it returns `nil` without panicking

#### Scenario: StateOf unknown step returns nil
- **WHEN** `workflow.StateOf(step)` is called for a Step not added to the Workflow
- **THEN** it returns `nil` without panicking

---

### Requirement: StepResult carries finish timestamp

`StepResult` SHALL include a `FinishedAt time.Time` field that records the moment
the step goroutine transitioned to a terminal status (`Succeeded`, `Failed`, `Canceled`,
or `Skipped`).

The timestamp SHALL be recorded using the Workflow's injected `clock.Clock`, so that
tests using a mock clock produce deterministic values.

Steps that are never executed (e.g., never transitioned to `Running`) SHALL have a
zero `FinishedAt` value.

#### Scenario: Succeeded step has FinishedAt set
- **WHEN** `step.Do(ctx)` returns `nil` and the step transitions to `Succeeded`
- **THEN** `StepResult.FinishedAt` is set to `clock.Now()` at the moment of transition

#### Scenario: Failed step has FinishedAt set
- **WHEN** `step.Do(ctx)` returns a non-nil error and the step transitions to `Failed`
- **THEN** `StepResult.FinishedAt` is set to `clock.Now()` at the moment of transition

#### Scenario: Canceled step has FinishedAt set
- **WHEN** a step is canceled and transitions to `Canceled`
- **THEN** `StepResult.FinishedAt` is set to `clock.Now()` at the moment of transition

#### Scenario: Skipped step has FinishedAt set
- **WHEN** a step's Condition evaluates to `Skipped` and the step never runs
- **THEN** `StepResult.FinishedAt` is set to `clock.Now()` at the moment of the skip transition

#### Scenario: Never-executed step has zero FinishedAt
- **WHEN** a step remains `Pending` at the end of workflow execution
- **THEN** `StepResult.FinishedAt` is the zero value of `time.Time`

#### Scenario: FinishedAt available in Condition functions
- **WHEN** a Condition function receives `map[Steper]StepResult` for upstream steps
- **THEN** `FinishedAt` is populated for all terminated upstream steps and available to the condition logic
