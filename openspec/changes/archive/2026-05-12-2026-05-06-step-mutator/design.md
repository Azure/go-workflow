# Step Mutator — Design

This change introduces type-dispatched step Mutators in `go-workflow` and deprecates the
`BuildStep` mechanism. Detailed rationale, alternatives, naming discussion, and
architectural reasoning are in the brainstorming spec at
`docs/superpowers/specs/2026-05-06-step-mutator-design.md`.

This document supplies the implementation-facing specifics needed to evaluate and
implement the change. Anything covered in the brainstorming spec is referenced rather
than duplicated.

---

## Summary of Decisions

| Decision | Choice | Reason |
|----------|--------|--------|
| Public name | `Mutator` (not `Middleware`) | Configure-only semantics; "middleware" implies wrap-around with `next`. Aligns with existing AKS `test.Mutator` and K8s `MutatingAdmissionWebhook`. |
| Function signature | `func(ctx context.Context, step T) Builder` | Returns a fluent `Builder` — the same vocabulary plan authors already use (Input, Output, BeforeStep, Retry, Timeout, When, etc.). `ctx` is the workflow-scoped ctx from `Workflow.Do(ctx)`, suitable for accessing logger / scenario name / test session ID. No `error` return: Mutators run at scheduling time and "failure" semantics are unclear there; failable work belongs in the returned Builder's `Input` callback. |
| When invoked | Once per step instance, just before its first attempt | AKS production usage is all static configuration. Per-attempt with config-mutation power silently grows callback slices on retry. |
| Selector | Type assertion `step.(T)` walking the `Unwrap()` chain (first matching layer wins) | Production targets concrete types; legacy `As[T] + Input` already followed `Unwrap`, so this preserves migration parity. Stops at workflow boundaries — parent Mutators reach inner steps via `PrependMutators`, not by walking across. |
| Registration | Slice field `Workflow.Mutators []Mutator` | Matches `StepInterceptors` / `AttemptInterceptors` style. No `Use(...)` method. |
| Stack position | Mutator merges into StepConfig **before** the first per-attempt onion (Interceptors / Before / Do / After) | Once-and-done, then the standard onion runs as if the config had been declared at Add time. |
| Merge ordering | Mutator-contributed callbacks **append** to plan-declared callbacks (plan first, Mutator after) | "Scenario customization layered on top of plan defaults" — Mutators observe/override plan state, not pre-empt it. Mirrors K8s admission webhook (mutating-after-default). |
| Sub-workflow propagation | Optional `MutatorReceiver` interface (`PrependMutators(mw []Mutator)`) | Same model as interceptor `PrependInterceptors`. |
| Builder scope | Only config for the step passed in is merged; merge destination is the wrapper key in the workflow that owns it (per `Config merge destination follows StateOf`) | Predictable scope; matches existing `Add`-time merge behavior; lets Mutator authors write `flow.Step(typedPointer).Input(...)` without worrying about which workflow level the state lives at. |
| Nested propagation | Parent prepends to inner's `Mutators` slice; inner dispatches against its own state map | Lazy inner steps work, multi-level nesting recurses cleanly, mirrors `InterceptorReceiver` pattern, avoids parent reaching across workflow boundaries. |
| Context substitution | Mutator function does not return a substituted ctx | Mutators run at scheduling time, before the per-attempt onion; they have no way to influence the ctx that `step.Do` receives. The Mutator DOES receive the workflow-scoped ctx as a read-only input. For per-attempt ctx substitution, the Mutator's returned Builder can include `BeforeStep`. |
| Return nil Builder | Allowed | Useful when the Mutator only mutates fields on the typed step pointer. |
| `BuildStep` fate | Deprecated, not removed | Allows AKS e2ev3 to migrate 117+ usages incrementally. |
| Pre-execution dump | Not supported | No real user; interceptor event stream covers actual need. |

---

## Coordination With the Step Interceptor Change

This change shares execution-model territory with
`docs/superpowers/specs/2026-05-06-step-interceptor-design.md` (separate session). The two
must land in the same release. Concretely:

- The Mutator merge is invoked from the same scheduling-time path that the interceptor
  change introduces (the path that takes a step from `Pending` to `Running`). The merge
  happens **before** the first AttemptInterceptor; both happen before the first `Before`
  callback.
- `MutatorsApplied` (or equivalent) is added to `State` to ensure once-per-step
  semantics. The interceptor change does not need this flag, so it is purely additive
  here.
- Sub-workflow propagation reuses the receiver-based mechanism: where the interceptor
  change calls `recv.PrependInterceptors(...)`, this change immediately follows with
  `recv.PrependMutators(...)`.

If the interceptor change's structural plan changes, this change MUST be re-aligned.
Conflicts to watch for: location of the scheduling-time hook, naming of `stepExecution`,
the field on `State` that records "first attempt scheduled".

---

## Implementation Notes

### Type-erased dispatch via generic constructor

The `Mutator` interface deliberately exposes only an unexported method:

```go
type Mutator interface {
    applyTo(ctx context.Context, step Steper) (matched bool, target Steper, builder Builder)
}
```

Returning `target` (the layer where `T` matched, possibly equal to `step` itself) allows
the caller to know which `Steper` key the user's Builder was authored against. The
merge logic (see "First-schedule merge") rebases the Builder's per-step config onto the
wrapper key `step` before merging.

This is so the only producer is `flow.Mutate[T](fn)`, which type-erases the user's typed
function `func(context.Context, T) Builder` into the `Mutator` interface. The
implementation walks the unwrap chain:

```go
func Mutate[T Steper](fn func(ctx context.Context, step T) Builder) Mutator {
    return mutatorFunc[T](fn)
}

type mutatorFunc[T Steper] func(ctx context.Context, step T) Builder

func (m mutatorFunc[T]) applyTo(ctx context.Context, step Steper) (bool, Steper, Builder) {
    // walk the Unwrap chain in pre-order; first layer whose concrete type matches T wins.
    var found T
    matched := false
    var matchedStep Steper
    Traverse(step, func(s Steper, _ []Steper) TraverseDecision {
        if typed, ok := s.(T); ok {
            found = typed
            matchedStep = s
            matched = true
            return TraverseStop
        }
        return TraverseContinue
    })
    if !matched {
        return false, nil, nil
    }
    return true, matchedStep, m(ctx, found)
}
```

Unwrap traversal does NOT cross workflow boundaries — `Traverse` stops when it would
descend into a `*Workflow`'s inner state. Inner-workflow steps are reached through
`PrependMutators`, not through cross-boundary unwrap.

The unexported method prevents users from hand-rolling implementations that bypass the
type-dispatch contract — important because user-supplied implementations could change the
selector semantics in surprising ways and fragment the model.

### First-schedule merge

Inside the path that schedules a step's first attempt (precise placement TBD pending
final shape of the interceptor change's `stepExecution`):

```go
// pseudocode — runs once per (workflow, wrapper) pair at wrapper's first scheduling
if !state.MutatorsApplied {
    for _, m := range w.Mutators {
        matched, target, b := m.applyTo(ctx, wrapper) // walks wrapper's Unwrap chain
        if !matched || b == nil {
            continue
        }
        for s, cfg := range b.AddToWorkflow() {
            if s == target {
                // rebase the per-step config onto the wrapper key in this workflow's state
                state.Config.Merge(cfg)
            }
            // configurations for other steps are silently ignored — Mutator scope is
            // the layer that was passed in (and its rebase target is the wrapper)
        }
    }
    state.MutatorsApplied = true
}
// then proceed: AttemptInterceptors → Before → Do → After
```

The merge destination is always `state` (the inner-workflow's `state[wrapper]`), even
when the matched layer `target` is several `Unwrap` levels below `wrapper`. This is the
same rule as the existing `Config merge destination follows StateOf` requirement in
`step-configuration`.

The `Merge` operation reuses the existing `StepConfig.Merge` (already present, used by
the `Idempotent Add with config merging` requirement). No new merge logic is introduced.

### Sub-workflow propagation

`SubWorkflow` and `*Workflow` (used as a step) implement `MutatorReceiver`:

```go
type MutatorReceiver interface {
    PrependMutators(mw []Mutator)
}

func (s *SubWorkflow) PrependMutators(mw []Mutator) {
    if len(mw) == 0 {
        return
    }
    s.w.Mutators = append(append([]Mutator{}, mw...), s.w.Mutators...)
}

func (w *Workflow) PrependMutators(mw []Mutator) {
    if len(mw) == 0 {
        return
    }
    w.Mutators = append(append([]Mutator{}, mw...), w.Mutators...)
}
```

The runtime invokes `PrependMutators` once per inner-workflow execution, before the
inner workflow begins scheduling its own steps. Inner steps that are added lazily inside
the composite step's `Do` are still reached, because Mutator evaluation in the inner
workflow is itself once-per-step at first scheduling.

Re-prepending the same Mutator list is safe: the once-per-step invariant
(`MutatorsApplied`) prevents double-execution.

### Idempotency story

Because Mutators run once per step, **Mutator authors do not need to write idempotent
functions**. They can append to slices, allocate new objects, etc., without worrying
about retries causing duplicate work. This is a deliberate change from the legacy
`As[T] + Input` pattern, where the `Input` callback was per-attempt and authors had to
defensively guard against being called twice.

### `*Workflow` as `Steper` already

`*Workflow` already implements `Steper` and is usable as a step in another workflow. We
add `PrependMutators` to it directly so nested workflows inherit Mutators automatically.
This is a small expansion of `Workflow`'s existing surface.

---

## Deprecation Plan for `BuildStep` and Related Surface

This change does not remove `BuildStep`. It marks it deprecated and **commits to removal
in the next major version** of `go-workflow` once production migration is complete.

### What gets removed at the next major version

The following symbols / behaviors SHALL be considered candidates for removal in the
next major release of `go-workflow`. They are all subsumed by the Mutator mechanism
(`flow.Mutate[T]`) plus `Do()`-time sub-workflow construction:

| Symbol / behavior | Replacement | Location |
|-------------------|-------------|----------|
| `StepBuilder.BuildStep(s Steper)` method (the lazy-build hook invoked by `Workflow.addStep`) | Sub-workflow construction inside `Do()` (see `composite-steps` requirement `Sub-workflow construction inside Do`) | `build_step.go` |
| The implicit invocation `w.BuildStep(step)` inside `Workflow.addStep` | n/a (callers construct their sub-workflow themselves at `Do` time) | `workflow.go` |
| The `BuildStep()` interface check / sentinel-by-method-signature trick used to detect composite steps with lazy initialization | n/a | `build_step.go` |
| `flow.SubWorkflow.Reset()` method (only invoked by the deprecated `BuildStep` path) | n/a (sub-workflow is constructed fresh inside `Do`, no reset needed) | `workflow.go` |
| The `BuildStep — lazy initialization hook` requirement in `composite-steps` spec | the new `Sub-workflow construction inside Do` requirement | `openspec/specs/composite-steps/spec.md` |
| The `BuildStep lifecycle hook` requirement in `step-configuration` spec | same | `openspec/specs/step-configuration/spec.md` |

`flow.As[T]`, `flow.Has[T]`, `flow.HasStep` are NOT in this list — they remain valid
introspection helpers for tests and debugging. Only the mutation-driven use of
`As[T] + Input` is replaced by `flow.Mutate[T]`.

### Deprecation timeline

This change (current release): **mark deprecated**

- Add `// Deprecated:` godoc to `StepBuilder.BuildStep` in `build_step.go`, pointing to
  `flow.Mutate` for cross-cutting modification and to the `Do`-time sub-workflow pattern
  for composite step construction.
- Add `// Deprecated:` godoc to `flow.SubWorkflow.Reset` in `workflow.go`, noting that
  it is only invoked by the deprecated `StepBuilder.BuildStep` path.
- Add `// Deprecated:` godoc to the `BuildStep` method template (the comment block in
  `build_step.go` that demonstrates how users should write the hook).
- Update `example/13_composite_step_test.go` `BuildStep` usage with a `// Deprecated:`
  doc-comment and add a parallel `ExampleCompositeViaDo` showing the new pattern.
- Mark the corresponding `composite-steps` requirement as **DEPRECATED** in the spec
  (already done in this change's `composite-steps/spec.md` delta).
- Behavior at runtime is **unchanged**. Existing code compiles and runs identically;
  only `go vet` / IDE / staticcheck warnings appear.

Follow-up change (separate proposal, between current and next major): **migrate**

- Migrate AKS e2ev3 production usages — primarily by converting `BuildStep` bodies into
  `Do`-time `flow.Workflow{}.Add(...)` blocks, and converting scenario-level
  `WorkflowMutator` helpers into `flow.Mutate[T]` registrations.
- Track AKS e2ev3 `BuildStep` usage count down to zero before opening the removal PR.

Next major version (e.g. `v2.0.0` or whichever bump the project uses for breaking
changes): **remove**

- Remove the `BuildStep()` method dispatch from `Workflow.addStep`. Steps that still
  define a `BuildStep()` method SHALL no longer have it invoked implicitly.
- Remove the `StepBuilder` embed point.
- Remove the implicit `SubWorkflow.Reset()` call. `Reset` itself MAY be kept as a public
  no-op helper or removed; this is decided at removal time based on whether any user
  code is calling `Reset` directly.
- Delete the `BuildStep — lazy initialization hook` requirement from
  `openspec/specs/composite-steps/spec.md` and the `BuildStep lifecycle hook` requirement
  from `openspec/specs/step-configuration/spec.md`. Both are replaced by the
  `Sub-workflow construction inside Do` requirement (already added in this change).
- The major version bump is the explicit signal that this is a breaking change; this is
  what justifies removing the implicit hook rather than leaving it deprecated forever.

### Why the next major version, not later

`BuildStep` exists solely to support the legacy `As[T] + Input` mutator pattern.
Once `flow.Mutate` lands and AKS migrates, no production user has a reason to call
`BuildStep`. Leaving it indefinitely would:

- Keep two parallel mechanisms in the public surface, confusing new users.
- Force the runtime to keep paying for the `BuildStep()` interface check on every
  `addStep` call.
- Preserve the unfortunate sentinel-by-method-signature trick, which is a footgun
  (any unrelated type that happens to define `BuildStep()` would be mistaken for a
  composite step).

A major-version bump is the standard tool for closing this kind of deprecation, and
this change registers the intent now so consumers know to plan for it.

### Future consideration: simplifying `SubWorkflow`

Once `BuildStep` and `SubWorkflow.Reset` are removed, `SubWorkflow` retains two
responsibilities that are still useful:

1. **Hold an inner `Workflow` as a struct field**, so a composite step has a stable
   handle through which the runtime can call `PrependMutators` / `PrependInterceptors`
   before the composite's `Do` runs.
2. **Default `Unwrap() []Steper`** so parent `As[T]` / `Has` traversal can see inner
   steps (also required for parent Mutator dispatch when the user has registered a
   Mutator targeting an inner type but the propagation receiver is the composite step).

Both of these are also satisfied by **embedding `*flow.Workflow` directly** in the
composite step struct. `*Workflow` already implements `Steper`, `Unwrap`, and (after
this change) `MutatorReceiver`. So `SubWorkflow` is, in principle, collapsible to
either:

- a type alias `type SubWorkflow = Workflow` (backward-compatible name)
- or removal entirely, with the migration recipe "embed `*flow.Workflow` instead"

The reasons NOT to do this collapse in the same change as `BuildStep` removal:

- `SubWorkflow` is a **value-embedded struct**, supporting zero-value composite step
  initialization (`var s MyComposite` works). `*flow.Workflow` is pointer-embedded and
  requires explicit construction (`&MyComposite{Workflow: &flow.Workflow{}}`). This
  difference is observable to users and changes the idiomatic pattern.
- `SubWorkflow` may have downstream behavior (`String`, formatting, JSON marshaling)
  that production users have come to depend on. None of this is currently surveyed.
- The `SubWorkflow` collapse is **independent in scope** from the `BuildStep` removal:
  `BuildStep` removal is about killing a misfeature; `SubWorkflow` collapse is about
  reducing API surface. Bundling them together would couple two unrelated migrations.

**Disposition for this change:** record the consideration here, but do NOT mark
`SubWorkflow` as deprecated and do NOT schedule it for removal in the next major. A
**separate proposal** evaluates the collapse on its own merits — surveying production
usage patterns (zero-value vs. explicit construction, formatting, etc.) and proposing
either:

- (a) An alias `type SubWorkflow = Workflow` that keeps the name and gives users one
  unified type to embed.
- (b) Removal, with a migration guide for replacing the embed.
- (c) Status quo, if the survey reveals enough zero-value usages or behavioral
  differences to justify keeping it.

That proposal is **out of scope here** but the link is recorded so the question is not
forgotten.

---

## Risk and Rollout

- The change is **additive** for existing code. Existing tests pass without modification.
- The Mutator field is `nil` for any workflow that doesn't use the feature; the
  scheduling-time check short-circuits on a `nil` slice.
- Migration is opt-in. AKS scenario-level helpers can be migrated module by module.
- The deprecation window for `BuildStep` is bounded: it ends at the next major version
  of `go-workflow`, by which time all in-repo and AKS-production usages must have
  migrated. See the Deprecation Plan above for the removal list.

---

## Out of Scope

- Removing `BuildStep` and related symbols (`StepBuilder`, the implicit `SubWorkflow.Reset`
  call, the `BuildStep()` interface check). These are scheduled for removal in the next
  major version of `go-workflow` (see the Deprecation Plan above for the full list and
  rationale). This change only marks them deprecated.
- Pre-execution dump / introspection (explicit non-goal; see brainstorming spec).
- Per-attempt Mutator API variant (explicit non-goal; if needed, the Mutator's returned
  Builder can include per-attempt callbacks).
- Mutator wrap-around / `next` semantics (explicit non-goal; the interceptor design
  covers wrap-around for users who need it).
- Selector generalization beyond exact concrete type (explicit non-goal).
- Removing `Has[T]` / `As[T]` / `HasStep` (kept as introspection helpers).
- Collapsing or removing `flow.SubWorkflow` itself. The `SubWorkflow.Reset()` method is
  marked deprecated alongside `BuildStep`, but the wrapper struct is preserved. A
  separate future proposal will evaluate whether to alias `SubWorkflow` to `Workflow`
  or remove it; see "Future consideration: simplifying `SubWorkflow`" in the
  Deprecation Plan section.
