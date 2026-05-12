# Workflow Option Consolidation — Design

This document covers the implementation-facing specifics. The brainstorming
output (decision log, alternatives considered, naming discussion) is at
`docs/superpowers/specs/2026-05-12-workflow-option-design.md`. Anything covered
there is referenced rather than duplicated.

---

## Summary of Decisions

| Decision | Choice | Reason |
|---|---|---|
| Configuration grouping | New `WorkflowOption` struct, exposed as `Workflow.Option` (named, not embedded) | Embedded would still promote field names onto user structs that embed `Workflow`, defeating the "one name on the user struct" goal. |
| Scalar field types | Pointers (`*int`, `*bool`, `clock.Clock`, `*StepOption`) | Distinguishes "unset → inherit from parent" from "explicit zero → child wins". Standard K8s / AWS-SDK pattern. |
| Slice field types | Stay `[]Mutator` etc. | Existing prepend semantics already encode "unset" as nil/empty; pointers would add no value. |
| Propagation interface | Single `WorkflowOptionReceiver { InheritOption(parent WorkflowOption) }` | Replaces `MutatorReceiver` and `InterceptorReceiver`. Merge details live inside `Workflow.InheritOption`, not in the contract. |
| Scalar inheritance rule | Child-nil pointer takes parent's value; non-nil child wins | Lets sub-workflow positively inherit `DontPanic` / `MaxConcurrency` from parent without restating. |
| Slice inheritance rule | Parent prepended to child | Preserves existing Mutator / Interceptor semantics. |
| Opt-out granularity | One `DontInherit bool` covering the whole Option | Per-field `Inherit*` companions would double API surface to expose an internal mechanism. The previous `IsolateInterceptors` shows whole-Option opt-out is sufficient. |
| Renaming `IsolateInterceptors` | `DontInherit` | Name matches `DontPanic`; semantics widen from interceptor-only to whole-Option. |
| Old receiver interfaces fate | Remove (no deprecation) | No widespread external implementations. Carrying parallel deprecated paths complicates the scheduler for one release with no benefit. |
| Old top-level configuration fields fate | Remove (breaking, no deprecation) | Same release as the receiver removal; migration is mechanical. Go does not effectively support `// Deprecated:` on struct fields (staticcheck recognizes it but most editor tooling doesn't). |
| `SubWorkflow` fate | Deprecate now, remove next major | Users embed it in their own structs; immediate removal would break their type definitions, not just call sites. One release window mirrors the `BuildStep` deprecation. |
| `SubWorkflow.PrependMutators` / `PrependInterceptors` | Remove now | The interfaces they satisfy no longer exist. A single `SubWorkflow.InheritOption` delegates to inner `w.InheritOption` for the deprecation window. |
| Propagation invocation site | Once-per-`Do()` prologue, after `init()`, before `tick` | Single call site; both Mutator and Interceptor inheritance happen at the same moment. The previous Mutator-at-`:641` / Interceptor-at-`:768` split was historical; nothing in the runtime requires it. |
| Receiver lookup | New `findOptionReceiver(s)`, walks `Unwrap()` chain, returns first match | Mirrors the removed `findInterceptorReceiver`. |
| Multiple-`Do()` accumulation prevention | Shallow snapshot of `w.Option` at `Do()` entry, defer restore | Replaces the `inheritedStep` / `inheritedAttempt` side fields and their special-cased "`reset()` must not clear these" rule. Allowed because `prependSlice` always allocates fresh, and overwriting nil-pointer fields with parent values does not mutate the parent's pointers. |
| Mid-run mutation of `Option` | Undefined behavior | Already covered by the existing "mutating a Workflow mid-run is undefined" rule; reaffirmed in `WorkflowOption` godoc. |
| Helper for pointer-to-value | Not provided | Users have `&v`, Go 1.26 `new(value)`, or their own `Ptr[T any]` helper. |

---

## Implementation Notes

### `WorkflowOption` and `WorkflowOptionReceiver`

New file `workflow_option.go`:

```go
type WorkflowOption struct {
    MaxConcurrency    *int
    DontPanic         *bool
    SkipAsError       *bool
    Clock             clock.Clock
    DefaultStepOption *StepOption

    Mutators            []Mutator
    StepInterceptors    []StepInterceptor
    AttemptInterceptors []AttemptInterceptor

    DontInherit bool
}

type WorkflowOptionReceiver interface {
    InheritOption(parent WorkflowOption)
}
```

### `Workflow` struct

The configuration fields disappear. Runtime fields stay:

```go
type Workflow struct {
    Option WorkflowOption
    StepBuilder

    steps        map[Steper]*State
    statusChange *sync.Cond
    leaseBucket  chan struct{}
    waitGroup    sync.WaitGroup
    isRunning    sync.Mutex
}
```

`inheritedStep` and `inheritedAttempt` are removed; the snapshot/restore in
`Do()` covers the same invariant.

### `InheritOption`

```go
func (w *Workflow) InheritOption(parent WorkflowOption) {
    if w.Option.DontInherit {
        return
    }
    if w.Option.MaxConcurrency == nil    { w.Option.MaxConcurrency = parent.MaxConcurrency }
    if w.Option.DontPanic == nil         { w.Option.DontPanic = parent.DontPanic }
    if w.Option.SkipAsError == nil       { w.Option.SkipAsError = parent.SkipAsError }
    if w.Option.Clock == nil             { w.Option.Clock = parent.Clock }
    if w.Option.DefaultStepOption == nil { w.Option.DefaultStepOption = parent.DefaultStepOption }
    w.Option.Mutators            = prependSlice(parent.Mutators,            w.Option.Mutators)
    w.Option.StepInterceptors    = prependSlice(parent.StepInterceptors,    w.Option.StepInterceptors)
    w.Option.AttemptInterceptors = prependSlice(parent.AttemptInterceptors, w.Option.AttemptInterceptors)
}
```

`prependSlice` MUST allocate a fresh backing array and never mutate either
input slice. This is what allows the snapshot at `Do()` entry to be a shallow
copy.

### `Do()` prologue

```go
func (w *Workflow) Do(ctx context.Context) error {
    if !w.isRunning.TryLock() { return ErrWorkflowIsRunning }
    defer w.isRunning.Unlock()

    optSnapshot := w.Option
    defer func() { w.Option = optSnapshot }()

    w.reset()
    w.init()

    for step := range w.steps {
        if recv := findOptionReceiver(step); recv != nil {
            recv.InheritOption(w.Option)
        }
    }

    return w.tick(ctx)
}
```

`findOptionReceiver` mirrors the removed `findInterceptorReceiver`:

```go
func findOptionReceiver(s Steper) WorkflowOptionReceiver {
    var found WorkflowOptionReceiver
    Walk(s, func(s Steper) bool {
        if r, ok := s.(WorkflowOptionReceiver); ok {
            found = r
            return false
        }
        return true
    })
    return found
}
```

### Internal accessors for scalars

The runtime currently reads `w.MaxConcurrency`, `w.DontPanic`, `w.SkipAsError`,
`w.Clock`, `w.DefaultOption` directly. Each call site is rewritten to use a
small helper that handles nil-pointer dereferencing and runtime defaults:

```go
func (w *Workflow) maxConcurrency() int       { if w.Option.MaxConcurrency == nil { return 0 }; return *w.Option.MaxConcurrency }
func (w *Workflow) dontPanic() bool           { return w.Option.DontPanic != nil && *w.Option.DontPanic }
func (w *Workflow) skipAsError() bool         { return w.Option.SkipAsError != nil && *w.Option.SkipAsError }
func (w *Workflow) clock() clock.Clock        { if w.Option.Clock == nil { return clock.New() }; return w.Option.Clock }
```

Known direct-read sites in `workflow.go` (line numbers approximate, pre-change):
`108`, `188`, `374-378`, `498`, `501`, `652`, `668`, `724`, `751`, `766`,
`776`, `778`, `800`, `824`. All are rewritten to call helpers.

### `SubWorkflow` during the deprecation window

```go
// Deprecated: Embed flow.Workflow directly. SubWorkflow will be removed in
// the next major version of go-workflow.
type SubWorkflow struct{ w Workflow }

func (s *SubWorkflow) Unwrap() Steper                      { return &s.w }
func (s *SubWorkflow) Add(builders ...Builder) *Workflow   { return s.w.Add(builders...) }
func (s *SubWorkflow) Do(ctx context.Context) error        { return s.w.Do(ctx) }
func (s *SubWorkflow) InheritOption(parent WorkflowOption) { s.w.InheritOption(parent) }
```

`SubWorkflow.Reset` (already deprecated for removal), `SubWorkflow.PrependMutators`,
and `SubWorkflow.PrependInterceptors` are removed in this change. The first
goes with the `BuildStep` deprecation; the latter two satisfy interfaces that
no longer exist.

---

## Coordination

This change touches the same scheduling-time path as the existing
`step-interceptor` and `mutators` capabilities. Specifically:

- The Mutator dispatch site in the scheduler (`workflow.go:641`,
  `if recv, ok := step.(MutatorReceiver); ok`) is removed. Mutators are now
  delivered to a sub-workflow via `WorkflowOptionReceiver.InheritOption` in
  the prologue, and the sub-workflow's own scheduling pass dispatches them.
  The Mutator authoring contract (signature, once-per-step, Builder return)
  is unchanged.
- The Interceptor dispatch site (`workflow.go:768`,
  `findInterceptorReceiver`) is removed. Interceptors propagate via the same
  `InheritOption` prologue pass. Effective interceptor chain construction
  inside the child workflow remains identical.
- `effectiveStepInterceptors` / `effectiveAttemptInterceptors` previously
  read from the side fields `inheritedStep` / `inheritedAttempt`. After this
  change, both the parent's and child's interceptors live in the same
  `Option.StepInterceptors` / `Option.AttemptInterceptors` slices (already
  merged by `InheritOption`), so the helpers simplify to direct returns.
- The `composite-steps` capability is updated to recommend embedding
  `flow.Workflow` directly; `flow.SubWorkflow` is preserved one release for
  in-flight migrations.

---

## Behavior Change to Document

Setting a scalar option (`DontPanic`, `MaxConcurrency`, etc.) on the parent
workflow now causes that value to flow into any sub-workflow that left the
field unset (nil pointer). Before this change those scalars did not
propagate. Two implications:

1. **Positive use case** — the motivating example: a parent that wants
   `DontPanic` for the whole tree no longer needs to restate it on every
   sub-workflow. This was previously a manual chore.
2. **Mitigation for users who relied on non-propagation**: set
   `Option.DontInherit = true` on the child, or set the desired non-default
   explicitly on the child (e.g., `MaxConcurrency: &noLimit` with `noLimit
   := 0` to keep the child unlimited under a parent that limits).

This is documented in `WorkflowOption` godoc, the `workflow-options` spec,
and the CHANGELOG.

---

## Test Plan

Two existing files require migration:

- `workflow_options_test.go` — every `&Workflow{ Field: ... }` literal is
  rewritten to use `Option: WorkflowOption{ ... }` with scalars wrapped as
  pointers. Assertions unchanged.
- `workflow_mutator_test.go` — any test that constructs Mutators on
  `Workflow.Mutators` is rewritten to `Option.Mutators`. Tests that exercise
  `PrependMutators` are rewritten to verify `InheritOption` instead.

One existing file likely requires migration if it touches interceptor
propagation (`workflow_test.go`, depending on what the existing tests cover).

New file: `workflow_option_inherit_test.go` covering the table-driven matrix
in the brainstorming spec (scalar nil/set/explicit-zero × all five scalar
fields, slice prepend × all three slice fields, `DontInherit`), plus
multi-level nesting and `Do()` snapshot/restore.

`SubWorkflow` deprecation smoke test (one happy-path case) lives alongside
the `SubWorkflow` definition or in a dedicated `subworkflow_deprecated_test.go`.

Examples: `example/13_mutators_test.go` and any other example referencing
`flow.SubWorkflow` migrate to embed `flow.Workflow` directly.
