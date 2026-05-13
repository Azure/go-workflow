# mutators Specification

## Purpose

This capability covers `flow.Mutator` — the type-dispatched, once-per-step
configuration contributor that lets workflow authors apply cross-cutting
defaults, callbacks, and retry/timeout policy to every Step of a chosen Go
type, including Steps that live inside nested sub-workflows.

## Requirements

### Requirement: Mutator interface and Mutate constructor

The `flow.Mutator` interface SHALL represent a type-dispatched, **once-per-step**
contribution of configuration to a Step. The interface has a single unexported method,
ensuring that the only producer is the generic constructor `flow.Mutate[T]`.

`flow.Mutate[T Steper](fn func(ctx context.Context, step T) Builder) Mutator` SHALL
construct a Mutator that:

- Performs a type assertion `step.(T)` against each step it is offered, **walking the
  `Unwrap()` chain** if the immediate step does not match. The first layer in the chain
  whose concrete type matches `T` is the one `fn` receives.
- Calls `fn` with the workflow's context and the type-asserted step instance if and only
  if some layer in the unwrap chain matches.
- Returns the `Builder` that `fn` returns, for the runtime to merge into the step's
  `StepConfig`.
- Treats `fn` returning a `nil` Builder as "no additional configuration" (still a valid
  Mutator — useful when `fn` only mutates fields on the step pointer).
- Is a no-op (returns `matched=false`) when no layer in the unwrap chain satisfies the
  assertion.

Selection is by concrete type along the unwrap chain only. There is no interface
matching and no name matching. The unwrap traversal mirrors `flow.As[T]`: it follows
`Unwrap() Steper` and `Unwrap() []Steper`, but does NOT cross workflow boundaries (a
nested workflow's inner steps are reached via the propagation mechanism in the
`Mutator propagation to nested workflows` requirement, not via in-place unwrap from
the parent).

The `ctx` passed to `fn` is the **workflow-scoped context** — i.e. the `ctx` that was
passed into `Workflow.Do(ctx)`. It is suitable for accessing values that are stable for
the life of the workflow (logger, scenario name, test session ID). It is NOT a
per-attempt context: the Mutator runs once per step at scheduling time, before any
attempt begins.

A Mutator does NOT return a substituted `context.Context`. The Mutator is not in the
per-attempt onion and cannot influence the `ctx` that `step.Do` receives. Cross-cutting
ctx substitution remains the responsibility of `BeforeStep` (per instance) or
`StepInterceptor` / `AttemptInterceptor` (cross-cutting). The Mutator MAY return a
`Builder` that includes a `BeforeStep` callback if it wants per-attempt ctx influence
through composition.

#### Scenario: Mutate runs against a matching type
- **WHEN** `flow.Mutate[*Foo]` is registered and the workflow contains a `*Foo` step
- **THEN** the Mutator function is invoked with that `*Foo` instance once, before its
  first attempt

#### Scenario: Mutate skips a non-matching type
- **WHEN** `flow.Mutate[*Foo]` is registered and the workflow contains only `*Bar` steps
- **THEN** the Mutator function is never invoked

#### Scenario: Mutate matches inner type through Unwrap chain
- **WHEN** `flow.Mutate[*Inner]` is registered and the workflow contains a wrapper step
  whose `Unwrap()` returns a `*Inner` (e.g. `flow.Name("...", inner)`)
- **THEN** the Mutator runs against the `*Inner` instance; the user function receives
  the typed `*Inner` pointer

#### Scenario: Mutate matches outer wrapper when wrapper itself is the target type
- **WHEN** `flow.Mutate[*Wrapper]` is registered and the workflow contains a `*Wrapper`
  wrapping a `*Inner`
- **THEN** the Mutator runs against the `*Wrapper` instance (first matching layer wins;
  `*Wrapper` matches before unwrap descends to `*Inner`)

#### Scenario: Returning nil Builder is valid
- **WHEN** the Mutator function returns `nil`
- **THEN** the runtime treats it as no additional configuration; no error, no panic

#### Scenario: Mutator function receives workflow-scoped ctx, returns Builder, no error
- **WHEN** a developer writes `flow.Mutate[*Foo](fn)`
- **THEN** the type of `fn` is `func(ctx context.Context, step *Foo) flow.Builder`,
  with no error return
- **AND** when the Mutator runs, `ctx` is the same context that was passed to
  `Workflow.Do(ctx)`

---

---

### Requirement: Mutator dispatch and merge destination

The runtime SHALL dispatch and merge Mutators using the following rule, hereafter
"**dispatch by the owning workflow**":

1. Each `*Workflow` (parent or inner) SHALL evaluate its own `Workflow.Option.Mutators`
   slice against every step in its own `state` map, at that step's first scheduling.
2. For each step `wrapper` in `w.state`, the runtime SHALL walk `wrapper`'s `Unwrap()`
   chain. For each layer `L` in the chain (including `wrapper` itself, in pre-order),
   the runtime SHALL ask each Mutator whether `L.(T)` matches. The first matching layer
   SHALL be passed to the Mutator's user function.
3. The `Builder` returned by the user function SHALL be merged into
   `w.state[wrapper].Config` — i.e. into the **wrapper key in the workflow that owns
   `wrapper` as a root**, regardless of which layer in the unwrap chain the Mutator
   matched. This matches the `Config merge destination follows StateOf` rule in
   `step-configuration`.
4. Parent Mutators SHALL reach inner-workflow steps only via the
   `WorkflowOptionReceiver.InheritOption` propagation mechanism defined in the
   `workflow-options` capability (parent prepends its `Option.Mutators` into the inner
   workflow's `Option.Mutators` slice). The parent SHALL NOT directly walk into a child
   workflow's `state` map to apply Mutators; that is the inner workflow's responsibility
   once it begins scheduling. This keeps once-per-step state, merge destinations, and
   lazily added inner steps consistent across all nesting depths.

This rule resolves three previously-ambiguous points:

- **Unwrap is supported** — `flow.Mutate[*Inner]` matches a `flow.Name("...", inner)`
  wrapper. Migration from the legacy `As[T] + Input` pattern does not break on wrapper
  types.
- **Config destination is well-defined** — the merge target is always the wrapper key
  in the workflow that actually owns the step, not the outer workflow on which the
  Mutator was registered.
- **Nested workflows are reached by propagation, not by parent walking** — a parent
  Mutator targeting `*X` where `*X` lives inside an inner workflow IS observed by
  `*X`, but the dispatch is performed by the inner workflow after `InheritOption`
  prepends the parent's contributions, not by the parent reaching across the workflow
  boundary.

#### Scenario: Mutator matches via Unwrap and merges into wrapper's state
- **GIVEN** a workflow `w` where `w.state` contains `wrapper = flow.Name("x", inner)`
  whose `Unwrap()` returns `inner` of type `*Inner`
- **AND** `w.Option.Mutators = [flow.Mutate[*Inner](func(ctx, x *Inner) Builder { return flow.Step(x).Input(fn) })]`
- **WHEN** `wrapper` reaches its first scheduling
- **THEN** the Mutator's user function receives the `*Inner` pointer `inner`
- **AND** `fn` is appended to `w.state[wrapper].Config.Before` (NOT to a state entry
  keyed on `inner`)

#### Scenario: Parent Mutator targeting inner type does not directly mutate inner state
- **GIVEN** `parent` contains `inner` (a `*Workflow` used as a step) which contains
  `x` of type `*X`
- **AND** `parent.Option.Mutators = [flow.Mutate[*X](fn)]`
- **WHEN** `parent.Do(ctx)` runs
- **THEN** `parent` SHALL invoke `inner.InheritOption(parent.Option)` once before
  scheduling, propagating the parent's `Option.Mutators` into `inner.Option.Mutators`
- **AND** the Mutator runs once against `x` inside `inner`'s scheduling pass; the
  Builder is merged into `inner.state[x].Config`, NOT into any state entry on `parent`

#### Scenario: Wrapper inside inner workflow is reached via propagation
- **GIVEN** `parent → inner → flow.Name("x", x)` where `x` is `*X`
- **AND** `parent.Option.Mutators = [flow.Mutate[*X](fn)]`
- **WHEN** the wrapper reaches first scheduling inside `inner`
- **THEN** the Mutator (propagated by `InheritOption`) unwraps the wrapper, matches
  `x`, and merges its Builder into `inner.state[wrapper].Config`

---

### Requirement: Workflow.Option.Mutators field

`flow.Workflow` SHALL expose a `Mutators []Mutator` field under
`Workflow.Option` (the new `WorkflowOption` grouping introduced by the
`workflow-options` capability). Mutators in this slice are evaluated in
slice order against each step in the Workflow's state map: element 0 runs
first, element n-1 runs last, among the Mutators whose target type matches
that step.

A workflow with `Option.Mutators == nil` or an empty slice SHALL behave
identically to one without any Mutators at all.

The previous top-level `Workflow.Mutators` field SHALL NOT exist.

#### Scenario: Slice order is preserved among matching Mutators
- **WHEN** two Mutators `[m1, m2]` both registered for `*Foo` are present under
  `Option.Mutators`, and `m1` and `m2` each return a Builder with a `BeforeStep`
  callback
- **THEN** the merged step's `Before` chain runs `m1`'s callback before `m2`'s

#### Scenario: Multiple Mutators for the same type all run
- **WHEN** two Mutators `m1`, `m2` are both registered for `*Foo` under `Option.Mutators`
- **THEN** both run, in slice order, before the first attempt of every `*Foo` step;
  their contributions are merged

#### Scenario: Nil Mutators field is a no-op
- **WHEN** `Workflow.Option.Mutators` is nil
- **THEN** the workflow runs identically to a workflow without the Mutator field

---

### Requirement: Mutator merge timing and ordering

Mutators SHALL be merged into a step's `StepConfig` **after** all `Workflow.Add` calls
for that step have completed, but **before** the step's first attempt begins.
Specifically, the merge happens when the step transitions from `Pending` to its first
scheduling — i.e. inside `Workflow.Do`, on demand, not at `Add` time.

Mutator-contributed `Before` / `After` / `Option` lists SHALL be **appended** to the
step's existing lists via the standard `StepConfig.Merge` rule (see
`step-configuration`'s `Idempotent Add with config merging` requirement). Consequently:

- **Before chain execution order:** plan-declared `Input` / `BeforeStep` run first;
  Mutator-contributed callbacks run after, in `Workflow.Option.Mutators` slice order.
- **After chain execution order:** plan-declared `Output` / `AfterStep` run first;
  Mutator-contributed callbacks run after, in `Workflow.Option.Mutators` slice order.
- **Multiple Mutators on the same step:** their callbacks all append after plan's;
  among Mutators, slice order is preserved.

`Upstreams` contributed by Mutators are unioned with existing upstreams (set union, not
ordered).

This ordering matches the "scenario customization layered on top of plan defaults"
mental model: a Mutator can observe or override state left by plan callbacks. A Mutator
cannot pre-empt plan callbacks; if pre-empting is needed (rare), register the behavior
as a plan-time `Input` instead.

Sub-workflow propagation interacts with this ordering as follows: parent Mutators are
prepended into the inner workflow's `Option.Mutators` slice (see
`Mutator propagation to nested workflows` requirement) so that, inside the inner
workflow, parent Mutators run before child Mutators. Both still run after the inner
step's plan-declared callbacks.

#### Scenario: Plan Input runs before Mutator Input
- **GIVEN** a step `s` added via `flow.Step(s).Input(planFn)`
- **AND** a Mutator `flow.Mutate[*S](func(ctx, s *S) flow.Builder {
  return flow.Step(s).Input(mutatorFn) })`
- **WHEN** the step executes its first attempt
- **THEN** `planFn` runs first, then `mutatorFn`, then `Do`

#### Scenario: Multiple Mutator Inputs run in slice order, after plan
- **GIVEN** a step `s` added via `flow.Step(s).Input(planFn)`
- **AND** `Workflow.Option.Mutators = [m1, m2]` where both target `*S` and contribute Inputs
  `mfn1`, `mfn2` respectively
- **WHEN** the step executes its first attempt
- **THEN** the Before chain runs `planFn`, then `mfn1`, then `mfn2`, then `Do`

#### Scenario: Plan After runs before Mutator After
- **GIVEN** a step with a plan-declared `Output(planAfter)` and a Mutator that returns
  `flow.Step(s).Output(mutatorAfter)`
- **WHEN** the step's `Do` returns successfully
- **THEN** `planAfter` runs first, then `mutatorAfter`

#### Scenario: Merge happens at first scheduling, not at Add time
- **GIVEN** a Mutator whose user function appends to an external slice when invoked
- **WHEN** `Workflow.Add(Step(s))` is called
- **THEN** the external slice is unchanged
- **WHEN** `Workflow.Do(ctx)` runs and `s` reaches its first attempt
- **THEN** the external slice has exactly one new entry (Mutator invoked once)

---

### Requirement: Mutators run once per step instance

A Mutator's user function SHALL be invoked at most **once** per step instance, just
before that step's first attempt begins (i.e. when the runtime moves the step out of
`Pending` state into the attempt loop).

The runtime SHALL track per-step state to ensure the Mutator chain runs exactly once,
even if the same step is somehow re-scheduled.

The configuration contributed by Mutators is merged into the step's `StepConfig` using
the same rules as the existing `Idempotent Add with config merging` requirement
(`step-configuration` capability):

- `Upstreams`: union
- `Before`: append in order (Mutator order, then existing)
- `After`: append in order
- `Option`: append in order

After the merge, the step proceeds to the standard per-attempt onion (Interceptors,
retry loop, Before chain, Do, After chain).

#### Scenario: Mutator runs exactly once across multiple attempts
- **WHEN** a `*Foo` step has `RetryOption{MaxAttempts: 3}` and `flow.Mutate[*Foo]` is
  registered, and the step fails twice then succeeds on attempt 3
- **THEN** the Mutator user function is invoked exactly once (before attempt 1)

#### Scenario: Mutator-contributed Before runs on every attempt
- **WHEN** `flow.Mutate[*Foo]` returns a Builder with a `BeforeStep` callback `b`,
  and the step retries 3 times
- **THEN** `b` runs once per attempt (3 times total) — because `BeforeStep` is
  per-attempt; the Mutator merge happens once but the resulting `Before` chain still
  fires per attempt

#### Scenario: Field mutation done in Mutator persists
- **WHEN** the Mutator user function sets `f.Field = "X"` on the typed `*Foo` pointer it
  receives
- **THEN** `f.Field == "X"` is observable from `Do` on every attempt; the field is set
  once and persists

#### Scenario: Mutator merge happens before first attempt's Before chain
- **WHEN** a Mutator returns a Builder with `Input(callback)` and the user has separately
  registered `flow.Step(s).Input(otherCallback)` in their plan
- **THEN** before the first attempt, both callbacks are present in the step's `Before`
  chain (in registration order: Mutator-registered ones come from the slice merge order)

---

### Requirement: Mutator-returned Builder scope

The runtime SHALL restrict the merge of a Mutator-returned `Builder` to the configuration
keyed by the step that was passed into the Mutator. Any configuration the Builder
declares for other steps SHALL be silently ignored.

This rule SHALL keep Mutator scope predictable: a `flow.Mutate[*Foo]` Mutator MUST only
configure the specific `*Foo` instance it was called with, even if the user mistakenly
constructs a Builder that mentions other steps.

#### Scenario: Builder with config for the passed-in step is merged
- **WHEN** `flow.Mutate[*Foo](func(ctx context.Context, f *Foo) flow.Builder { return flow.Step(f).Input(fn) })`
  is registered and runs against a `*Foo` instance
- **THEN** the `Input` callback `fn` is added to that `*Foo`'s `Before` chain

#### Scenario: Builder with config for an unrelated step is ignored
- **WHEN** a Mutator's user function (mistakenly or intentionally) returns
  `flow.Step(otherStep).Input(...)` instead of `flow.Step(passedIn).Input(...)`
- **THEN** the configuration for `otherStep` is silently dropped; `passedIn` is unchanged

---

### Requirement: Mutator propagation to nested workflows

A nested workflow (whether `*Workflow` used directly as a step or a user
struct embedding `flow.Workflow`) SHALL receive its parent's Mutators via
the `WorkflowOptionReceiver.InheritOption` mechanism defined in the
`workflow-options` capability.

Specifically, the parent SHALL invoke `restore := child.InheritOption(parent.Option)`
once in its `Do()` prologue and `defer restore()`. The implementation of
`InheritOption` MUST prepend the parent's `Option.Mutators` to the child's
`Option.Mutators` slice (parent contributions run first within the child),
allocating a fresh slice rather than mutating the parent's or child's input.

The parent SHALL NOT directly walk into a child workflow's `state` map to
apply Mutators; that remains the inner workflow's responsibility once it
begins its own scheduling pass with the merged `Option.Mutators` slice.

The propagation is invoked once per inner-workflow execution. Inner steps
that are added lazily (e.g. inside the composite step's `Do`) are still
reached, because Mutator evaluation in the inner workflow is itself
once-per-step at first scheduling.

The previous `MutatorReceiver` interface and `PrependMutators` method SHALL
NOT exist.

#### Scenario: Parent Mutator reaches step inside nested *Workflow
- **GIVEN** `parent.Option.Mutators = [flow.Mutate[*Foo](fn)]`
- **AND** a child `*Workflow` containing a `*Foo` step
- **WHEN** parent runs, with child added as a step
- **THEN** parent invokes `child.InheritOption(parent.Option)` once before scheduling
- **AND** the inner workflow's first-schedule pass evaluates the propagated Mutator against `*Foo`

#### Scenario: Parent Mutator reaches inner *Foo when child is wrapped
- **GIVEN** `parent.Option.Mutators = [flow.Mutate[*Foo](fn)]`
- **AND** a child `*Workflow` containing a `*Foo` step, added to parent via `flow.Name(child, "name")`
- **WHEN** parent runs
- **THEN** the parent's Mutator reaches the inner `*Foo` via `findOptionReceiver`'s `Unwrap` walk

#### Scenario: Lazily added inner steps are reached
- **WHEN** a composite step embeds `flow.Workflow`, calls `s.Add(new(Foo))` inside its
  `Do`, and the parent has `flow.Mutate[*Foo]` registered under `Option.Mutators`
- **THEN** the lazily added `*Foo` is seen by the parent Mutator at its first scheduling
  inside the inner workflow

#### Scenario: Multi-level Mutator propagation preserves order
- **GIVEN** grandparent `Option.Mutators = [A]`, parent `Option.Mutators = [B]`, child `Option.Mutators = [C]`
- **WHEN** grandparent runs the parent which runs the child
- **THEN** the child's effective Mutators (during its own scheduling) is `[A, B, C]`

#### Scenario: Child Mutator runs after parent
- **WHEN** the parent registers Mutator `pm` and a child `*Workflow` registers
  Mutator `cm` (both via `Option.Mutators`), both targeting `*Foo`
- **THEN** for any `*Foo` inside the child, `pm` runs before `cm`; their `Before` chain
  contributions are appended in that order

---

### Requirement: Mutator vs Input/BeforeStep usage guidance

The framework SHALL preserve both `flow.Step(s).Input(fn)` (per-instance binding) and
`flow.Mutate[*T](fn)` (per-type binding) as parallel mechanisms with distinct intended
scopes. Neither MUST be removed in favor of the other during the deprecation window for
`BuildStep`.

The intended scope distinction is:

- `flow.Step(s).Input(fn)` (in a plan) binds to one specific step instance, lexically
  known. Use it when you are writing a plan or composite step and you hold the step
  instance. The modification only matters for that instance.
- `flow.Mutate[*T](fn)` binds to a Go type. Use it when you want a modification to apply
  to every `*T` step in the workflow tree (including sub-workflows and steps added by
  other plans), and you do not want to enumerate them.

Anti-patterns:

- **Using `Mutate` to modify a single specific instance.** It works but is misleading;
  the next reader of the code assumes "all `*T`". Use `Input` instead.
- **Using `Input` (via `As[T]` traversal) to apply a cross-plan policy.** This is the
  legacy pattern that `Mutate` is designed to replace. Each plan author must pre-build
  sub-workflows so traversal can find targets, and missing-match failures are silent.

For ctx substitution at workflow level (across all step types), use `StepInterceptor` or
`AttemptInterceptor`.

#### Scenario: Both mechanisms coexist
- **WHEN** a workflow uses both `flow.Step(s).Input(fn1)` for a specific instance and
  `flow.Mutate[*S](func(ctx, s *S) Builder { return flow.Step(s).Input(fn2) })` for
  cross-cutting policy
- **THEN** both `fn1` and `fn2` SHALL be invoked on `s`'s Before chain in the documented
  order (plan first, Mutator after)

