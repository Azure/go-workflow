## Context

`ErrWorkflow` is defined as `map[Steper]StepResult`. The `Error()` method iterates this map directly, producing non-deterministic output. `StepResult` currently holds only `Status StepStatus` and `Err error` — no timing information.

The workflow already has an injected `clock.Clock` field (from `github.com/benbjohnson/clock`) used for retry/timeout, so clock-based timestamping is available without new dependencies.

Step goroutines terminate via a shared `defer` block in `workflow.go` that calls `state.SetStatus(status)` and `state.SetError(err)` before signalling. This is the single canonical termination point — the right place to record `FinishedAt`.

## Goals / Non-Goals

**Goals:**
- `ErrWorkflow.Error()` produces deterministic, execution-finish-time-ordered output.
- `ErrWorkflow.Unwrap()` returns errors in the same order.
- `StepResult.FinishedAt` is populated for all terminated steps and available to `Condition` functions and external observers.
- Tests remain deterministic via the existing `clock.Clock` injection.

**Non-Goals:**
- Recording `StartedAt` — out of scope for this change.
- Changing the `ErrWorkflow` underlying type from `map` to an ordered structure — the map stays; sorting happens only at output time.
- Displaying the timestamp in `Error()` output — ordering is the goal, not showing timestamps to users.

## Decisions

### D1: Add `FinishedAt time.Time` to `StepResult` (not to `State` separately)

`StepResult` is the public snapshot type returned from `GetStepResult()`, passed into `Condition` functions, and embedded in `ErrWorkflow`. Adding the field here makes it available to all consumers with no additional API surface.

Alternative considered: add a separate `finishedAt` field to `State` only and use it just for sorting in `ErrWorkflow.Error()`. Rejected — this hides useful information from `Condition` authors and duplicates the timestamp concept.

### D2: Record timestamp at the step goroutine's defer, using `w.Clock.Now()`

The defer in the step goroutine is the single termination point for all outcomes (success, failure, cancel, panic). Recording `FinishedAt` there — alongside `SetStatus` and `SetError` — is atomic from the workflow's perspective and covers all code paths.

`w.Clock.Now()` keeps tests deterministic; tests using `clock.NewMock()` already control time for retry/timeout assertions.

### D3: Sort in `Error()` and `Unwrap()` by `FinishedAt` ascending; zero-time steps last, then by `String(step)` for stability

Steps that never executed (Skipped by condition before running, Pending) will have zero `FinishedAt`. They sort to the end. Among steps with the same timestamp (possible with mocked clocks or extremely fast steps), `String(step)` provides a stable secondary sort. This matches the mental model: "what ran first, then what failed."

Alternative: sort only in `Error()`, leave `Unwrap()` unordered. Rejected — consistency between the two methods avoids surprising behavior when callers use `errors.As`/`errors.Is` traversal order for logic.

### D4: No new exported helper to set `FinishedAt` — set it directly in the workflow goroutine

`State` already has unexported setters (`SetStatus`, `SetError`). We add `SetFinishedAt(t time.Time)` following the same pattern, called only from the workflow's internal goroutine. This keeps the mutation path narrow.

## Risks / Trade-offs

- **Struct literal breakage**: Any code constructing `StepResult{val1, val2}` positionally (without field names) will fail to compile after the new field is added. This is caught at compile time and is trivially fixable. It is unlikely in practice since `StepResult` is a library type.
- **Mock clock in condition tests**: `Condition` unit tests that construct `StepResult` manually (e.g., `condition_test.go`) will need to populate `FinishedAt` explicitly if they care about ordering. Tests that don't check `ErrWorkflow.Error()` output need no changes.
- **Tied to wall clock resolution**: On systems with low-resolution clocks, two steps terminating in the same tick will fall back to name-based ordering. This is acceptable — the output is still deterministic.

## Migration Plan

No migration needed. `FinishedAt` is an additive field. Existing `StepResult` values constructed by field name (`StepResult{Status: ..., Err: ...}`) get a zero `FinishedAt` and continue to compile. The sort in `Error()` is purely cosmetic — no behavioral changes to workflow execution.
