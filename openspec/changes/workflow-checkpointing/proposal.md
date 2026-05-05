## Why

go-workflow runs entirely in-process. If the process crashes mid-execution, all workflow
progress is lost and the entire workflow must restart from the beginning. For long-running
workflows this is costly — early steps that already succeeded must redo their work.

Temporal solves this with event sourcing: every activity completion is durably recorded,
and replay reconstructs the workflow state after a crash. go-workflow cannot replicate that
architecture (it has no server, no event log), but it can offer a lightweight equivalent:
**checkpoint-based resume**.

The idea: serialize the workflow's execution state (step statuses + step business data) to
JSON after each step terminates. On restart, reconstruct the workflow from code as usual,
then call `LoadCheckpoint` to restore state — steps that already succeeded are skipped
automatically.

## What Cannot Be Persisted

- **Closures and callbacks** (`Input`, `Output`, `BeforeStep`, `AfterStep`) are Go functions
  and cannot be serialized. They are re-registered from code on every run.
- **DAG topology** (`DependsOn` graph) is always reconstructed from code — never persisted.

What IS persisted: each step's terminal **status** and its **business data** (struct fields),
via standard `encoding/json`.

## Design

### MarshalJSON / LoadCheckpoint — not symmetric UnmarshalJSON

The natural first instinct is `json.Marshal(w)` + `json.Unmarshal(data, w)`. However, pure
`UnmarshalJSON` cannot work on an empty `Workflow` because Go has no type registry — the
framework cannot reconstruct `*FetchData` from the string `"FetchData"` alone.

The correct pattern is **asymmetric**:

```go
// 1. Build workflow from code as normal (step instances exist in memory)
w := &flow.Workflow{}
fetchUser    := &FetchData{URL: "/user"}
fetchProduct := &FetchData{URL: "/product"}
w.Add(
    flow.Step(flow.Named("fetch-user",    fetchUser)),
    flow.Step(flow.Named("fetch-product", fetchProduct)),
    flow.Step(processStep).DependsOn(fetchUser, fetchProduct),
)

// 2. Load checkpoint — step instances already exist, just restore their state
data, _ := store.Get("job-2026-05-04")
if data != nil {
    if err := w.LoadCheckpoint(data); err != nil { /* handle */ }
}

// 3. Run — steps whose status was restored as Succeeded are skipped automatically
w.Do(ctx)

// 4. Save checkpoint after each step (via AfterStep or EventSink)
data, _ = json.Marshal(w)
store.Set("job-2026-05-04", data)
```

`w.MarshalJSON()` is valid and useful. `w.UnmarshalJSON()` is intentionally not provided
(or is a no-op that returns an error explaining the correct pattern).

### Step identity — the key problem

`w.steps` is keyed by `Steper` pointer, which is unique within a process but meaningless
across restarts. Serialization requires a **stable string key** per step.

The checkpoint key must be determined at `Add()` time and frozen — it must not change
after `Do()` executes and mutates step fields. This rules out deriving the key from
`flow.String()` or any method that reads result fields set during execution.

The framework resolves a step's checkpoint key at `Add()` time using this priority order:

1. **`flow.Name(step, "my-name")` wrapper** — explicit static string declared at DAG build
   time. This is the canonical approach. `flow.Name` already exists in the codebase.
2. **Type name alone** — valid only if exactly one instance of that type exists in the
   workflow (e.g. a single `*ProcessData` → key is `"ProcessData"`).
3. **Multiple instances with no static name** — `MarshalJSON` returns an error, forcing
   the user to use `flow.Name()` explicitly.

**`flow.NameFunc` / `flow.NameStringer` are explicitly excluded from checkpoint key
resolution.** Their names are evaluated at runtime and may change between the time the
workflow is built and the time `MarshalJSON` is called (e.g. a step whose `String()`
depends on a field written by `Do()`). Using them as checkpoint keys would produce
unstable identifiers that break resume. If a step is wrapped with `NameFunc` or
`NameStringer`, `MarshalJSON` returns an error directing the user to use `flow.Name()`
instead.

```go
// Single instance — naming optional, type name used automatically
w.Add(flow.Step(processStep))  // key: "ProcessData"

// Multiple instances of same type — static name required
w.Add(
    flow.Name(&FetchData{URL: "/user"},    "fetch-user"),
    flow.Name(&FetchData{URL: "/product"}, "fetch-product"),
)
```

### JSON structure

`MarshalJSON` only serializes **terminated** steps (Succeeded, Failed, Skipped, Canceled).
Running and Pending steps are omitted entirely.

This has two important consequences:

1. **No pause needed for live checkpoint saving.** The only risk of a data race would be
   serializing a step whose struct fields are being mutated concurrently inside `Do()`.
   Since terminated steps have already returned from `Do()`, their data is stable.
   Running steps are skipped, so there is nothing to race on. `json.Marshal(w)` is safe
   to call at any point during workflow execution without pausing or locking.

2. **Running steps always re-run on resume.** This is correct behavior — a step that was
   mid-execution when the process crashed has unknown partial state and must restart.

**Flat workflow:**
```json
{
  "fetch-user": {
    "status": "Succeeded",
    "data": { "URL": "/user", "Result": "{ ... }" }
  },
  "fetch-product": {
    "status": "Failed",
    "data": { "URL": "/product", "Result": "" },
    "error": "connection refused"
  }
}
```

**Nested workflow (Workflow-as-a-Step):**

Sub-workflows use a nested JSON structure that mirrors the actual DAG hierarchy.
The outer entry for the sub-workflow step carries its own status, and a `"steps"` key
holds the inner workflow's terminated steps recursively.

```json
{
  "fetch-user": {
    "status": "Succeeded",
    "data": { "URL": "/user", "Result": "{ ... }" }
  },
  "InnerWorkflow": {
    "status": "Running",
    "steps": {
      "SubStepA": { "status": "Succeeded", "data": { ... } },
      "SubStepB": { "status": "Failed",    "data": { ... }, "error": "..." }
    }
  }
}
```

`LoadCheckpoint` recurses into `"steps"` when restoring a sub-workflow step, matching
inner step instances by name within the inner workflow's own `steps` map. The outer
workflow's key for the sub-workflow step (`"InnerWorkflow"`) is resolved by the same
Named/Name()/type-name priority as any other step.

Step `data` is serialized via the step's own `json.Marshal`. Steps that do not implement
`json.Marshaler` are serialized with `encoding/json` reflection — this works automatically
for plain data structs. Fields tagged `json:"-"` are excluded as usual.

### Resume semantics

| Persisted status | Behavior on resume |
|------------------|--------------------|
| `Succeeded`      | Skip — treat as already done, downstream can proceed |
| `Skipped`        | Skip — preserve original skip decision |
| `Failed`         | Re-run — the step failed last time, try again |
| `Canceled`       | Re-run — the cancellation may have been transient |
| `Running`        | Re-run — process crashed mid-execution |

### Saving the checkpoint

The framework does not dictate where or when checkpoints are saved — that is the caller's
responsibility. Since `MarshalJSON` only serializes terminated steps and is safe to call
concurrently with a running workflow, **no pause is needed**.

The recommended pattern is to save after each step terminates, via a global `AfterStep`
callback or an `EventSink` consumer (once that feature lands). This gives the finest
checkpoint granularity — one step at a time — without any coordination overhead:

```go
// Using DefaultOption to attach a global AfterStep to all steps
w.DefaultOption = &flow.StepOption{
    // AfterStep is called after every step terminates (after Do() returns)
}
// Or via EventSink on terminal events — the step's Do() has already returned,
// so json.Marshal(w) is safe to call immediately.
```

The caller chooses the storage backend. The framework is storage-agnostic: it only provides
`MarshalJSON` and `LoadCheckpoint`; the caller writes the bytes wherever makes sense
(local file, Redis, SQL, object storage, etc.).

There is no `WorkflowID` field on `Workflow`. The checkpoint key is entirely the caller's
concern — it can be a job date, a request ID, a UUID, or any stable identifier for this
particular execution.

### Idempotency requirement

Checkpointing does not make steps idempotent automatically. **Steps with side effects must
be idempotent themselves** — or check whether the side effect already occurred before
performing it. This is the same requirement Temporal places on Activities.

For steps whose output is large or cannot be reconstructed from struct fields alone,
the recommended pattern is to persist outputs to external storage inside `Do()` and reload
them in an `Input` callback on resume:

```go
Step(fetchUser).Input(func(ctx context.Context, s *FetchData) error {
    if s.Result != "" {
        return nil  // already loaded from checkpoint, skip
    }
    result, err := externalStore.Get(s.URL)
    s.Result = result
    return err
})
```

## Open Questions

- Should `LoadCheckpoint` restore `Failed` step data (struct fields) so that a retried step
  can inspect what was attempted before? Probably yes — useful for partial-progress patterns.

- Should `MarshalJSON` include only terminated steps, or all steps including `Pending`?
  Only terminated is smaller and sufficient; `Pending` steps have no meaningful data yet.

- Should there be a `w.ClearCheckpoint()` or similar that resets all persisted statuses
  without rebuilding the whole workflow? This would allow forced full reruns without code
  changes.

- For sub-workflows (Workflow-as-a-Step): should the outer workflow's `MarshalJSON`
  recursively include inner workflow step states? This requires the inner workflow to also
  participate in the checkpoint key namespace, which may require namespacing
  (e.g. `"InnerWorkflow.SubStepA"`).

## Relationship to Other Changes

- **`heartbeat-and-liveness`**: heartbeat payloads serve intra-run progress recovery
  (across retries within one process run). Checkpointing serves inter-run recovery
  (across process restarts). The two are orthogonal and composable.
- **`structured-event-sink`**: checkpoint saving can be implemented as an EventSink
  consumer for a clean integration point, once that feature lands.

## Impact

- `workflow.go` — implement `MarshalJSON` and `LoadCheckpoint([]byte) error`.
- `step.go` / `wrap.go` — add `flow.Named(name string, step S) *NamedStep[S]` wrapper
  (analogous to `flow.Mock`); add step key resolution logic.
- No new external dependencies in the root module.
- `contrib/checkpoint/` submodule (optional): ready-made save/load helpers for common
  stores (file, Redis, SQL) to reduce boilerplate.
- New spec: `openspec/specs/checkpointing/spec.md`.
- No breaking changes.
