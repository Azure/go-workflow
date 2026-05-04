## ADDED Requirements

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

If a Step implements `BuildStep()`, the Workflow calls it exactly once when the Step is
first added. This allows composite Steps to initialize their internal sub-workflow
lazily at add-time rather than at construction time.

If the Step also implements `Reset()`, the Workflow calls `Reset()` before `BuildStep()`
to allow the Step to clear any previous state before rebuilding.

`BuildStep()` is NOT called again if the same Step pointer is added a second time.

#### Scenario: BuildStep called once on first Add
- **WHEN** a Step implementing `BuildStep()` is added to a Workflow
- **THEN** `BuildStep()` is called exactly once, regardless of how many times the
  Step is subsequently added

#### Scenario: Reset called before BuildStep
- **WHEN** a Step implements both `Reset()` and `BuildStep()`
  and is added to a Workflow
- **THEN** `Reset()` is called first, then `BuildStep()`

---

### Requirement: SubWorkflow — Workflow as a Step

`SubWorkflow` is an embeddable helper that exposes a nested `Workflow` as a Step.
Embedding `flow.SubWorkflow` in a struct gives it `Do`, `Add`, `Unwrap`, and `Reset` methods.
The outer Workflow orchestrates the composite Step; the inner Workflow orchestrates the
sub-steps. The outer Workflow can reach into the inner Steps using `Has`, `As`, and
`HasStep`.

#### Scenario: Workflow can be used directly as a Step
- **WHEN** a `*flow.Workflow` is added as a Step inside another Workflow
- **THEN** the outer Workflow treats the inner Workflow as an opaque Step and calls
  its `Do` method, which in turn executes the inner DAG

#### Scenario: Sub-steps are reachable from the outer Workflow
- **WHEN** an outer Workflow contains a composite Step that embeds `SubWorkflow`
  with inner Steps
- **THEN** `flow.As[*InnerStepType](outerWorkflow)` returns the inner Step instances

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
