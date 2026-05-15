## Context

go-workflow exposes structured observability through two interceptor interfaces declared in `interceptor.go`:

- `StepInterceptor.InterceptStep(ctx, step, next)` — wraps the **full lifetime** of a step (across all retries), invoked exactly once per step.
- `AttemptInterceptor.InterceptAttempt(ctx, step, attempt, next)` — wraps a **single attempt**, invoked once per attempt with a 0-based index.

The Workflow stores them in two slice fields and chains the lowest-index entry on the outside. Steps that resolve to `Skipped` or `Canceled` via their `Condition` are settled inline in `tick()` and never enter either chain (see capability `step-interceptor`).

That extension point is exactly the right shape for OpenTelemetry tracing: a **step span** maps to the outer chain, a **per-attempt span** maps to the inner chain, and the existing `Steper` identity gives a stable key for span naming and attributes via `flow.String(step)`.

The repository today is a single Go module (`github.com/Azure/go-workflow`) with no contrib subdirectory and no OpenTelemetry dependency. This change introduces the first contrib submodule, sets the precedent for all future contrib packages (e.g. logging, metrics, slog), and converts the repo into a multi-module workspace.

Stakeholders: library users who need traces today (presently hand-rolling interceptors), library maintainers (must keep core dependency-light and avoid OTel API churn bleeding into core users).

## Goals / Non-Goals

**Goals:**

- Provide a one-call API to wire OpenTelemetry traces into any `*flow.Workflow` via the existing interceptor extension points — no `BeforeStep` / `AfterStep` hooks, no fork of the core types.
- Ship as an **independent Go module** so the OpenTelemetry dependency does not enter the core module's transitive graph.
- Ship sane defaults (span names from `flow.String(step)`, retry-aware naming, automatic error recording) while letting users override every default through functional options.
- Keep the two interceptor factories **independently usable**: a user may register only the step layer, only the attempt layer, or both, and the result is always coherent.
- Demonstrate the pattern with a runnable godoc `Example` so other contrib modules (e.g. metrics) have a template.

**Non-Goals:**

- OTel **metrics** and **logs** signals. Out of scope for v0.1; addressed in a follow-up change if needed.
- A **workflow-level** span. The interceptor API is step-scoped; a workflow span is the caller's responsibility (`ctx, span := tracer.Start(ctx, "my-workflow"); defer span.End(); w.Do(ctx)`). Documented in package godoc, not implemented.
- Changing core behavior. No edits to any file under the root module. The Skipped/Canceled bypass is documented as inherited behavior, not redesigned.
- Auto-instrumentation discovery (e.g. `otel.GetTracerProvider()` magic in tests). Tests inject a `TracerProvider` explicitly via `WithTracerProvider`.
- Propagation across process boundaries. Step `ctx` carries whatever the caller put in; we do not inject/extract carriers.

## Decisions

### D1. Independent submodule, not a subpackage

`contrib/otel/` gets its own `go.mod` declaring `github.com/Azure/go-workflow/contrib/otel` at module path. During development the contrib `go.mod` carries:

```
require github.com/Azure/go-workflow v0.0.0
replace github.com/Azure/go-workflow => ../..
```

so `go test ./...` in the contrib dir builds against the in-tree core. Released tags (`contrib/otel/v0.1.0`) drop or pin the replace.

**Rationale:** OpenTelemetry's `go.opentelemetry.io/otel` pulls in a non-trivial dep tree. Core users who don't want traces should not pay for them in their `go.sum`. This is the pattern used by `gin-contrib`, `otelhttp`, etc.

**Alternative considered:** Subpackage under core (`github.com/Azure/go-workflow/otel`). Rejected — would make OTel a hard transitive dep of every core user.

### D2. Two factories, no glue helper

Public API is exactly:

```go
func NewStepInterceptor(opts ...Option) flow.StepInterceptor
func NewAttemptInterceptor(opts ...Option) flow.AttemptInterceptor
```

Users wire them with the existing `flow.WithStepInterceptor` / `flow.WithAttemptInterceptor` workflow options.

**Rationale:** Per user decision in brainstorming. Mirrors the underlying interceptor model 1:1, lets users register only one layer if they want, and avoids inventing a new "OTel option" abstraction. Cost: caller writes two lines instead of one.

**Alternative considered:** A single `otel.Tracing(opts...)` returning a `flow.WorkflowOption` that registers both. Rejected for v0.1 — hides the orthogonality, harder to register only the attempt layer (a common pattern for low-overhead deployments).

### D3. Functional options, shared `config`

Both factories take the same `Option` type and the same internal `config` struct. Options that only make sense for one layer (e.g. `WithAttemptSpanNamer`) are still accepted by the other; they are simply ignored.

**Rationale:** One option type means users don't have to remember two namespaces (`StepOption` vs `AttemptOption`). The shared config is internal — package surface stays small.

**Alternative considered:** Separate `StepOption` / `AttemptOption`. Rejected — doubles the API surface for almost no safety benefit; the only "wrong" combo is silently ignored, never an error.

### D4. Span naming and attribute conventions

| Concern | Default |
|---|---|
| Tracer name | `"github.com/Azure/go-workflow/contrib/otel"` (overridable via `WithTracerName`) |
| Step span name | `flow.String(step)` |
| Attempt span name | `fmt.Sprintf("%s (attempt %d)", flow.String(step), attempt)` |
| Step span attrs | `workflow.step.name = flow.String(step)`; `workflow.step.status` = `"success"` \| `"error"` (set on End from the `next` error) |
| Attempt span attrs | `workflow.step.name`; `workflow.step.attempt = attempt` |
| Error path | `next` returns non-nil → `span.RecordError(err)` + `span.SetStatus(codes.Error, err.Error())`. `context.Canceled` is recorded the same way as any other error. |

Custom namers / attribute providers override only what they replace; defaults still apply to non-overridden fields. The `workflow.step.*` namespace is chosen to match the `workflow-options` and `step-configuration` capabilities' terminology.

**Rationale:** Aligned with user decision. `context.Canceled = Error` keeps the rule one-line and avoids the "is this user-initiated cancel or upstream timeout?" classification problem we have no signal to answer.

### D5. Tracer acquisition

`config` resolves a `trace.Tracer` exactly once at factory-call time:

```go
tp := cfg.tracerProvider
if tp == nil { tp = otel.GetTracerProvider() }
tracer := tp.Tracer(cfg.tracerName)
```

This means swapping the global provider after `NewStepInterceptor` returns will not be observed. Documented.

**Rationale:** Avoids per-step `otel.GetTracerProvider()` lookups (each call is a `sync.Map` access). Matches `otelhttp.NewHandler` behavior.

### D6. Test strategy

Tests live in `contrib/otel/*_test.go` and use:

- `sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanRecorder))` to produce real spans synchronously.
- `tracetest.NewSpanRecorder` to capture them.
- `flow.NoOpStep` / minimal handcrafted `Steper` impls for the workflow.
- An exported `(*Workflow).Do(ctx)` to drive end-to-end runs.

The example (`example_test.go`) uses `stdouttrace` to demonstrate a real exporter — it is a build-checked godoc Example, not asserted on output.

### D7. Version & release

- Initial tag `contrib/otel/v0.1.0`. Pre-1.0 because OTel semconv naming for "workflow" is not standardized — we may rename `workflow.step.*` attributes once a convention exists upstream.
- CI must add a job that runs `go test ./...` inside `contrib/otel/`. Tracked as a follow-up infra task referenced from `tasks.md`.

## Risks / Trade-offs

- **OTel API breakage** → contrib module pinned to a single major; core unaffected. Mitigation: only depend on stable `go.opentelemetry.io/otel/trace` interfaces (Tracer, Span, SpanStartOption); no SDK types in the public API.
- **Multi-module repo cognitive overhead** → developers must `cd contrib/otel && go test ./...` separately from root. Mitigation: documented in `contrib/otel/README.md` and reinforced by CI splitting jobs by module.
- **`workflow.step.*` attribute names may clash with future OTel semconv** → could force a v1 rename. Accepted: pre-1.0 lets us rename without breaking SemVer promises.
- **`context.Canceled` always being `Error`** → users running graceful shutdowns will see error-status spans. Mitigation: documented loud and clear; we accept this for v0.1 simplicity. Users who need different semantics can wrap the interceptor (composability is preserved).
- **Replace directive in `contrib/otel/go.mod`** → breaks `go install github.com/Azure/go-workflow/contrib/otel@latest` if not removed before tagging a release. Mitigation: release checklist item + mention in `tasks.md`.
- **Single shared `config` for both layers** → user passes `WithAttemptSpanNamer` to `NewStepInterceptor`; it silently no-ops. Mitigation: godoc on each option says which layer it affects.

## Migration Plan

This is a new submodule with no prior version — no migration of existing users. For repository operators:

1. Land the change on the development branch.
2. CI gains a `go-test-contrib-otel` job (`cd contrib/otel && go test ./...`).
3. Tag `contrib/otel/v0.1.0` after first release. Before tagging, remove the `replace` directive (or pin it to the matching core version) so `go get github.com/Azure/go-workflow/contrib/otel@v0.1.0` resolves cleanly.

Rollback: revert the change branch. No core files are modified, so rollback is purely deletion of `contrib/otel/`.

## Open Questions

None blocking. Future follow-ups (out of scope here):

- Should `contrib/otel` provide a metrics interceptor in v0.2?
- Should we adopt OTel semconv attribute names if/when a "task/workflow" namespace is standardized?
