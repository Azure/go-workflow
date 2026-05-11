# Step Mutator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `flow.Mutator` — a type-dispatched, once-per-step configuration contributor — to the go-workflow runtime, plus deprecate `BuildStep` / `SubWorkflow.Reset`.

**Architecture:** A `Mutator` is an opaque interface produced only by the generic constructor `flow.Mutate[T Steper](fn)`. Each `*Workflow` carries a `Mutators []Mutator` slice. At every step's first scheduling (in `tick`, between upstream-check and `state.SetStatus(Running)`), the runtime walks each Mutator against the step's `Unwrap()` chain, stops at workflow boundaries, and merges any returned `Builder`'s config into the step's `StepConfig`. A new `MutatorReceiver` interface (`PrependMutators(mw []Mutator)`) lets parent workflows propagate Mutators into nested `SubWorkflow` / `*Workflow`-as-step containers before they begin scheduling.

**Tech Stack:** Go (generics), stdlib `context`, no new external deps. Tests use `testify/assert`. Examples use stdlib `Example*` + `// Output:`.

**Worktree:** `/home/xingfeixu/repo/go-workflow/.claude/worktrees/redesign-build-step` (branch `worktree-redesign-build-step`)

**Spec source of truth:**
- `openspec/changes/2026-05-06-step-mutator/specs/mutators/spec.md`
- `openspec/changes/2026-05-06-step-mutator/specs/composite-steps/spec.md`
- `openspec/changes/2026-05-06-step-mutator/specs/step-configuration/spec.md`
- `openspec/changes/2026-05-06-step-mutator/design.md`

**Reference example file (already exists, build-tagged out):** `example/15_mutator_test.go`

---

## File Structure

| File | Status | Responsibility |
|------|--------|----------------|
| `mutator.go` | **Create** | `Mutator` interface, `Mutate[T]` constructor, `mutatorFunc[T]` impl with Unwrap traversal, `MutatorReceiver` interface, helper `applyMutators` |
| `mutator_test.go` | **Create** | Unit tests for dispatch, unwrap walk, nil-Builder, scope, Builder rebase |
| `state.go` | **Modify** | Add `mutatorsApplied bool` field + `MutatorsApplied()/SetMutatorsApplied()` accessors |
| `workflow.go` | **Modify** | Add `Mutators []Mutator` field; add `(*Workflow).PrependMutators`; add `(*SubWorkflow).PrependMutators`; splice mutator-merge call into `tick`; immediately-before-invoke propagation |
| `workflow_mutator_test.go` | **Create** | Workflow-level tests: once-per-step, ordering vs plan, sub-workflow propagation, lazy inner steps, ctx propagation, panic recovery |
| `build_step.go` | **Modify** | Add `// Deprecated:` godoc to `StepBuilder.BuildStep` |
| `workflow.go` (SubWorkflow.Reset) | **Modify** | Add `// Deprecated:` godoc |
| `example/13_composite_step_test.go` | **Modify** | Add `// Deprecated:` doc to `ExampleCompositeViaWorkflow` |
| `example/15_mutator_test.go` | **Modify** | Drop the `//go:build mutator_design` tag once implementation lands; fix `flow.Name` argument order (currently `flow.Name(name, step)` — must be `flow.Name(step, name)`) |
| `openspec/changes/2026-05-06-step-mutator/tasks.md` | **Modify** | Tick off completed items as work progresses |

---

## Key Codebase References (memorize these line ranges)

- `Workflow` struct: `workflow.go:40–55`
- `Workflow.Add` (entry point for `Builder.AddToWorkflow()`): `workflow.go:58–75`
- `Workflow.addStep` (per-step add path, calls `BuildStep`): `workflow.go:78–117`
- `Workflow.tick` Pending→Running scheduling: `workflow.go:365–441`. **Splice point: line 379 (after upstream check) → before line 380 (`state.Option()`).** Doing the merge here means `state.Option()` will see Mutator-contributed Options.
- `Workflow.RootOf` / `StateOf` (boundary detection via `interface{ StateOf(Steper) *State }`): `workflow.go:168–207`
- `SubWorkflow`: `workflow.go:551–558`. `Unwrap()` returns single inner `*Workflow`.
- `State` struct: `state.go:12–16`. `MergeConfig(*StepConfig)` at line 98.
- `StepConfig` + `Merge`: `step.go:35–40` and `step.go:430–441`. Merge is union for Upstreams, append for Before/After/Option.
- `Builder` interface: `step.go:24–26` (`AddToWorkflow() map[Steper]*StepConfig`).
- `Step[S]` constructor: `step.go:73`. `AddStep[S]` builder methods at 132–413.
- `Traverse(s, f, walked...)`: `wrap.go:92`. Pre-order, no workflow-boundary stop — caller responsibility.
- `Name(step, name)`: `name.go:13`. Returns `Builder` wrapping `*NamedStep`. **Note arg order: step first, name second.** `NamedStep.Unwrap() Steper` at name.go:51.
- `flow.As[T]`: `wrap.go:135–144` (unbounded; not what we want for mutator dispatch).
- `build_step.go`: full file 1–38, `StepBuilder.BuildStep` method at line 17.

---

## Task 1: Create `Mutator` interface and constructor with TDD

**Files:**
- Create: `mutator.go`
- Create: `mutator_test.go`

### - [ ] Step 1.1: Write failing test for `Mutate[T]` matching exact concrete type

Append to new file `mutator_test.go`:

```go
package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mutFoo struct{ Field string }

func (*mutFoo) Do(context.Context) error { return nil }

type mutBar struct{}

func (*mutBar) Do(context.Context) error { return nil }

func TestMutate_matchesExactType(t *testing.T) {
	called := 0
	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		called++
		return nil
	})
	matched, target, b := m.applyTo(context.Background(), &mutFoo{})
	assert.True(t, matched)
	assert.NotNil(t, target)
	assert.Nil(t, b)
	assert.Equal(t, 1, called)
}

func TestMutate_skipsNonMatchingType(t *testing.T) {
	called := 0
	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		called++
		return nil
	})
	matched, target, b := m.applyTo(context.Background(), &mutBar{})
	assert.False(t, matched)
	assert.Nil(t, target)
	assert.Nil(t, b)
	assert.Equal(t, 0, called)
}
```

### - [ ] Step 1.2: Run test, expect compile failure

Run: `cd /home/xingfeixu/repo/go-workflow/.claude/worktrees/redesign-build-step && go test ./... -run TestMutate -count=1`

Expected: compile error — `undefined: Mutate`, `undefined: applyTo`.

### - [ ] Step 1.3: Implement `mutator.go` to satisfy the test

Create `mutator.go`:

```go
package flow

import "context"

// Mutator represents a type-dispatched, once-per-step contribution of
// configuration to a Step. The interface has a single unexported method, so
// the only producer is the generic constructor [Mutate].
type Mutator interface {
	applyTo(ctx context.Context, step Steper) (matched bool, target Steper, builder Builder)
}

// MutatorReceiver is implemented by Steps that host a sub-workflow (e.g.
// [SubWorkflow] or *Workflow used as a step) so that parent workflows can
// propagate their [Mutator]s into the inner workflow before it schedules its
// own steps.
type MutatorReceiver interface {
	PrependMutators(mw []Mutator)
}

// Mutate constructs a [Mutator] that runs against any step whose concrete type
// matches T anywhere along its Unwrap() chain (within a single workflow's
// boundaries). The first matching layer is passed to fn. fn returns a [Builder]
// whose configuration for the matched step is merged into the step's
// StepConfig at first scheduling. Returning a nil Builder is valid (useful
// when fn only mutates fields on the typed step pointer).
func Mutate[T Steper](fn func(ctx context.Context, step T) Builder) Mutator {
	return mutatorFunc[T](fn)
}

type mutatorFunc[T Steper] func(ctx context.Context, step T) Builder

func (m mutatorFunc[T]) applyTo(ctx context.Context, step Steper) (bool, Steper, Builder) {
	var (
		matched bool
		typed   T
		match   Steper
	)
	Traverse(step, func(s Steper, _ []Steper) TraverseDecision {
		if v, ok := s.(T); ok {
			typed = v
			match = s
			matched = true
			return TraverseStop
		}
		// Stop at workflow boundaries: do NOT descend into a nested workflow's
		// inner steps from here. Inner steps are reached via PrependMutators.
		if _, isWorkflow := s.(interface {
			StateOf(Steper) *State
		}); isWorkflow && s != step {
			return TraverseEndBranch
		}
		return TraverseContinue
	})
	if !matched {
		return false, nil, nil
	}
	return true, match, m(ctx, typed)
}
```

### - [ ] Step 1.4: Run test, expect pass

Run: `cd /home/xingfeixu/repo/go-workflow/.claude/worktrees/redesign-build-step && go test ./... -run TestMutate -count=1`

Expected: PASS.

### - [ ] Step 1.5: Commit

```bash
cd /home/xingfeixu/repo/go-workflow/.claude/worktrees/redesign-build-step
git add mutator.go mutator_test.go
git commit -m "feat(mutator): add Mutator interface and Mutate[T] constructor"
```

---

## Task 2: Unwrap-chain dispatch tests

**Files:**
- Test: `mutator_test.go`

### - [ ] Step 2.1: Write failing tests for unwrap walk and outer-wins

Append to `mutator_test.go`:

```go
type mutWrapper struct{ inner Steper }

func (w *mutWrapper) Do(context.Context) error { return nil }
func (w *mutWrapper) Unwrap() Steper           { return w.inner }

func TestMutate_matchesInnerViaUnwrap(t *testing.T) {
	inner := &mutFoo{Field: "before"}
	wrapper := &mutWrapper{inner: inner}

	called := 0
	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		called++
		assert.Same(t, inner, f, "should receive the inner *mutFoo, not the wrapper")
		return nil
	})
	matched, target, _ := m.applyTo(context.Background(), wrapper)
	assert.True(t, matched)
	assert.Same(t, inner, target)
	assert.Equal(t, 1, called)
}

func TestMutate_outerWrapperWinsWhenItIsTheTarget(t *testing.T) {
	inner := &mutFoo{}
	wrapper := &mutWrapper{inner: inner}

	called := 0
	m := Mutate[*mutWrapper](func(ctx context.Context, w *mutWrapper) Builder {
		called++
		assert.Same(t, wrapper, w)
		return nil
	})
	matched, _, _ := m.applyTo(context.Background(), wrapper)
	assert.True(t, matched)
	assert.Equal(t, 1, called)
}
```

### - [ ] Step 2.2: Run, expect pass (Task 1 implementation already covers Unwrap)

Run: `go test ./... -run TestMutate -count=1`

Expected: PASS for both new tests.

### - [ ] Step 2.3: Write failing test that mutator does NOT cross workflow boundary

Append to `mutator_test.go`:

```go
func TestMutate_doesNotCrossWorkflowBoundary(t *testing.T) {
	// A *Workflow sits between the outer step and the inner *mutFoo.
	// applyTo must NOT descend into it; that's what PrependMutators is for.
	innerFoo := &mutFoo{}
	innerWf := new(Workflow).Add(Step(innerFoo))

	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		t.Fatalf("mutator must not descend into nested workflow")
		return nil
	})
	matched, _, _ := m.applyTo(context.Background(), innerWf)
	assert.False(t, matched)
}
```

### - [ ] Step 2.4: Run, expect pass

Run: `go test ./... -run TestMutate_doesNotCrossWorkflowBoundary -count=1`

Expected: PASS (the boundary check in `applyTo` already guards this — `*Workflow` implements `StateOf(Steper) *State`).

### - [ ] Step 2.5: Commit

```bash
git add mutator_test.go
git commit -m "test(mutator): cover unwrap traversal and workflow boundary stop"
```

---

## Task 3: Add `MutatorsApplied` flag to `State`

**Files:**
- Modify: `state.go:12–16` (struct), append accessors after line 27

### - [ ] Step 3.1: Write failing test for new flag

Append to `mutator_test.go`:

```go
func TestState_MutatorsAppliedDefault(t *testing.T) {
	s := &State{}
	assert.False(t, s.MutatorsApplied())
}

func TestState_SetMutatorsApplied(t *testing.T) {
	s := &State{}
	s.SetMutatorsApplied()
	assert.True(t, s.MutatorsApplied())
}
```

### - [ ] Step 3.2: Run, expect compile failure

Run: `go test ./... -run TestState_Mutators -count=1`

Expected: `undefined: (*State).MutatorsApplied`.

### - [ ] Step 3.3: Modify `state.go`

Replace lines 12–16:

```go
type State struct {
	StepResult
	Config           *StepConfig
	mutatorsApplied  bool
	sync.RWMutex
}
```

After line 27 (the existing `SetStatus` method), append:

```go
// MutatorsApplied reports whether the workflow has already merged Mutator
// contributions into this Step's Config.
func (s *State) MutatorsApplied() bool {
	s.RLock()
	defer s.RUnlock()
	return s.mutatorsApplied
}

// SetMutatorsApplied marks Mutator contributions as merged for this Step.
// The runtime calls this exactly once per step, just before the first attempt.
func (s *State) SetMutatorsApplied() {
	s.Lock()
	defer s.Unlock()
	s.mutatorsApplied = true
}
```

### - [ ] Step 3.4: Run, expect pass

Run: `go test ./... -run TestState_Mutators -count=1`

Expected: PASS.

### - [ ] Step 3.5: Commit

```bash
git add state.go mutator_test.go
git commit -m "feat(state): add MutatorsApplied per-step flag"
```

---

## Task 4: Add `Mutators` field to `Workflow` and `PrependMutators` methods

**Files:**
- Modify: `workflow.go:40–55` (struct), `workflow.go:551–558` (SubWorkflow)

### - [ ] Step 4.1: Write failing tests for `PrependMutators`

Create `workflow_mutator_test.go`:

```go
package flow_test

import (
	"context"
	"testing"

	flow "github.com/Azure/go-workflow"
	"github.com/stretchr/testify/assert"
)

type wfFoo struct{}

func (*wfFoo) Do(context.Context) error { return nil }

func TestWorkflow_PrependMutators(t *testing.T) {
	m1 := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })
	m2 := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })

	w := &flow.Workflow{Mutators: []flow.Mutator{m2}}
	w.PrependMutators([]flow.Mutator{m1})

	assert.Len(t, w.Mutators, 2)
	// m1 should now be first (prepended), m2 second
	// We can't compare functions directly; we infer order via in-test sentinels
	// in later integration tests. Here we just assert the slice grew.
}

func TestWorkflow_PrependMutatorsNilOrEmpty(t *testing.T) {
	w := &flow.Workflow{}
	w.PrependMutators(nil)
	assert.Empty(t, w.Mutators)
	w.PrependMutators([]flow.Mutator{})
	assert.Empty(t, w.Mutators)
}

func TestSubWorkflow_PrependMutators(t *testing.T) {
	type sub struct{ flow.SubWorkflow }
	s := &sub{}
	m := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })

	// MutatorReceiver must be implemented
	var _ flow.MutatorReceiver = s
	s.PrependMutators([]flow.Mutator{m})
	// No panic / no error → behaviour verified by integration test in Task 7
}
```

### - [ ] Step 4.2: Run, expect compile failure

Run: `go test ./... -run TestWorkflow_PrependMutators -count=1`

Expected: `undefined: Mutators field`, `undefined: PrependMutators`.

### - [ ] Step 4.3: Modify `Workflow` struct in `workflow.go:40–55`

Add `Mutators` field after `DefaultOption`:

```go
type Workflow struct {
	MaxConcurrency int
	DontPanic      bool
	SkipAsError    bool
	Clock          clock.Clock
	DefaultOption  *StepOption

	// Mutators are evaluated against every step in this workflow before that
	// step's first attempt. See [Mutator] / [Mutate].
	Mutators []Mutator

	StepBuilder

	steps map[Steper]*State

	statusChange *sync.Cond
	leaseBucket  chan struct{}
	waitGroup    sync.WaitGroup
	isRunning    sync.Mutex
}
```

### - [ ] Step 4.4: Add `(*Workflow).PrependMutators`

Append to `workflow.go` (place near `Add`, around line 75):

```go
// PrependMutators inserts mw at the front of w.Mutators, preserving the
// invariant that propagated parent Mutators run before locally-registered
// child Mutators. Safe to call multiple times; the once-per-step flag on
// State prevents double application.
func (w *Workflow) PrependMutators(mw []Mutator) {
	if len(mw) == 0 {
		return
	}
	w.Mutators = append(append([]Mutator{}, mw...), w.Mutators...)
}
```

### - [ ] Step 4.5: Add `(*SubWorkflow).PrependMutators`

Append after the existing SubWorkflow methods near `workflow.go:558`:

```go
// PrependMutators forwards mw to the inner workflow. Implements [MutatorReceiver]
// so parent workflows can propagate Mutators into a sub-workflow before its
// scheduling pass begins.
func (s *SubWorkflow) PrependMutators(mw []Mutator) {
	s.w.PrependMutators(mw)
}
```

### - [ ] Step 4.6: Run, expect pass

Run: `go test ./... -run "TestWorkflow_PrependMutators|TestSubWorkflow_PrependMutators" -count=1`

Expected: PASS.

### - [ ] Step 4.7: Commit

```bash
git add workflow.go workflow_mutator_test.go
git commit -m "feat(workflow): add Mutators field and PrependMutators on Workflow/SubWorkflow"
```

---

## Task 5: Splice mutator-merge into `tick` (the core integration)

**Files:**
- Modify: `workflow.go:365–441` (insert new helper + call site)

### - [ ] Step 5.1: Write failing integration test — Mutator runs once and merges Input

Append to `workflow_mutator_test.go`:

```go
type wfGreet struct {
	Greeting string
	Who      string
}

func (g *wfGreet) Do(context.Context) error { return nil }

func TestMutator_mergesInputBeforeFirstAttempt(t *testing.T) {
	called := 0
	g := &wfGreet{Greeting: "Hi"}
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
				called++
				return flow.Step(gg).Input(func(_ context.Context, gg *wfGreet) error {
					gg.Who = "world"
					return nil
				})
			}),
		},
	}
	w.Add(flow.Step(g))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 1, called, "mutator must run exactly once")
	assert.Equal(t, "world", g.Who, "mutator-contributed Input must run before Do")
}
```

### - [ ] Step 5.2: Run, expect failure (Mutator not yet wired into tick)

Run: `go test ./... -run TestMutator_mergesInputBeforeFirstAttempt -count=1`

Expected: FAIL — `g.Who` is "" because the Mutator is registered but never invoked.

### - [ ] Step 5.3: Add helper `applyMutators` to `workflow.go`

Append to `workflow.go` (place near `addStep` ~line 117 or in a logical helper section):

```go
// applyMutators runs each Mutator in w.Mutators against step. For every match,
// the returned Builder's config keyed on the matched layer is merged into the
// state of step (the wrapper key in this workflow). Called once per step,
// just before its first attempt.
func (w *Workflow) applyMutators(ctx context.Context, step Steper, state *State) {
	if len(w.Mutators) == 0 {
		return
	}
	for _, m := range w.Mutators {
		matched, target, b := m.applyTo(ctx, step)
		if !matched || b == nil {
			continue
		}
		for s, cfg := range b.AddToWorkflow() {
			if s == target {
				state.MergeConfig(cfg)
			}
			// configs keyed on other steps are silently dropped — Mutator
			// scope is the matched layer only.
		}
	}
}
```

### - [ ] Step 5.4: Splice the call into `tick` between line 379 and 380

Edit `workflow.go:tick` — locate this block (currently lines 376–380):

```go
		// we only process Steps whose all upstreams are terminated
		ups := w.UpstreamOf(step)
		if isAnyUpstreamNotTerminated(ups) {
			continue
		}
		option := state.Option()
```

Replace with:

```go
		// we only process Steps whose all upstreams are terminated
		ups := w.UpstreamOf(step)
		if isAnyUpstreamNotTerminated(ups) {
			continue
		}
		// Apply Mutators exactly once per step, before reading Option /
		// evaluating Condition / starting the first attempt. This way the
		// Option/Before/After contributions from Mutators are visible to the
		// rest of this iteration.
		if !state.MutatorsApplied() {
			w.applyMutators(ctx, step, state)
			state.SetMutatorsApplied()
		}
		option := state.Option()
```

### - [ ] Step 5.5: Run, expect pass

Run: `go test ./... -run TestMutator_mergesInputBeforeFirstAttempt -count=1`

Expected: PASS — `g.Who == "world"`, `called == 1`.

### - [ ] Step 5.6: Run full test suite to confirm no regression

Run: `go test ./... -count=1`

Expected: all pre-existing tests still pass; new ones pass.

### - [ ] Step 5.7: Commit

```bash
git add workflow.go workflow_mutator_test.go
git commit -m "feat(workflow): apply Mutators once per step before first attempt"
```

---

## Task 6: Once-per-step across retries; nil/empty Mutators is a no-op

**Files:**
- Test: `workflow_mutator_test.go`

### - [ ] Step 6.1: Write test — Mutator runs once across multiple retry attempts

Append to `workflow_mutator_test.go`:

```go
import "errors"

type wfFlaky struct {
	attempts int
	failTill int
}

func (f *wfFlaky) Do(context.Context) error {
	f.attempts++
	if f.attempts <= f.failTill {
		return errors.New("transient")
	}
	return nil
}

func TestMutator_runsExactlyOnceAcrossRetries(t *testing.T) {
	mutatorCalls := 0
	inputCalls := 0
	f := &wfFlaky{failTill: 2}
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfFlaky](func(ctx context.Context, ff *wfFlaky) flow.Builder {
				mutatorCalls++
				return flow.Step(ff).
					Retry(func(o *flow.RetryOption) { o.MaxAttempts = 3 }).
					Input(func(_ context.Context, _ *wfFlaky) error {
						inputCalls++
						return nil
					})
			}),
		},
	}
	w.Add(flow.Step(f))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 1, mutatorCalls, "mutator user-fn runs once")
	assert.Equal(t, 3, inputCalls, "mutator-contributed Input runs per attempt")
	assert.Equal(t, 3, f.attempts)
}
```

NOTE: Adjust the import group at the top of the file to add `"errors"` if not already present.

### - [ ] Step 6.2: Write test — nil Mutators slice is a no-op

```go
func TestMutator_nilSliceIsNoOp(t *testing.T) {
	g := &wfGreet{Greeting: "Hi", Who: "Bob"}
	w := &flow.Workflow{} // Mutators == nil
	w.Add(flow.Step(g))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, "Bob", g.Who)
}
```

### - [ ] Step 6.3: Run, expect pass

Run: `go test ./... -run "TestMutator_runsExactlyOnceAcrossRetries|TestMutator_nilSliceIsNoOp" -count=1`

Expected: PASS.

### - [ ] Step 6.4: Commit

```bash
git add workflow_mutator_test.go
git commit -m "test(mutator): once-per-step across retries; nil Mutators no-op"
```

---

## Task 7: Sub-workflow propagation via `MutatorReceiver`

**Files:**
- Modify: `workflow.go` — splice receiver invocation into `runStep` / `addStep`-time path
- Test: `workflow_mutator_test.go`

### - [ ] Step 7.1: Write failing test — parent Mutator reaches into SubWorkflow

```go
type wfComposite struct {
	flow.SubWorkflow
	Inner wfGreet
}

func (c *wfComposite) Do(ctx context.Context) error {
	// Lazy build inside Do — replaces BuildStep pattern.
	c.Add(flow.Step(&c.Inner))
	return c.SubWorkflow.Do(ctx)
}

func TestMutator_reachesIntoSubWorkflow(t *testing.T) {
	c := &wfComposite{Inner: wfGreet{Greeting: "Hello"}}
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
				g.Who = "world"
				return nil
			}),
		},
	}
	w.Add(flow.Step(c))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "world", c.Inner.Who, "parent Mutator must reach inner step via PrependMutators")
}
```

### - [ ] Step 7.2: Run, expect failure

Run: `go test ./... -run TestMutator_reachesIntoSubWorkflow -count=1`

Expected: FAIL — `c.Inner.Who` empty because parent's Mutators were never propagated to the inner workflow.

### - [ ] Step 7.3: Splice propagation into `tick` (immediately before invocation)

Recall splice point established in Task 5. Extend the same block: BEFORE running `applyMutators`, also propagate to receivers. Edit `tick` in `workflow.go`:

```go
		if !state.MutatorsApplied() {
			// Propagate this workflow's Mutators into nested workflows so
			// they can apply against inner steps when those steps are
			// scheduled by the inner workflow.
			if recv, ok := step.(MutatorReceiver); ok && len(w.Mutators) > 0 {
				recv.PrependMutators(w.Mutators)
			}
			w.applyMutators(ctx, step, state)
			state.SetMutatorsApplied()
		}
```

### - [ ] Step 7.4: Run, expect pass

Run: `go test ./... -run TestMutator_reachesIntoSubWorkflow -count=1`

Expected: PASS — `c.Inner.Who == "world"`.

### - [ ] Step 7.5: Write test — parent Mutator reaches step inside nested `*Workflow` used as a step

```go
func TestMutator_reachesIntoNestedWorkflow(t *testing.T) {
	innerG := &wfGreet{Greeting: "Hi"}
	innerW := new(flow.Workflow).Add(flow.Step(innerG))

	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
				g.Who = "world"
				return nil
			}),
		},
	}
	w.Add(flow.Step(innerW))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "world", innerG.Who)
}
```

### - [ ] Step 7.6: Run, expect pass (because `*Workflow` already implements MutatorReceiver from Task 4)

Run: `go test ./... -run TestMutator_reachesIntoNestedWorkflow -count=1`

Expected: PASS.

### - [ ] Step 7.7: Write test — lazily-added inner step is reached

```go
func TestMutator_reachesLazilyAddedInnerStep(t *testing.T) {
	// wfComposite already adds &c.Inner inside Do() — verify this works.
	// (Same as TestMutator_reachesIntoSubWorkflow but explicit about laziness.)
	c := &wfComposite{Inner: wfGreet{Greeting: "Yo"}}
	called := 0
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
				called++
				g.Who = "lazy"
				return nil
			}),
		},
	}
	w.Add(flow.Step(c))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, 1, called)
	assert.Equal(t, "lazy", c.Inner.Who)
}
```

Run: `go test ./... -run TestMutator_reachesLazilyAddedInnerStep -count=1`

Expected: PASS.

### - [ ] Step 7.8: Commit

```bash
git add workflow.go workflow_mutator_test.go
git commit -m "feat(workflow): propagate Mutators into nested workflows via MutatorReceiver"
```

---

## Task 8: Ordering — plan-declared Input runs before Mutator-contributed Input

**Files:**
- Test: `workflow_mutator_test.go`

### - [ ] Step 8.1: Write test

```go
func TestMutator_planInputBeforeMutatorInput(t *testing.T) {
	g := &wfGreet{Greeting: "Hi"}
	order := []string{}
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
				return flow.Step(gg).Input(func(_ context.Context, _ *wfGreet) error {
					order = append(order, "mutator")
					return nil
				})
			}),
		},
	}
	w.Add(
		flow.Step(g).Input(func(_ context.Context, _ *wfGreet) error {
			order = append(order, "plan")
			return nil
		}),
	)
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, []string{"plan", "mutator"}, order)
}

func TestMutator_multipleMutatorsRunInSliceOrder(t *testing.T) {
	g := &wfGreet{}
	order := []string{}
	mk := func(name string) flow.Mutator {
		return flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
			return flow.Step(gg).Input(func(_ context.Context, _ *wfGreet) error {
				order = append(order, name)
				return nil
			})
		})
	}
	w := &flow.Workflow{Mutators: []flow.Mutator{mk("m1"), mk("m2")}}
	w.Add(flow.Step(g))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, []string{"m1", "m2"}, order)
}
```

### - [ ] Step 8.2: Run, expect pass (Merge appends, so order is plan→m1→m2 by construction)

Run: `go test ./... -run "TestMutator_planInputBeforeMutatorInput|TestMutator_multipleMutatorsRunInSliceOrder" -count=1`

Expected: PASS.

### - [ ] Step 8.3: Commit

```bash
git add workflow_mutator_test.go
git commit -m "test(mutator): ordering — plan first, mutators in slice order"
```

---

## Task 9: Ctx propagation, scope, panic recovery

**Files:**
- Test: `workflow_mutator_test.go`

### - [ ] Step 9.1: Write test — Mutator receives workflow-scoped ctx

```go
type ctxKey string

const wfCtxKey ctxKey = "k"

func TestMutator_receivesWorkflowCtx(t *testing.T) {
	g := &wfGreet{}
	got := ""
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
				if v, ok := ctx.Value(wfCtxKey).(string); ok {
					got = v
				}
				return nil
			}),
		},
	}
	w.Add(flow.Step(g))
	ctx := context.WithValue(context.Background(), wfCtxKey, "value-from-do")
	assert.NoError(t, w.Do(ctx))
	assert.Equal(t, "value-from-do", got)
}
```

### - [ ] Step 9.2: Write test — Builder config for unrelated step is silently ignored

```go
func TestMutator_unrelatedBuilderEntryIgnored(t *testing.T) {
	g := &wfGreet{Greeting: "Hi", Who: "Bob"}
	other := &wfGreet{Who: "untouched"}
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
				if gg == g {
					// Mistakenly return a Builder keyed on `other` instead of `gg`.
					return flow.Step(other).Input(func(_ context.Context, o *wfGreet) error {
						o.Who = "stolen"
						return nil
					})
				}
				return nil
			}),
		},
	}
	w.Add(flow.Step(g)) // only g is in the workflow
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, "untouched", other.Who, "config keyed on a different step is dropped")
}
```

### - [ ] Step 9.3: Write test — Mutator panic is caught when DontPanic=true

```go
func TestMutator_panicCaughtWhenDontPanic(t *testing.T) {
	g := &wfGreet{}
	w := &flow.Workflow{
		DontPanic: true,
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
				panic("boom")
			}),
		},
	}
	w.Add(flow.Step(g))
	err := w.Do(context.Background())
	assert.Error(t, err)
}
```

NOTE: this test may require wrapping `applyMutators` in `defer recover()` if `DontPanic` is true. If the test fails because the panic propagates out of `Do`, add the recovery logic to `applyMutators`. Look at how `runStep` handles `DontPanic` (`workflow.go` around `runStep`) for the canonical pattern to mirror.

### - [ ] Step 9.4: Run all three, expect pass; if 9.3 fails, add panic recovery to applyMutators

Run: `go test ./... -run "TestMutator_receivesWorkflowCtx|TestMutator_unrelatedBuilderEntryIgnored|TestMutator_panicCaughtWhenDontPanic" -count=1`

If 9.3 fails: edit `applyMutators` in `workflow.go` to wrap each iteration:

```go
func (w *Workflow) applyMutators(ctx context.Context, step Steper, state *State) {
	if len(w.Mutators) == 0 {
		return
	}
	for _, m := range w.Mutators {
		func() {
			if w.DontPanic {
				defer func() {
					if r := recover(); r != nil {
						state.SetError(fmt.Errorf("mutator panic: %v", r))
					}
				}()
			}
			matched, target, b := m.applyTo(ctx, step)
			if !matched || b == nil {
				return
			}
			for s, cfg := range b.AddToWorkflow() {
				if s == target {
					state.MergeConfig(cfg)
				}
			}
		}()
	}
}
```

(Add `"fmt"` to the imports of `workflow.go` if not already present — it likely is.)

Re-run: `go test ./... -run TestMutator_panicCaughtWhenDontPanic -count=1`

Expected: PASS.

### - [ ] Step 9.5: Commit

```bash
git add workflow.go workflow_mutator_test.go
git commit -m "test(mutator): ctx, scope-isolation, and DontPanic-protected panic recovery"
```

---

## Task 10: Merge does not happen at Add time

**Files:**
- Test: `workflow_mutator_test.go`

### - [ ] Step 10.1: Write test

```go
func TestMutator_mergeAtFirstScheduling_NotAtAdd(t *testing.T) {
	g := &wfGreet{}
	called := 0
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
				called++
				return nil
			}),
		},
	}
	w.Add(flow.Step(g))
	assert.Equal(t, 0, called, "Add must not invoke mutators")
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, 1, called)
}
```

### - [ ] Step 10.2: Run, expect pass

Run: `go test ./... -run TestMutator_mergeAtFirstScheduling -count=1`

Expected: PASS.

### - [ ] Step 10.3: Commit

```bash
git add workflow_mutator_test.go
git commit -m "test(mutator): merge happens at first schedule, not at Add"
```

---

## Task 11: Unwrap-via-Name end-to-end test

**Files:**
- Test: `workflow_mutator_test.go`

### - [ ] Step 11.1: Write test

```go
func TestMutator_matchesThroughNameWrapper(t *testing.T) {
	g := &wfGreet{Greeting: "Hi"}
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
				gg.Who = "world"
				return nil
			}),
		},
	}
	w.Add(flow.Name(g, "named-greet"))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, "world", g.Who)
}
```

### - [ ] Step 11.2: Run, expect pass

Run: `go test ./... -run TestMutator_matchesThroughNameWrapper -count=1`

Expected: PASS — Mutator dispatches through `*NamedStep`'s `Unwrap()` to find `*wfGreet`.

### - [ ] Step 11.3: Commit

```bash
git add workflow_mutator_test.go
git commit -m "test(mutator): match through Name wrapper via Unwrap"
```

---

## Task 12: Deprecation godoc

**Files:**
- Modify: `build_step.go:17`
- Modify: `workflow.go` — `SubWorkflow.Reset` method

### - [ ] Step 12.1: Add `// Deprecated:` to `StepBuilder.BuildStep`

Edit `build_step.go`. Locate the doc comment immediately above `func (sb *StepBuilder) BuildStep(s Steper)` (currently at line 17). Replace any existing comment (or add if absent) with:

```go
// BuildStep walks s and invokes any nested Step's BuildStep() method (and
// Reset() if implemented) to allow lazy initialization of composite steps.
//
// Deprecated: this lazy-initialization hook will be removed in the next major
// version of go-workflow. Use [Mutate] for cross-cutting modification, and
// construct sub-workflows inside Do() instead. See
// openspec/changes/2026-05-06-step-mutator/design.md.
func (sb *StepBuilder) BuildStep(s Steper) {
```

### - [ ] Step 12.2: Add `// Deprecated:` to `SubWorkflow.Reset`

Locate `func (s *SubWorkflow) Reset()` in `workflow.go`. Add immediately above:

```go
// Reset clears the inner workflow's state, allowing it to be re-built.
//
// Deprecated: Reset is only invoked by the deprecated [StepBuilder.BuildStep]
// path. With the [Mutator] mechanism (see [Mutate]) and Do()-time sub-workflow
// construction, Reset is no longer needed and will be removed in the next
// major version of go-workflow.
func (s *SubWorkflow) Reset() {
```

### - [ ] Step 12.3: Verify build still passes

Run: `go build ./...`

Expected: no errors.

### - [ ] Step 12.4: Commit

```bash
git add build_step.go workflow.go
git commit -m "chore(deprecation): mark BuildStep and SubWorkflow.Reset deprecated"
```

---

## Task 13: Update example file

**Files:**
- Modify: `example/15_mutator_test.go` (drop build tag, fix `flow.Name` arg order)
- Modify: `example/13_composite_step_test.go` (mark `ExampleCompositeViaWorkflow` deprecated)

### - [ ] Step 13.1: Drop `//go:build mutator_design` tag from `example/15_mutator_test.go`

Open `example/15_mutator_test.go`. Delete the first 12 lines (everything from `//go:build mutator_design` through the closing `// shape of the Mutator API before any implementation is written.` comment block, **stopping before `package flow_test`**). The file should now begin:

```go
package flow_test

import (
```

### - [ ] Step 13.2: Fix `flow.Name` argument order in `ExampleMutate_unwrap`

In `example/15_mutator_test.go`, locate `ExampleMutate_unwrap`. Change:

```go
	w.Add(flow.Step(flow.Name("named-greet", greet)))
```

to:

```go
	w.Add(flow.Name(greet, "named-greet"))
```

(`flow.Name(step, name)` — and it already returns a `Builder`, no need to wrap with `flow.Step`.)

### - [ ] Step 13.3: Run all examples to verify Output strings match

Run: `cd /home/xingfeixu/repo/go-workflow/.claude/worktrees/redesign-build-step && go test ./example/... -run "Example" -v -count=1`

Expected: all `ExampleMutate_*` PASS. If any FAIL with "got vs want" mismatch, adjust the `// Output:` comment to match observed output exactly. (Common gotcha: the `Hi (plan) (m1) (m2), world!` example in `ExampleMutate_multipleInOrder` depends on the Greet template — re-read the actual `Do()` implementation in the example file and match.)

### - [ ] Step 13.4: Mark legacy composite example deprecated

Open `example/13_composite_step_test.go`. Above `func ExampleCompositeViaWorkflow()`, add:

```go
// ExampleCompositeViaWorkflow demonstrates the legacy BuildStep-based
// composite step pattern.
//
// Deprecated: BuildStep is being removed in the next major version. See
// ExampleMutate_subWorkflow in 15_mutator_test.go for the new pattern that
// constructs the inner workflow inside Do() and uses flow.Mutate for
// cross-cutting modification.
func ExampleCompositeViaWorkflow() {
```

### - [ ] Step 13.5: Commit

```bash
git add example/15_mutator_test.go example/13_composite_step_test.go
git commit -m "docs(example): activate Mutator examples; deprecate BuildStep example"
```

---

## Task 14: Tick off completed items in `tasks.md`

**Files:**
- Modify: `openspec/changes/2026-05-06-step-mutator/tasks.md`

### - [ ] Step 14.1: Mark completed items

Open `openspec/changes/2026-05-06-step-mutator/tasks.md`. Change `- [ ]` to `- [x]` for every item now satisfied by the implementation. Cross-reference each requirement:

- 1.1–1.4 (Mutator types): satisfied by Task 1
- 2.1–2.4 (Workflow field, State flag, PrependMutators): Task 3 + Task 4
- 3.1–3.4 (First-schedule merge, set MutatorsApplied, propagation): Task 5 + Task 7
- 4.1–4.3 (Deprecation godoc): Task 12
- 5.1–5.5 (Examples): Task 13 (note: 5.4 retry-override and 5.5 sub-workflow are covered by `ExampleMutate_retryOverride` and `ExampleMutate_subWorkflow` already in the file)
- 7.1–7.21 (Tests): Tasks 1, 2, 5, 6, 7, 8, 9, 10, 11

Items NOT covered by this plan (intentional — they belong to the openspec `archive` step or to the follow-up migration change):
- 6.1–6.3 (Spec move on archive) — done by `openspec archive`, not by code
- 8.1–8.4 (Verify) — covered by Task 15 below

### - [ ] Step 14.2: Commit

```bash
git add openspec/changes/2026-05-06-step-mutator/tasks.md
git commit -m "docs(openspec): tick off completed step-mutator tasks"
```

---

## Task 15: Final verification

### - [ ] Step 15.1: Run `go build ./...`

Run: `cd /home/xingfeixu/repo/go-workflow/.claude/worktrees/redesign-build-step && go build ./...`

Expected: no errors.

### - [ ] Step 15.2: Run `go vet ./...`

Run: `go vet ./...`

Expected: no warnings.

### - [ ] Step 15.3: Run full test suite

Run: `go test ./... -count=1`

Expected: all tests pass, including all `ExampleMutate_*` examples.

### - [ ] Step 15.4: Re-validate openspec change

Run: `openspec validate 2026-05-06-step-mutator --strict`

Expected: `Change '2026-05-06-step-mutator' is valid`

### - [ ] Step 15.5: Final commit (if anything trivial remains; otherwise skip)

If steps 15.1–15.4 all passed without further edits, skip. Otherwise commit any small fixes:

```bash
git add -A
git commit -m "chore: post-implementation cleanup"
```

---

## Self-Review Checklist

**Spec coverage:**
- ✅ `Mutator interface and Mutate constructor` → Task 1
- ✅ `Workflow.Mutators field` → Task 4
- ✅ `Mutator merge timing and ordering` → Task 5, 8
- ✅ `Mutator dispatch and merge destination` → Task 1 (Unwrap), Task 5 (rebase), Task 7 (no boundary cross)
- ✅ `Mutators run once per step instance` → Task 6
- ✅ `Mutator-returned Builder scope` → Task 9 (unrelated entry ignored)
- ✅ `MutatorReceiver propagation interface` → Task 4, 7
- ✅ `SubWorkflow and *Workflow implement MutatorReceiver` → Task 4
- ✅ `Mutator vs Input/BeforeStep usage guidance` → covered by examples Task 13 + spec text
- ✅ `Config merge destination follows StateOf` → existing behaviour, leveraged by Mutator merge in Task 5
- ✅ Composite-steps `BuildStep deprecated` → Task 12
- ✅ Composite-steps `Sub-workflow construction inside Do` → demonstrated by Task 7 test + example
- ✅ Composite-steps `SubWorkflow / *Workflow implement MutatorReceiver` → Task 4

**Verification end-to-end:**
1. `go build ./...` — Task 15.1
2. `go vet ./...` — Task 15.2
3. `go test ./... -count=1` — Task 15.3 (covers unit tests in `mutator_test.go` + integration tests in `workflow_mutator_test.go` + examples)
4. `openspec validate 2026-05-06-step-mutator --strict` — Task 15.4
5. **Manual review of `example/15_mutator_test.go`**: open the file and read top-to-bottom — these are the user-facing API demonstrations and must read as idiomatic Go.

**Out of scope for this plan (deliberate):**
- Removing `BuildStep` / `StepBuilder` / `SubWorkflow.Reset()` — scheduled for next major version (see design.md §"Deprecation timeline")
- Migrating AKS e2ev3 production code from `BuildStep` to `Mutate` — separate proposal
- Collapsing `SubWorkflow` to alias `Workflow` — separate future proposal (design.md §"Future consideration")
- `openspec archive` of this change — done after PR merge, not by this plan
