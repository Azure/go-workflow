## Purpose

This capability covers the step-configuration surface of the go-workflow library.

## Requirements

### Requirement: Step builder API — DependsOn

`DependsOn` declares that the receiver Steps must execute after all listed upstream Steps
have terminated. The relationship is many-to-many: any number of Steps may depend on any
number of upstreams.

#### Scenario: Single downstream, single upstream
- **WHEN** `flow.Step(b).DependsOn(a)` is added to a Workflow
- **THEN** Step `a` terminates before Step `b` starts

#### Scenario: Multiple downstreams share one upstream
- **WHEN** `flow.Steps(b, c).DependsOn(a)` is added
- **THEN** both `b` and `c` wait for `a` to terminate before starting

#### Scenario: One downstream waits for multiple upstreams
- **WHEN** `flow.Step(c).DependsOn(a, b)` is added
- **THEN** `c` waits until both `a` and `b` are terminated

---

### Requirement: Pipe and BatchPipe sugar

`Pipe(steps...)` creates a linear chain where each step depends on the previous one.
`BatchPipe(batches...)` creates a linear chain between *groups* of steps, where all
steps within a batch run in parallel and the next batch waits for the entire previous batch.

#### Scenario: Pipe creates sequential chain
- **WHEN** `flow.Pipe(a, b, c)` is added
- **THEN** execution order is: `a` → `b` → `c` (each waits for the previous)

#### Scenario: BatchPipe runs each batch in parallel internally
- **WHEN** `flow.BatchPipe(flow.Steps(a, b), flow.Steps(c, d))` is added
- **THEN** `a` and `b` run in parallel; after both terminate `c` and `d` run in parallel

---

### Requirement: Idempotent Add with config merging

Adding the same Step to a Workflow more than once SHALL merge the configurations rather
than replacing them. Upstreams, `Before` callbacks, `After` callbacks, and `Option`
functions accumulate across all `Add` calls for the same Step.

#### Scenario: DependsOn accumulates across Add calls
- **WHEN** the same Step is added twice with different `DependsOn` targets
- **THEN** the Step depends on the union of both upstream sets

#### Scenario: Input callbacks accumulate in declaration order
- **WHEN** the same Step receives `Input` callbacks in multiple `Add` calls
- **THEN** all callbacks execute in the order they were declared

#### Scenario: Timeout last-write wins
- **WHEN** the same Step receives `Timeout` in multiple `Add` calls
- **THEN** the last `Timeout` value takes effect (Option slice is evaluated left-to-right,
  last writer wins because it overwrites the same field on `StepOption`)

---

### Requirement: Input callbacks (typed BeforeStep)

`Input` is a generic, type-safe variant of `BeforeStep`. It accepts a function
`func(context.Context, S) error` where `S` is the concrete Step type, and it is called
before `Do` on every attempt (including retries).

#### Scenario: Input receives the concrete Step pointer
- **WHEN** an `Input` callback is declared for a Step of type `*MyStep`
- **THEN** the callback receives the actual `*MyStep` pointer, allowing field mutation

#### Scenario: Input error aborts Do
- **WHEN** an `Input` callback returns a non-nil error
- **THEN** `Do` is NOT called; the Step fails with `ErrBeforeStep` wrapping the error

#### Scenario: Input is called per retry attempt
- **WHEN** a Step has both `Input` and `Retry` configured
- **THEN** the `Input` callback is called again before each retry attempt

---

### Requirement: Output callbacks (typed AfterStep for success)

`Output` is a type-safe `AfterStep` that is only invoked when `Do` returns `nil`.
It can be used to copy results out of a Step into an outer scope.

#### Scenario: Output only fires on success
- **WHEN** `Do` returns a non-nil error
- **THEN** the `Output` callback is NOT called

#### Scenario: Output receives the concrete Step pointer
- **WHEN** `Do` returns `nil` and an `Output` callback is registered
- **THEN** the callback receives the `*S` pointer after `Do`, allowing result extraction

---

### Requirement: BeforeStep callbacks

`BeforeStep` callbacks run before `Do` in the order they were declared. Each callback
receives and returns a `context.Context`, allowing downstream callbacks and `Do` to
receive a modified context (e.g., with added values).

#### Scenario: Context threading through BeforeStep chain
- **WHEN** a `BeforeStep` callback adds a value to the context and returns it
- **THEN** the modified context is passed to subsequent `BeforeStep` callbacks and to `Do`

#### Scenario: BeforeStep short-circuits on first error
- **WHEN** a `BeforeStep` callback returns a non-nil error
- **THEN** subsequent `BeforeStep` callbacks and `Do` are NOT called;
  the error is wrapped in `ErrBeforeStep`

---

### Requirement: AfterStep callbacks

`AfterStep` callbacks run after `Do` (or after a `BeforeStep` error) in declaration order.
Each callback receives the current error and returns an error, allowing it to suppress,
replace, or enrich the error.

#### Scenario: AfterStep can replace the error
- **WHEN** an `AfterStep` callback returns a different error than it received
- **THEN** the new error becomes the Step's result

#### Scenario: AfterStep runs even when Do fails
- **WHEN** `Do` returns a non-nil error
- **THEN** all `AfterStep` callbacks still execute in order

#### Scenario: AfterStep receives BeforeStep error
- **WHEN** a `BeforeStep` callback fails and produces an `ErrBeforeStep`
- **THEN** `AfterStep` callbacks receive that `ErrBeforeStep` as their `err` argument

---

### Requirement: DefaultOption propagation

A `DefaultOption` set on the `Workflow` is prepended to the Option slice of every
Step added after it is set. Step-level options declared after the default take precedence
because the slice is evaluated left-to-right and later writers overwrite earlier ones.

#### Scenario: DefaultOption applies to all Steps
- **WHEN** `workflow.DefaultOption` has a `Timeout` of 10 minutes
  and a Step is added without an explicit `Timeout`
- **THEN** `workflow.StateOf(step).Option().Timeout` equals 10 minutes

#### Scenario: Step-level option overrides DefaultOption
- **WHEN** `workflow.DefaultOption` has a `Timeout` of 10 minutes
  and a Step is added with `.Timeout(5 * time.Minute)`
- **THEN** the effective timeout for that Step is 5 minutes

#### Scenario: Timeout last-write wins
- **WHEN** both `DefaultOption` and the Step's own option declare a `Timeout`
- **THEN** the Step's explicit timeout takes effect (last writer wins in the option chain)

---

### Requirement: BuildStep lifecycle hook

If a Step implements `BuildStep()`, that method is called exactly once when the Step
is first added to a Workflow (via `workflow.Add`). `Reset()` is called on the Step
before `BuildStep()` to initialize it to a clean state.

#### Scenario: Reset called before BuildStep
- **WHEN** a Step that implements both `Reset()` and `BuildStep()` is added to a Workflow
- **THEN** `Reset()` is called before `BuildStep()` is called

### Requirement: Config merge destination follows StateOf

A `StepConfig` declared via `workflow.Add(flow.Step(s).X(...))` SHALL be merged into the
innermost workflow whose `state` map contains `s` as a key, not into the workflow on
which `Add` was invoked.

This rule SHALL apply uniformly to all `StepConfig` fields: `Upstreams` (via the
`Cross-boundary upstream registration` rule in `composite-steps`), `Before`, `After`,
and `Option`.

The destination is computed by `StateOf(s)`, which:

1. Returns `w.state[s]` if `s` is a direct root of `w`.
2. Otherwise walks roots of `w` and, for each root that implements
   `StateOf(Steper) *State` (i.e. `*Workflow` and `SubWorkflow`-embedding steps),
   recursively asks that root for `StateOf(s)`.
3. Returns the deepest non-nil match.

This is existing runtime behavior; this requirement codifies it because the Mutator
mechanism (`mutators` capability) relies on it being well-defined: a Mutator-returned
Builder is merged through the same `StateOf`-driven destination logic.

#### Scenario: Add on outer merges Input into inner state
- **GIVEN** an `inner` workflow containing step `x`, and `outer` containing `inner` as a step
- **WHEN** `outer.Add(flow.Step(x).Input(fn))` is called
- **THEN** `fn` is appended to `inner.StateOf(x).Config.Before`
- **AND** `outer.StateOf(inner).Config.Before` does NOT contain `fn`

#### Scenario: Add on outer merges Option into inner state
- **GIVEN** an `inner` workflow containing step `x`, and `outer` containing `inner` as a step
- **WHEN** `outer.Add(flow.Step(x).Timeout(5 * time.Minute))` is called
- **THEN** the timeout takes effect on `x` via `inner.StateOf(x).Config.Option`

#### Scenario: Multi-level nesting reaches innermost
- **GIVEN** `outer → midA → midB → x`
- **WHEN** `outer.Add(flow.Step(x).Input(fn))` is called
- **THEN** `fn` ends up in `midB.StateOf(x).Config.Before` (the innermost workflow that
  has `x` in its `state` map), not in `outer` or `midA`

---

### Requirement: Mutators field on Workflow

`flow.Workflow` SHALL expose a `Mutators []Mutator` field that holds zero or more
`flow.Mutator` instances (constructed via `flow.Mutate[T]`).

Mutators are evaluated **once per step instance**, just before the step's first attempt
begins. Each matched Mutator contributes a `flow.Builder` whose configuration for the
matched step is merged into the step's existing `StepConfig` using the same merge rules
as the `Idempotent Add with config merging` requirement (`Upstreams` union, `Before`
append, `After` append, `Option` append).

After the merge, the step proceeds through the standard per-attempt onion (Interceptors,
retry loop, Before chain, Do, After chain). The Mutator's contribution is therefore
visible to all attempts of the step.

The `Mutators` slice is independent of `Before` / `After` / `Option` configuration on
`StepConfig`: Mutators are workflow-level (apply to all matching steps), while
`StepConfig` is per-step-instance.

#### Scenario: Mutator contribution merges with plan-declared StepConfig
- **WHEN** a workflow has `Mutators = [flow.Mutate[*Foo](func(ctx context.Context, f *Foo) flow.Builder {
  return flow.Step(f).Input(callbackA) })]` and a `*Foo` step is added with
  `flow.Step(foo).Input(callbackB)`
- **THEN** before the first attempt of `foo`, both `callbackA` and `callbackB` are
  present in the step's `Before` chain (Mutator contribution merged into existing config)

#### Scenario: Mutator contribution survives across attempts
- **WHEN** a Mutator contributes a `Before` callback and the step retries 3 times
- **THEN** the contributed callback runs on every attempt (because it lives in the
  step's permanent `Before` chain after merge)

#### Scenario: Mutator runs once per step
- **WHEN** a Mutator's user function appends to a workflow-scoped slice and the matched
  step retries 3 times
- **THEN** the slice has exactly one new entry (Mutator user function ran once)

