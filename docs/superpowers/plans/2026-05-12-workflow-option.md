# Workflow Option Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consolidate `Workflow`'s nine top-level configuration fields into a single `WorkflowOption` struct, unify Mutator/Interceptor propagation under one `WorkflowOptionReceiver` interface, deprecate `SubWorkflow`, and make every scalar configuration option (including `DontPanic`, `MaxConcurrency`) cleanly inheritable from parent to sub-workflow.

**Architecture:** Add `workflow_option.go` containing `WorkflowOption` (pointer scalars + slice fields + `DontInherit bool`) and the `WorkflowOptionReceiver { InheritOption(parent WorkflowOption) }` interface. `Workflow` exposes one named field `Option WorkflowOption` and implements the receiver; merge rules are scalar-nil-inherits / slices-prepended / `DontInherit`-no-op. The scheduler propagates Option once per child in the parent's `Do()` prologue via a new `findOptionReceiver` walker, replacing the two ad-hoc dispatch sites for Mutators and Interceptors. `Do()` snapshots `w.Option` and restores via defer, eliminating the `inheritedStep`/`inheritedAttempt` side fields. `SubWorkflow` is deprecated but kept for one release; old receiver interfaces and `Prepend*` methods are removed.

**Tech Stack:** Go 1.22+ (using generics), `github.com/benbjohnson/clock`, `github.com/stretchr/testify`.

**Spec references:**
- Brainstorming: `docs/superpowers/specs/2026-05-12-workflow-option-design.md`
- OpenSpec change: `openspec/changes/2026-05-12-workflow-option/`

---

## File Layout

| File | Role | Action |
|---|---|---|
| `workflow_option.go` | New: `WorkflowOption` struct, `WorkflowOptionReceiver` interface, `findOptionReceiver`, `prependSlice` helper | Create |
| `workflow.go` | Remove 9 top-level fields + `inheritedStep`/`inheritedAttempt` + `PrependMutators`/`PrependInterceptors`. Add `Option WorkflowOption`, `InheritOption`. Rewrite `Do()` prologue (snapshot/restore + propagation pass). Rewrite scalar reads via accessor helpers. Update `Reset()` godoc. Update `SubWorkflow` (deprecated; new `InheritOption`; remove `PrependMutators`/`PrependInterceptors`). Remove Mutator dispatch at `:641` and Interceptor dispatch at `:768`. Simplify `effective*Interceptors`. | Modify |
| `interceptor.go` | Remove `InterceptorReceiver`, `findInterceptorReceiver`. Update godoc on `StepInterceptor` to reference `Option.StepInterceptors`. | Modify |
| `mutator.go` | Remove `MutatorReceiver`. Update godoc referencing it. | Modify |
| `workflow_option_test.go` | New: table-driven `InheritOption` matrix, multi-level nesting, snapshot/restore, scalar-inheritance behavior change (parent DontPanic flows to child), Reset contract. | Create |
| `workflow_options_test.go` | Migrate every `&Workflow{Field: ...}` literal to `Option: WorkflowOption{...}` with pointer wrappers. | Modify |
| `workflow_mutator_test.go` | Migrate `w.Mutators = ...` → `w.Option.Mutators = ...`. Rewrite `PrependMutators` exercises to use `InheritOption`. | Modify |
| `workflow_test.go`, `branch_test.go`, `wrap_test.go`, `execution_model_test.go`, `step_configuration_test.go`, `retry_test.go`, `condition_test.go`, `noop_test.go`, `mutator_test.go`, `error_test.go`, `name_test.go`, `testutil_test.go` | Migrate any references to the removed fields/interfaces. | Modify as needed |
| `example/*.go` | Migrate examples that use the nine former fields, `SubWorkflow`, or the removed interfaces. | Modify |
| `openspec/specs/workflow-options/spec.md`, `openspec/specs/composite-steps/spec.md`, `openspec/specs/mutators/spec.md`, `openspec/specs/step-interceptor/spec.md` | Apply the MODIFIED / ADDED / REMOVED requirements from the change's spec deltas. | Modify |

---

## Approach Notes

**Order of work:** add new types first (so the rest can reference them), then rewrite `Workflow` internals (preserve behavior under the new shape), then propagation (delete two old dispatch sites, add one new one), then removals, then `SubWorkflow` deprecation, then test/example/spec migration, then verification.

**TDD strategy:** for the *new* behaviors (`InheritOption` merge rules, snapshot/restore, scalar inheritance) we write failing tests first. For the *mechanical migration* (renaming `w.MaxConcurrency` → `w.Option.MaxConcurrency` etc.) we don't add new tests — the existing test suite is the regression net; we run `go test ./...` after each task to confirm green.

**Commit cadence:** one commit per task. Each commit must leave the tree compiling (or, where unavoidable mid-refactor, clearly note the staging point so the next task fixes it). For the big "remove old fields" step we batch the move + accessor rewrite + test migration into one commit because the tree wouldn't compile otherwise.

---

## Task 1: Add WorkflowOption types and helpers (no wiring yet)

**Files:**
- Create: `workflow_option.go`
- Test: `workflow_option_test.go`

This task introduces the new types in isolation. They are not yet referenced from `Workflow`, so the existing code continues to compile and pass tests.

- [ ] **Step 1.1: Write the failing test for `prependSlice`**

Create `workflow_option_test.go`:

```go
package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrependSlice(t *testing.T) {
	t.Run("parent nil returns child slice unchanged-shape", func(t *testing.T) {
		child := []int{1, 2}
		got := prependSlice[int](nil, child)
		assert.Equal(t, []int{1, 2}, got)
	})

	t.Run("child nil returns parent-prepended fresh slice", func(t *testing.T) {
		parent := []int{1, 2}
		got := prependSlice[int](parent, nil)
		assert.Equal(t, []int{1, 2}, got)
	})

	t.Run("both populated: parent prepended to child", func(t *testing.T) {
		parent := []int{1, 2}
		child := []int{3, 4}
		got := prependSlice[int](parent, child)
		assert.Equal(t, []int{1, 2, 3, 4}, got)
	})

	t.Run("does not mutate parent or child", func(t *testing.T) {
		parent := []int{1, 2}
		child := []int{3, 4}
		_ = prependSlice[int](parent, child)
		assert.Equal(t, []int{1, 2}, parent, "parent must not be mutated")
		assert.Equal(t, []int{3, 4}, child, "child must not be mutated")
	})

	t.Run("returned slice has a fresh backing array when both non-empty", func(t *testing.T) {
		parent := []int{1, 2}
		child := []int{3, 4}
		got := prependSlice[int](parent, child)
		// Mutating got must not affect parent or child.
		got[0] = 99
		assert.Equal(t, []int{1, 2}, parent)
		assert.Equal(t, []int{3, 4}, child)
	})
}
```

- [ ] **Step 1.2: Run the test, verify it fails**

```bash
go test ./... -run TestPrependSlice -v
```

Expected: build failure (`prependSlice` undefined).

- [ ] **Step 1.3: Create `workflow_option.go` with `prependSlice` plus the types**

```go
package flow

import "github.com/benbjohnson/clock"

// WorkflowOption groups all configuration that a Workflow exposes to its
// caller AND inherits from a parent Workflow when used as a sub-workflow step.
//
// Scalar fields are pointers so that "unset" (nil) and "explicit zero value"
// are distinguishable. On parent → child Option inheritance, a nil pointer on
// the child means "inherit from parent (or use the runtime default)"; a
// non-nil pointer is the child's own choice and wins over the parent.
//
// Slice fields are not pointer-typed; on inheritance, the parent's slice is
// prepended to the child's slice (parent contributions run first), preserving
// the existing Mutator and Interceptor propagation semantics.
//
// Mutating a Workflow's Option after Do() has started is undefined behavior.
type WorkflowOption struct {
	// MaxConcurrency caps simultaneously-running Steps. nil or 0 means
	// unlimited; a positive value installs a buffered-channel lease bucket.
	MaxConcurrency *int

	// DontPanic, if non-nil and true, recovers panics in Step Do / Input /
	// BeforeStep / AfterStep callbacks and surfaces them as ErrPanic.
	DontPanic *bool

	// SkipAsError, if non-nil and true, counts Skipped terminal status as a
	// workflow failure (so Do returns ErrWorkflow even if no Step actually
	// failed).
	SkipAsError *bool

	// Clock is the time source used for Step timeouts, per-try timeouts in
	// the retry loop, and backoff waits. nil means real wall clock
	// (clock.New()). Inject a clock.Mock in tests to control time.
	Clock clock.Clock

	// StepDefaults, if non-nil, is prepended as the FIRST option to every
	// Step's Option list as a baseline. Per-step Option calls (Retry,
	// Timeout, When, …) still win over it.
	StepDefaults *StepOption

	// Mutators is the workflow-level list of cross-cutting step Mutators.
	// On inheritance, the parent's Mutators are prepended (parent contributions
	// run first within the child).
	Mutators []Mutator

	// StepInterceptors wraps each Step's full lifetime (across retries).
	// On inheritance, the parent's slice is prepended to the child's.
	StepInterceptors []StepInterceptor

	// AttemptInterceptors wraps each individual attempt (Before → Do → After).
	// On inheritance, the parent's slice is prepended to the child's.
	AttemptInterceptors []AttemptInterceptor

	// DontInherit, when true on a sub-workflow Workflow, makes InheritOption
	// a no-op: nothing flows in from the parent. Replaces the previous
	// IsolateInterceptors flag and now governs the entire WorkflowOption,
	// not just interceptors. Naming aligns with DontPanic.
	DontInherit bool
}

// WorkflowOptionReceiver is implemented by any Step that contains a
// sub-workflow. The parent's Do() prologue locates the nearest receiver in
// each root step's Unwrap chain and calls InheritOption ONCE before any
// scheduling begins, so the child's Do() observes the merged Option.
//
// *Workflow itself implements this interface; users get inheritance for
// free by embedding flow.Workflow in their own Step type.
type WorkflowOptionReceiver interface {
	InheritOption(parent WorkflowOption)
}

// prependSlice returns a fresh slice equal to parent ++ child. It MUST NOT
// mutate either input. The fresh backing array is what allows callers to
// snapshot-and-restore a WorkflowOption with a shallow copy: parent and
// child slice headers retain their original backing arrays.
func prependSlice[T any](parent, child []T) []T {
	if len(parent) == 0 {
		// Return a copy so callers can mutate without affecting child.
		// But for the no-prepend case it's safe to alias; callers don't
		// rely on freshness when parent contributes nothing. Choose copy
		// for uniformity.
		if len(child) == 0 {
			return nil
		}
		out := make([]T, len(child))
		copy(out, child)
		return out
	}
	if len(child) == 0 {
		out := make([]T, len(parent))
		copy(out, parent)
		return out
	}
	out := make([]T, 0, len(parent)+len(child))
	out = append(out, parent...)
	out = append(out, child...)
	return out
}

// findOptionReceiver returns the first WorkflowOptionReceiver in the Step
// tree rooted at s, walking via Unwrap in pre-order. Returns nil if none of
// the unwrapped Steps satisfies WorkflowOptionReceiver.
//
// This lets a sub-workflow be wrapped in a Steper-only wrapper (e.g.
// NamedStep, which embeds the Steper interface and therefore does not
// promote InheritOption) without losing parent-Option inheritance.
func findOptionReceiver(s Steper) WorkflowOptionReceiver {
	var found WorkflowOptionReceiver
	Traverse(s, func(s Steper, _ []Steper) TraverseDecision {
		if r, ok := s.(WorkflowOptionReceiver); ok {
			found = r
			return TraverseStop
		}
		return TraverseContinue
	})
	return found
}
```

- [ ] **Step 1.4: Run the prependSlice tests, verify pass**

```bash
go test ./... -run TestPrependSlice -v
```

Expected: PASS (5 sub-tests).

- [ ] **Step 1.5: Verify the full suite still builds and passes**

```bash
go build ./... && go test ./...
```

Expected: PASS. (We have added new types but not wired them anywhere.)

- [ ] **Step 1.6: Commit**

```bash
git add workflow_option.go workflow_option_test.go
git commit -m "feat: add WorkflowOption, WorkflowOptionReceiver, prependSlice (unwired)"
```

---

## Task 2: Wire WorkflowOption into Workflow struct (with accessor helpers)

**Files:**
- Modify: `workflow.go`

This is the big breaking-shape change. After this task: `Workflow` has `Option WorkflowOption` and the nine top-level fields are gone; all internal reads go through unexported accessors. The tree won't compile in the middle of this task — tests and examples are migrated in Tasks 3-7.

- [ ] **Step 2.1: In `workflow.go`, replace the `Workflow` struct definition**

Replace lines roughly 45-92 (the current `type Workflow struct { ... }`) with:

```go
// Workflow orchestrates a collection of Steps connected by dependency edges
// into a Directed Acyclic Graph (DAG).
//
// You declare the graph with the helpers in step.go: Step / Steps / Pipe /
// BatchPipe (and branching helpers If / Switch from branch.go), then hand
// them to Workflow.Add:
//
//	workflow.Add(
//	    Step(a),
//	    Steps(b, c).DependsOn(a),    // a runs first, then b and c in parallel.
//	    Pipe(d, e, f),               // d -> e -> f.
//	    BatchPipe(
//	        Steps(g, h),
//	        Steps(i, j),
//	    ),                           // g, h finish, then i, j run in parallel.
//	)
//
// Workflow.Do executes the graph in topological order. Each step that becomes
// runnable runs in its own goroutine, with the following guarantee:
//
//	When a step's worker goroutine starts, every upstream step is already
//	in a terminal status (Succeeded / Failed / Canceled / Skipped). The
//	step's Condition then decides whether it actually runs (Running) or is
//	settled inline as Skipped / Canceled.
//
// All workflow-level configuration lives in [WorkflowOption], exposed via
// Workflow.Option. See [WorkflowOption] for the available fields and the
// parent → child inheritance rules used when a Workflow is run as a step
// inside another Workflow.
//
// Per-step configuration: use Step / Steps / Pipe (see step.go).
// Composite steps:        use Has / As / HasStep (see wrap.go).
type Workflow struct {
	// Option groups all workflow-level configuration (concurrency cap,
	// panic policy, skip-as-error, clock, default step options, mutators,
	// interceptors, and inheritance opt-out). See [WorkflowOption].
	Option WorkflowOption

	StepBuilder // embeds the BuildStep memo so Workflow.Add can call BuildStep on new steps once.

	steps map[Steper]*State // root step → its State (status + StepConfig).

	statusChange *sync.Cond     // signals to the tick loop when a worker terminates.
	leaseBucket  chan struct{}  // bounded-channel "permit pool" enforcing Option.MaxConcurrency; nil means unlimited.
	waitGroup    sync.WaitGroup // tracks worker goroutines so Do() can wait for them on exit.
	isRunning    sync.Mutex     // single-runner guard: TryLock fails fast if Do/Reset is re-entered.
}
```

Notes:
- The `inheritedStep` / `inheritedAttempt` fields are GONE — snapshot/restore in `Do()` (Task 4) replaces them.
- The long comment block about `inheritedStep` lifecycle is also GONE.

- [ ] **Step 2.2: Add unexported scalar accessors near the top of `workflow.go`**

Add directly after the `Workflow` struct (or just before `Add`):

```go
// Scalar accessors: handle nil-pointer dereference and runtime defaults.
// All in-code reads of these scalars MUST go through these accessors.

func (w *Workflow) maxConcurrency() int {
	if w.Option.MaxConcurrency == nil {
		return 0
	}
	return *w.Option.MaxConcurrency
}

func (w *Workflow) dontPanic() bool {
	return w.Option.DontPanic != nil && *w.Option.DontPanic
}

func (w *Workflow) skipAsError() bool {
	return w.Option.SkipAsError != nil && *w.Option.SkipAsError
}

func (w *Workflow) clock() clock.Clock {
	if w.Option.Clock == nil {
		return clock.New()
	}
	return w.Option.Clock
}
```

- [ ] **Step 2.3: In `Workflow.Add` (around line 99-118), rewrite the `DefaultOption` read**

Find:
```go
				if w.DefaultOption != nil && config != nil {
					config.Option = slices.Insert(config.Option, 0, func(o *StepOption) {
						*o = *w.DefaultOption
					})
				}
```

Replace with:
```go
				if w.Option.StepDefaults != nil && config != nil {
					config.Option = slices.Insert(config.Option, 0, func(o *StepOption) {
						*o = *w.Option.StepDefaults
					})
				}
```

Also update the comment above `Add` (around line 99-100):
```
// If Option.StepDefaults is set, it is prepended to every step's Option
// list as a SEED — so per-step Option calls (Retry, Timeout, When, …) still
// win.
```

- [ ] **Step 2.4: Remove `Workflow.PrependMutators` method**

Delete lines roughly 120-129 (the entire `// PrependMutators inserts mw at the front …` doc + the function body).

- [ ] **Step 2.5: In `applyMutators`, rewrite reads of `w.Mutators` and `w.DontPanic`**

Find (around line 184-193):
```go
	if len(w.Mutators) == 0 {
		return
	}

	if w.DontPanic {
```
… and the `for _, m := range w.Mutators {`.

Replace with:
```go
	if len(w.Option.Mutators) == 0 {
		return
	}

	if w.dontPanic() {
```
… and `for _, m := range w.Option.Mutators {`.

- [ ] **Step 2.6: Rewrite `Workflow.reset()` (around line 363-384)**

Find:
```go
func (w *Workflow) reset() {
	for _, state := range w.steps {
		state.SetStepResult(StepResult{Status: Pending})
	}
	if w.Clock == nil {
		w.Clock = clock.New()
	}
	w.statusChange = sync.NewCond(&sync.Mutex{})
	if w.MaxConcurrency > 0 {
		w.leaseBucket = make(chan struct{}, w.MaxConcurrency)
	} else {
		w.leaseBucket = nil
	}
}
```

Replace with:
```go
// reset is the per-Do internal reset: clear all step results back to Pending,
// install a fresh statusChange Cond, and re-allocate the concurrency lease
// bucket sized for Option.MaxConcurrency.
//
// reset does NOT touch w.Option: parent → child Option inheritance is
// preserved by the snapshot/restore in Do() (see Workflow.Do).
func (w *Workflow) reset() {
	for _, state := range w.steps {
		state.SetStepResult(StepResult{Status: Pending})
	}
	w.statusChange = sync.NewCond(&sync.Mutex{})
	if mc := w.maxConcurrency(); mc > 0 {
		w.leaseBucket = make(chan struct{}, mc)
	} else {
		w.leaseBucket = nil
	}
}
```

Note: we no longer mutate `w.Option.Clock`; nil-handling is done by `w.clock()`. The line `if w.Clock == nil { w.Clock = clock.New() }` was destructive (it wrote to the user-supplied struct); the new accessor pattern is non-destructive.

- [ ] **Step 2.7: Rewrite `Workflow.Reset()` (around line 352-361)**

Find:
```go
// Reset prepares the Workflow for a fresh run from outside (the user's POV).
// It rejects with ErrWorkflowIsRunning if a Do call is currently in flight.
//
// Difference vs the internal reset(): Reset() ALSO clears the inherited
// interceptor slices set by a parent during a previous run. The internal
// reset() must NOT clear them — see the inheritedStep / inheritedAttempt
// lifecycle docs above.
func (w *Workflow) Reset() error {
	if !w.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer w.isRunning.Unlock()
	w.reset()
	w.inheritedStep = nil
	w.inheritedAttempt = nil
	return nil
}
```

Replace with:
```go
// Reset rewinds every Step's status back to Pending so the Workflow can be
// Do()-ed again. Reset rejects with ErrWorkflowIsRunning if a Do call is
// currently in flight.
//
// Reset does NOT modify w.steps (the set of Steps registered via Add) — a
// Workflow built once can be Do()-ed any number of times via Reset/Do
// cycles, with the same DAG each time. To start from an empty set of Steps,
// allocate a new Workflow.
//
// Reset does NOT modify w.Option either. Cross-run accumulation of
// parent-inherited contributions is prevented by the snapshot/restore in
// Do(), not by Reset. Calling Reset between runs is therefore optional from
// an Option-isolation standpoint; its purpose is purely to rewind per-step
// status for re-execution.
func (w *Workflow) Reset() error {
	if !w.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer w.isRunning.Unlock()
	w.reset()
	return nil
}
```

- [ ] **Step 2.8: Replace `Workflow.PrependInterceptors` with `InheritOption`**

Find lines roughly 386-411 (the entire `// PrependInterceptors implements InterceptorReceiver …` doc + function).

Replace with:
```go
// InheritOption implements [WorkflowOptionReceiver]. A parent Workflow calls
// this method on each sub-workflow root step's first receiver (located via
// findOptionReceiver) ONCE in the parent's Do() prologue.
//
// Merge rules:
//   - if w.Option.DontInherit is true, this is a no-op;
//   - for each scalar pointer (MaxConcurrency, DontPanic, SkipAsError) and
//     interface/pointer (Clock, StepDefaults) field: if the child's field is
//     nil, the parent's value is copied in; non-nil child fields are
//     preserved;
//   - for each slice (Mutators, StepInterceptors, AttemptInterceptors): a
//     fresh slice equal to parent ++ child replaces the child's field.
//
// w.Option mutations made here are reverted at the end of Do() via a
// snapshot/restore, so multiple Do() invocations do not accumulate parent
// contributions.
func (w *Workflow) InheritOption(parent WorkflowOption) {
	if w.Option.DontInherit {
		return
	}
	if w.Option.MaxConcurrency == nil {
		w.Option.MaxConcurrency = parent.MaxConcurrency
	}
	if w.Option.DontPanic == nil {
		w.Option.DontPanic = parent.DontPanic
	}
	if w.Option.SkipAsError == nil {
		w.Option.SkipAsError = parent.SkipAsError
	}
	if w.Option.Clock == nil {
		w.Option.Clock = parent.Clock
	}
	if w.Option.StepDefaults == nil {
		w.Option.StepDefaults = parent.StepDefaults
	}
	w.Option.Mutators = prependSlice(parent.Mutators, w.Option.Mutators)
	w.Option.StepInterceptors = prependSlice(parent.StepInterceptors, w.Option.StepInterceptors)
	w.Option.AttemptInterceptors = prependSlice(parent.AttemptInterceptors, w.Option.AttemptInterceptors)
}
```

- [ ] **Step 2.9: Rewrite `effectiveStepInterceptors` / `effectiveAttemptInterceptors` (lines ~413-437)**

Replace BOTH with:
```go
// effectiveStepInterceptors returns the chain to invoke for THIS run. With
// parent → child Option inheritance now performed eagerly in Do()'s prologue
// (writing into w.Option.StepInterceptors directly), the effective chain IS
// simply w.Option.StepInterceptors.
func (w *Workflow) effectiveStepInterceptors() []StepInterceptor {
	return w.Option.StepInterceptors
}

// effectiveAttemptInterceptors mirrors effectiveStepInterceptors for AttemptInterceptors.
func (w *Workflow) effectiveAttemptInterceptors() []AttemptInterceptor {
	return w.Option.AttemptInterceptors
}
```

- [ ] **Step 2.10: Rewrite the scalar reads inside the scheduling/execution code**

Find these reads and update each one:

| Line (approx) | Old | New |
|---|---|---|
| 498 | `if w.SkipAsError && err.AllSucceeded() {` | `if w.skipAsError() && err.AllSucceeded() {` |
| 501 | `if !w.SkipAsError && err.AllSucceededOrSkipped() {` | `if !w.skipAsError() && err.AllSucceededOrSkipped() {` |
| 652 | `FinishedAt: w.Clock.Now(),` | `FinishedAt: w.clock().Now(),` |
| 668 | `FinishedAt: w.Clock.Now(),` | `FinishedAt: w.clock().Now(),` |
| 724 | `if ex.w.DontPanic {` | `if ex.w.dontPanic() {` |
| 751 | `FinishedAt: ex.w.Clock.Now(),` | `FinishedAt: ex.w.clock().Now(),` |
| 776 | `notAfter = ex.w.Clock.Now().Add(*option.Timeout)` | `notAfter = ex.w.clock().Now().Add(*option.Timeout)` |
| 778 | `ctx, cancel = ex.w.Clock.WithDeadline(ctx, notAfter)` | `ctx, cancel = ex.w.clock().WithDeadline(ctx, notAfter)` |
| 800 | `if ex.w.DontPanic {` | `if ex.w.dontPanic() {` |
| 824 | `if ex.w.DontPanic {` | `if ex.w.dontPanic() {` |

Note: `clock()` returns a fresh `clock.New()` on every call when nil. To avoid re-allocating, capture once per use site if performance matters. For correctness this is fine since `clock.New()` returns a value-typed wrapper.

Also update the godoc comments that mention `DontPanic` / `MaxConcurrency` / `Clock` field names — these should now reference `Option.DontPanic` / `Option.MaxConcurrency` / `Option.Clock`. Specifically:
- Comment on line ~365 ("MaxConcurrency") → `Option.MaxConcurrency`
- Comment on line ~634 ("recover when DontPanic is set") → `recover when Option.DontPanic is set`
- Comment on line ~711 ("When DontPanic is true") → `When Option.DontPanic is true`
- Comment on line ~817 ("when DontPanic is true") → `when Option.DontPanic is true`
- Comment on line ~844 ("MaxConcurrency is unset") → `Option.MaxConcurrency is unset`
- Comment on line ~859 ("MaxConcurrency is unset") → `Option.MaxConcurrency is unset`

- [ ] **Step 2.11: Save and verify it does not compile yet (expected)**

```bash
go build ./...
```

Expected: build failure. There are still references to the old fields from:
- The Mutator dispatch site at `:641` (handled in Task 3).
- The Interceptor dispatch site at `:768` (handled in Task 3).
- `SubWorkflow.PrependMutators` / `PrependInterceptors` (handled in Task 5).
- Test files (handled in Tasks 6-7).

DO NOT commit yet — continue to Task 3.

---

## Task 3: Rewrite propagation in Do() prologue (replace two dispatch sites)

**Files:**
- Modify: `workflow.go`

After this task, the Mutator-at-`:641` and Interceptor-at-`:768` dispatch sites are gone, replaced by a single prologue pass that calls `InheritOption` once per receiver. The build still fails because `interceptor.go` / `mutator.go` / `SubWorkflow` / tests still reference old symbols — fixed in Tasks 4-7.

- [ ] **Step 3.1: Locate and remove the Mutator dispatch site**

In the scheduling code around line 638-643, find:

```go
			// Apply Mutators exactly once per step, before reading Option /
			// Before / After. ...
			if !state.MutatorsApplied {
				if recv, ok := step.(MutatorReceiver); ok && len(w.Mutators) > 0 {
					recv.PrependMutators(w.Mutators)
				}
				w.applyMutators(ctx, step)
				...
```

Remove only the `if recv, ok := step.(MutatorReceiver); ok ... { recv.PrependMutators(w.Mutators) }` block. KEEP `w.applyMutators(ctx, step)` and the `state.MutatorsApplied = true` mechanics — those are still needed for the local-mutator dispatch. The "propagate to child" job is now done by the prologue pass via `InheritOption`.

Result (approximate):

```go
			if !state.MutatorsApplied {
				w.applyMutators(ctx, step)
				...
```

- [ ] **Step 3.2: Locate and remove the Interceptor dispatch site**

Around line 767-770, find:

```go
	if recv := findInterceptorReceiver(ex.step); recv != nil {
		recv.PrependInterceptors(ex.w.effectiveStepInterceptors(), ex.w.effectiveAttemptInterceptors())
	}
```

Delete those 3 lines. The job is now done by the prologue pass.

- [ ] **Step 3.3: Rewrite `Workflow.Do()` prologue with snapshot + propagation pass**

Find the existing `Workflow.Do()` (around line 450-505) and replace with:

```go
func (w *Workflow) Do(ctx context.Context) error {
	// Single-runner guard.
	if !w.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer w.isRunning.Unlock()

	// Snapshot Option so any InheritOption writes performed below (and
	// transitively by nested workflows during their own Do() prologue) are
	// reverted at the end of THIS Do() call. The snapshot is a shallow copy;
	// this is correct because:
	//   - InheritOption only overwrites nil pointer fields with the parent's
	//     pointer values (not mutating the parent's targets), and
	//   - prependSlice always allocates a fresh slice, so neither the
	//     snapshot's slice header nor the parent's slice is mutated.
	optSnapshot := w.Option
	defer func() { w.Option = optSnapshot }()

	// Nothing to do.
	if w.Empty() {
		return nil
	}

	w.reset()

	// Reject cycles before launching any work.
	if err := w.preflight(); err != nil {
		return err
	}

	// Propagate w.Option into every sub-workflow root step exactly once,
	// BEFORE the tick loop dispatches anything. Receivers are located via
	// pre-order Unwrap walk so a sub-workflow may be wrapped in a Steper-only
	// wrapper (e.g. NamedStep) without losing inheritance.
	for step := range w.steps {
		if recv := findOptionReceiver(step); recv != nil {
			recv.InheritOption(w.Option)
		}
	}

	// Tick loop: each time a step terminates it Signal()s the cond, we wake
	// up and tick() again. Inline-settled steps may unblock more steps within
	// the same tick (no signal needed for those — see tick()).
	w.statusChange.L.Lock()
	for {
		if done := w.tick(ctx); done {
			break
		}
		w.statusChange.Wait()
	}
	w.statusChange.L.Unlock()

	// Drain worker goroutines so we don't return while children are still alive.
	w.waitGroup.Wait()

	// Build the per-step error map and decide the overall outcome.
	err := make(ErrWorkflow)
	for step, state := range w.steps {
		err[step] = state.GetStepResult()
	}
	if w.skipAsError() && err.AllSucceeded() {
		return nil
	}
	if !w.skipAsError() && err.AllSucceededOrSkipped() {
		return nil
	}
	return err
}
```

Note the changes vs. before:
- Removed the `defer func() { w.inheritedStep = nil; w.inheritedAttempt = nil }()` block — those fields no longer exist.
- Added `optSnapshot` / `defer w.Option = optSnapshot`.
- Added the propagation `for step := range w.steps { ... }` loop after `preflight()`.
- `w.SkipAsError` → `w.skipAsError()` (already handled in Task 2.10, but reaffirmed here).

- [ ] **Step 3.4: Verify it still won't compile (expected) — continue to Task 4**

```bash
go build ./...
```

Expected: build failure (`InterceptorReceiver`, `findInterceptorReceiver`, `MutatorReceiver` still referenced in `interceptor.go` / `mutator.go` / `workflow.go` SubWorkflow methods).

---

## Task 4: Remove old receiver interfaces and helper

**Files:**
- Modify: `interceptor.go`, `mutator.go`

- [ ] **Step 4.1: In `interceptor.go`, delete `InterceptorReceiver` and `findInterceptorReceiver`**

Find lines 42-78 (approximate — covers the type declaration, godoc, and the `findInterceptorReceiver` function). Delete the entire block.

Also remove any imports that become unused (the `Traverse` import is still used elsewhere if any; usually nothing to clean).

- [ ] **Step 4.2: In `mutator.go`, delete `MutatorReceiver`**

Find lines 12-21 (the `// MutatorReceiver is implemented by …` doc plus the `type MutatorReceiver interface { ... }` block). Delete.

Also update the godoc reference inside the `applyTo` body comment (around line 49) — replace `Inner steps are reached via PrependMutators.` with `Inner steps are reached via the WorkflowOptionReceiver.InheritOption mechanism (see workflow_option.go).`.

- [ ] **Step 4.3: Verify build still fails because of SubWorkflow (expected)**

```bash
go build ./...
```

Expected: build errors from `SubWorkflow.PrependMutators` / `SubWorkflow.PrependInterceptors` (still in `workflow.go`) referencing the deleted interfaces / methods.

---

## Task 5: Deprecate SubWorkflow (and re-tag StepBuilder)

**Files:**
- Modify: `workflow.go`, `build_step.go`

- [ ] **Step 5.1: Re-tag `StepBuilder` as Deprecated in its godoc**

In `build_step.go`, the existing godoc only deprecates the `BuildStep` *method*. With this change we publicly mark the `StepBuilder` *type* itself as deprecated so users who embed it directly (none known, but the type is exported) are warned. The behavior is unchanged — `Workflow` still embeds `StepBuilder`, the `BuildStep()` user hook still fires once per step via `Workflow.Add` → `w.BuildStep(step)`.

Replace the existing type-level doc (above `type StepBuilder struct{ built Set[Steper] }`) with:

```go
// StepBuilder is the per-Workflow memo that ensures every Step's optional
// BuildStep() hook fires at most once.
//
// A Step type can implement BuildStep() to assemble its internal sub-steps
// lazily — typically the first time it is added to a Workflow:
//
//	type StepImpl struct{}
//	func (s *StepImpl) Unwrap() []flow.Steper { return /* internal steps */ }
//	func (s *StepImpl) Do(ctx context.Context) error { /* ... */ }
//	func (s *StepImpl) BuildStep()                  { /* assemble children */ }
//
//	workflow.Add(
//	    flow.Step(new(StepImpl)), // BuildStep() fires here, exactly once.
//	)
//
// The StepBuilder is embedded in Workflow itself, so Workflow.Add transparently
// invokes BuildStep on every newly seen step.
//
// Deprecated: StepBuilder and the BuildStep() user hook will be removed in
// the next major version of go-workflow, together with [SubWorkflow] and
// [SubWorkflow.Reset]. Use [Mutate] for cross-cutting modification, and
// construct sub-workflows inside Do() (or at the embedding type's
// construction time) instead.
type StepBuilder struct{ built Set[Steper] }
```

The existing `// Deprecated:` paragraph on the `BuildStep(s Steper)` *method* stays as-is; this step just adds the same notice at the *type* level so embedders see it.

- [ ] **Step 5.2: Replace the entire `SubWorkflow` block in `workflow.go`**

Find lines ~891-931 (the existing `// SubWorkflow makes any user struct …` doc plus the struct, methods, and `PrependMutators` / `PrependInterceptors` helpers).

Replace with:

```go
// SubWorkflow makes any user struct behave as a Step that contains a
// Workflow. Embed it in your own struct to get Add/Do/Unwrap and Option
// inheritance for free.
//
// Deprecated: Embed flow.Workflow directly instead. With workflow-level
// configuration consolidated under [Workflow.Option] (one named field),
// embedding flow.Workflow promotes only that single Option name onto your
// type — the same surface area SubWorkflow previously hid. SubWorkflow will
// be removed in the next major version of go-workflow.
//
//	// Recommended pattern:
//	type Deploy struct {
//	    flow.Workflow
//	    Region string
//	}
type SubWorkflow struct{ w Workflow }

func (s *SubWorkflow) Unwrap() Steper                    { return &s.w }
func (s *SubWorkflow) Add(builders ...Builder) *Workflow { return s.w.Add(builders...) }
func (s *SubWorkflow) Do(ctx context.Context) error      { return s.w.Do(ctx) }

// Reset clears the inner workflow so a subsequent BuildStep() can rebuild
// from scratch.
//
// Deprecated: Reset is only invoked by the deprecated [StepBuilder.BuildStep]
// path. With the [Mutator] mechanism (see [Mutate]) and Do()-time sub-workflow
// construction, Reset is no longer needed and will be removed together with
// SubWorkflow in the next major version of go-workflow.
func (s *SubWorkflow) Reset() { s.w = Workflow{} }

// InheritOption forwards to the inner Workflow so SubWorkflow continues to
// participate in parent → child Option propagation during the deprecation
// window. Implements [WorkflowOptionReceiver].
func (s *SubWorkflow) InheritOption(parent WorkflowOption) {
	s.w.InheritOption(parent)
}
```

Note: `PrependMutators` and `PrependInterceptors` methods are gone. The interfaces they satisfied no longer exist.

- [ ] **Step 5.3: Try to build (expected: source compiles; tests/examples may not)**

```bash
go build ./...
```

Expected: PASS. The non-test source tree now compiles cleanly under the new shape. Tests and examples are migrated in Tasks 6-8.

---

## Task 6: Write the InheritOption test matrix

**Files:**
- Modify: `workflow_option_test.go` (append to file created in Task 1)

We add new tests against the NOW-working `Workflow.InheritOption` and `findOptionReceiver`. These exercise behavior that did not exist before.

- [ ] **Step 6.1: Add the table-driven InheritOption matrix**

Append to `workflow_option_test.go`:

```go
import (
	// already imported: "testing", "github.com/stretchr/testify/assert"
	"context"
	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/require"
)

// Helpers for the matrix tests.
func ptr[T any](v T) *T { return &v }

// noopMutator is a Mutator that matches nothing; used as a slice element to
// verify slice order without exercising dispatch.
type noopMutator struct{ tag string }

func (noopMutator) applyTo(context.Context, Steper) (bool, Steper, Builder) {
	return false, nil, nil
}

// stepInterceptorTag returns a StepInterceptor that appends a tag to a shared
// slice so we can observe ordering. Used only in tests that exercise
// dispatch; the matrix tests below just compare slice identity / length.
func stepInterceptorTag(tag string, out *[]string) StepInterceptor {
	return func(ctx context.Context, step Steper, next func(context.Context) error) error {
		*out = append(*out, tag)
		return next(ctx)
	}
}

func TestWorkflow_InheritOption(t *testing.T) {
	mockClock := clock.NewMock()
	otherClock := clock.NewMock()

	t.Run("scalar nil inherits parent's value", func(t *testing.T) {
		parent := WorkflowOption{
			MaxConcurrency: ptr(4),
			DontPanic:      ptr(true),
			SkipAsError:    ptr(true),
			Clock:          mockClock,
			StepDefaults:   &StepOption{},
		}
		child := &Workflow{}
		child.InheritOption(parent)
		assert.Equal(t, 4, *child.Option.MaxConcurrency)
		assert.Equal(t, true, *child.Option.DontPanic)
		assert.Equal(t, true, *child.Option.SkipAsError)
		assert.Equal(t, mockClock, child.Option.Clock)
		assert.Equal(t, parent.StepDefaults, child.Option.StepDefaults)
	})

	t.Run("scalar non-nil: child wins", func(t *testing.T) {
		parent := WorkflowOption{MaxConcurrency: ptr(4)}
		child := &Workflow{Option: WorkflowOption{MaxConcurrency: ptr(8)}}
		child.InheritOption(parent)
		assert.Equal(t, 8, *child.Option.MaxConcurrency)
	})

	t.Run("explicit zero (*int=0) wins over parent's non-zero", func(t *testing.T) {
		// Child wants unlimited concurrency under a parent that limits.
		parent := WorkflowOption{MaxConcurrency: ptr(4)}
		zero := 0
		child := &Workflow{Option: WorkflowOption{MaxConcurrency: &zero}}
		child.InheritOption(parent)
		assert.Equal(t, 0, *child.Option.MaxConcurrency,
			"explicit *int=0 child must win over parent's *int=4")
	})

	t.Run("clock nil inherits", func(t *testing.T) {
		parent := WorkflowOption{Clock: mockClock}
		child := &Workflow{}
		child.InheritOption(parent)
		assert.Equal(t, mockClock, child.Option.Clock)
	})

	t.Run("clock non-nil: child wins", func(t *testing.T) {
		parent := WorkflowOption{Clock: mockClock}
		child := &Workflow{Option: WorkflowOption{Clock: otherClock}}
		child.InheritOption(parent)
		assert.Equal(t, otherClock, child.Option.Clock)
	})

	t.Run("Mutators: parent prepended", func(t *testing.T) {
		mP := noopMutator{tag: "parent"}
		mC := noopMutator{tag: "child"}
		parent := WorkflowOption{Mutators: []Mutator{mP}}
		child := &Workflow{Option: WorkflowOption{Mutators: []Mutator{mC}}}
		child.InheritOption(parent)
		require.Len(t, child.Option.Mutators, 2)
		assert.Equal(t, "parent", child.Option.Mutators[0].(noopMutator).tag)
		assert.Equal(t, "child", child.Option.Mutators[1].(noopMutator).tag)
	})

	t.Run("StepInterceptors: parent prepended", func(t *testing.T) {
		var log []string
		parentS := stepInterceptorTag("P", &log)
		childS := stepInterceptorTag("C", &log)
		parent := WorkflowOption{StepInterceptors: []StepInterceptor{parentS}}
		child := &Workflow{Option: WorkflowOption{StepInterceptors: []StepInterceptor{childS}}}
		child.InheritOption(parent)
		require.Len(t, child.Option.StepInterceptors, 2)
	})

	t.Run("AttemptInterceptors: parent prepended", func(t *testing.T) {
		dummyP := AttemptInterceptor(func(ctx context.Context, step Steper, attempt int, next func(context.Context) error) error {
			return next(ctx)
		})
		dummyC := AttemptInterceptor(func(ctx context.Context, step Steper, attempt int, next func(context.Context) error) error {
			return next(ctx)
		})
		parent := WorkflowOption{AttemptInterceptors: []AttemptInterceptor{dummyP}}
		child := &Workflow{Option: WorkflowOption{AttemptInterceptors: []AttemptInterceptor{dummyC}}}
		child.InheritOption(parent)
		require.Len(t, child.Option.AttemptInterceptors, 2)
	})

	t.Run("DontInherit: complete no-op", func(t *testing.T) {
		mP := noopMutator{tag: "parent"}
		parent := WorkflowOption{
			MaxConcurrency: ptr(4),
			DontPanic:      ptr(true),
			Mutators:       []Mutator{mP},
		}
		child := &Workflow{Option: WorkflowOption{
			DontInherit: true,
			Mutators:    []Mutator{noopMutator{tag: "child"}},
		}}
		child.InheritOption(parent)
		assert.Nil(t, child.Option.MaxConcurrency, "DontInherit must leave scalars untouched")
		assert.Nil(t, child.Option.DontPanic)
		require.Len(t, child.Option.Mutators, 1, "DontInherit must NOT prepend parent slices")
		assert.Equal(t, "child", child.Option.Mutators[0].(noopMutator).tag)
	})

	t.Run("parent slice is not mutated by InheritOption", func(t *testing.T) {
		mP := noopMutator{tag: "parent"}
		parentMutators := []Mutator{mP}
		parent := WorkflowOption{Mutators: parentMutators}
		child := &Workflow{Option: WorkflowOption{Mutators: []Mutator{noopMutator{tag: "child"}}}}
		child.InheritOption(parent)
		require.Len(t, parentMutators, 1, "parent's slice must not be appended into")
		assert.Equal(t, "parent", parentMutators[0].(noopMutator).tag)
	})
}
```

- [ ] **Step 6.2: Run the matrix tests**

```bash
go test ./... -run TestWorkflow_InheritOption -v
```

Expected: PASS. If a test fails, the merge rules in Task 2.8 are wrong — fix them before proceeding.

- [ ] **Step 6.3: Commit Tasks 2-6 as one squashed change**

The tree compiles for source and the new InheritOption tests pass; tests for the legacy API surface still need migration (next tasks). Squash because the intermediate states do not compile.

```bash
git add workflow.go workflow_option.go workflow_option_test.go interceptor.go mutator.go
git commit -m "feat: consolidate Workflow config into Option; add WorkflowOptionReceiver

- Replace nine top-level Workflow fields with one Option WorkflowOption (pointer scalars + slices + DontInherit).
- Add WorkflowOptionReceiver / Workflow.InheritOption; merge rules: nil scalar
  inherits parent, slices prepended, DontInherit is a no-op.
- Rewrite Do() prologue: snapshot/restore Option, propagate via findOptionReceiver
  once per child sub-workflow root step, replacing the two ad-hoc dispatch sites.
- Remove inheritedStep/inheritedAttempt side fields and the special-cased
  reset() exclusion logic.
- Remove MutatorReceiver, InterceptorReceiver, findInterceptorReceiver,
  Workflow.PrependMutators, Workflow.PrependInterceptors.
- Rename DefaultOption → Option.StepDefaults, IsolateInterceptors → Option.DontInherit.
- Deprecate SubWorkflow; add SubWorkflow.InheritOption delegating to inner Workflow.
- Drop now-destructive Clock-defaulting in reset(); accessor handles nil.
- Add WorkflowOption.InheritOption test matrix.

Tests/examples migrated in follow-up commits."
```

(Note: tests still won't all pass — migrate in subsequent tasks.)

---

## Task 7: Migrate test files

**Files:**
- Modify: `workflow_options_test.go`, `workflow_mutator_test.go`, `workflow_test.go`, `branch_test.go`, `wrap_test.go`, `execution_model_test.go`, `step_configuration_test.go`, `retry_test.go`, `condition_test.go`, `noop_test.go`, `mutator_test.go`, `error_test.go`, `name_test.go`, `testutil_test.go` (whichever reference the removed symbols)

The mechanical sed-friendly substitutions:

| Old | New |
|---|---|
| `Workflow{MaxConcurrency: N, …}` | `Workflow{Option: WorkflowOption{MaxConcurrency: &mc, …}}` (declare `mc := N` above) |
| `Workflow{DontPanic: true, …}` | `Workflow{Option: WorkflowOption{DontPanic: &dp, …}}` (declare `dp := true`) |
| `Workflow{SkipAsError: true, …}` | `Workflow{Option: WorkflowOption{SkipAsError: &se, …}}` |
| `Workflow{Clock: c, …}` | `Workflow{Option: WorkflowOption{Clock: c, …}}` |
| `Workflow{DefaultOption: &so, …}` | `Workflow{Option: WorkflowOption{StepDefaults: &so, …}}` |
| `Workflow{Mutators: m, …}` | `Workflow{Option: WorkflowOption{Mutators: m, …}}` |
| `Workflow{StepInterceptors: si, …}` | `Workflow{Option: WorkflowOption{StepInterceptors: si, …}}` |
| `Workflow{AttemptInterceptors: ai, …}` | `Workflow{Option: WorkflowOption{AttemptInterceptors: ai, …}}` |
| `Workflow{IsolateInterceptors: true, …}` | `Workflow{Option: WorkflowOption{DontInherit: true, …}}` |
| `w.MaxConcurrency = N` | `w.Option.MaxConcurrency = ptr(N)` (or any equivalent) |
| `w.DontPanic = true` | `w.Option.DontPanic = ptr(true)` |
| `w.SkipAsError = true` | `w.Option.SkipAsError = ptr(true)` |
| `w.Clock = c` | `w.Option.Clock = c` |
| `w.DefaultOption = &so` | `w.Option.StepDefaults = &so` |
| `w.Mutators = m` | `w.Option.Mutators = m` |
| `w.StepInterceptors = si` | `w.Option.StepInterceptors = si` |
| `w.AttemptInterceptors = ai` | `w.Option.AttemptInterceptors = ai` |
| `w.IsolateInterceptors = true` | `w.Option.DontInherit = true` |
| `w.PrependMutators(m)` | `w.InheritOption(WorkflowOption{Mutators: m})` |
| `w.PrependInterceptors(si, ai)` | `w.InheritOption(WorkflowOption{StepInterceptors: si, AttemptInterceptors: ai})` |
| `MutatorReceiver` reference | `WorkflowOptionReceiver` |
| `InterceptorReceiver` reference | `WorkflowOptionReceiver` |
| `findInterceptorReceiver` call | `findOptionReceiver` (look for the same `Unwrap`-walking semantics) |

- [ ] **Step 7.1: Inventory remaining test files referencing the old symbols**

Run:

```bash
grep -rn 'MaxConcurrency\|DontPanic\|SkipAsError\|DefaultOption\|IsolateInterceptors\|PrependMutators\|PrependInterceptors\|MutatorReceiver\|InterceptorReceiver\|findInterceptorReceiver\|w\.Mutators\b\|w\.StepInterceptors\b\|w\.AttemptInterceptors\b\|w\.Clock\b' --include='*_test.go'
```

This produces the working list. Migrate each file in turn. Group commits by file or by sensible chunks of 3-5 files.

- [ ] **Step 7.2: For each test file, apply the substitution table**

Example concrete migration — `workflow_options_test.go`. Find a pattern like:

```go
w := &Workflow{MaxConcurrency: 2}
```

Replace with:

```go
mc := 2
w := &Workflow{Option: WorkflowOption{MaxConcurrency: &mc}}
```

If the test is asserting `w.MaxConcurrency == 2`, change the assertion to:

```go
require.NotNil(t, w.Option.MaxConcurrency)
assert.Equal(t, 2, *w.Option.MaxConcurrency)
```

For tests of `IsolateInterceptors`, rename to `DontInherit`. The behavior matrix is widened (now opts out of EVERYTHING, not just interceptors) — but every existing test only ever observes the interceptor behavior, so the substitution is semantics-preserving for those tests.

- [ ] **Step 7.3: Migrate `PrependMutators` / `PrependInterceptors` test exercises**

The tests that previously called `w.PrependMutators(...)` were asserting that the parent's Mutators slice was prepended into `w.Mutators`. Equivalent assertion under the new API:

Before:
```go
w := &Workflow{Mutators: []Mutator{childM}}
w.PrependMutators([]Mutator{parentM})
require.Len(t, w.Mutators, 2)
assert.Equal(t, parentM, w.Mutators[0])
```

After:
```go
w := &Workflow{Option: WorkflowOption{Mutators: []Mutator{childM}}}
w.InheritOption(WorkflowOption{Mutators: []Mutator{parentM}})
require.Len(t, w.Option.Mutators, 2)
assert.Equal(t, parentM, w.Option.Mutators[0])
```

- [ ] **Step 7.4: Migrate `SubWorkflow`-using tests (DO NOT remove SubWorkflow)**

`SubWorkflow` is deprecated but still works. Test files that use it should:
- Continue to work as-is for the SubWorkflow-as-deprecated-path smoke tests.
- For any test that exercises the OLD `SubWorkflow.PrependMutators` / `SubWorkflow.PrependInterceptors` methods, replace with `subWorkflow.InheritOption(...)`.

- [ ] **Step 7.5: Add a SubWorkflow smoke test**

In `workflow_test.go` (or a new `subworkflow_deprecated_test.go`):

```go
//nolint:staticcheck // deprecation smoke test
func TestSubWorkflow_InheritOption_DeprecationSmoke(t *testing.T) {
	// Verify that during the deprecation window, a step embedding SubWorkflow
	// still receives the parent's Option via the delegated InheritOption.
	type composite struct {
		SubWorkflow
	}

	parent := &Workflow{Option: WorkflowOption{DontPanic: ptr(true)}}
	c := &composite{}
	// Inner step inside SubWorkflow.
	innerCalled := false
	innerStep := Func("inner", func(ctx context.Context) error {
		innerCalled = true
		return nil
	})
	c.Add(Step(innerStep))
	parent.Add(Step(c))

	require.NoError(t, parent.Do(context.Background()))
	assert.True(t, innerCalled)
	// During parent.Do(), the inner Workflow inside c.SubWorkflow should have
	// observed DontPanic=true via the delegated InheritOption.
	// We assert indirectly via a Func that panics; this assertion is in a
	// separate test below.
}

func TestSubWorkflow_InheritOption_PanicRecovered(t *testing.T) {
	type composite struct {
		SubWorkflow
	}

	parent := &Workflow{Option: WorkflowOption{DontPanic: ptr(true)}}
	c := &composite{}
	// Inner step that panics.
	c.Add(Step(Func("panicker", func(ctx context.Context) error {
		panic("boom")
	})))
	parent.Add(Step(c))

	// With DontPanic propagated, parent.Do must return an error (not panic).
	err := parent.Do(context.Background())
	require.Error(t, err)
}
```

- [ ] **Step 7.6: Add a scalar inheritance behavior test (motivating use case)**

Append to `workflow_option_test.go`:

```go
func TestWorkflow_ScalarInheritance_DontPanicFlowsToChild(t *testing.T) {
	// Motivating use case: parent sets DontPanic=true, child workflow has no
	// DontPanic set; the child's panicking step should be caught.
	child := &Workflow{}
	child.Add(Step(Func("panicker", func(ctx context.Context) error {
		panic("boom")
	})))

	parent := &Workflow{Option: WorkflowOption{DontPanic: ptr(true)}}
	parent.Add(Step(child))

	err := parent.Do(context.Background())
	require.Error(t, err, "parent.DontPanic=true should propagate so child's panic is recovered")
}

func TestWorkflow_DoSnapshotRestore_NoAccumulationAcrossRuns(t *testing.T) {
	// After parent.Do() returns, the child's Option.Mutators must be back to
	// its pre-inheritance value; running parent.Do() N times should NOT cause
	// Mutators to accumulate in the child.
	child := &Workflow{Option: WorkflowOption{Mutators: []Mutator{noopMutator{tag: "C"}}}}
	child.Add(Step(NoOp("noop")))

	parent := &Workflow{Option: WorkflowOption{Mutators: []Mutator{noopMutator{tag: "P"}}}}
	parent.Add(Step(child))

	for i := 0; i < 3; i++ {
		require.NoError(t, parent.Reset())
		require.NoError(t, parent.Do(context.Background()))
		require.Len(t, child.Option.Mutators, 1, "run %d: child Mutators must not accumulate", i)
		assert.Equal(t, "C", child.Option.Mutators[0].(noopMutator).tag)
	}
}

func TestWorkflow_MultiLevelMutatorPropagation(t *testing.T) {
	// grandparent → parent → child, each with one no-op Mutator. Mid-run,
	// the child's Option.Mutators must be observed as [gp, p, c].
	gp := noopMutator{tag: "gp"}
	p := noopMutator{tag: "p"}
	c := noopMutator{tag: "c"}

	child := &Workflow{Option: WorkflowOption{Mutators: []Mutator{c}}}
	// Use a Func step that captures child.Option.Mutators DURING execution.
	var observed []Mutator
	child.Add(Step(Func("observer", func(ctx context.Context) error {
		observed = child.Option.Mutators
		return nil
	})))

	parent := &Workflow{Option: WorkflowOption{Mutators: []Mutator{p}}}
	parent.Add(Step(child))

	grandparent := &Workflow{Option: WorkflowOption{Mutators: []Mutator{gp}}}
	grandparent.Add(Step(parent))

	require.NoError(t, grandparent.Do(context.Background()))
	require.Len(t, observed, 3)
	assert.Equal(t, "gp", observed[0].(noopMutator).tag)
	assert.Equal(t, "p", observed[1].(noopMutator).tag)
	assert.Equal(t, "c", observed[2].(noopMutator).tag)
}
```

- [ ] **Step 7.7: Run the full test suite**

```bash
go test ./... -count=1
```

Expected: PASS. If any test fails, fix the specific migration (almost certainly a leftover `w.MaxConcurrency` etc.).

- [ ] **Step 7.8: Commit**

```bash
git add .
git commit -m "test: migrate all tests to Option-shape and add inheritance/snapshot tests"
```

---

## Task 8: Migrate examples

**Files:**
- Modify: `example/*.go` files that reference the changed symbols

- [ ] **Step 8.1: Inventory example files**

```bash
grep -rln 'MaxConcurrency\|DontPanic\|SkipAsError\|DefaultOption\|IsolateInterceptors\|PrependMutators\|PrependInterceptors\|MutatorReceiver\|InterceptorReceiver\|SubWorkflow' example/
```

- [ ] **Step 8.2: Apply the same substitution table from Task 7 to each example**

- [ ] **Step 8.3: For examples that use `flow.SubWorkflow`, update them to embed `flow.Workflow` directly**

Before:
```go
type Deploy struct {
    flow.SubWorkflow
    Region string
}
```

After:
```go
type Deploy struct {
    flow.Workflow
    Region string
}
```

- [ ] **Step 8.4: Add a new example showing scalar inheritance**

Create `example/14_scalar_inheritance_test.go`:

```go
package example

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// ExampleScalarInheritance demonstrates how a parent Workflow's Option flows
// into a sub-workflow when the child leaves the field unset (nil pointer).
//
// In this example, only the parent sets DontPanic; the child workflow
// inherits it automatically, so the child's panicking step is recovered
// rather than crashing the process.
func ExampleScalarInheritance() {
	child := &flow.Workflow{}
	child.Add(flow.Step(flow.Func("panicker", func(ctx context.Context) error {
		panic("boom")
	})))

	dontPanic := true
	parent := &flow.Workflow{
		Option: flow.WorkflowOption{DontPanic: &dontPanic},
	}
	parent.Add(flow.Step(child))

	err := parent.Do(context.Background())
	fmt.Println(err != nil)
	// Output: true
}
```

- [ ] **Step 8.5: Run example tests**

```bash
go test ./example/... -count=1
```

Expected: PASS.

- [ ] **Step 8.6: Commit**

```bash
git add example/
git commit -m "example: migrate to Option-shape; add scalar-inheritance example"
```

---

## Task 9: Documentation and godoc

**Files:**
- Modify: `workflow.go`, `interceptor.go`, `mutator.go`, `state.go`, `error.go`, `README.md` (if any field is referenced)

- [ ] **Step 9.1: Update `Workflow.Add` godoc with the strong warning**

Find the `// Add wires Builders …` doc comment above `Workflow.Add` (around line 94-100). Append (after the existing doc):

```
// WARNING: Add MUST NOT be called from inside the same Workflow's Do() (or
// any method transitively reachable from Do()) UNLESS guarded by sync.Once.
// Calling Add unguarded inside Do() leads to undefined behavior — possible
// failure modes include callback chains accumulating across runs (each
// BeforeStep/AfterStep firing N times on the N-th invocation), duplicate
// Step entries if new pointers are allocated each call, and parent
// introspection returning multiple matches. See the "Composite step MUST
// NOT call Add inside Do unguarded" scenario in the composite-steps spec.
//
// Acceptable patterns for sub-workflow construction:
//
//   1. Build at construction time (recommended): the constructor calls
//      x.Add(...) before returning.
//   2. Construct inline inside Do() with a fresh *flow.Workflow: do NOT
//      embed flow.Workflow; instead, allocate w := &flow.Workflow{} inside
//      Do(), populate via w.Add(...), and call w.Do(ctx). To inherit
//      parent Option, the containing step must implement
//      WorkflowOptionReceiver.
//   3. Lazy build guarded by sync.Once: embed flow.Workflow and call
//      x.Add(...) from inside x.once.Do(...).
```

- [ ] **Step 9.2: Update `Workflow` type godoc to describe sub-workflow patterns**

Append to the existing `// Workflow orchestrates …` doc:

```
// Sub-workflows:
//
//   type Deploy struct {
//       flow.Workflow            // embed: get Add/Do/Unwrap/InheritOption for free
//       Region string
//   }
//
//   func NewDeploy(region string) *Deploy {
//       d := &Deploy{Region: region}
//       d.Add(flow.Step(/* … */))  // construction-time build (recommended)
//       return d
//   }
//
// The embedded Workflow's Option is reachable as d.Option. When d is added
// to a parent Workflow, the parent's Option propagates into d.Option via
// the WorkflowOptionReceiver.InheritOption mechanism, with the merge rules
// described in [WorkflowOption].
```

- [ ] **Step 9.3: Update godoc references to removed symbols in other files**

- `state.go:112` — change `// an error when Workflow.DontPanic is true.` to `// an error when Workflow.Option.DontPanic is true.`.
- `error.go:29` — change `Workflow.DontPanic` reference to `Workflow.Option.DontPanic`.
- `error.go:166` — same treatment.
- `mutator.go` — any leftover godoc mentioning `MutatorReceiver` or `PrependMutators` is replaced with `WorkflowOptionReceiver.InheritOption`.
- `interceptor.go` — any leftover godoc mentioning `InterceptorReceiver` or `PrependInterceptors` is replaced with `WorkflowOptionReceiver.InheritOption` and `Workflow.Option.StepInterceptors` / `AttemptInterceptors`.

- [ ] **Step 9.4: Update README**

Locate sub-workflow / configuration sections in `README.md` (use `grep -n 'SubWorkflow\|MaxConcurrency\|DontPanic\|DefaultOption' README.md`). Update each to reflect:
- Sub-workflow pattern: embed `flow.Workflow`, not `flow.SubWorkflow`.
- Configuration: under `Option WorkflowOption`, with pointer scalars.
- A short "scalar inheritance" note.

If README has no examples touching these fields, skip the README edit and rely on godoc examples.

- [ ] **Step 9.5: Add CHANGELOG entry**

Append to `CHANGELOG.md` (create if missing):

```markdown
## Unreleased

### Breaking changes

- `Workflow` no longer exposes the nine top-level configuration fields
  (`MaxConcurrency`, `DontPanic`, `SkipAsError`, `Clock`, `DefaultOption`,
  `Mutators`, `StepInterceptors`, `AttemptInterceptors`,
  `IsolateInterceptors`). All configuration is now under
  `Workflow.Option WorkflowOption`. Scalar fields are pointer-typed so that
  "unset" and "explicit zero" are distinguishable. Migration is mechanical:

      // before
      &Workflow{MaxConcurrency: 4, DontPanic: true}

      // after
      mc := 4; dp := true
      &Workflow{Option: WorkflowOption{MaxConcurrency: &mc, DontPanic: &dp}}

- `DefaultOption` is renamed to `Option.StepDefaults`.
- `IsolateInterceptors` is renamed to `Option.DontInherit` and now opts out
  of inheriting the entire WorkflowOption from a parent, not just
  interceptors.
- `MutatorReceiver`, `InterceptorReceiver`, `Workflow.PrependMutators`,
  `Workflow.PrependInterceptors`, and `findInterceptorReceiver` are
  removed. Implement `WorkflowOptionReceiver.InheritOption` instead.

### Deprecations

- `flow.SubWorkflow` is deprecated; embed `flow.Workflow` directly.
  SubWorkflow will be removed in the next major version of go-workflow.
- `flow.StepBuilder` (the type) is now also marked deprecated at the type
  level (the method was already deprecated). The type, the `BuildStep()`
  user hook, and the implicit invocation in `Workflow.Add` will all be
  removed in the next major version of go-workflow, together with
  `SubWorkflow` and `SubWorkflow.Reset`. Behavior is unchanged in this
  release.

### Behavior changes

- Scalar configuration (`DontPanic`, `MaxConcurrency`, `SkipAsError`,
  `Clock`, `StepDefaults`) now propagates from a parent Workflow into a
  sub-workflow when the child leaves the field nil. Previously these did
  not propagate. To opt out, set `Option.DontInherit = true` on the child,
  or restate the desired value on the child explicitly.
```

- [ ] **Step 9.6: Commit**

```bash
git add .
git commit -m "docs: update godoc, README, and CHANGELOG for Option consolidation"
```

---

## Task 10: Apply OpenSpec deltas to live specs

**Files:**
- Modify: `openspec/specs/workflow-options/spec.md`, `openspec/specs/composite-steps/spec.md`, `openspec/specs/mutators/spec.md`, `openspec/specs/step-interceptor/spec.md`

The change directory `openspec/changes/2026-05-12-workflow-option/specs/*/spec.md` contains the MODIFIED / ADDED / REMOVED requirement deltas. This task applies them to the live spec files.

- [ ] **Step 10.1: Apply `workflow-options` delta**

Open both files side by side:
- Source (delta): `openspec/changes/2026-05-12-workflow-option/specs/workflow-options/spec.md`
- Target: `openspec/specs/workflow-options/spec.md`

For each `### Requirement: X` block in the delta:
- If marked MODIFIED, replace the existing requirement in the target with the delta's version.
- If under `## ADDED Requirements`, append to the target.
- If under `## REMOVED Requirements`, delete the corresponding requirement from the target.

- [ ] **Step 10.2: Apply `composite-steps` delta**

Same procedure. Particular care:
- `SubWorkflow — Workflow as a Step` requirement: target should now include the `Deprecated:` paragraph and the SubWorkflow-InheritOption scenario.
- Remove the two `*Workflow / SubWorkflow implements MutatorReceiver` requirements.
- Add the new `*Workflow implements WorkflowOptionReceiver` requirement.
- Add the four new sub-workflow build-timing scenarios.

- [ ] **Step 10.3: Apply `mutators` delta**

- Update field references to `Workflow.Option.Mutators`.
- Replace `MutatorReceiver` / `PrependMutators` propagation references with `WorkflowOptionReceiver.InheritOption`.

- [ ] **Step 10.4: Apply `step-interceptor` delta**

- Update field references to `Workflow.Option.StepInterceptors` / `AttemptInterceptors`.
- Remove `InterceptorReceiver` and `IsolateInterceptors` requirements.
- Add the new `DontInherit` requirement.
- Update propagation requirement to use `WorkflowOptionReceiver.InheritOption`.

- [ ] **Step 10.5: Sanity check the updated specs**

```bash
grep -n 'MutatorReceiver\|InterceptorReceiver\|IsolateInterceptors\|PrependMutators\|PrependInterceptors\|DefaultOption\|inheritedStep\|inheritedAttempt' openspec/specs/
```

Expected: empty output (no stale references to removed symbols).

- [ ] **Step 10.6: Commit**

```bash
git add openspec/specs/
git commit -m "spec: apply workflow-option deltas to live workflow-options/composite-steps/mutators/step-interceptor specs"
```

---

## Task 11: Final verification

**Files:**
- (No edits; running verification commands)

- [ ] **Step 11.1: Build**

```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 11.2: Vet**

```bash
go vet ./...
```

Expected: no output (success).

- [ ] **Step 11.3: Full test suite**

```bash
go test ./... -race -count=1
```

Expected: PASS for every package (including `./example/...`). The `-race` flag exercises the snapshot/restore concurrency story.

- [ ] **Step 11.4: Grep for leftover references to removed symbols in source**

```bash
grep -rn 'PrependMutators\|PrependInterceptors\|MutatorReceiver\|InterceptorReceiver\|findInterceptorReceiver\|IsolateInterceptors\|inheritedStep\|inheritedAttempt' --include='*.go'
```

Expected: empty (no source-side references to removed symbols).

- [ ] **Step 11.5: Grep for direct reads of the removed fields**

```bash
grep -rn 'w\.MaxConcurrency\|w\.DontPanic\|w\.SkipAsError\|w\.Clock\|w\.DefaultOption\|w\.Mutators\|w\.StepInterceptors\|w\.AttemptInterceptors' --include='*.go' | grep -v 'w\.Option\.' | grep -v '_test.go'
```

Expected: empty (every source-side scalar read goes through accessors; every slice read goes through `w.Option.X`).

- [ ] **Step 11.6: Confirm `SubWorkflow` smoke test passes**

```bash
go test ./... -run TestSubWorkflow -v
```

Expected: PASS.

- [ ] **Step 11.7: Confirm scalar inheritance behavior change is exercised**

```bash
go test ./... -run TestWorkflow_ScalarInheritance -v
go test ./... -run TestWorkflow_DoSnapshotRestore -v
go test ./... -run TestWorkflow_MultiLevelMutatorPropagation -v
```

Expected: PASS.

- [ ] **Step 11.8: Final commit (only if any docs changed in Tasks 11)**

Most likely no diff at this stage; skip if `git status` is clean.

---

## Self-Review (Plan-Author Internal)

**Spec coverage check.** Walking the change's spec deltas:

- `workflow-options`:
  - Every MODIFIED requirement (MaxConcurrency, DontPanic, SkipAsError, StepDefaults, Clock) → covered by Task 2 + Task 7 (field/test migration).
  - ADDED: `WorkflowOption groups workflow-level configuration` → Task 1 (type) + Task 7 (migration test).
  - ADDED: `WorkflowOptionReceiver propagates Option to sub-workflows` → Task 1 (interface) + Task 2 (`Workflow.InheritOption`) + Task 3 (`Do()` prologue propagation) + Task 6 (test matrix) + Task 7 (multi-level test).
  - ADDED: `DontInherit opts out of all parent Option inheritance` → Task 2 (merge rule) + Task 6 (test).
  - ADDED: `Do() snapshots and restores Option` → Task 3 (snapshot/restore) + Task 7 (no-accumulation test).
  - ADDED: `Reset rewinds per-step status without touching the step set` → Task 2.7 (Reset godoc + behavior) + tests covered by existing `branch_test.go:170` etc.
  - REMOVED: top-level fields / `MutatorReceiver` / `InterceptorReceiver` / `IsolateInterceptors` / `inheritedXxx` → Tasks 2, 4.
- `composite-steps`:
  - MODIFIED `SubWorkflow — Workflow as a Step` (deprecated) + new SubWorkflow-InheritOption scenario → Task 5 + Task 7.5 smoke test.
  - MODIFIED `Sub-workflow construction inside Do` → covered by Task 9.1 godoc warning + 9.2 patterns block. (No new test needed beyond the existing scenarios.)
  - ADDED `*Workflow implements WorkflowOptionReceiver` → Task 2.8 + Task 6 test matrix.
  - REMOVED two MutatorReceiver requirements → Task 4.
  - Four new build-timing scenarios → Task 9 godoc; runtime guard is explicitly OUT-OF-SCOPE per the change's Section 11 future-work.
- `mutators`: field rename + InheritOption propagation → Tasks 2.5 + 3 + 7.
- `step-interceptor`: field rename + InheritOption propagation + DontInherit → Tasks 2.9 + 2.10 + 3 + 7.

**Placeholder scan.** No "TBD", "TODO", "fill in later", "similar to" — every step has the actual code or substitution table. ✓

**Type consistency.** `Workflow.Option.X` naming is consistent throughout (`StepDefaults`, not `DefaultStepOption`; `DontInherit`, not `IsolateInterceptors`; `InheritOption`, not `PrependOption` / `SetOption`). Accessor names `maxConcurrency() / dontPanic() / skipAsError() / clock()` are unexported and used consistently in Tasks 2.10 and 3.3. ✓

**Build-stability invariant.** Tasks 2-5 are squashed into a single commit (Step 6.3) because the intermediate tree does not compile. After that commit, every subsequent task leaves the tree compiling.

**Out-of-scope items, deliberately:**
- `go vet` analyzer for "Add called from Do()" — listed as Section 11 future work in the change's tasks.md; not implemented here.
- `Workflow.Reset` semantic widening (clearing `steps`) — explicitly rejected during brainstorming; current behavior preserved with updated godoc only.
- Helper `flow.Ptr[T]` / `flow.Bool` / `flow.Int` — explicitly rejected during brainstorming.
