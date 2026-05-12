# mutators — Spec Delta

## MODIFIED Requirements

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

---

### Requirement: Mutator propagation to nested workflows

A nested workflow (whether `*Workflow` used directly as a step or a user
struct embedding `flow.Workflow`) SHALL receive its parent's Mutators via
the `WorkflowOptionReceiver.InheritOption` mechanism defined in the
`workflow-options` capability.

Specifically, the parent SHALL invoke `child.InheritOption(parent.Option)`
once in its `Do()` prologue. The implementation of `InheritOption` MUST
prepend the parent's `Option.Mutators` to the child's `Option.Mutators`
slice (parent contributions run first within the child).

The parent SHALL NOT directly walk into a child workflow's `state` map to
apply Mutators; that remains the inner workflow's responsibility once it
begins its own scheduling pass with the merged `Option.Mutators` slice.

The previous `MutatorReceiver` interface and `PrependMutators` method SHALL
NOT exist.

#### Scenario: Parent Mutator reaches inner *Foo via InheritOption
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

#### Scenario: Multi-level Mutator propagation preserves order
- **GIVEN** grandparent `Option.Mutators = [A]`, parent `Option.Mutators = [B]`, child `Option.Mutators = [C]`
- **WHEN** grandparent runs the parent which runs the child
- **THEN** the child's effective Mutators (during its own scheduling) is `[A, B, C]`

---

## REMOVED Requirements

### Requirement: ~~Top-level Workflow.Mutators field~~

**Reason:** Moved to `Workflow.Option.Mutators` under the new `WorkflowOption`
grouping.

### Requirement: ~~MutatorReceiver propagation interface~~

**Reason:** Replaced by `WorkflowOptionReceiver.InheritOption`, which carries
`Mutators` as one component of the inherited `WorkflowOption`.
