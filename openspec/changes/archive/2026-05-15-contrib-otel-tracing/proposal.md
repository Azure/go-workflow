## Why

go-workflow already has a clean two-layer interceptor model (`StepInterceptor` + `AttemptInterceptor`) that is the natural extension point for cross-cutting observability — but the repo ships no batteries-included integration with any tracing system. Users who want OpenTelemetry traces today have to hand-roll a pair of interceptors, get span parenting / retry semantics right, and re-derive sensible span names and attributes. That is repetitive boilerplate every project copies.

A first-party `contrib/otel` module turns the interceptor extension point into a one-liner, ships sensible defaults, and demonstrates the recommended way to build other observability integrations against the interceptor API.

## What Changes

- Add `contrib/otel/` as a **new, independent Go module** at module path `github.com/Azure/go-workflow/contrib/otel`. Released and tagged separately from the core module so its OpenTelemetry dependency stays out of core users' transitive graph.
- New public API in package `flowotel`:
  - `NewStepInterceptor(opts ...Option) flow.StepInterceptor`
  - `NewAttemptInterceptor(opts ...Option) flow.AttemptInterceptor`
  - `Option` functional options: `WithTracerProvider`, `WithTracerName`, `WithStepSpanNamer`, `WithAttemptSpanNamer`, `WithStepAttributes`, `WithAttemptAttributes`.
- Default span behavior:
  - Step span name = `flow.String(step)`; attempt span name = `flow.String(step) + " (attempt N)"`.
  - Step span attributes: `workflow.step.name`, `workflow.step.status`. Attempt span attributes: `workflow.step.name`, `workflow.step.attempt`.
  - Errors (including `context.Canceled`) → `RecordError` + `SetStatus(codes.Error, …)`.
  - The two interceptors are independent — when both registered, the attempt span is a child of the step span; when only one is registered, it works standalone against the caller's `ctx`.
- Document the existing core behavior that `Skipped` / `Canceled by Condition` steps bypass the interceptor chain, so they will not produce spans (no change to core).
- Add a runnable godoc `Example` (`contrib/otel/example_test.go`) that wires the SDK + a stdout exporter to a minimal workflow.
- Dependency policy: contrib/otel depends only on the OpenTelemetry **API** packages (`go.opentelemetry.io/otel`, `…/otel/trace`); the SDK and `tracetest` are test-only dependencies.

## Capabilities

### New Capabilities

- `contrib-otel`: OpenTelemetry traces integration for go-workflow, delivered as an independent submodule that exposes two interceptor factories (step-level and attempt-level) wrapping the core `StepInterceptor` / `AttemptInterceptor` extension points.

### Modified Capabilities

<!-- None. Core `step-interceptor` capability is referenced as a dependency but its requirements are unchanged. -->

## Impact

- **New module**: `contrib/otel/` with its own `go.mod`, `go.sum`, source files, tests, and example. Replaces the placeholder change directory `contrib-common-steps/`.
- **No source changes** to the existing core module (`github.com/Azure/go-workflow`). Its `go.mod` and existing files are untouched.
- **New external dependencies** (scoped to the contrib module only):
  - Runtime: `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/trace`.
  - Test-only: `go.opentelemetry.io/otel/sdk`, `go.opentelemetry.io/otel/sdk/trace`, `go.opentelemetry.io/otel/sdk/trace/tracetest`, plus `stdouttrace` for the example.
- **Repository becomes multi-module**. CI must build/test `./contrib/otel` in addition to the root module. The contrib module uses a `replace github.com/Azure/go-workflow => ../..` directive so it builds against in-tree core during development; releases drop or pin the replace.
- **Versioning**: contrib/otel will be tagged independently as `contrib/otel/v0.x.y` per Go submodule conventions. Initial release is `v0.1.0` (pre-1.0 surface).
