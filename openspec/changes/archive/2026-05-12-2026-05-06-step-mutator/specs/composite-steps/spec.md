## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: Sub-workflow construction inside Do

The framework SHALL support constructing a composite Step's sub-workflow inside its
`Do(ctx)` method, using a freshly created `flow.Workflow{}` and standard `Add` /
`Step` / `Pipe` builders. This pattern MUST NOT require implementing `BuildStep`.

When this pattern is used in combination with `flow.SubWorkflow` embedding, the inner
workflow SHALL remain visible to parent-level Mutators via the `MutatorReceiver`
propagation mechanism (see the `mutators` capability spec).

When this pattern is used WITHOUT `flow.SubWorkflow` embedding (e.g. by constructing a
local `flow.Workflow{}` inside `Do` and discarding it after `Do` returns), the inner
workflow SHALL be invisible to parent introspection helpers (`Has`, `As`, `HasStep`) and
to parent Mutator propagation. This is intentional: such a step is fully self-contained
and opaque to the outer workflow.

#### Scenario: Composite step constructs sub-workflow in Do
- **WHEN** a composite step embeds `flow.SubWorkflow` and inside `Do` calls `s.Reset()`
  followed by `s.Add(...)` then `s.Do(ctx)`
- **THEN** the sub-workflow runs correctly and parent Mutators reach its inner steps via
  `PrependMutators` (invoked once per inner-workflow execution; once-per-step semantics
  inside the inner workflow then take over)

#### Scenario: Local sub-workflow inside Do is opaque to parent
- **WHEN** a step constructs a `flow.Workflow{}` inside `Do` without embedding
  `SubWorkflow` and without exposing it via `Unwrap`
- **THEN** parent `flow.As[T](outer)` does not find inner steps and parent Mutators do
  not reach them

---

### Requirement: SubWorkflow implements MutatorReceiver

`flow.SubWorkflow` SHALL implement the `MutatorReceiver` interface defined in the
`mutators` capability spec. Its `PrependMutators` implementation prepends the supplied
Mutators to the inner workflow's `Mutators` slice on each invocation.

This requirement is what makes the deprecated `BuildStep` + `SubWorkflow` pattern and the
new `Do`-time + `SubWorkflow` pattern interchangeable from the parent Mutator
perspective.

#### Scenario: Parent Mutator propagates to SubWorkflow
- **WHEN** a parent workflow has `flow.Mutate[*Foo]` registered and a step embedding
  `flow.SubWorkflow` adds a `*Foo` to its inner workflow
- **THEN** the parent Mutator runs against the inner `*Foo` instance before each of its
  attempts

---

### Requirement: *Workflow implements MutatorReceiver

`*flow.Workflow`, when used directly as a Step in another workflow, SHALL implement the
`MutatorReceiver` interface. Its `PrependMutators` implementation prepends the supplied
Mutators to its own `Mutators` slice.

#### Scenario: Nested *Workflow inherits parent Mutators
- **WHEN** a `*flow.Workflow` containing `*Foo` is added as a step in another workflow
  that has `flow.Mutate[*Foo]` registered
- **THEN** the parent Mutator runs against the inner `*Foo` before each of its attempts
