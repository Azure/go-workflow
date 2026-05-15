# contrib/otel

OpenTelemetry tracing integration for [go-workflow](../..) — implemented as
two interceptor factories that plug into the existing `StepInterceptor` and
`AttemptInterceptor` extension points.

```go
import (
    flow "github.com/Azure/go-workflow"
    "github.com/Azure/go-workflow/contrib/otel"
)

w := &flow.Workflow{
    Option: flow.WorkflowOption{
        StepInterceptors: []flow.StepInterceptor{
            flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)),
        },
        AttemptInterceptors: []flow.AttemptInterceptor{
            flowotel.NewAttemptInterceptor(flowotel.WithTracerProvider(tp)),
        },
    },
}
```

See the package godoc and the runnable `Example` for the full wiring.

## What you get by default

- One **step span** per Step lifetime (covering all retries) — name
  `flow.String(step)`, attributes `workflow.step.name`, `workflow.step.status`
  (`"success"` or `"error"`).
- One **attempt span** per individual attempt — name
  `"<step> (attempt N)"`, attributes `workflow.step.name`,
  `workflow.step.attempt` (int64).
- Errors recorded with `RecordError` + `SetStatus(codes.Error)`; this
  includes `context.Canceled`.
- Steps that are `Skipped` or `Canceled` by their `Condition` produce no
  spans (they bypass the interceptor chain in core).

Every default can be overridden via the `With*` options. See the godoc.

## Dependency policy

`contrib/otel` is an **independent Go module** (`github.com/Azure/go-workflow/contrib/otel`)
so the OpenTelemetry dependency does not enter the core module's transitive
graph. Runtime requires are limited to the OpenTelemetry **API**
(`go.opentelemetry.io/otel`, `…/otel/trace`); the SDK and exporters are
test-only dependencies.

## Working on the module

Because the contrib module is separate, run its tests from inside the module:

    cd contrib/otel && go test ./...

The module's `go.mod` carries `replace github.com/Azure/go-workflow => ../..`
so it builds against in-tree core during development.

## Open follow-ups

- **CI**: add a job that runs `go test ./...` (and `-race`) inside
  `contrib/otel/` in addition to the root-module job.
- **Release**: before tagging `contrib/otel/v0.1.0`, drop or pin the
  `replace github.com/Azure/go-workflow => ../..` directive in
  `contrib/otel/go.mod` so `go get
  github.com/Azure/go-workflow/contrib/otel@v0.1.0` resolves cleanly.
