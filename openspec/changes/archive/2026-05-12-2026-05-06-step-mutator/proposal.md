## Why

Cross-cutting modifications of specific step types in production AKS e2ev3 today rely on
a two-part mechanism:

1. `BuildStep` — composite/sub-workflow steps materialize their inner DAG at `Add` time
   so that the outer workflow can see all nested steps.
2. Scenario-level mutators (`test.Mutator` / `WorkflowMutator`) walk the resulting tree
   with `flow.As[T](w)` and attach `Input` callbacks to mutate matching step instances.

This pair is fragile. `BuildStep` is invoked implicitly at `Add` time, can't return
errors, silently invokes a `Reset()` method by name, and uses a sentinel-by-method-signature
trick to detect sub-workflows. Production code routinely calls `BuildStep` again manually
inside `Do` or `Input` callbacks because the framework's "build at Add time" contract no
longer matches real usage. Mutators that match no step fail silently. Mutators implemented
via `As[T] + Input` can only attach `Input` callbacks — they cannot override `Retry`,
`Timeout`, `When`, or attach `Before`/`After` chains across all matching steps. And the
whole `BuildStep` machinery exists only to support tree traversal by mutators.

Add a first-class `flow.Mutator` to the workflow runtime that expresses "for any step of
type `*T`, configure it like this" by returning a `flow.Builder` — the same builder users
already use to declare per-step configuration in their plans. With it:

- Scenario-level mutator helpers compile to a one-line `flow.Mutate[T](fn)` registration.
- Mutators can override any aspect of step configuration: `Input`, `Output`,
  `BeforeStep`, `AfterStep`, `Retry`, `Timeout`, `When`, etc. — by returning a
  `flow.Builder` that carries the desired config.
- `As[T]` is no longer needed for mutation — only for ad-hoc introspection in tests.
- Sub-workflows can be constructed lazily inside `Do`, since cross-cutting mutation no
  longer requires the outer workflow to see the inner DAG up front.
- `BuildStep` becomes deprecated and can be removed in a follow-up change after AKS
  migrates its 117+ production usages.

A non-goal: pre-execution dump / introspection of the fully expanded workflow tree. No
production user has the requirement; the post-execution event stream from the Step
Interceptor design covers the "what happened" need with strictly more accuracy.

## What Changes

- Add `flow.Mutator` interface, `flow.Mutate[T](fn func(ctx context.Context, step T) Builder) Mutator`
  constructor, and `flow.MutatorReceiver` propagation interface.
- Add `Workflow.Mutators []Mutator` field.
- Wire **once-per-step** Mutator merging into the scheduling-time path (the path that
  takes a step from `Pending` to its first attempt). The merge runs before the first
  AttemptInterceptor and contributes to the step's `StepConfig` via the existing
  `StepConfig.Merge` rules.
- Add `MutatorsApplied bool` (or equivalent) on `State` to enforce once-per-step.
- Have `flow.SubWorkflow` implement `MutatorReceiver` so Mutators registered on a parent
  workflow propagate into nested sub-workflows.
- Have `*flow.Workflow` (used as a step) implement `MutatorReceiver` so nested workflows
  used directly as steps propagate naturally.
- Mark `BuildStep` (the user hook), `StepBuilder.BuildStep`, and the implicit
  `SubWorkflow.Reset()` invocation as `// Deprecated:` with pointers to Mutator-based
  patterns in their godoc.
- Add an example demonstrating the new pattern (`flow.Mutate` returning a `Builder` +
  `Do`-time sub-workflow construction) alongside the existing deprecated `BuildStep`
  example.
- Update the `composite-steps` spec to mark the `BuildStep` requirement as deprecated
  and to add the new sub-workflow-via-`Do` pattern as an alternative.
- Add a new `mutators` capability spec describing the interface, dispatch, position,
  Builder scope, and propagation rules.

## Capabilities

### New Capabilities

- `mutators`: type-dispatched once-per-step configuration contribution, Builder-returning
  signature, sub-workflow propagation, scope rules, and failure semantics.

### Modified Capabilities

- `composite-steps`: deprecate the `BuildStep` requirement; add a new requirement for
  `Do`-time sub-workflow construction; document `MutatorReceiver` propagation on
  `SubWorkflow` and `*Workflow`.
- `step-configuration`: add `Mutators` field on `Workflow`; clarify the merge rules apply
  to Mutator contributions identically to repeat `Add` calls.

## Impact

- **No breaking changes to existing workflow definitions.** `BuildStep` continues to run
  at `Add` time during the deprecation window. Existing scenario-level mutators using
  `flow.As[T](w)` keep working. The only public-API change that could break code is
  positional struct literals of `Workflow{...}` that pass 4+ arguments; this pattern is
  not used anywhere we know of (consumers use field-named literals or `&Workflow{}` with
  later assignment).
- **Authoring change for new Mutators (positive):** Mutators run **once per step**, not
  per attempt. Mutator authors no longer need to write idempotent functions. This is
  strictly easier than the legacy `As[T] + Input` pattern (where `Input` was per-attempt).
- **Coordination requirement:** This change depends on the Step Interceptor design
  landing in the same release; both share the scheduling-time path and define each
  other's stack ordering.
- **No new dependencies.**
- **No removals in this change.** A follow-up change will migrate AKS e2ev3 production
  code from `BuildStep` to `flow.Mutate` + `Do()`-time sub-workflow construction. The
  **next major version of `go-workflow`** will then remove `StepBuilder.BuildStep`, the
  implicit `BuildStep()` invocation in `Workflow.addStep`, the implicit
  `SubWorkflow.Reset()` call, and the corresponding `composite-steps` /
  `step-configuration` requirements. See `design.md` "Deprecation Plan for `BuildStep`
  and Related Surface" for the full removal list and timeline.
