## 1. Extend StepResult with FinishedAt

- [ ] 1.1 Add `FinishedAt time.Time` field to `StepResult` struct in `error.go`
- [ ] 1.2 Add `SetFinishedAt(t time.Time)` method to `State` in `state.go`, following the same pattern as `SetStatus`/`SetError`
- [ ] 1.3 Add `GetFinishedAt() time.Time` method to `State` if needed for symmetry

## 2. Record Timestamp at Step Termination

- [ ] 2.1 In the step goroutine defer block in `workflow.go`, call `state.SetFinishedAt(w.Clock.Now())` just before `state.SetStatus(status)` and `state.SetError(err)`
- [ ] 2.2 For condition-evaluated skips (where a step is skipped without running), ensure `SetFinishedAt` is also called at the point the skip status is assigned in `tick()`

## 3. Sort ErrWorkflow Output

- [ ] 3.1 Add `sort` import to `error.go`
- [ ] 3.2 Implement a helper `sortedSteps(e ErrWorkflow) []Steper` that returns steps sorted by `FinishedAt` ascending, zero-time last, tie-broken by `String(step)` lexicographically
- [ ] 3.3 Rewrite `ErrWorkflow.Error()` to use `sortedSteps` for iteration
- [ ] 3.4 Rewrite `ErrWorkflow.Unwrap()` to use `sortedSteps` for iteration

## 4. Tests

- [ ] 4.1 Add a test asserting `StepResult.FinishedAt` is populated after workflow execution (use `clock.NewMock()` and advance time between steps)
- [ ] 4.2 Add a test for `ErrWorkflow.Error()` output ordering: run a 3-step serial workflow, verify output is in execution order regardless of step name sort order
- [ ] 4.3 Add a test for tie-breaking: construct an `ErrWorkflow` with two steps sharing identical `FinishedAt`, verify alphabetical order in output
- [ ] 4.4 Add a test for zero-`FinishedAt` steps appearing last in output
- [ ] 4.5 Verify existing tests in `condition_test.go` that construct `StepResult` manually still compile and pass (update literals to use field names if needed)

## 5. Verify

- [ ] 5.1 Run `go build ./...` — no compile errors
- [ ] 5.2 Run `go test ./...` — all tests pass
- [ ] 5.3 Run `go vet ./...` — no issues
