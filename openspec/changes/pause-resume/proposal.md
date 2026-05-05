## Why

Currently a running workflow can only be stopped by canceling the context, which is destructive —
it cancels all in-flight steps and the workflow returns an error. There is no way to temporarily
pause execution (e.g., during a maintenance window, or when an operator notices something is wrong
and wants to inspect state before letting it continue) and then resume from exactly where it left off.

Temporal supports pausing a workflow execution; go-workflow should offer an equivalent that fits
the single-process model.

## What Changes

- A `Pause()` method on `Workflow` that suspends scheduling of new steps without canceling
  currently-running ones.
- A `Resume()` method that re-enables scheduling and wakes the tick loop.
- Running steps finish normally; paused state means "no new steps will be started until resumed".

## Proposed Design

### State machine addition

Add a `paused` flag (protected by the existing `statusChange` mutex):

```
Normal running:  paused = false  → tick() schedules all runnable steps
Paused:          paused = true   → tick() skips scheduling, only updates status
Resumed:         paused = false  → statusChange.Signal() wakes the loop
```

### API

```go
func (w *Workflow) Pause() error   // error if workflow is not running
func (w *Workflow) Resume() error  // error if workflow is not running or not paused
func (w *Workflow) IsPaused() bool
```

### Behavior

- `Pause()` sets the flag; in-flight step goroutines are NOT interrupted. They will finish
  naturally and update status, but the tick loop will not start new goroutines.
- `Resume()` clears the flag and signals `statusChange` so the tick loop wakes and schedules
  any steps that became runnable while paused.
- Calling `Pause()` on an already-paused workflow is a no-op (or returns a sentinel error —
  TBD based on what's more ergonomic).
- Context cancellation still works orthogonally: canceling the context while paused will
  cancel in-flight steps; once they drain, the workflow exits normally through the existing
  error path.

### Optional: Pause with drain

A `PauseAfterCurrent()` variant that also waits for all currently-running steps to finish
before fully pausing — useful if the caller wants a clean quiescence point before inspecting
state.

```go
func (w *Workflow) PauseAfterCurrent(ctx context.Context) error
```

This blocks until the running goroutine count reaches zero, then sets `paused = true`.

### Open Questions

- Should `Pause()` / `Resume()` be goroutine-safe (callable from any goroutine)? Yes —
  the only reasonable use case is calling them from outside the workflow goroutine.
- Should there be a `Paused` step status for steps that were Pending when Pause was called?
  Probably not — they remain `Pending`. The pause is a scheduler-level concept, not a
  step-level status.
- Should the `EventSink` emit a `WorkflowPaused` / `WorkflowResumed` event?
  Yes, if the event-sink feature lands.

## Impact

- `workflow.go` — add `paused bool` field (under `statusChange.L`), `Pause()`, `Resume()`,
  `IsPaused()` methods, guard in `tick()`.
- No changes to `Steper` interface or `StepConfig`.
- New spec: `openspec/specs/pause-resume/spec.md`.
- No breaking changes.
