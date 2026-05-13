# Workflow Option Consolidation — Brainstorming Spec

**Date:** 2026-05-12
**Topic:** Group `Workflow`'s scattered configuration fields into a single
`WorkflowOption` value, unify the parent→child propagation interfaces into one
`WorkflowOptionReceiver`, and remove `SubWorkflow` (deprecation now, removal in
the next major).

This is the brainstorming output. The implementation-facing change lives at
`openspec/changes/2026-05-12-workflow-option/`.

---

## Why

Today `Workflow` carries nine configuration fields directly on the struct
(`MaxConcurrency`, `DontPanic`, `SkipAsError`, `Clock`, `DefaultOption`,
`Mutators`, `StepInterceptors`, `AttemptInterceptors`, `IsolateInterceptors`).
The list keeps growing, the propagation rules into sub-workflows are
inconsistent (Mutators / Interceptors propagate via two separate ad-hoc
interfaces; the other six don't propagate at all), and the surface that gets
promoted into a user struct that embeds `Workflow` is wide and noisy.

Three problems fall out of this:

1. **Visual clutter.** `Workflow` mixes runtime state and configuration in one
   place; the godoc reader has to know which is which.
2. **Inconsistent propagation.** `MutatorReceiver` and `InterceptorReceiver`
   are two separate interfaces with two separate type assertions in the
   scheduler. Any future workflow-level option that should "flow into a child"
   would need a third interface.
3. **`SubWorkflow` no longer earns its keep.** Its only remaining job is to
   keep the inner `Workflow` field unexported on a user struct that embeds it.
   With the configuration fields collapsed under a single `Option` field, that
   value disappears — the user struct ends up with one promoted name
   (`Option`) either way. After `BuildStep` was deprecated alongside the
   Mutator API, `SubWorkflow.Reset` is also already deprecated and removed in
   the next major. Keeping `SubWorkflow` adds a parallel type with no unique
   capability.

## What Changes

- Introduce `WorkflowOption` struct that owns all nine fields. Scalar fields
  become pointer-typed (`*int`, `*bool`, `clock.Clock`, `*StepOption`) so
  unset / explicitly-zero are distinguishable; slice fields stay slices.
- `Workflow` exposes one named field `Option WorkflowOption`. The nine former
  top-level fields are removed.
- `IsolateInterceptors` is renamed `DontInherit`; semantics extend from
  "don't inherit interceptors" to "don't inherit any of the parent's
  WorkflowOption". Naming aligns with `DontPanic`.
- Introduce single propagation interface `WorkflowOptionReceiver` with a
  single method `InheritOption(parent WorkflowOption)`. `*Workflow` implements
  it.
- Remove `MutatorReceiver`, `InterceptorReceiver`,
  `Workflow.PrependMutators`, `Workflow.PrependInterceptors`,
  `findInterceptorReceiver`. They are not in widespread external use; clean
  removal is preferred over a parallel deprecated path.
- `SubWorkflow` is deprecated (godoc `// Deprecated:` notice) but kept for one
  release window, because it is something users embed in their own structs.
  Its `PrependMutators` / `PrependInterceptors` methods are removed (the
  interfaces they satisfy no longer exist); a single `InheritOption`
  delegating to the inner `Workflow` is added.
- The scheduling loop is rewritten: a single `Do()` prologue pass walks each
  root step's Unwrap chain via `findOptionReceiver`, and calls
  `InheritOption(parent.Option)` exactly once before scheduling begins.
- The `inheritedStep` / `inheritedAttempt` side fields and their careful
  `reset()` exclusion logic are removed. The new mechanism uses a
  snapshot-and-restore of `w.Option` around `Do()`, which is simpler and
  prevents accumulation across repeated `Do()` calls without special-cased
  state.
- The runtime no longer offers a helper to construct pointer values
  (`flow.Bool`, `flow.Int`, etc.). Callers use the language: `mc := 4;
  Option: WorkflowOption{MaxConcurrency: &mc}`, or Go 1.26's `new(value)`,
  or their own `Ptr[T]` helper.

## Decisions Locked In

| Decision | Choice | Reason |
|---|---|---|
| `Option` is a named field, not embedded | `Workflow.Option WorkflowOption` | An embedded `WorkflowOption` would still be promoted onto user structs that embed `Workflow`, defeating the "one name on the user struct" goal. |
| Scalar fields are pointers; slices stay slices | `*int`, `*bool`, `clock.Clock`, `*StepOption`; `[]Mutator` etc. | Pointers cleanly distinguish "unset" from "explicit zero" so child can override parent's `DontPanic=true` with `DontPanic=false`. Slices already encode "unset" as nil/empty and have an established prepend semantics for cross-cutting concerns. |
| Single `InheritOption` method, not three | `WorkflowOptionReceiver { InheritOption(parent WorkflowOption) }` | Keeps API surface small; merge details (scalar override vs slice prepend vs `DontInherit` no-op) live inside `Workflow.InheritOption`, not in the contract. |
| Scalar inheritance: child nil → take parent's value | Standard Kubernetes / AWS-SDK pattern | Lets a sub-workflow positively inherit `DontPanic` / `MaxConcurrency` from its parent without restating it. Direct answer to the use case "I want `DontPanic` to flow into the child." |
| Slice inheritance: parent prepended to child | Same as today's `PrependMutators` / `PrependInterceptors` | Mutators and Interceptors are cross-cutting workflow-level concerns; parent's contributions running before child's is the existing, correct behavior. |
| `DontInherit` is one boolean, not nine | Whole-Option opt-out | Field-by-field opt-out (one `Inherit*` companion per field) doubles the API surface to expose an internal mechanism. The existing `IsolateInterceptors` shows the whole-Option pattern is sufficient in practice. |
| Old receiver interfaces and `*Workflow.Prepend*` methods are removed, not deprecated | Hard removal | They have no production users outside this repo. Carrying a deprecated parallel propagation path complicates the scheduler for one release with no benefit. |
| `SubWorkflow` is deprecated, not removed | One-release window | Users embed it inside their own structs; an immediate removal would break their type definitions, not just call sites. Removing in the next major is consistent with the existing `BuildStep` deprecation. |
| `inheritedStep` / `inheritedAttempt` side fields removed | Replaced by `Do()` snapshot/restore of `w.Option` | The original side-channel existed because `reset()` couldn't safely clear the parent's just-prepended slices. Snapshotting at `Do()` entry and restoring at exit is simpler and eliminates the special-cased rule entirely. |
| Sub-workflow via direct embedding of `flow.Workflow` | The official recommended pattern | With the nine top-level fields collapsed, embedding `flow.Workflow` only promotes one configuration name (`Option`) plus `Add` / `Do` / `InheritOption`. `SubWorkflow`'s only remaining unique feature was hiding the nine fields, which no longer exist. |
| Behavior of mutating `child.Option` after `parent.Do()` has started but before `child.Do()` runs | Undefined | Already covered by the existing "mutating a Workflow mid-run is undefined" rule; explicitly noted in `WorkflowOption` godoc. |
| Helper for pointer-to-value | Not provided | Users have `&v`, Go 1.26 `new(value)`, or their own one-line `Ptr[T any]` helper. |

## Architecture

### Type layout

```go
// workflow_option.go (new)

// WorkflowOption groups all configuration that a Workflow exposes to its
// caller AND inherits from a parent Workflow when used as a sub-workflow
// step.
//
// Scalar fields are pointers so that "unset" and "explicit zero value" are
// distinguishable: a nil pointer means "inherit from parent (or use the
// runtime default)"; a non-nil pointer is the child's own choice. Slice
// fields are not pointer-typed; the parent's slice is prepended to the
// child's slice (parent contributions run first), preserving the existing
// Mutator and Interceptor propagation semantics.
//
// Mutating a Workflow's Option after Do() has started is undefined behavior.
type WorkflowOption struct {
    MaxConcurrency    *int
    DontPanic         *bool
    SkipAsError       *bool
    Clock             clock.Clock // interface; nil = inherit / wall clock
    StepDefaults *StepOption // already a pointer

    Mutators            []Mutator           // parent prepended on inherit
    StepInterceptors    []StepInterceptor   // parent prepended on inherit
    AttemptInterceptors []AttemptInterceptor // parent prepended on inherit

    // When true and this Workflow is used as a sub-workflow step,
    // InheritOption is a no-op: nothing flows in from the parent. Replaces
    // the previous IsolateInterceptors flag and now governs the entire
    // WorkflowOption, not just interceptors.
    DontInherit bool
}

// WorkflowOptionReceiver is implemented by any Step that contains a
// sub-workflow. The parent's Do() prologue locates the nearest receiver in
// each root step's Unwrap chain and calls InheritOption ONCE before any
// scheduling begins, so the child's Do() observes the merged Option.
//
// *Workflow itself implements this interface; users get inheritance for
// free by embedding flow.Workflow in their own Step type.
type WorkflowOptionReceiver interface {
    InheritOption(parent WorkflowOption)
}
```

```go
// workflow.go

type Workflow struct {
    Option WorkflowOption
    StepBuilder

    steps        map[Steper]*State
    statusChange *sync.Cond
    leaseBucket  chan struct{}
    waitGroup    sync.WaitGroup
    isRunning    sync.Mutex
}

// InheritOption implements WorkflowOptionReceiver.
func (w *Workflow) InheritOption(parent WorkflowOption) {
    if w.Option.DontInherit {
        return
    }
    if w.Option.MaxConcurrency == nil    { w.Option.MaxConcurrency = parent.MaxConcurrency }
    if w.Option.DontPanic == nil         { w.Option.DontPanic = parent.DontPanic }
    if w.Option.SkipAsError == nil       { w.Option.SkipAsError = parent.SkipAsError }
    if w.Option.Clock == nil             { w.Option.Clock = parent.Clock }
    if w.Option.StepDefaults == nil { w.Option.StepDefaults = parent.StepDefaults }

    w.Option.Mutators            = prependSlice(parent.Mutators, w.Option.Mutators)
    w.Option.StepInterceptors    = prependSlice(parent.StepInterceptors, w.Option.StepInterceptors)
    w.Option.AttemptInterceptors = prependSlice(parent.AttemptInterceptors, w.Option.AttemptInterceptors)
}
```

`prependSlice` allocates a fresh backing array; it never mutates either input
slice. This guarantee is what allows the snapshot/restore mechanism (below) to
be a shallow copy.

### Internal accessors

The runtime needs effective scalar values. Add unexported helpers on
`*Workflow`:

```go
func (w *Workflow) maxConcurrency() int       { if w.Option.MaxConcurrency == nil { return 0 }; return *w.Option.MaxConcurrency }
func (w *Workflow) dontPanic() bool           { return w.Option.DontPanic != nil && *w.Option.DontPanic }
func (w *Workflow) skipAsError() bool         { return w.Option.SkipAsError != nil && *w.Option.SkipAsError }
func (w *Workflow) clock() clock.Clock        { if w.Option.Clock == nil { return clock.New() }; return w.Option.Clock }
func (w *Workflow) defaultStepOption() *StepOption { return w.Option.StepDefaults }
```

All existing `w.MaxConcurrency`, `w.DontPanic`, `w.SkipAsError`, `w.Clock`,
`w.DefaultOption` reads in the runtime are rewritten to call these.

### Propagation flow

```go
func (w *Workflow) Do(ctx context.Context) error {
    if !w.isRunning.TryLock() {
        return ErrWorkflowIsRunning
    }
    defer w.isRunning.Unlock()

    // Snapshot Option so InheritOption writes are reverted at exit, allowing
    // multiple Do() runs without accumulation.
    optSnapshot := w.Option
    defer func() { w.Option = optSnapshot }()

    w.reset()
    w.init()

    // Propagate parent Option into any sub-workflow root steps before tick.
    for step := range w.steps {
        if recv := findOptionReceiver(step); recv != nil {
            recv.InheritOption(w.Option)
        }
    }

    return w.tick(ctx)
}

func findOptionReceiver(s Steper) WorkflowOptionReceiver {
    var found WorkflowOptionReceiver
    Walk(s, func(s Steper) bool {
        if r, ok := s.(WorkflowOptionReceiver); ok {
            found = r
            return false // stop on first match
        }
        return true
    })
    return found
}
```

Snapshot is a shallow copy of `WorkflowOption`. `InheritOption` only ever
writes via `prependSlice` (which allocates fresh) or by overwriting nil
pointer fields (which doesn't touch parent's pointer values). So the restore
at defer is correct without a deep copy.

### `SubWorkflow` during the deprecation window

```go
// Deprecated: Embed flow.Workflow directly. SubWorkflow will be removed in
// the next major version of go-workflow.
type SubWorkflow struct{ w Workflow }

func (s *SubWorkflow) Unwrap() Steper                      { return &s.w }
func (s *SubWorkflow) Add(builders ...Builder) *Workflow   { return s.w.Add(builders...) }
func (s *SubWorkflow) Do(ctx context.Context) error        { return s.w.Do(ctx) }

// InheritOption forwards to the inner Workflow so SubWorkflow continues to
// participate in parent → child Option propagation during the deprecation
// window.
func (s *SubWorkflow) InheritOption(parent WorkflowOption) { s.w.InheritOption(parent) }
```

`SubWorkflow.Reset` and `SubWorkflow.PrependMutators` /
`PrependInterceptors` are removed (the latter two satisfy interfaces that no
longer exist; `Reset` was already deprecated for removal).

## User Migration

**Before:**

```go
type Deploy struct {
    flow.SubWorkflow
    Region string
}

w := &flow.Workflow{
    MaxConcurrency: 4,
    DontPanic:      true,
    Mutators:       []flow.Mutator{logMutator},
    IsolateInterceptors: false,
}
```

**After:**

```go
type Deploy struct {
    flow.Workflow            // embed Workflow directly
    Region string
}

mc := 4
dp := true
w := &flow.Workflow{
    Option: flow.WorkflowOption{
        MaxConcurrency: &mc,
        DontPanic:      &dp,
        Mutators:       []flow.Mutator{logMutator},
    },
}
```

## Removed in This Release (No Deprecation)

- `MutatorReceiver` (interface)
- `InterceptorReceiver` (interface)
- `Workflow.PrependMutators` (method)
- `Workflow.PrependInterceptors` (method)
- `findInterceptorReceiver` (function)
- `Workflow.IsolateInterceptors` (renamed → `Option.DontInherit`)
- The nine top-level option fields on `Workflow` (moved into `Option`)
- `SubWorkflow.PrependMutators` / `SubWorkflow.PrependInterceptors`
- `Workflow.inheritedStep` / `inheritedAttempt` (internal; replaced by
  Option snapshot/restore)

## Removed in the Next Major

- `SubWorkflow` (struct)
- `SubWorkflow.Reset` (already deprecated)
- `SubWorkflow.InheritOption` (goes with the type)
- All godoc references to `SubWorkflow`

## Testing Strategy

### Migration of existing tests

`workflow_options_test.go` and any test that constructs `&Workflow{ Field: ...
}` for one of the nine former fields is rewritten mechanically to use
`Option: WorkflowOption{ Field: ... }`, with pointer fields wrapped. Test
*assertions* do not change — the user-observable behavior of every existing
field is preserved.

### New: `workflow_option_inherit_test.go`

Table-driven matrix:

- Scalar nil → inherits parent's value
- Scalar non-nil → child wins
- Scalar explicit zero (e.g., `*int = 0`) → child wins, distinguished from
  nil/inherit
- `Mutators` / `StepInterceptors` / `AttemptInterceptors` → parent prepended
- `Clock` nil → inherits parent
- `StepDefaults` nil → inherits parent
- `DontInherit = true` → no field changes regardless of parent

Plus dedicated tests:

- **Multi-level nesting:** grandparent → parent → child, each with one
  Mutator; child's effective `Mutators` is `[grandparent, parent, child]`.
- **`Do()` snapshot/restore:** running `parent.Do()` twice with a child that
  has `Mutators=[B]` and parent `Mutators=[A]` results in child's effective
  `Mutators` being `[A, B]` *during* each run and `[B]` *after* each run, with
  no accumulation across runs.
- **`SubWorkflow` deprecation smoke:** an embedded `SubWorkflow` still
  receives the parent's Option via the delegated `InheritOption`.

### Untouched

`execution_model_test.go`, `branch_test.go`, `condition_test.go`,
`retry_test.go`, `wrap_test.go`, `mutator_test.go` content — the scheduling
core, branching, condition, retry, wrapping, and Mutator semantics themselves
are unchanged.

### Examples

`example/13_mutators_test.go` and any example referencing `flow.SubWorkflow`
update to embed `flow.Workflow` and use `Option`. A short note pointing to the
deprecated `SubWorkflow` is preserved during the deprecation window.
