## Purpose

This capability covers the composite-steps surface of the go-workflow library.

## Requirements

### Requirement: Steper interface — the Step contract

Any type that implements `Do(context.Context) error` satisfies the `Steper` interface and
can be added to a Workflow. Steps are stored as map keys, so the concrete type MUST be
comparable. Using a pointer to a struct (`*MyStep`) is the idiomatic approach.

Empty structs SHALL NOT be used as Step implementations because all `struct{}` values are
equal in Go, making it impossible to distinguish different instances in the Workflow map.

#### Scenario: Pointer to struct is a valid Step
- **WHEN** `&MyStep{}` is added to a Workflow
- **THEN** each distinct pointer is treated as a distinct Step in the DAG

#### Scenario: Two pointers to different instances are independent Steps
- **WHEN** `step1 := &MyStep{}` and `step2 := &MyStep{}` are both added
- **THEN** they are treated as two independent Steps with their own statuses

---

### Requirement: Func helpers for anonymous Steps

`flow.Func(name, fn)` wraps `func(context.Context) error` into a named Step.
`flow.FuncI`, `flow.FuncO`, and `flow.FuncIO` provide typed input/output variants.
The `Name` field is used as the string representation.

#### Scenario: Func creates a named Step from an anonymous function
- **WHEN** `flow.Func("my-step", fn)` is added to a Workflow
- **THEN** it behaves as a regular Step; its string representation is `"my-step"`

#### Scenario: FuncIO carries typed input and output fields
- **WHEN** `flow.FuncIO("step", fn)` is used and `Input` is set on the returned `*Function`
- **THEN** `fn` receives the value of `Function.Input` and its return value is written to
  `Function.Output`

---

### Requirement: Unwrap protocol for composite Steps

A Step that wraps or embeds other Steps SHALL implement one of:
- `Unwrap() Steper` — wraps a single inner Step
- `Unwrap() []Steper` — wraps multiple inner Steps

The Workflow uses `Unwrap` to determine the *root* Step for orchestration: only
top-level Steps (those not wrapped by another Step) are orchestrated by the Workflow.
When a composite Step is added, the inner Steps are promoted to wrapped positions and
the composite is treated as the single root.

#### Scenario: Wrapper Step replaces inner Step as root
- **WHEN** a composite Step wrapping `innerStep` is added to a Workflow
  that already contains `innerStep` as a root
- **THEN** the composite Step becomes the root; `innerStep.Do` is called only by the composite

#### Scenario: Inner Step config is preserved
- **WHEN** `innerStep` has `DependsOn` or `Input` declared before being wrapped
- **THEN** those configurations are merged onto the composite Step's state

---

### Requirement: Has, As, and HasStep traversal

`Has[T](step)` reports whether any Step in the tree rooted at `step` satisfies type `T`.
`As[T](step)` returns all Steps in the tree that satisfy type `T`, in pre-order.
`HasStep(step, target)` reports whether `target` appears anywhere in the tree of `step`.

Traversal follows `Unwrap() Steper` and `Unwrap() []Steper` recursively.

#### Scenario: Has finds a type in the tree
- **WHEN** a composite Step wraps a `*MyStep` and `flow.Has[*MyStep](composite)` is called
- **THEN** it returns `true`

#### Scenario: As returns all matching types
- **WHEN** a Workflow contains several `*SayHello` Steps at various nesting levels
  and `flow.As[*SayHello](workflow)` is called
- **THEN** it returns all `*SayHello` instances in pre-order traversal order

#### Scenario: HasStep matches by pointer identity
- **WHEN** `flow.HasStep(composite, innerStep)` is called
- **THEN** it returns `true` if and only if `innerStep` is the exact same pointer
  found somewhere in the composite's tree

---

### Requirement: BuildStep — lazy initialization hook

The framework SHALL preserve the `BuildStep` lazy-initialization hook for backward
compatibility during the deprecation window. New code SHOULD use `flow.Mutate` for
cross-cutting modification and construct sub-workflows inside `Do()` (see the new
`Sub-workflow construction inside Do` requirement below).

The implicit `BuildStep()` invocation at `Add` time, together with the implicit
`SubWorkflow.Reset()` call that precedes it, SHALL be **removed in the next major
version of `go-workflow`**. From that release on, Steps that define a `BuildStep()`
method will no longer have it invoked automatically; composite-step authors must
construct their inner workflow inside `Do()` instead. This deprecation window is
bounded: it ends at the next major version bump, by which time all production usages
must have migrated.

If a Step implements `BuildStep()`, the Workflow SHALL call it exactly once when the
Step is first added. This allows composite Steps to initialize their internal
sub-workflow lazily at add-time rather than at construction time.

If the Step also implements `Reset()`, the Workflow SHALL call `Reset()` before
`BuildStep()` to allow the Step to clear any previous state before rebuilding.

`BuildStep()` SHALL NOT be called again if the same Step pointer is added a second time.

#### Scenario: BuildStep called once on first Add
- **WHEN** a Step implementing `BuildStep()` is added to a Workflow
- **THEN** `BuildStep()` is called exactly once, regardless of how many times the
  Step is subsequently added

#### Scenario: Reset called before BuildStep
- **WHEN** a Step implements both `Reset()` and `BuildStep()`
  and is added to a Workflow
- **THEN** `Reset()` is called first, then `BuildStep()`

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

### Requirement: Cross-boundary upstream registration

When a Step inside a composite or sub-workflow declares a dependency via `DependsOn`,
the Workflow registers the upstream at the **lowest workflow layer that can see both
the step and its upstream**.

This is necessary because each Workflow only orchestrates its own root Steps.
If the upstream lives outside the sub-workflow that owns the step, the dependency
must be registered on the outer Workflow's root entry for that step (i.e. the
sub-workflow itself), not on the inner step's state. Conversely, if both the step
and its upstream live inside the same sub-workflow, the dependency is registered
inside that sub-workflow so its scheduler can enforce the ordering.

The Workflow finds the correct layer by comparing the ancestor paths of both the
step and the upstream in the full workflow tree and registering on the deepest
common ancestor that implements scheduling (`StateOf` / `RootOf`).

#### Scenario: Inner step depends on outer step

- **GIVEN** an `inner` Workflow containing step `a`, added as a root Step of `outer`
- **AND** step `b` exists only in `outer` (unknown to `inner`)
- **WHEN** `outer.Add(Step(a).DependsOn(b))` is called
- **THEN** the upstream `b` is recorded on `outer`'s state for `inner` (the root of `a`)
- **AND** `inner`'s own state for `a` does NOT contain `b`
  (because `inner` has no knowledge of `b`)

#### Scenario: Inner step depends on sibling inner step

- **GIVEN** an `inner` Workflow containing steps `a` and `b`, added as a root Step of `outer`
- **WHEN** `outer.Add(Step(a).DependsOn(b))` is called
- **THEN** the upstream `b` is recorded on `inner`'s state for `a`
- **AND** `outer`'s state for `inner` does NOT contain `b`
  (because the dependency is fully contained within `inner`)

---

### Requirement: NoOp — placeholder Step

`flow.NoOp(name)` creates a Step whose `Do` always returns `nil`. It is useful as
a named synchronization point or placeholder in a DAG.

#### Scenario: NoOp always succeeds
- **WHEN** a `NoOp` Step is executed
- **THEN** it returns `nil` and transitions to `Succeeded`

---

### Requirement: Workflow tree rendering

`workflow.String()` (or its equivalent) SHALL render the DAG as an indented tree for
human-readable debugging, showing each root Step, its direct upstreams (dependencies),
and recursing into sub-workflows.

#### Scenario: Tree string output
- **WHEN** `fmt.Sprint(workflow)` (or equivalent) is called on a Workflow with Steps
- **THEN** the output contains the names of all added Steps

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

#### Scenario: Inline sub-workflow inherits parent Option only via explicit InheritOption
- **GIVEN** `type Y struct { inheritedOpt flow.WorkflowOption }` with
  `func (y *Y) InheritOption(p flow.WorkflowOption) (restore func()) { prev := y.inheritedOpt; y.inheritedOpt = p; return func() { y.inheritedOpt = prev } }`
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

### Requirement: *Workflow implements WorkflowOptionReceiver

`*flow.Workflow`, when used directly as a Step in another workflow (whether
added directly or embedded in a user struct), SHALL implement
`WorkflowOptionReceiver` (defined in the `workflow-options` capability spec).
Its `InheritOption` implementation merges the parent's `WorkflowOption`
into its own per the rules in `workflow-options`, and returns a `restore func()`
that the parent defers to rewind the receiver's `Option` to its
pre-inheritance state.

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