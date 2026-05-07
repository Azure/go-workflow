# ErrWorkflow Execution Order Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ErrWorkflow.Error()` output steps in execution-finish order by adding a `FinishedAt time.Time` field to `StepResult` and sorting on output.

**Architecture:** Add `FinishedAt` to the public `StepResult` struct; record it via a new `State.SetFinishedAt` setter in the single step-goroutine defer that already calls `SetStatus`/`SetError`; for condition-skipped steps record it at the `state.SetStatus(nextStatus)` site in `tick()`; sort in `ErrWorkflow.Error()` and `Unwrap()` using a shared helper.

**Tech Stack:** Go stdlib (`sort`, `time`), `github.com/benbjohnson/clock` (already a project dependency, used for testable time)

---

## Files

| File | Change |
|------|--------|
| `error.go` | Add `FinishedAt time.Time` to `StepResult`; add `sort` import; add `sortedSteps` helper; rewrite `Error()` and `Unwrap()` |
| `state.go` | Add `SetFinishedAt(time.Time)` and `GetFinishedAt() time.Time` methods to `State` |
| `workflow.go` | Call `state.SetFinishedAt(w.Clock.Now())` in the step-goroutine defer and in the condition-skip branch of `tick()` |
| `error_test.go` | Add tests for `ErrWorkflow` ordering |
| `execution_model_test.go` | Add test that `StepResult.FinishedAt` is populated after execution |
| `condition_test.go` | Update `StepResult{...}` literals to use field names |

---

## Task 1: Add `FinishedAt` to `StepResult`

**Files:**
- Modify: `error.go` (around line 106 — the `StepResult` struct)

- [ ] **Step 1: Add the field**

  In `error.go`, update `StepResult`:

  ```go
  // StepResult contains the status and error of a Step.
  type StepResult struct {
  	Status     StepStatus
  	Err        error
  	FinishedAt time.Time
  }
  ```

  Also add `"time"` to the import block at the top of `error.go`:

  ```go
  import (
  	"fmt"
  	"runtime"
  	"sort"
  	"strings"
  	"time"
  )
  ```

- [ ] **Step 2: Verify it compiles**

  ```bash
  go build ./...
  ```

  Expected: compile error in `condition_test.go` about `StepResult` composite literal — **this is expected**, we'll fix it in Task 4.

---

## Task 2: Add `SetFinishedAt` / `GetFinishedAt` to `State`

**Files:**
- Modify: `state.go` (after `SetError`/`GetError` around line 33)

- [ ] **Step 1: Add setter and getter**

  In `state.go`, after the `SetError` method, add:

  ```go
  func (s *State) GetFinishedAt() time.Time {
  	s.RLock()
  	defer s.RUnlock()
  	return s.FinishedAt
  }
  func (s *State) SetFinishedAt(t time.Time) {
  	s.Lock()
  	defer s.Unlock()
  	s.FinishedAt = t
  }
  ```

  Add `"time"` to the import block in `state.go`:

  ```go
  import (
  	"context"
  	"sync"
  	"time"
  )
  ```

- [ ] **Step 2: Verify it compiles (ignoring test errors)**

  ```bash
  go build ./...
  ```

  Expected: same compile errors as before in tests only. Core package builds.

---

## Task 3: Record `FinishedAt` at Step Termination

**Files:**
- Modify: `workflow.go`

There are **two termination sites** to update:

### Site A — step goroutine defer (running steps)

Around line 402–406 in `workflow.go`:

```go
defer func() {
    state.SetStatus(status)
    state.SetError(err)
    w.unlease()
    w.signalStatusChange()
}()
```

### Site B — condition-skip in `tick()` (steps skipped before running)

Around line 387–393:

```go
if nextStatus := cond(ctx, ups); nextStatus.IsTerminated() {
    state.SetStatus(nextStatus)
    w.waitGroup.Add(1)
    go func() {
        defer w.waitGroup.Done()
        w.signalStatusChange()
    }()
    continue
}
```

- [ ] **Step 1: Update the step-goroutine defer (Site A)**

  Change the defer to record `FinishedAt` before setting status:

  ```go
  defer func() {
      state.SetFinishedAt(w.Clock.Now())
      state.SetStatus(status)
      state.SetError(err)
      w.unlease()
      w.signalStatusChange()
  }()
  ```

- [ ] **Step 2: Update the condition-skip branch (Site B)**

  Change the condition-skip block to also record `FinishedAt`:

  ```go
  if nextStatus := cond(ctx, ups); nextStatus.IsTerminated() {
      state.SetFinishedAt(w.Clock.Now())
      state.SetStatus(nextStatus)
      w.waitGroup.Add(1)
      go func() {
          defer w.waitGroup.Done()
          w.signalStatusChange()
      }()
      continue
  }
  ```

- [ ] **Step 3: Verify it compiles**

  ```bash
  go build ./...
  ```

  Expected: same test-only errors, core package builds.

---

## Task 4: Fix `condition_test.go` Struct Literal

**Files:**
- Modify: `condition_test.go` (around line 28)

The composite literal `flow.StepResult{Status: ..., Err: ...}` already uses field names, so it will compile without changes. Verify this:

- [ ] **Step 1: Check the literal**

  ```bash
  grep -n "StepResult{" condition_test.go
  ```

  Expected output:
  ```
  28:				ups[s] = flow.StepResult{
  ```

  The block at line 28–31 uses named fields (`Status:`, `Err:`), so it is fine. No edit needed.

- [ ] **Step 2: Verify tests compile**

  ```bash
  go test -run NOMATCH ./... 2>&1 | grep -v "^ok"
  ```

  Expected: no compile errors.

---

## Task 5: Implement `sortedSteps` and Rewrite `Error()` / `Unwrap()`

**Files:**
- Modify: `error.go`

- [ ] **Step 1: Write a failing test first**

  In `error_test.go`, add:

  ```go
  func TestErrWorkflowErrorOrdering(t *testing.T) {
  	t.Run("sorted by FinishedAt ascending", func(t *testing.T) {
  		now := time.Now()
  		type namedStep struct{ name string }
  		// We need actual Steper values; use the existing test helpers.
  		// Build ErrWorkflow manually with known FinishedAt values.
  		a := &namedStep{"A-step"}
  		b := &namedStep{"B-step"}
  		c := &namedStep{"C-step"}

  		// Use a real workflow so steps are proper Steper instances.
  		// Instead, we test via a real workflow run with mock clock.
  		mockClock := clock.NewMock()
  		w := &flow.Workflow{Clock: mockClock}

  		errA := fmt.Errorf("A failed")
  		errB := fmt.Errorf("B failed")
  		errC := fmt.Errorf("C failed")
  		_ = a; _ = b; _ = c; _ = errA; _ = errB; _ = errC; _ = now
  		// Real ordering test is in TestErrWorkflowOrderingIntegration below.
  		_ = w
  	})
  }
  ```

  > Actually, skip the manual struct test — the integration test below is more meaningful. Remove the stub above and add only the integration test.

  Replace the above with this in `error_test.go`:

  ```go
  import (
  	"context"
  	"fmt"
  	"strings"
  	"testing"
  	"time"

  	flow "github.com/Azure/go-workflow"
  	"github.com/benbjohnson/clock"
  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  // serialStep is a step that signals when it starts and waits to be released.
  type serialStep struct {
  	name    string
  	started chan struct{}
  	release chan struct{}
  	err     error
  }

  func newSerialStep(name string, err error) *serialStep {
  	return &serialStep{name: name, started: make(chan struct{}, 1), release: make(chan struct{}), err: err}
  }
  func (s *serialStep) Do(_ context.Context) error {
  	s.started <- struct{}{}
  	<-s.release
  	return s.err
  }
  func (s *serialStep) String() string { return s.name }

  func TestErrWorkflowOutputOrdering(t *testing.T) {
  	// Build a 3-step serial chain: A -> B -> C
  	// Step names are chosen so alphabetical != execution order: C, A, B
  	mockClock := clock.NewMock()
  	stepC := newSerialStep("C-first", fmt.Errorf("C failed"))
  	stepA := newSerialStep("A-second", fmt.Errorf("A failed"))
  	stepB := newSerialStep("B-third", fmt.Errorf("B failed"))

  	w := &flow.Workflow{Clock: mockClock}
  	w.Add(
  		flow.Step(stepC),
  		flow.Step(stepA).DependsOn(stepC),
  		flow.Step(stepB).DependsOn(stepA),
  	)

  	done := make(chan error, 1)
  	go func() { done <- w.Do(context.Background()) }()

  	// C runs first — let it finish
  	<-stepC.started
  	mockClock.Add(time.Second)
  	close(stepC.release)

  	// A runs second
  	<-stepA.started
  	mockClock.Add(time.Second)
  	close(stepA.release)

  	// B runs third
  	<-stepB.started
  	mockClock.Add(time.Second)
  	close(stepB.release)

  	err := <-done
  	require.Error(t, err)

  	var errW flow.ErrWorkflow
  	require.ErrorAs(t, err, &errW)

  	output := errW.Error()
  	posC := strings.Index(output, "C-first")
  	posA := strings.Index(output, "A-second")
  	posB := strings.Index(output, "B-third")

  	assert.Greater(t, posA, posC, "A-second should appear after C-first in output")
  	assert.Greater(t, posB, posA, "B-third should appear after A-second in output")
  }

  func TestErrWorkflowTieBreakByName(t *testing.T) {
  	// Two steps with identical FinishedAt → sort by name
  	mockClock := clock.NewMock()
  	now := mockClock.Now()

  	e := flow.ErrWorkflow{
  		// We can't easily construct Steper keys without running a workflow.
  		// Test via integration: two parallel steps finishing at same clock tick.
  	}
  	_ = e; _ = now
  	// See TestErrWorkflowTieBreakIntegration below.
  }

  func TestErrWorkflowTieBreakIntegration(t *testing.T) {
  	// Two parallel steps, both fail at the same clock tick → output is alphabetical.
  	mockClock := clock.NewMock()
  	stepZ := newSerialStep("Z-step", fmt.Errorf("Z failed"))
  	stepA := newSerialStep("A-step", fmt.Errorf("A failed"))

  	w := &flow.Workflow{Clock: mockClock}
  	w.Add(flow.Step(stepZ), flow.Step(stepA))

  	done := make(chan error, 1)
  	go func() { done <- w.Do(context.Background()) }()

  	// Both steps start in parallel; release them before advancing clock
  	<-stepZ.started
  	<-stepA.started
  	// Advance clock THEN release — both get same timestamp
  	mockClock.Add(time.Second)
  	close(stepZ.release)
  	close(stepA.release)

  	err := <-done
  	require.Error(t, err)

  	var errW flow.ErrWorkflow
  	require.ErrorAs(t, err, &errW)

  	output := errW.Error()
  	posA := strings.Index(output, "A-step")
  	posZ := strings.Index(output, "Z-step")
  	assert.Less(t, posA, posZ, "A-step should appear before Z-step (tie-break by name)")
  }
  ```

- [ ] **Step 2: Run test to verify it fails**

  ```bash
  go test -run "TestErrWorkflow" ./... -v 2>&1 | tail -20
  ```

  Expected: FAIL — output ordering assertions fail (map iteration is random).

- [ ] **Step 3: Implement `sortedSteps` and update `Error()` / `Unwrap()`**

  In `error.go`, add the helper and update both methods:

  ```go
  // sortedSteps returns the steps in ErrWorkflow sorted by FinishedAt ascending.
  // Steps with zero FinishedAt (never ran) sort last.
  // Tie-break: lexicographic order of String(step).
  func sortedSteps(e ErrWorkflow) []Steper {
  	steps := make([]Steper, 0, len(e))
  	for step := range e {
  		steps = append(steps, step)
  	}
  	sort.Slice(steps, func(i, j int) bool {
  		ti := e[steps[i]].FinishedAt
  		tj := e[steps[j]].FinishedAt
  		zeroI := ti.IsZero()
  		zeroJ := tj.IsZero()
  		if zeroI != zeroJ {
  			return !zeroI // non-zero before zero
  		}
  		if !ti.Equal(tj) {
  			return ti.Before(tj)
  		}
  		return String(steps[i]) < String(steps[j])
  	})
  	return steps
  }

  func (e ErrWorkflow) Unwrap() []error {
  	steps := sortedSteps(e)
  	rv := make([]error, 0, len(e))
  	for _, step := range steps {
  		rv = append(rv, e[step].Err)
  	}
  	return rv
  }

  // ErrWorkflow will be printed as:
  //
  //	Step: [Status]
  //		error message
  func (e ErrWorkflow) Error() string {
  	var builder strings.Builder
  	for _, step := range sortedSteps(e) {
  		builder.WriteString(fmt.Sprintf("%s: ", String(step)))
  		builder.WriteString(fmt.Sprintln(e[step].Error()))
  	}
  	return builder.String()
  }
  ```

- [ ] **Step 4: Run the ordering tests**

  ```bash
  go test -run "TestErrWorkflow" ./... -v 2>&1 | tail -30
  ```

  Expected: all pass.

- [ ] **Step 5: Commit**

  ```bash
  git add error.go state.go workflow.go error_test.go condition_test.go
  git commit -m "feat: add FinishedAt to StepResult, sort ErrWorkflow output by execution order"
  ```

---

## Task 6: Test `FinishedAt` Population in `execution_model_test.go`

**Files:**
- Modify: `execution_model_test.go`

- [ ] **Step 1: Write failing test**

  Add to `execution_model_test.go`:

  ```go
  func TestStepResultFinishedAtPopulated(t *testing.T) {
  	mockClock := clock.NewMock()
  	step := &succeededStep{}  // uses the existing test helper in testutil_test.go
  	w := &Workflow{Clock: mockClock}
  	w.Add(Step(step))

  	mockClock.Add(time.Second) // advance so FinishedAt is non-zero
  	err := w.Do(context.Background())
  	assert.NoError(t, err)

  	state := w.StateOf(step)
  	result := state.GetStepResult()
  	assert.False(t, result.FinishedAt.IsZero(), "FinishedAt should be populated after step execution")
  	assert.Equal(t, mockClock.Now(), result.FinishedAt)
  }
  ```

  Check what helpers exist in `testutil_test.go`:

  ```bash
  grep -n "succeededStep\|failedStep\|type.*Step" testutil_test.go | head -20
  ```

  Adjust the step type name based on what you see.

- [ ] **Step 2: Run to verify it fails**

  ```bash
  go test -run "TestStepResultFinishedAtPopulated" ./... -v
  ```

  Expected: FAIL — `FinishedAt` is zero because we haven't wired it up yet.

  > **Note:** If Task 3 is already done, this test may already pass. In that case, skip to Step 4.

- [ ] **Step 3: Verify wiring from Task 3 makes it pass**

  The `SetFinishedAt` calls added in Task 3 should make this pass. Run:

  ```bash
  go test -run "TestStepResultFinishedAtPopulated" ./... -v
  ```

  Expected: PASS.

- [ ] **Step 4: Commit**

  ```bash
  git add execution_model_test.go
  git commit -m "test: verify StepResult.FinishedAt is populated after step execution"
  ```

---

## Task 7: Full Test Suite and Final Verification

- [ ] **Step 1: Run all tests**

  ```bash
  go test ./... -count=1
  ```

  Expected: all pass, no failures.

- [ ] **Step 2: Run vet**

  ```bash
  go vet ./...
  ```

  Expected: no output (no issues).

- [ ] **Step 3: Run build**

  ```bash
  go build ./...
  ```

  Expected: no output (clean build).

- [ ] **Step 4: Final commit if anything was adjusted**

  ```bash
  git status
  ```

  If there are uncommitted changes:

  ```bash
  git add -p
  git commit -m "fix: address review feedback"
  ```

---

## Self-Review Notes

- **`condition_test.go`**: The literal at line 28 already uses named fields (`Status:`, `Err:`), so it compiles without edits. The plan reflects this (Task 4 is a verify-only step).
- **Clock timing in tests**: `clock.NewMock()` starts at a fixed non-zero time, so `mockClock.Now()` after `Add(time.Second)` gives a consistent value. The test in Task 6 advances before the run — but `FinishedAt` is recorded *during* the run at whatever `Clock.Now()` returns then. Adjust the assertion to `assert.False(t, result.FinishedAt.IsZero())` if the exact value is hard to pin.
- **Parallel steps tie-break test**: both goroutines call `w.Clock.Now()` in their defers. With `clock.Mock`, concurrent calls return the same value, which is exactly what we need for the tie-break test.
- **`StateOf` visibility**: `w.StateOf(step)` is used in existing tests — it's an exported method so it's accessible from `_test` package.
