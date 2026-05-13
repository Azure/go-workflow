# composite-steps — Spec Delta

## MODIFIED Requirements

### Requirement: SubWorkflow — Workflow as a Step

**Deprecated:** New code SHOULD embed `flow.Workflow` directly instead. The
`flow.SubWorkflow` type is preserved for one release window to allow
in-flight users to migrate; it SHALL be removed in the next major version
of `go-workflow`.

`SubWorkflow` is an embeddable helper that exposes a nested `Workflow` as a
Step. Embedding `flow.SubWorkflow` in a struct gives it `Do`, `Add`, and
`Unwrap` methods. The outer Workflow orchestrates the composite Step; the
inner Workflow orchestrates the sub-steps. The outer Workflow can reach into
the inner Steps using `Has`, `As`, and `HasStep`.

During the deprecation window, `SubWorkflow` SHALL implement
`WorkflowOptionReceiver.InheritOption` by delegating to the embedded
`Workflow`, so parent → child Option propagation continues to work for
`SubWorkflow`-embedding steps.

`SubWorkflow.Reset` remains deprecated (per the existing Mutator-era
deprecation) and SHALL be removed together with `SubWorkflow` itself in the
next major version.

#### Scenario: Workflow can be used directly as a Step
- **WHEN** a `*flow.Workflow` is added as a Step inside another Workflow
- **THEN** the outer Workflow treats the inner Workflow as an opaque Step and calls
  its `Do` method, which in turn executes the inner DAG

#### Scenario: Sub-steps are reachable from the outer Workflow
- **WHEN** an outer Workflow contains a composite Step that embeds `SubWorkflow`
  with inner Steps
- **THEN** `flow.As[*InnerStepType](outerWorkflow)` returns the inner Step instances

#### Scenario: SubWorkflow inherits parent Option during the deprecation window
- **WHEN** an outer Workflow with `Option.DontPanic = &true` contains a
  composite step that embeds `flow.SubWorkflow`
- **THEN** the inner workflow observes `DontPanic = true` via the delegated
  `InheritOption`

---

### Requirement: Sub-workflow construction inside Do

The framework SHALL support constructing a composite Step's sub-workflow inside its
`Do(ctx)` method, using a freshly created `flow.Workflow{}` and standard `Add` /
`Step` / `Pipe` builders. This pattern MUST NOT require implementing `BuildStep`.

The recommended pattern is to embed `flow.Workflow` directly in the
composite step's struct:

```go
type Deploy struct {
    flow.Workflow
    Region string
}

func (d *Deploy) Do(ctx context.Context) error {
    d.Add(/* ... */)
    return d.Workflow.Do(ctx)
}
```

When this pattern is used, the inner workflow SHALL remain visible to
parent-level Mutators and Interceptors via the
`WorkflowOptionReceiver.InheritOption` propagation mechanism (see the
`workflow-options` capability spec).

When this pattern is used WITHOUT exposing the inner workflow via embedding
or `Unwrap` (e.g. by constructing a local `flow.Workflow{}` inside `Do` and
discarding it after `Do` returns), the inner workflow SHALL be invisible to
parent introspection helpers (`Has`, `As`, `HasStep`) and to parent Option
propagation. This is intentional: such a step is fully self-contained and
opaque to the outer workflow.

#### Scenario: Composite step embeds Workflow and inherits parent Option
- **WHEN** a composite step embeds `flow.Workflow` and the parent has Mutators registered
- **THEN** the parent's Mutators reach the inner steps via `InheritOption` (invoked once
  per inner-workflow execution; once-per-step semantics inside the inner workflow then
  take over)

#### Scenario: Local sub-workflow inside Do is opaque to parent
- **WHEN** a step constructs a `flow.Workflow{}` inside `Do` without embedding it
  in the step's struct and without exposing it via `Unwrap`
- **THEN** parent `flow.As[T](outer)` does not find inner steps and parent Option
  does not propagate to them

#### Scenario: Composite step embedding flow.Workflow can be Do()-ed multiple times
- **GIVEN** `type X struct { flow.Workflow }` whose constructor calls `x.Add(...)` once at construction time
- **WHEN** the parent runs N times (with `parent.Reset()` between runs)
- **THEN** each run executes the inner DAG once with no accumulation, because
  `Workflow.Add` is idempotent on the step pointer key (the same step pointer
  added twice merges its `StepConfig` into the existing entry rather than
  creating a duplicate scheduling unit)

#### Scenario: Composite step MUST NOT call Add inside Do unguarded
- **GIVEN** `type X struct { flow.Workflow }` whose `Do(ctx)` calls `x.Add(...)`
  unconditionally on every invocation (no `sync.Once`, no fresh-Workflow
  inline construction)
- **WHEN** the parent invokes `x.Do()` more than once on the same `X` instance
  (across separate parent runs OR via the retry loop within a single parent run)
- **THEN** the behavior is **undefined**. The framework MAY exhibit any of:
  - per-step `BeforeStep`/`AfterStep` callbacks accumulated across runs
    (each callback firing N times on the N-th invocation), because
    `StepConfig.Merge` appends callback chains and does not deduplicate
  - duplicate Step entries of the same logical type in `x.steps` if
    `Add` is called with newly-allocated step pointers each `Do()`
  - parent introspection (`flow.As`, `Has`) returning multiple matches when
    only one was expected
  - Mutator dispatch firing against stale step instances retained from
    previous invocations
- **REASON** `Workflow.steps` and `StepConfig` are designed for build-once /
  execute-many. The pointer-key idempotence of `Add` prevents duplicate
  scheduling on identical pointers but does not deduplicate appended
  `Before`/`After` callback chains. The framework SHALL NOT add a runtime
  guard for this misuse: the parent's `isRunning` lock is held at the
  outer scope, not inside `x`'s scope, and `x.isRunning` is acquired and
  released across each retry attempt — neither offers a stable signal for
  distinguishing legitimate `sync.Once`-guarded lazy initialisation from
  unguarded re-`Add` per invocation.
- **REQUIRED PATTERNS** instead — choose exactly one:
  1. **Build at construction time** — the constructor (e.g., `NewX(...)`)
     calls `x.Add(...)` before returning. Recommended when the inner DAG
     is known at construction.
  2. **Construct inline inside Do() with a fresh *flow.Workflow** — do NOT
     embed `flow.Workflow` in `X`; instead, allocate `w := &flow.Workflow{}`
     inside `x.Do`, populate via `w.Add(...)`, and call `w.Do(ctx)`. The
     containing step MUST implement `WorkflowOptionReceiver` to forward
     parent Option (see the next scenario). Recommended when the inner
     DAG depends on runtime state in `ctx`.
  3. **Lazy build guarded by sync.Once** — embed `flow.Workflow` and call
     `x.Add(...)` from inside `x.once.Do(...)` so the build happens
     exactly once across all invocations. Acceptable when the user
     explicitly wants a single-construction, multi-execution lifecycle
     on the same `X`.

#### Scenario: Local sub-workflow inside Do is opaque to parent
- **WHEN** a step constructs a `flow.Workflow{}` inside `Do` without embedding it
  in the step's struct and without exposing it via `Unwrap`
- **THEN** parent `flow.As[T](outer)` does not find inner steps and parent Option
  does not propagate to them

#### Scenario: Inline sub-workflow inherits parent Option only via explicit InheritOption
- **GIVEN** `type Y struct { inheritedOpt flow.WorkflowOption }` with
  `func (y *Y) InheritOption(p flow.WorkflowOption) { y.inheritedOpt = p }`
  and a `Do(ctx)` that constructs `w := &flow.Workflow{Option: y.inheritedOpt}`
  then calls `w.Add(...)` and `w.Do(ctx)`
- **WHEN** the parent runs and propagates Option via `findOptionReceiver`
- **THEN** `findOptionReceiver` discovers `Y` (because `Y` directly implements
  `WorkflowOptionReceiver`); `y.InheritOption` records the parent's Option;
  the inline `*flow.Workflow` constructed inside `y.Do` then inherits it
- **WITHOUT** the explicit `InheritOption` method on `Y`, the parent's Option
  does NOT reach the inline sub-workflow (the inline workflow is opaque to
  the parent per the preceding scenario)

---

## ADDED Requirements

### Requirement: *Workflow implements WorkflowOptionReceiver

`*flow.Workflow`, when used directly as a Step in another workflow (whether
added directly or embedded in a user struct), SHALL implement
`WorkflowOptionReceiver` (defined in the `workflow-options` capability spec).
Its `InheritOption` implementation merges the parent's `WorkflowOption`
into its own per the rules in `workflow-options`.

This is the recommended pattern for sub-workflows: a user struct embeds
`flow.Workflow` directly and gets `Add`, `Do`, `Unwrap`, and Option
inheritance for free, with only one configuration name (`Option`) promoted
onto the user struct.

#### Scenario: Embedded *Workflow inherits parent Option
- **GIVEN** `type Deploy struct { flow.Workflow; Region string }` and a parent with `Option.DontPanic = &true`
- **WHEN** the parent runs, with `&Deploy{Region: "westus2"}` added as a step
- **THEN** the embedded inner workflow observes `DontPanic = true` for its execution

#### Scenario: Nested *Workflow added directly inherits parent Option
- **GIVEN** a parent with `Option.Mutators = [M]` and a child `*flow.Workflow` containing a step matching `M`
- **WHEN** the parent runs with the child added as a step
- **THEN** `M` runs against the matching inner step

---

## REMOVED Requirements

### Requirement: ~~SubWorkflow implements MutatorReceiver~~

**Reason:** `MutatorReceiver` no longer exists. `SubWorkflow` participates in
parent → child propagation via `WorkflowOptionReceiver.InheritOption` during
its deprecation window (see the modified `SubWorkflow — Workflow as a Step`
requirement above).

### Requirement: ~~*Workflow implements MutatorReceiver~~

**Reason:** `MutatorReceiver` no longer exists. `*Workflow` now implements
`WorkflowOptionReceiver` instead (see the new `*Workflow implements
WorkflowOptionReceiver` requirement above).
