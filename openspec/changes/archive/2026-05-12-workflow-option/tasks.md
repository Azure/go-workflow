# Tasks

## 1. New Types

- [x] 1.1 Create `workflow_option.go` with `WorkflowOption` struct (pointer scalars, slice fields, `DontInherit bool`)
- [x] 1.2 Add `WorkflowOptionReceiver` interface with `InheritOption(parent WorkflowOption)`
- [x] 1.3 Implement `prependSlice[T any](parent, child []T) []T` helper that always allocates a fresh backing array (godoc: "MUST NOT mutate either input")

## 2. Workflow Refactor

- [x] 2.1 In `workflow.go`, replace the nine top-level fields (`MaxConcurrency`, `DontPanic`, `SkipAsError`, `Clock`, `DefaultOption`, `Mutators`, `StepInterceptors`, `AttemptInterceptors`, `IsolateInterceptors`) with a single `Option WorkflowOption` field
- [x] 2.2 Remove `inheritedStep` / `inheritedAttempt` side fields from `Workflow`
- [x] 2.3 Add `(*Workflow).InheritOption(parent WorkflowOption)` implementing the merge rules (nil scalar → parent; non-nil scalar → child; slices → parent prepended; `DontInherit=true` → no-op)
- [x] 2.4 Add unexported scalar accessor helpers: `maxConcurrency()`, `dontPanic()`, `skipAsError()`, `clock()`. `Option.StepDefaults` is read directly (already a pointer, nil-safe at call sites).
- [x] 2.5 Rewrite all in-repo reads of the former fields to call the helpers (`workflow.go`, plus any other file that touches these — verify with `grep`)

## 3. Propagation Rewrite

- [x] 3.1 Add `findOptionReceiver(s Steper) WorkflowOptionReceiver` mirroring the removed `findInterceptorReceiver`
- [x] 3.2 In `Workflow.Do()`, after `init()` and before `tick`, walk root steps and call `recv.InheritOption(w.Option)` once per receiver
- [x] 3.3 In `Workflow.Do()`, snapshot `w.Option` immediately after acquiring `isRunning` lock and `defer` restore
- [x] 3.4 Remove the Mutator dispatch site that called `recv.PrependMutators` (currently around `workflow.go:641`)
- [x] 3.5 Remove the Interceptor dispatch site that called `findInterceptorReceiver` / `recv.PrependInterceptors` (currently around `workflow.go:768`)
- [x] 3.6 Simplify `effectiveStepInterceptors` / `effectiveAttemptInterceptors` to return `w.Option.StepInterceptors` / `w.Option.AttemptInterceptors` directly (parent contributions are already merged in by `InheritOption`)

## 4. Removals

- [x] 4.1 Delete `MutatorReceiver` interface from `mutator.go`
- [x] 4.2 Delete `InterceptorReceiver` interface and `findInterceptorReceiver` from `interceptor.go`
- [x] 4.3 Delete `(*Workflow).PrependMutators` method
- [x] 4.4 Delete `(*Workflow).PrependInterceptors` method
- [x] 4.5 Delete `(*SubWorkflow).PrependMutators` and `(*SubWorkflow).PrependInterceptors` methods

## 5. SubWorkflow Deprecation

- [x] 5.1 Add `// Deprecated: Embed flow.Workflow directly. SubWorkflow will be removed in the next major version of go-workflow.` to the `SubWorkflow` type godoc
- [x] 5.2 Add `(*SubWorkflow).InheritOption(parent WorkflowOption)` delegating to `s.w.InheritOption(parent)` so `SubWorkflow`-embedding steps continue to participate in propagation during the deprecation window
- [x] 5.3 Verify `SubWorkflow.Reset` retains its existing deprecation notice (no change needed if already correct)
- [x] 5.4 Add `// Deprecated:` notice at the `StepBuilder` *type* godoc level (the method `StepBuilder.BuildStep` is already deprecated). The notice SHALL state that the type, the `BuildStep()` user hook, and `SubWorkflow` will all be removed together in the next major version. Behavior is unchanged this release — `Workflow` still embeds `StepBuilder`, `Workflow.Add` still calls `w.BuildStep(step)`, user `BuildStep()` hooks still fire once.

## 6. Spec Updates

- [x] 6.1 Apply MODIFIED Requirements from `changes/2026-05-12-workflow-option/specs/workflow-options/spec.md` to `openspec/specs/workflow-options/spec.md`
- [x] 6.2 Apply MODIFIED Requirements from `changes/2026-05-12-workflow-option/specs/composite-steps/spec.md` to `openspec/specs/composite-steps/spec.md`:
   - Mark `SubWorkflow — Workflow as a Step` as deprecated
   - Remove `SubWorkflow implements MutatorReceiver` requirement
   - Remove `*Workflow implements MutatorReceiver` requirement
   - Add `*Workflow implements WorkflowOptionReceiver` requirement (recommended sub-workflow pattern via direct embedding)
- [x] 6.3 Apply MODIFIED Requirements from `changes/2026-05-12-workflow-option/specs/mutators/spec.md` to `openspec/specs/mutators/spec.md`:
   - Update field reference `Workflow.Mutators` → `Workflow.Option.Mutators`
   - Replace `PrependMutators` propagation references with `InheritOption(parent.Option)` references
- [x] 6.4 Apply MODIFIED Requirements from `changes/2026-05-12-workflow-option/specs/step-interceptor/spec.md` to `openspec/specs/step-interceptor/spec.md`:
   - Update field references `Workflow.StepInterceptors` / `AttemptInterceptors` → `Workflow.Option.StepInterceptors` / `AttemptInterceptors`
   - Remove `InterceptorReceiver` requirement
   - Remove `IsolateInterceptors` requirement; add reference to `Option.DontInherit`
   - Update propagation requirement to use `WorkflowOptionReceiver.InheritOption`

## 7. Tests

- [x] 7.1 Migrate `workflow_options_test.go`: every `&Workflow{ Field: ... }` literal rewritten to `Option: WorkflowOption{ ... }`; scalars wrapped as pointers; assertions unchanged
- [x] 7.2 Migrate `workflow_mutator_test.go`: `Workflow.Mutators` references → `Option.Mutators`; tests of `PrependMutators` rewritten to exercise `InheritOption`
- [x] 7.3 Migrate any other test file that references the nine former fields or the removed receiver interfaces
- [x] 7.4 Create `workflow_option_inherit_test.go` with table-driven matrix:
   - Scalar nil → parent's value used
   - Scalar non-nil → child wins
   - Scalar explicit zero (`*int = 0`) → child wins, distinguished from inherit
   - Each of `Mutators` / `StepInterceptors` / `AttemptInterceptors` → parent prepended
   - `Clock` nil → parent's clock used
   - `StepDefaults` nil → parent's value used
   - `DontInherit = true` → no field changes regardless of parent
- [x] 7.5 Test: multi-level nesting (grandparent → parent → child each with one Mutator) yields effective `[grandparent, parent, child]` Mutators on the child
- [x] 7.6 Test: `Do()` snapshot/restore — `parent.Do()` runs twice; child's effective Mutators is `[A, B]` during each run and `[B]` after each run, no accumulation
- [x] 7.7 Test: `SubWorkflow` deprecation smoke — embedded `SubWorkflow` still receives parent's Option via delegated `InheritOption`
- [x] 7.8 Test: scalar inheritance covers the motivating use case — parent with `DontPanic=true`, child without setting it; verify child's executed code path also recovers panics
- [x] 7.9 Test: `Workflow{}` zero value (no Option set) runs identically to before for a single-workflow case (no panic, no overhead)

## 8. Examples

- [x] 8.1 Update `example/13_mutators_test.go` example that references `flow.SubWorkflow` to embed `flow.Workflow` directly and use `Option`
- [x] 8.2 Update any other example file referencing the nine former fields or `flow.SubWorkflow`
- [x] 8.3 Add example demonstrating scalar inheritance: parent sets `DontPanic`, child inherits it without restating

## 9. Documentation

- [x] 9.1 Update `Workflow` godoc: remove field-by-field documentation of the nine former fields; add reference to `Option` and `WorkflowOption`
- [x] 9.2 Add `WorkflowOption` and `WorkflowOptionReceiver` godoc with merge rules and snapshot/restore note
- [x] 9.3 Update README sub-workflow section if any to recommend embedding `flow.Workflow` directly; note `SubWorkflow` deprecation
- [x] 9.4 Add CHANGELOG entry summarizing the breaking change, migration steps, and the scalar-inheritance behavior change
   - **N/A:** repo does not maintain a CHANGELOG file; release notes captured in the OpenSpec change folder instead.
- [x] 9.5 Rewrite `(*Workflow).Reset()` godoc: drop the obsolete `inheritedStep` / `inheritedAttempt` paragraph; state the new contract — Reset rewinds per-step status only, never modifies `w.steps` or `w.Option`; note that calling Reset between Do() runs is optional from an Option-isolation standpoint (the snapshot/restore in Do() already prevents accumulation)
- [x] 9.6 In the godoc for `flow.Workflow` (or a dedicated example), call out the two sub-workflow patterns and their tradeoffs:
   - Embed `flow.Workflow` and call `Add` at construction time (recommended; supports introspection and parent Option inheritance)
   - Construct `&flow.Workflow{}` inline inside `Do()` (opaque to parent unless the host step also implements `WorkflowOptionReceiver` to forward parent Option)
   - Lazy build guarded by `sync.Once` (acceptable for single-construction, multi-execution lifecycle on the same composite step)
- [x] 9.7 Add a strong warning to `(*Workflow).Add` godoc: calling `Add` from inside the same Workflow's `Do()` (or any method transitively reachable from `Do()`) is **forbidden** unless guarded by `sync.Once`. State the failure modes (callback accumulation, duplicate steps, multi-match introspection, stale Mutator dispatch) explicitly. Reference the `composite-steps` "Composite step MUST NOT call Add inside Do unguarded" scenario.

## 11. Future Work (Out of Scope)

- [ ] 11.1 (future) Static analysis check (`go vet` analyzer or standalone linter) that flags `flow.Workflow.Add` called transitively from a `Do()` method without a `sync.Once` guard. The framework SHALL NOT add a runtime panic for this misuse — neither the parent's `isRunning` lock nor `x.isRunning` (acquired/released across retry attempts) offers a stable signal for distinguishing legitimate `sync.Once`-guarded lazy init from unguarded re-`Add`. A separate lint tool is the right layer.

## 10. Verify

- [x] 10.1 `go build ./...` — no compile errors
- [x] 10.2 `go test ./...` — all migrated and new tests pass
- [x] 10.3 `go vet ./...` — no issues
- [x] 10.4 `grep -rn "PrependMutators\|PrependInterceptors\|MutatorReceiver\|InterceptorReceiver\|IsolateInterceptors\|inheritedStep\|inheritedAttempt\|findInterceptorReceiver" --include="*.go"` returns only expected residual hits (none in source; possibly historical refs in archived openspec)
- [x] 10.5 `grep -rn "DefaultOption\|w\.MaxConcurrency\|w\.DontPanic\|w\.SkipAsError\|w\.Clock\|w\.Mutators\|w\.StepInterceptors\|w\.AttemptInterceptors" --include="*.go" | grep -v _test.go` — no source-side direct reads of the removed fields
- [x] 10.6 Confirm `SubWorkflow`-embedding example compiles, runs, and inherits parent Option correctly
