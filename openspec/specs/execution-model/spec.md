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
