## 1. Define Mutator Types

- [x] 1.1 Create `mutator.go` with `Mutator` interface (single unexported method `applyTo(ctx context.Context, step Steper) (matched bool, builder Builder)`)
- [x] 1.2 Add `Mutate[T Steper](fn func(ctx context.Context, step T) Builder) Mutator` generic constructor
- [x] 1.3 Add internal `mutatorFunc[T Steper]` type implementing `Mutator.applyTo` via `step.(T)` assertion, forwarding `ctx` to `fn`
- [x] 1.4 Add `MutatorReceiver` interface with `PrependMutators(mw []Mutator)`

## 2. Wire Workflow Field and State

- [x] 2.1 Add `Mutators []Mutator` field to `Workflow` in `workflow.go`
- [x] 2.2 Add `MutatorsApplied bool` (or equivalent — e.g. an internal sentinel-based marker) to `State` in `state.go` to enforce once-per-step
- [x] 2.3 Add `(*Workflow).PrependMutators(mw []Mutator)` so `*Workflow` (used as a step) propagates Mutators into nested workflows
- [x] 2.4 Add `(*SubWorkflow).PrependMutators(mw []Mutator)` mirroring `*Workflow`, prepending parent Mutators to inner workflow's

## 3. First-Schedule Merge

- [x] 3.1 In the scheduling-time path that takes a step from `Pending` to its first attempt (precise location depends on the Step Interceptor change's `stepExecution`), add a once-per-step Mutator merge block guarded by `state.MutatorsApplied`
- [x] 3.2 For each matched Mutator: call `applyTo(ctx, step)` (where `ctx` is the workflow ctx from `Workflow.Do(ctx)`); if it returns `(true, b)` with non-nil `b`, iterate `b.AddToWorkflow()` and merge only the entry whose key equals `step` into `state.Config` via existing `StepConfig.Merge`
- [x] 3.3 Set `state.MutatorsApplied = true` after the loop, before the first AttemptInterceptor invocation
- [x] 3.4 Immediately before invoking a step that hosts a sub-workflow, if the step implements `MutatorReceiver`, call `PrependMutators(parent.Mutators)` so the inner workflow's first-schedule pass sees parent Mutators

## 4. Deprecation Notes

- [x] 4.1 Add `// Deprecated: Use flow.Mutate or construct sub-workflows in Do()` to `StepBuilder.BuildStep` godoc in `build_step.go`
- [x] 4.2 Add `// Deprecated:` to `flow.SubWorkflow.Reset` godoc in `workflow.go`, noting it is only invoked by the deprecated `StepBuilder.BuildStep` path
- [ ] 4.3 Add `// Deprecated:` to the `BuildStep` doc-comment template in `build_step.go` that shows users how to write the hook

## 5. Examples

- [x] 5.1 Add `// Deprecated:` doc to `ExampleCompositeViaWorkflow` in `example/13_composite_step_test.go`, pointing to the new example
- [ ] 5.2 Add `ExampleCompositeViaDo` showing sub-workflow construction inside `Do()` without `BuildStep`
- [x] 5.3 Add `ExampleMutate_input` showing `flow.Mutate[*MyStep]` returning a Builder with `Input(...)` registered on `Workflow.Mutators`
- [x] 5.4 Add `ExampleMutate_retryOverride` showing a Mutator that returns a Builder with `Retry(...)` to override retry policy across all instances of a type
- [x] 5.5 Add `ExampleMutate_subWorkflow` showing parent Mutators reaching into a `SubWorkflow`-embedded composite step via `PrependMutators`

## 6. Spec Updates

- [ ] 6.1 Add new spec `openspec/specs/mutators/spec.md` covering the requirements in `changes/2026-05-06-step-mutator/specs/mutators/spec.md` (move on archive)
- [ ] 6.2 Apply MODIFIED Requirements from `changes/2026-05-06-step-mutator/specs/composite-steps/spec.md` to `openspec/specs/composite-steps/spec.md`:
   - mark `BuildStep — lazy initialization hook` as deprecated
   - add `Sub-workflow construction inside Do()` requirement
   - add `MutatorReceiver propagation` requirement
- [ ] 6.3 Apply ADDED Requirements from `changes/2026-05-06-step-mutator/specs/step-configuration/spec.md` to `openspec/specs/step-configuration/spec.md`:
   - add `Mutators field on Workflow` requirement

## 7. Tests

- [x] 7.1 Test: `Mutate[*Foo]` runs against a `*Foo` step, does not run against a `*Bar` step
- [x] 7.1a Test: Mutator function receives the workflow-scoped ctx (the same ctx passed to `Workflow.Do(ctx)`); a value placed in ctx via `context.WithValue` before `Workflow.Do` is observable in the Mutator
- [x] 7.2 Test: Mutator's user function is invoked exactly once across multiple attempts (use `RetryOption{MaxAttempts: 3}`, count invocations)
- [x] 7.3 Test: Mutator-returned Builder's `Input` callback runs on every attempt (3 attempts → 3 invocations)
- [x] 7.4 Test: Mutator returning `nil` Builder is a no-op; field mutation done in the user function persists into `Do`
- [x] 7.5 Test: Mutator returning a Builder with `Retry(...)` overrides retry policy of matched steps
- [ ] 7.6 Test: Mutator returning a Builder with `Before/After` adds those callbacks to the step's chain
- [x] 7.7 Test: Multiple Mutators registered for the same type run in slice order; their `Before` contributions appear in the same order in the merged chain
- [x] 7.8 Test: Mutator-returned Builder with config for a step *other than* the one passed in has that config silently ignored
- [x] 7.9 Test: Parent workflow Mutator reaches `*Foo` inside a `SubWorkflow`-embedded composite step
- [x] 7.10 Test: Parent workflow Mutator reaches `*Foo` inside a nested `*Workflow` used as a step
- [x] 7.11 Test: Lazily added inner step (added inside composite step's `Do`) is reached by parent Mutator
- [x] 7.12 Test: A workflow with `Mutators == nil` runs identically to a workflow without Mutators (no overhead, no panic)
- [x] 7.13 Test: Mutator's user function panicking is caught by the workflow's panic recovery (when `DontPanic` is true)
- [x] 7.14 Test: Plan-declared `Input` and Mutator-contributed `Input` both run, in expected order
- [x] 7.15 Test: Plan `Input` runs **before** Mutator-contributed `Input`; with multiple Mutators, Mutator-contributed Inputs run in `Workflow.Mutators` slice order, all after plan
- [x] 7.16 Test: Mutator merge does not happen at `Add` time (assert an external slice mutated only after `Do` reaches the step)
- [x] 7.17 Test: `Mutate[*Inner]` matches a `flow.Name("...", inner)` wrapper via Unwrap; the user function receives the typed `*Inner` pointer
- [ ] 7.18 Test: When Mutator matches via Unwrap, the resulting Builder's config merges into `w.state[wrapper].Config` (the wrapper key in the owning workflow), not into a state entry keyed on the inner step
- [x] 7.19 Test: `Mutate[*Wrapper]` registered against a `*Wrapper` wrapping `*Inner` matches `*Wrapper` (outer layer wins; user function receives `*Wrapper`, not `*Inner`)
- [ ] 7.20 Test: Parent Mutator targeting `*X` does NOT directly write into inner workflow's state map; the inner workflow performs the merge after `PrependMutators` (assert inner's state[wrapper].Config gets the contribution, parent's state map is untouched)
- [ ] 7.21 Test: Existing `Config merge destination follows StateOf` rule: `outer.Add(flow.Step(x).Input(fn))` where `x` lives in `inner` results in `fn` appearing in `inner.StateOf(x).Config.Before`, not in any outer state

## 8. Verify

- [ ] 8.1 `go build ./...` — no compile errors
- [ ] 8.2 `go test ./...` — all existing and new tests pass
- [ ] 8.3 `go vet ./...` — no issues
- [ ] 8.4 Confirm with the Step Interceptor change author that the shared scheduling-time host and ordering match before merge
