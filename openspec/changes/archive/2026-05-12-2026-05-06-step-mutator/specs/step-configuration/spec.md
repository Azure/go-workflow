## ADDED Requirements

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
