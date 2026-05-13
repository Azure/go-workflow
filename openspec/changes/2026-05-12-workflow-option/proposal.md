# Workflow Option Consolidation

## Why

`Workflow` carries nine configuration fields directly on the struct
(`MaxConcurrency`, `DontPanic`, `SkipAsError`, `Clock`, `DefaultOption`,
`Mutators`, `StepInterceptors`, `AttemptInterceptors`, `IsolateInterceptors`)
and three independent propagation mechanisms (`MutatorReceiver`,
`InterceptorReceiver`, none-for-the-rest). Three problems:

1. **Cluttered struct surface.** Reading the `Workflow` godoc, the user must
   distinguish nine configuration fields from runtime-state fields by hand.
   Embedding `Workflow` in a user struct (the recommended way to build a
   sub-workflow) promotes all nine onto the user's type.
2. **Inconsistent propagation.** Mutators and Interceptors propagate via two
   different ad-hoc interfaces and two separate type-asserts in the
   scheduler. The other six configuration fields don't propagate at all.
   Adding a third workflow-level option that should flow into a child would
   need a third interface.
3. **`SubWorkflow` no longer earns its keep.** Its only remaining unique
   feature is hiding the inner `Workflow`'s exported field set. Once the nine
   configuration fields collapse under one `Option` named field, the
   "exposure" is reduced to a single name (`Option`) regardless of whether
   the user embeds `Workflow` or `SubWorkflow`. After Mutators deprecated
   `BuildStep`, `SubWorkflow.Reset` was already deprecated. Keeping
   `SubWorkflow` adds a parallel type with no unique capability.

Brainstorming context, alternatives considered, and per-decision rationale
are in `docs/superpowers/specs/2026-05-12-workflow-option-design.md`.

## What Changes

- Add `WorkflowOption` struct and `WorkflowOptionReceiver` interface in a new
  `workflow_option.go`. Scalar fields are pointer-typed (`*int`, `*bool`,
  `clock.Clock`, `*StepOption`) so unset / explicit-zero are distinguishable.
  Slice fields stay slices; parent slices are prepended to the child's on
  inheritance.
- Replace `Workflow`'s nine top-level configuration fields with a single
  `Option WorkflowOption` named field. **Breaking change.**
- Rename `IsolateInterceptors` → `Option.DontInherit`; semantics widen from
  "don't inherit interceptors" to "don't inherit any of the parent's
  WorkflowOption". Naming aligns with `DontPanic`.
- `*Workflow` implements `WorkflowOptionReceiver` via a new `InheritOption`
  method. Merge rules: nil scalar pointer → take parent's value; non-nil →
  child wins; slices → parent prepended; `DontInherit=true` → no-op.
- Rewrite the scheduler's parent→child propagation: a single `Do()` prologue
  pass walks each root step's Unwrap chain via a new `findOptionReceiver` and
  calls `InheritOption(w.Option)` once before `tick`. Replaces the two
  separate type-asserts at the previous `workflow.go:641` (Mutator) and
  `:768` (Interceptor) call sites.
- Replace `Workflow.inheritedStep` / `inheritedAttempt` side fields with a
  shallow snapshot of `w.Option` at `Do()` entry plus a `defer` restore at
  exit. Eliminates the special-case rule that `reset()` cannot clear those
  fields.
- **Remove (no deprecation):** `MutatorReceiver`, `InterceptorReceiver`,
  `Workflow.PrependMutators`, `Workflow.PrependInterceptors`,
  `findInterceptorReceiver`, `Workflow.IsolateInterceptors`,
  `SubWorkflow.PrependMutators`, `SubWorkflow.PrependInterceptors`. They are
  not in widespread external use; clean removal is preferred over carrying a
  parallel deprecated path.
- **Deprecate (remove next major):** `flow.SubWorkflow` (struct). It is
  embedded in user structs, so removal would break their type definitions
  rather than just call sites — one release window of grace. Add
  `SubWorkflow.InheritOption` delegating to the inner `Workflow` so
  inheritance still works during the window. `SubWorkflow.Reset` was already
  deprecated; remains deprecated.
- No helper for pointer-to-value (`flow.Bool`, `flow.Int`) — callers use
  `&v`, Go 1.26 `new(value)`, or their own one-line generic.
- Update `example/13_mutators_test.go` and any other examples that reference
  `flow.SubWorkflow` to embed `flow.Workflow` directly and use `Option`.

## Capabilities

### Modified Capabilities

- `workflow-options`: every requirement is rewritten — fields are now
  accessed under `Workflow.Option`, scalar fields are pointer-typed; new
  requirements describe `WorkflowOption`, `WorkflowOptionReceiver`,
  `InheritOption` propagation rules (scalar nil-inherit, slice prepend),
  `DontInherit` opt-out, `Do()` snapshot/restore semantics, and the
  prologue-pass propagation site.
- `composite-steps`: `flow.SubWorkflow` requirement marked deprecated;
  `SubWorkflow implements MutatorReceiver` and `*Workflow implements
  MutatorReceiver` requirements removed (the interface no longer exists);
  added requirement that `*Workflow` implements `WorkflowOptionReceiver` and
  is the recommended pattern for sub-workflows.
- `mutators`: requirements about `MutatorReceiver` propagation rewritten to
  reference `WorkflowOptionReceiver.InheritOption` instead. `Workflow.Mutators
  field` requirement updated to read `Workflow.Option.Mutators`.
- `step-interceptor`: `InterceptorReceiver` requirement removed;
  `IsolateInterceptors` requirement removed; replaced with reference to
  `WorkflowOptionReceiver` and `Option.DontInherit`. Field references updated
  to `Workflow.Option.StepInterceptors` /
  `Workflow.Option.AttemptInterceptors`.

## Impact

- **Breaking change to all callers** that construct `&Workflow{ Field: ... }`
  for any of the nine former top-level fields. Migration is mechanical: wrap
  in `Option: WorkflowOption{ ... }` and convert scalars to pointers.
- **Breaking change to anything implementing `MutatorReceiver` or
  `InterceptorReceiver` directly.** No known external implementations.
  Migration: implement `WorkflowOptionReceiver.InheritOption` instead.
- **Behavior change (positive):** scalar configuration fields (`DontPanic`,
  `MaxConcurrency`, `SkipAsError`, `Clock`, `StepDefaults`) now propagate
  from parent into sub-workflows when the child leaves them nil. Previously
  they didn't propagate at all. Existing code that sets these on the parent
  but didn't expect them on the child needs to set `Option.DontInherit` on
  the child or restate the desired non-default. This is the answer to the
  "I want sub-workflow to also be `DontPanic`" use case.
- **No behavior change to slice propagation.** `Mutators`,
  `StepInterceptors`, `AttemptInterceptors` continue to be parent-prepended
  (just via `InheritOption` instead of `PrependMutators` / `PrependInterceptors`).
- **No new dependencies.**
- **`SubWorkflow` deprecation window**: one minor release. The next major
  version of `go-workflow` removes the type entirely; users embedding
  `flow.SubWorkflow` MUST migrate to `flow.Workflow` before that release.
