## Why

Today go-workflow requires all steps to be declared before `workflow.Do()` is called. The DAG
is static. This is fine for most cases, but a common pattern cannot be expressed cleanly:
a step produces a list of items at runtime, and you want to process each item in parallel
as an independent step — the classic fan-out / map-reduce pattern.

Current workarounds:
1. Pre-allocate a fixed-size slice of steps with the maximum possible items — wasteful and
   requires knowing the count upfront.
2. Use a `SubWorkflow` step that builds and runs its own inner workflow dynamically — works,
   but the inner workflow is invisible to the outer workflow's status tracking and event sink.
3. Use a single step that processes all items sequentially or with manual goroutines — loses
   the framework's retry-per-item, timeout-per-item, and observability guarantees.

The goal is to let a step declare children at runtime, which the workflow then schedules
as first-class steps.

## What Changes

A new optional interface `Spawner` (name TBD) that a step can implement. After the step's
`Do()` returns successfully, the workflow calls `Spawn()` to get new steps, injects them into
the running DAG, and schedules them with the step as their upstream.

## Proposed Approaches

### Approach A — `Spawner` interface (post-Do hook)

```go
type Spawner interface {
    Spawn() []Builder
}
```

After `Do()` succeeds, the workflow calls `Spawn()` and merges the returned builders into
the live DAG. Simple, zero new concepts, fully backward compatible.

Downside: `Spawn()` is called after `Do()`, so items must be stored on the step struct
between `Do()` and `Spawn()` — minor coupling, but acceptable.

### Approach B — Dynamic `Add()` during `Do()`

Pass a "workflow handle" into `Do()` via context:

```go
handle := flow.WorkflowHandle(ctx)
handle.Add(flow.Step(newStep))
```

More flexible, but context-threading is implicit and the concurrency story is tricky
(adding steps while the scheduler tick is running).

### Approach C — `Builder` result from `Do()`

Change `Steper` to `Do(context.Context) (Builder, error)` — spawned steps are the return
value. Clean, but **breaks the existing interface** — too invasive.

### Recommendation

Start with **Approach A**. It is the least invasive, requires no interface changes, and
covers the primary use case. Approach B can be layered on top later via a context value.

## Capabilities

### New Capabilities (Approach A)

- `Spawner` interface: if a step implements `Spawn() []Builder`, the workflow calls it after
  a successful `Do()` and schedules the returned steps as downstream of the spawning step.
- Spawned steps participate in all normal workflow features: retry, timeout, condition, event sink.
- `workflow.Do()` does not return until all spawned steps (and their descendants) are terminated.

### Open Questions

- What if `Spawn()` returns a step that was already in the workflow? Should it merge configs
  or panic? Probably merge (same semantics as `workflow.Add()`).
- Should spawned steps be visible in `workflow.Steps()` and the event sink before they run?
  Yes — once added to the live DAG they should behave like any other step.
- Should `Spawn()` be called on retry too, or only on final success?
  Only on final success — otherwise retries produce duplicate children.
- Error handling: if `Spawn()` itself returns an error... it doesn't in this design (returns
  `[]Builder`). Errors surface through the spawned steps' own `Do()` calls.

## Impact

- New `Spawner` interface in `step.go` (or `workflow.go`).
- `workflow.go` `tick()` or post-`runStep` logic — check if step implements `Spawner`,
  call `Spawn()`, merge result into `w.steps`.
- New spec: `openspec/specs/dynamic-fanout/spec.md`.
- No breaking changes.
