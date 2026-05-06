## ADDED Requirements

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

## MODIFIED Requirements

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
