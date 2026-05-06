## Why

When a workflow fails, `ErrWorkflow.Error()` outputs steps in random order because `ErrWorkflow` is a `map[Steper]StepResult` and Go map iteration is non-deterministic. This makes failure traces hard to read and impossible to compare across runs, hindering debugging.

## What Changes

- Add `FinishedAt time.Time` field to `StepResult` to record when each step terminated.
- Record the finish timestamp (using the workflow's injected `clock.Clock`) in the step goroutine, just before signalling status change.
- `ErrWorkflow.Error()` sorts steps by `FinishedAt` ascending (steps that never ran sort last, then by name for stability).
- `ErrWorkflow.Unwrap()` returns errors in the same sorted order for consistency.

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `execution-model`: `StepResult` gains a `FinishedAt time.Time` field populated at step termination; `ErrWorkflow.Error()` and `Unwrap()` now produce output in execution-finish order instead of random map iteration order.

## Impact

- `StepResult` gains a new exported field — additive, not breaking for existing code that constructs or reads `StepResult` by field name. Code using `StepResult{Status: ..., Err: ...}` struct literals (without field names) would break at compile time, but that pattern is unlikely and easily fixed.
- `Condition` functions receive `map[Steper]StepResult` — the new field is available to condition authors at no extra cost.
- The workflow's `clock.Clock` field (already present) is used for timestamping, keeping tests deterministic.
- No new dependencies. No API removals.
