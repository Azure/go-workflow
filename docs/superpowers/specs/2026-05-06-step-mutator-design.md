# Step Mutator Design

**Date:** 2026-05-06
**Status:** Draft
**Scope:** go-workflow type-dispatched step mutators to replace the `BuildStep` + `As[T]` pattern

---

## Why

Today, cross-cutting modifications of specific step types (e.g. "enable feature X on every
`*CreateManagedCluster` request, no matter where it appears in the workflow tree") are
implemented in production via two cooperating mechanisms:

1. **`BuildStep`** — composite/sub-workflow steps must implement this hook so their inner
   steps are materialized into the outer workflow tree at `Add` time.
2. **Mutators** (a higher-level AKS concept, `test.Mutator`) — ordinary code that walks the
   tree with `flow.As[T](w)`, finds the targeted step instances, and attaches `Input`
   callbacks to mutate them.

This pair has serious problems:

- `BuildStep`'s contract is fragile: it is run implicitly at `Add` time, can't return
  errors, relies on a sentinel-by-method-signature trick to detect sub-workflows, and
  silently calls a `Reset()` method by name. Production code (`DeploySwiftV2Pod`,
  `swiftv1ponv6steps.go`, others) routinely calls `BuildStep` again manually inside `Do`
  or inside `Input` callbacks, signaling that the framework's contract no longer matches
  real usage.
- Mutators that find no matching step fail silently — the mutation is just skipped, often
  producing wrong test behavior with no error.
- Mutators implemented via `As[T] + Input` can only modify step *fields*. To override
  retry policy, timeout, or `When`, or to add new `Before`/`After` callbacks across all
  matching steps, the workflow author has no clean tool — they end up with `Input`
  callbacks that are misnamed for the work they do.
- The whole "Add-time materialize, then walk the tree" workflow exists only to support
  the mutator system. Without it, `BuildStep` would not need to exist; sub-workflows
  could be built lazily inside `Do`, like any other Go object.

The proposal: introduce a **type-dispatched Mutator** mechanism in `go-workflow` that
directly expresses "for every `*T` step, configure it like this" by returning a
`flow.Builder` — the same builder users already use to declare per-step configuration in
their plans. With it, the AKS scenario-level mutator pattern compiles down to a single
`flow.Mutate` registration, `As[T]` traversal disappears for that purpose, and `BuildStep`
can be deprecated and eventually removed.

This design also clarifies a non-goal: pre-execution dump / introspection of the full
expanded tree is not a use case we will support. No production user has asked for it; the
post-execution event stream from the Step Interceptor work covers the real
"what happened" need with strictly more accuracy.

---

## Concepts

### Mutator vs Interceptor

These are deliberately separate mechanisms serving different purposes. They answer
different questions, and their names are chosen to reflect that.

| Aspect | StepInterceptor / AttemptInterceptor | Mutator |
|--------|--------------------------------------|---------|
| Question answered | "What is happening to every step?" | "Configure every `*T` step like this." |
| Selector | every step | type assertion `s.(T)` |
| When invoked | per attempt (every retry too) | **per step instance, once** (the first time the step is scheduled) |
| Signature | `(ctx, info, next) → error` | `(ctx, *T) → flow.Builder` |
| Wraps execution? | yes — controls whether `next` runs | no — produces config, runtime merges it |
| Can short-circuit? | yes (skip `next`) | no |
| Can change ctx? | yes (substitutes ctx for `next`) | no — ctx is read-only input |
| Primary use | observability, tracing, metrics, events | scenario-level config: feature flags, retry/timeout/When override, cross-cutting Input/Output |
| Failure mode | wraps the step; `next` error propagates | no error return — failable work belongs in the returned Builder's `Input` |
| Industry analogue | gRPC / HTTP middleware (`(ctx, req, next) → resp`) | K8s MutatingAdmissionWebhook (`(obj) → mut(obj)`) |

A Mutator is **not** an interceptor variant. It does not wrap step execution and it does
not run on every attempt. It runs once per step instance and produces additional
configuration that the workflow runtime merges into that step's `StepConfig`.

#### Why not just a helper on top of Interceptor?

The reader's instinct here is reasonable: an interceptor already receives the step via
`info.Step` and can call `next`. Could a Mutator just be a typed wrapper around a
`StepInterceptor`?

```go
// hypothetical helper (NOT what we're building)
func MutateInterceptor[T Steper](fn func(context.Context, T) error) StepInterceptor {
    return StepInterceptorFunc(func(ctx, info, next) error {
        if s, ok := info.Step.(T); ok {
            if err := fn(ctx, s); err != nil { return err }
        }
        return next(ctx)
    })
}
```

This helper exists in design space. It is rejected as a replacement for `Mutator` for six
distinct reasons. Each reason on its own may not justify a new mechanism; together they
do.

1. **Timing — per-attempt vs once.** Interceptors run once per attempt; a step with 5
   interceptors retrying 3 times incurs 15 type assertions and 15 closures. Mutators run
   **once per step**, then merge into `StepConfig`, after which the standard onion
   executes against merged config without re-dispatching. Putting "configure every `*T`"
   onto the interceptor mechanism silently moves a one-time setup cost onto every
   attempt.

2. **Expressive power — what each can change.** An interceptor wraps `next(ctx)`. It can
   change ctx, observe errors, decide to skip the step. It cannot change the
   `RetryOption` (the retry loop is *outside* AttemptInterceptors per the interceptor
   spec stack), the `Condition` (consumed before scheduling), the `Timeout`, or attach a
   `BeforeStep` that runs *inside* the same retry loop the interceptor is outside of. A
   Mutator merges `StepConfig` before the first attempt, so all of these become
   trivially overridable. The legacy `As[T] + Input` pattern was constrained to
   "modify fields via per-attempt Input" for exactly this reason — every other
   cross-cutting need had no clean tool.

3. **Type safety — compile-time vs runtime.** With the interceptor helper, the type
   parameter is a runtime assertion buried inside the closure. Misspelling the type does
   not produce a compile error; it just silently never matches. This is precisely the
   silent-no-match failure mode that bites AKS today (a mutator targeting a renamed step
   type stops doing anything; tests still "pass"). `flow.Mutate[T](fn)` gives the user
   `func(ctx, T) Builder` — the type is checked at the call site of `Mutate`, the
   compiler enforces signature correctness, and the function body cannot type-mismatch.

4. **Failure semantics — `next` control vs no `next`.** An interceptor that forgets
   `return next(ctx)` silently skips the step. A Mutator has no `next`; "skip the
   step" is not a state it can produce. The result is a smaller surface for user bugs:
   a Mutator can either succeed (apply config) or fail (its returned Builder or its
   merge is malformed) — it cannot accidentally short-circuit execution.

5. **Mental model — observation vs configuration.** Interceptor's natural reading is "I
   wrap each step's execution." Mutator's natural reading is "I declare what `*T` steps
   look like at scenario time." Scenario authors who write `flow.Mutate[*CreateMC]`
   communicate intent (cross-cutting customization) at a glance; readers who see
   `StepInterceptorFunc` with a type assertion buried inside have to parse the closure
   to learn what is going on. Two API names, two intents — for the reader's benefit, not
   the implementer's.

6. **Architectural prerequisite for retiring `BuildStep`.** This is the most important
   and the most hidden. `BuildStep` exists only so that the mutator system can
   `flow.As[T](w)` over a tree that includes lazily-constructed sub-workflows. A
   per-attempt interceptor that type-asserts each step *also* meets that need
   technically — but only for "change a field," because (per reason 2) interceptors
   can't change `RetryOption` / `Condition` / etc. A Mutator that merges `StepConfig`
   on first scheduling reaches sub-workflow steps at the right time **and** can change
   all the things plan authors care about. Only with this combination is `BuildStep`
   removable. A "helper on top of interceptor" cannot retire `BuildStep`; it can only
   replace the field-modification subset of today's `As[T] + Input` pattern.

A typed helper around an interceptor remains useful for **observability** scenarios
("trace every `*HTTPCall`"). If we add one later, it would be a tool for
type-narrowed observability, not a replacement for `Mutator`. The two mechanisms answer
different questions; they share signature shape only by coincidence, and merging them
would force one of them to be wrong for its own job.

### Why per-step (once), not per-attempt

Earlier drafts of this design ran the Mutator function on every attempt. We rejected that
for three reasons:

1. **Real usage is static configuration.** AKS production mutators
   (`mutate/customize_managed_cluster.go`, etc.) all set static fields or attach
   permanent callbacks. None of them depends on per-attempt state. Per-attempt evaluation
   would force every Mutator author to think about idempotency unnecessarily.
2. **Per-attempt with config-mutating power is a footgun.** If the Mutator returns a
   builder that adds a `BeforeStep` callback and runs every attempt, the step's
   `StepConfig.Before` slice grows by one entry per retry — silently. No production
   pattern wants that.
3. **The right tool for per-attempt logic already exists.** A Mutator can attach an
   `Input` or `BeforeStep` callback (they are themselves per-attempt). So a per-step
   Mutator that returns `Input(fn)` gives a per-attempt effect through composition —
   without giving up the once-and-done semantics for everything else.

If a future use case genuinely needs a per-attempt callback registered cross-cuttingly,
the right model is for the Mutator to add a `BeforeStep` (already per-attempt). No
separate per-attempt Mutator API is needed.

### Why the Mutator returns `flow.Builder`

The framework already has a fluent builder for declaring step configuration:

```go
flow.Step(s).
    Input(...).
    BeforeStep(...).
    AfterStep(...).
    Output(...).
    DependsOn(...).
    Retry(...).      // via WithOption
    Timeout(...).
    When(...)
```

A `flow.Builder` is the unified return type of all these helpers (its `AddToWorkflow()`
method returns `map[Steper]*StepConfig`). By having a Mutator return `flow.Builder`, the
Mutator author writes:

```go
flow.Mutate[*CreateMC](func(ctx context.Context, c *CreateMC) flow.Builder {
    return flow.Step(c).
        Input(func(ctx, c *CreateMC) error { EnableFeatureX(c.Request); return nil }).
        Retry(func(o *flow.RetryOption) { o.MaxAttempts = 5 })
})
```

This is **the same vocabulary plan authors already use**. There is no second API to
learn, no `*StepConfig` poking, no special way to express "add a Before callback". The
runtime simply merges the returned `Builder`'s `StepConfig` into the step's existing
config (using the same merge logic as repeat `Add` calls — see the `Idempotent Add with
config merging` requirement in `step-configuration`).

### Naming alignment with existing AKS code

In AKS e2ev3, `test.Mutator` already exists as a scenario-level concept (a Mutator is "a
piece of test customization that adapts a Plan for a Scenario"). That higher-level type
typically resolves down to "find these steps in the workflow and modify their requests" —
exactly what `flow.Mutator` will express in one line.

The two concepts share the word because they are the same idea at different layers:

- `test.Mutator` — scenario-level: "this Scenario differs from its Plan by enabling
  feature X."
- `flow.Mutator` — step-level: "for any step of type `*T`, configure it like this."

`test.Mutator` implementations will internally produce `flow.Mutator` instances and append
them to the workflow's `Mutators` slice. The conceptual chain is consistent end-to-end.

### Selector by exact type

A Mutator is registered for exactly one Go type via a generic constructor:

```go
flow.Mutate[*CreateManagedCluster](fn)
```

Selection is `step.(*CreateManagedCluster)`. There is no interface matching, no
embedded-type walk, no name matching. A step embedded inside a wrapper is **not** matched
by a Mutator registered for the wrapper's type. If a user wants both, they register both.

Rationale: the production mutator system targets a small, known set of concrete API call
types (`CreateManagedCluster`, `UpgradeManagedCluster`, `CreateAgentPool`, etc.). Exact
type matching is what users already mean when they write
`flow.As[*CreateManagedCluster](w)` — generalizing further would invite surprising
matches.

### When Mutators run

A Mutator runs **once per step instance**, on the first time the workflow schedules that
step (i.e. just before the step's first attempt begins, after its upstreams have
terminated). At that point:

1. The runtime iterates `w.Mutators` in slice order.
2. For each Mutator whose target type matches this step's concrete type, the runtime
   calls the user function and obtains a `flow.Builder`.
3. The returned `Builder`'s `AddToWorkflow()` is invoked; its `StepConfig` for this step
   is merged into the step's existing `StepConfig` using the same idempotent-merge rules
   that apply when the same step is added twice via `w.Add(...)`.
4. The Mutator's contribution is therefore visible to the `Before` chain, the retry
   policy, and the `Condition` from the very first attempt onward.

The Mutator is **not** invoked again on subsequent attempts. The configuration it
contributed remains in effect.

### Why "first schedule" rather than "Add time"

Sub-workflow steps that are added lazily (e.g. by a composite step's `Do` calling
`s.Add(new(*CreateMC))`) are not visible to the outer workflow at outer-`Add` time. By
deferring Mutator evaluation to the first time the step is scheduled by *any* workflow
(outer or nested), we cover both eagerly-added and lazily-added steps under one rule.

The runtime tracks a `mutatorsApplied` flag (or equivalent) on each step's `State` so
that re-scheduling never re-runs Mutators on the same step.

### Position in the execution stack

The Mutator-contributed configuration is merged *before* execution begins. When the
step's first attempt runs, the per-attempt onion looks identical to a step whose
`Before`/`After`/etc. were declared at `Add` time:

```
StepInterceptor[0..n]                            ← workflow-level lifecycle
  └── [retry loop]
        └── AttemptInterceptor[0..n]             ← per-attempt observability
              └── BeforeStep callbacks (including any from Mutators)
                    └── step.Do
                          └── AfterStep callbacks (including any from Mutators)
```

Mutators do not introduce a new layer of the onion. They contribute to the existing
`Before`/`After` lists.

### Order across multiple Mutators

The `Mutators` slice is ordered. Element 0 runs first, element n-1 runs last. For a given
step instance, only Mutators whose type parameter matches the step run, but among those,
slice order is preserved.

When two Mutators both register `Before` callbacks via their returned Builders, the
`Before` callbacks accumulate in the order the Mutators ran (because the merge rule for
`Before`/`After` is append, per `step-configuration`'s `Idempotent Add with config
merging` requirement).

### Merge timing and ordering relative to plan callbacks

Mutators are merged **after** all `Workflow.Add(...)` calls for a step have completed,
just before the step's first attempt begins. The plan author's `Step(s).Input(...)` /
`Output(...)` are already in `StepConfig.Before` / `After` when the Mutator merge
happens; the Mutator's contributions are **appended** to those existing lists.

Concrete execution order for a step that has both plan-declared and Mutator-declared
callbacks:

```
attempt:
  ├─ planFn      (from Step(s).Input(planFn))    — runs first
  ├─ mutator1Fn  (from flow.Mutate[*S], slot 0)  — runs second
  ├─ mutator2Fn  (from flow.Mutate[*S], slot 1)  — runs third
  ├─ s.Do(ctx)
  ├─ planAfter   (from Step(s).Output(planAfter))
  ├─ mutator1Aft (from flow.Mutate[*S] returned Output)
  └─ mutator2Aft (...)
```

**Why plan first, Mutator after** (rather than the reverse):

1. **Mental model match.** A Mutator is a scenario-level customization that "decorates"
   a plan. Decorations going on top of defaults is the natural reading. Reversing it
   would make Mutator look like "default value provider", which it isn't.
2. **Override semantics.** With plan-first-Mutator-after, a Mutator can read the value
   the plan set and override it (e.g. plan sets `SKU = "basic"`, Mutator sets
   `SKU = "premium"`). The reverse ordering would make override impossible without
   undoing plan state — which a callback can't do.
3. **Industry precedent.** K8s admission webhooks run *after* default-set: the API
   server fills in defaults, then mutating webhooks customize. Tekton's mutating webhook
   model is identical. Airflow's `task_policy` runs at scheduling time, after DAG
   construction. We follow the same pattern.
4. **Cannot be circumvented.** If a Mutator genuinely needs to run *before* a plan
   callback (rare), the right answer is to make that behavior a plan-level concern (move
   it into `Input` at `Add` time). Mutators are not the right tool for "pre-empt plan
   defaults" because they conceptually layer above the plan.

`Upstreams` and `Option` merging follow the same append/union rules from
`StepConfig.Merge`; there is no per-type tweak for Mutator-contributed config.

### Mutator vs Input / BeforeStep — when to use which

A Mutator **returns** a `flow.Builder`, which can in turn declare `Input` / `BeforeStep`
on the step. So at first glance the mechanisms overlap. The distinction is **binding
scope and authoring location**:

| | `flow.Step(s).Input(fn)` (in a plan) | `flow.Mutate[*T](fn)` |
|---|---|---|
| Binding | one specific step instance, lexically known | a Go type — every step of that type, anywhere |
| Registered at | plan construction time | scenario / test setup time |
| Author knows the targets? | yes — author holds the pointer | no — discovered by type at first scheduling |
| Scope | local to the plan | cross-plan, cross-sub-workflow |
| Cardinality | 1 step | N steps (zero or more) |

Guidance:

- **Use `Input` / `BeforeStep` directly in a plan** when:
  - You are writing a plan or composite step and you hold the step instance.
  - The modification only matters for *this* instance.
  - The modification is plan-local.

- **Use `flow.Mutate`** when:
  - You want a modification to apply to every `*T` step regardless of which plan or
    sub-workflow added it.
  - The modification is a cross-cutting policy that scenario authors register without
    knowing the plan internals.
  - You want to override `Retry`, `Timeout`, `When`, or attach `Before`/`After`
    callbacks across all instances of `*T`.

Anti-patterns:

- **Don't use `Mutate` to modify a single specific instance.** It works, but it's
  misleading; the next reader assumes "all `*T`". Use `Input` instead.
- **Don't write `for _, s := range flow.As[*T](w) { w.Add(flow.Step(s).Input(...)) }`
  — that is exactly the legacy pattern `Mutate` replaces.

### Why `Mutator` receives ctx but cannot substitute it

The Mutator function receives a `context.Context` but does not return a substituted ctx.
Two distinct decisions:

**Why ctx is passed in.** Production AKS mutators routinely need workflow-scoped values
to do their work — typically a `log.Logger` (via `log.FromContextOrDiscard(ctx)`), a
scenario name, or a test session ID. Without ctx, those mutators would either lose
diagnostic capability or be forced to wrap their logic inside an `Input` callback just to
gain access to ctx — which silently downgrades a once-per-step mutation into per-attempt
work and destroys the `flow.Mutate` model's idempotency guarantee. We pass the
**workflow-scoped ctx** (the same `ctx` that was passed into `Workflow.Do(ctx)`) because
that is the right scope for "stable for the life of this workflow" values like logger
and scenario.

**Why ctx cannot be substituted.** A Mutator runs at scheduling time, not inside the
per-attempt onion. The ctx that `step.Do` receives is derived later by the
StepInterceptor / AttemptInterceptor / retry / timeout chain; the Mutator is not on that
chain and has no architecturally meaningful place to insert a substituted ctx. If a
caller needs to inject values into the ctx seen by a step on each attempt, the right
tools are:

- The Mutator's returned `Builder` can include a `BeforeStep` callback (which itself
  receives ctx and can substitute it for `step.Do`). The composition is
  `Mutate[*T](func(ctx context.Context, s *T) flow.Builder { return
  flow.Step(s).BeforeStep(fn) })`.
- For cross-cutting ctx substitution at workflow scope, `StepInterceptor` /
  `AttemptInterceptor` already wrap `next(ctx)` and can substitute freely.

---

## Architecture

### `Mutator` interface and constructor

```go
// Mutator contributes additional configuration to a step of a specific type. The runtime
// invokes a Mutator at most once per matching step, just before the step's first attempt.
//
// Construct only via flow.Mutate[T]; the interface's only method is unexported so user
// code cannot bypass the type-dispatch contract.
type Mutator interface {
    // applyTo invokes the Mutator's user function against step if step matches the
    // Mutator's target type. ctx is the workflow-scoped context (the same ctx passed
    // to Workflow.Do). Returns:
    //   matched: true if the type assertion succeeded
    //   builder: the user-returned configuration to merge into step's StepConfig
    //            (nil if matched is false, or if the user returned a nil Builder)
    applyTo(ctx context.Context, step Steper) (matched bool, builder Builder)
}

// Mutate constructs a typed Mutator. The function fn receives the workflow-scoped
// context and the type-asserted step instance, and SHALL return a flow.Builder that
// the runtime will merge into step's existing StepConfig. fn MAY return nil to indicate
// "no additional configuration" (useful when fn only mutates fields on the typed step
// pointer it received).
//
// ctx is suitable for accessing workflow-stable values (logger, scenario name, test
// session ID). It is NOT a per-attempt context — the Mutator runs once per step at
// scheduling time.
//
// The returned Builder typically carries configuration for the same step that was passed
// in (i.e. via flow.Step(passed)). The runtime ignores any configuration in the Builder
// for steps other than the one passed to the Mutator.
func Mutate[T Steper](fn func(ctx context.Context, step T) Builder) Mutator
```

### Workflow struct addition

```go
type Workflow struct {
    // ... existing fields, including StepInterceptors / AttemptInterceptors ...

    // Mutators are evaluated against every step the first time the step is scheduled.
    // Only Mutators whose target type matches the concrete step run. Slice order is
    // preserved among matching Mutators; their contributions to StepConfig accumulate.
    Mutators []Mutator
}
```

Mutators are populated the same way as interceptors — direct slice assignment when
constructing the workflow. There is no `Use(...)` method.

### First-schedule evaluation

When the runtime is about to schedule a step for the first time (specifically, when
moving the step out of `Pending` and into the attempt loop), it iterates `w.Mutators`:

```go
// pseudocode in tick() / stepExecution.start, before the first attempt
// ctx here is the workflow-scoped ctx from Workflow.Do(ctx)
if !state.MutatorsApplied {
    for _, m := range w.Mutators {
        matched, b := m.applyTo(ctx, step)
        if !matched || b == nil {
            continue
        }
        for s, cfg := range b.AddToWorkflow() {
            if s == step {
                state.Config.Merge(cfg)
            }
            // configurations for other steps in the returned Builder are ignored —
            // a Mutator's scope is the step that was passed in
        }
    }
    state.MutatorsApplied = true
}
```

The exact placement (`tick()` vs `stepExecution.start` vs new helper) depends on the
final shape of the Step Interceptor change. The invariant is: the merge happens before
the Before chain, before the retry loop, and exactly once per step instance.

If a Mutator's user function panics or contributes invalid config, the panic propagates
through the normal step-execution panic recovery (controlled by `Workflow.DontPanic`).

### Sub-workflow propagation

The Mutator list propagates into sub-workflows by the same mechanism interceptors use:
composite steps (and `*Workflow` used as a step) implement an additional optional
interface:

```go
// MutatorReceiver is implemented by composite steps that host a sub-workflow. Before the
// sub-workflow is run, the runtime calls PrependMutators with the parent workflow's
// Mutators so they reach inner steps.
type MutatorReceiver interface {
    PrependMutators(mw []Mutator)
}
```

Both `*flow.Workflow` (used as a step) and `*flow.SubWorkflow` implement this. The
runtime calls `PrependMutators` once before invoking the inner workflow's first
scheduling pass. Prepending preserves "parent Mutators run before child Mutators".

A step may implement `InterceptorReceiver`, `MutatorReceiver`, both, or neither.

### Coordination with the Step Interceptor design

This change shares the runtime's per-step lifecycle territory with the Step Interceptor
change. The two MUST land in the same release. Concretely:

- The Mutator merge step is invoked in the same place where the interceptor design adds
  per-attempt invocations — specifically, in the path that takes a step from `Pending` to
  `Running`. Mutator merge is **before** the first AttemptInterceptor; both are before
  the first `Before` callback.
- Sub-workflow propagation reuses the receiver-based mechanism the interceptor design
  introduces (see `InterceptorReceiver`); we add an analogous `MutatorReceiver`.

If the interceptor change's structural plan changes, this change MUST be re-aligned.

---

## API

### New types

```go
// Mutator contributes additional configuration to steps of a specific type. Construct
// only via flow.Mutate[T].
type Mutator interface {
    applyTo(ctx context.Context, step Steper) (matched bool, builder Builder)
}

// Mutate constructs a typed Mutator that runs fn against any step whose concrete type is
// T. fn receives the workflow-scoped context and the type-asserted step, and returns a
// flow.Builder whose configuration for the passed-in step is merged into that step's
// StepConfig before its first attempt.
func Mutate[T Steper](fn func(ctx context.Context, step T) Builder) Mutator

// MutatorReceiver is implemented by composite steps that host a sub-workflow.
type MutatorReceiver interface {
    PrependMutators(mw []Mutator)
}
```

### Workflow field

```go
// Mutators run once per matched step, contributing additional StepConfig before the
// step's first attempt. Slice order is preserved among matching Mutators.
Mutators []Mutator
```

### SubWorkflow propagation

```go
// SubWorkflow implements MutatorReceiver alongside InterceptorReceiver.
func (s *SubWorkflow) PrependMutators(mw []Mutator) {
    if len(mw) == 0 { return }
    s.w.Mutators = append(append([]Mutator{}, mw...), s.w.Mutators...)
}

// *Workflow used directly as a step does the same.
func (w *Workflow) PrependMutators(mw []Mutator) {
    if len(mw) == 0 { return }
    w.Mutators = append(append([]Mutator{}, mw...), w.Mutators...)
}
```

### Usage

Replacing a typical AKS scenario-level mutator helper:

```go
// Before (uses BuildStep + As[T] traversal; can only attach Input)
return test.WorkflowMutator(func(w *flow.Workflow) *flow.Workflow {
    for _, createMC := range flow.As[*azcontainerservice.CreateManagedCluster](w) {
        w.Add(flow.Step(createMC).Input(func(ctx context.Context, c *azcontainerservice.CreateManagedCluster) error {
            f(ctx, log.FromContextOrDiscard(ctx), c.GetRequest())
            return nil
        }))
    }
    return w
})

// After (uses flow.Mutate; works without BuildStep, no traversal needed)
return test.WorkflowMutator(func(w *flow.Workflow) *flow.Workflow {
    w.Mutators = append(w.Mutators, flow.Mutate[*azcontainerservice.CreateManagedCluster](
        func(ctx context.Context, c *azcontainerservice.CreateManagedCluster) flow.Builder {
            return flow.Step(c).Input(func(ctx context.Context, c *azcontainerservice.CreateManagedCluster) error {
                f(ctx, log.FromContextOrDiscard(ctx), c.GetRequest())
                return nil
            })
        },
    ))
    return w
})
```

Other natural uses unlocked by returning a Builder:

```go
// Override retry policy across all Azure API calls
w.Mutators = append(w.Mutators, flow.Mutate[*AzureAPICall](
    func(ctx context.Context, c *AzureAPICall) flow.Builder {
        return flow.Step(c).Retry(func(o *flow.RetryOption) {
            o.MaxAttempts = 5
            o.Backoff = backoff.NewExponentialBackOff()
        })
    },
))

// Force a Condition on a whole class of step
w.Mutators = append(w.Mutators, flow.Mutate[*DestroyXxx](
    func(ctx context.Context, d *DestroyXxx) flow.Builder {
        return flow.Step(d).When(flow.Always)
    },
))

// Hook Output across all *CreateManagedCluster to record cluster IDs in the test report
w.Mutators = append(w.Mutators, flow.Mutate[*CreateManagedCluster](
    func(ctx context.Context, c *CreateManagedCluster) flow.Builder {
        return flow.Step(c).Output(func(ctx context.Context, c *CreateManagedCluster) error {
            report.Clusters = append(report.Clusters, c.Response.Name)
            return nil
        })
    },
))

// Inject a fake HTTP client into every HTTP-call step (no per-attempt callback needed —
// just a once-per-step field set). The ctx parameter is unused here but available if
// the chosen client depends on workflow-scoped values.
w.Mutators = append(w.Mutators, flow.Mutate[*HTTPCall](
    func(ctx context.Context, h *HTTPCall) flow.Builder {
        h.Client = fakeClient
        return nil   // no additional config; the field set above is enough
    },
))
```

Note the last example: a Mutator MAY return `nil` if it accomplishes its goal by directly
mutating fields on the step instance (which it received as a typed pointer). This works
because Mutators run before the first attempt, so any field set is visible to the Before
chain and to `Do`.

The `ctx` parameter in each example is the workflow-scoped ctx from `Workflow.Do(ctx)`.
Mutators commonly use it to read a logger, scenario name, or test session ID. Mutators
do not return a substituted ctx; that is `BeforeStep`'s or interceptor's job.

---

## What Does Not Change

- `BeforeStep` / `AfterStep` / `Input` / `Output` — API and behavior unchanged. They
  continue to be the per-instance mechanism for plan-local data flow.
- `StepConfig`, `StepOption`, `RetryOption` — unchanged. The merge rules used by Mutator
  application are exactly those used by the existing `Idempotent Add with config merging`
  requirement.
- `Has[T]` / `As[T]` / `HasStep` — kept as introspection helpers. They remain useful for
  in-test assertions and debugging, but their role as the mutator system's selector is
  retired.
- `Workflow.Add` time semantics — unchanged for the dependency graph. `BuildStep` is
  still invoked at `Add` time for any step that implements it (see the Deprecation
  section below).
- The interceptor design — Mutators share its scheduling-time path but do not modify the
  interceptor APIs.
- The higher-level `test.Mutator` interface in AKS e2ev3 — unchanged; existing
  implementations keep working. Migration to `flow.Mutate` is per-implementation and
  optional during the deprecation window.

---

## Deprecation: `BuildStep`

This change marks `BuildStep` (the user-facing hook with signature `BuildStep()`) and the
`SubWorkflow.Reset()` implicit-call behavior as deprecated. The mechanism is **not removed
in this change**; that is a follow-up change once the AKS test suite has migrated all
117+ production usages.

What is deprecated:

- The user-facing `interface{ BuildStep() }` hook on Steper implementations.
- The implicit `Reset()` invocation by `StepBuilder.BuildStep` immediately before
  `BuildStep()`.
- The recommendation to embed `flow.SubWorkflow` and define `BuildStep()` to populate it
  at `Add` time. Going forward, sub-workflows SHOULD be constructed directly inside `Do`:

  ```go
  func (u *UpgradeAndValidate) Do(ctx context.Context) error {
      var w flow.Workflow
      w.Add(flow.Step(&u.UpgradeSucceeded).DependsOn(&u.UpgradeAgentPool))
      return w.Do(ctx)
  }
  ```

  Composite-step authors who embed `flow.SubWorkflow` get parent Mutator propagation for
  free via `PrependMutators` and do not need extra work.

What is **not** deprecated yet:

- `flow.SubWorkflow` itself remains — it is a useful container even when populated lazily.
- `Has[T]` / `As[T]` / `HasStep` remain.
- The internal `StepBuilder` machinery remains in place for the duration of the
  deprecation window so existing code continues to work.

A `// Deprecated:` comment is added to:

- The doc comment of `StepBuilder.BuildStep` in `build_step.go`.
- The doc comment of `flow.SubWorkflow.Reset` in `workflow.go`.
- The `BuildStep` example in `example/13_composite_step_test.go` (with a pointer to the
  Mutator example).

A follow-up change (out of scope here) will:

1. Migrate AKS e2ev3 production usages from `BuildStep` to either inline `Do`
   construction or `flow.Mutate` (as appropriate).
2. Remove `StepBuilder`, the implicit `Reset` call, and the `BuildStep` invocation from
   `Workflow.addStep`.
3. Update `composite-steps` spec to drop the `BuildStep — lazy initialization hook`
   requirement.

---

## Breaking Changes

This change is **non-breaking** for existing code in the common case. Every existing
workflow keeps working:

- `BuildStep` is still invoked at `Add` time, just deprecated.
- `Has` / `As` / `HasStep` keep their behavior.
- `SubWorkflow.Reset()` keeps being called.
- Existing scenario-level mutator code that uses `flow.As[T](w)` keeps working.

The new exposed surface is:

- `Workflow.Mutators []Mutator` — new field on `Workflow`. Code that constructs
  `Workflow{...}` with positional struct literals would break, but that pattern is not
  used anywhere we know of.
- `flow.Mutate[T](fn) Mutator` — new exported generic function and `Mutator` interface.
  Pure additions; no name collisions in the existing public API.
- `flow.MutatorReceiver` — new exported interface.

There is one **subtle behavior difference** to flag for review compared to the old
`As[T] + Input` pattern:

- The old pattern attached an `Input` callback (which is per-attempt). With `flow.Mutate`,
  if you want per-attempt behavior, you put it inside the returned `Builder`'s `Input`
  callback — which is also per-attempt. So the semantics for Input-style mutations are
  equivalent.
- The new capability is that you can also override `Retry` / `Timeout` / `When` and
  attach `Before` / `After` once per step. These are real new powers, not breaking
  changes.

---

## Files Affected

| File | Change |
|------|--------|
| New `mutator.go` | Add `Mutator` interface, `Mutate[T]` constructor, `MutatorReceiver` interface |
| `workflow.go` | Add `Mutators []Mutator` field; invoke Mutator merge in the first-schedule path; add `MutatorsApplied` tracking on `State`; call `PrependMutators` on sub-workflows; add `(*Workflow).PrependMutators` |
| `state.go` | Add `MutatorsApplied bool` field (or equivalent) to `State` |
| `wrap.go` (or `workflow.go` near SubWorkflow) | `SubWorkflow` implements `MutatorReceiver` via `PrependMutators` |
| `build_step.go` | Add `// Deprecated:` notes to `StepBuilder.BuildStep` |
| `example/13_composite_step_test.go` | Add `// Deprecated:` note; add a parallel example showing the Mutator / Do-time pattern |

---

## Open Questions

| Question | Resolution |
|----------|------------|
| Why "Mutator" and not "Middleware"? | "Middleware" carries a wrap-around expectation (`(ctx, req, next) → resp`) that this mechanism does not provide. "Mutator" matches the semantics (configure-only, no `next`, no short-circuit) and aligns with AKS `test.Mutator` and K8s `MutatingAdmissionWebhook`. |
| Per-attempt or per-step (once)? | Per-step, once. AKS production usage is all static config. Per-attempt with config-mutation power is a footgun (silent slice growth on retry). For per-attempt effects, the Mutator's returned Builder can include `Input`/`BeforeStep`. |
| Why return `flow.Builder`? | It's the existing vocabulary for declaring step config. No second API to learn. Naturally composes with `Input`, `Output`, `BeforeStep`, `Retry`, `Timeout`, `When`. |
| Does the Mutator receive a `context.Context`? | Yes — the workflow-scoped ctx from `Workflow.Do(ctx)`. It is a read-only input; suitable for accessing logger, scenario name, test session ID. AKS production mutators routinely need `log.FromContextOrDiscard(ctx)`; not passing ctx would force them to wrap their logic in an `Input` callback and silently downgrade to per-attempt semantics. |
| Does the Mutator return a substituted ctx? | No — ctx substitution happens later in the per-attempt onion (BeforeStep / interceptors); the Mutator is not on that chain and has nowhere to insert a substitution. For per-attempt ctx influence, return a Builder that includes `BeforeStep`. |
| Does the Mutator return an `error`? | No — the Mutator runs at scheduling time where "fail this step" semantics are awkward (the step hasn't started yet). Failable work belongs in the returned Builder's `Input` callback, which runs per-attempt and integrates naturally with retry. |
| Can the Mutator return nil? | Yes — useful when the Mutator only sets fields on the typed step pointer it received. |
| Should the selector support interfaces or embedded types? | No — exact concrete type only. Production usage targets concrete types; interface dispatch invites surprises. |
| How does a Mutator reach sub-workflow steps? | `MutatorReceiver` interface; `SubWorkflow` and `*Workflow` (used as step) implement `PrependMutators`. |
| What happens if a Mutator's Builder returns config for a step other than the one passed in? | Ignored. The Mutator's scope is the matched step; configurations targeting other steps are silently dropped. (We could panic instead; deferred until we see misuse.) |
| What happens when a Mutator's user function panics? | Propagates through the normal step-execution panic recovery (`Workflow.DontPanic`). |
| Is pre-execution dump supported? | No. No production user has the requirement; the interceptor event stream covers actual need. |
| Is `BuildStep` removed? | Not in this change. Deprecated; removed in a follow-up after AKS migration. |
| Can a user register two Mutators for the same type? | Yes. They run in slice order; their contributions accumulate via the standard StepConfig merge rules. |
| What happens if a step type matches no Mutator? | Nothing — Mutator loop is a no-op for that step. |
| Does `Mutate` reach steps inside `*Workflow` used as a step? | Yes — `*Workflow` implements `PrependMutators`. |
