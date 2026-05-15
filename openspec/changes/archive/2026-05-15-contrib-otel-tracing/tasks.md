## 1. Module bootstrap

- [x] 1.1 Create directory `contrib/otel/` and run `go mod init github.com/Azure/go-workflow/contrib/otel` inside it.
- [x] 1.2 Set `go 1.23` in the new `contrib/otel/go.mod`; add `replace github.com/Azure/go-workflow => ../..` and a `require github.com/Azure/go-workflow v0.0.0` directive so the module builds against in-tree core.
- [x] 1.3 Add runtime dependencies `go.opentelemetry.io/otel` and `go.opentelemetry.io/otel/trace`; run `go mod tidy` and commit `contrib/otel/go.sum`.
- [x] 1.4 Verify that the root `go.mod` and `go.sum` show no diff after the contrib module is created.

## 2. Public API surface

- [x] 2.1 Create `contrib/otel/options.go` with the unexported `config` struct and the exported `Option` function type.
- [x] 2.2 Implement `WithTracerProvider`, `WithTracerName`, `WithStepSpanNamer`, `WithAttemptSpanNamer`, `WithStepAttributes`, `WithAttemptAttributes`. Document on each which interceptor layer it affects (and that it is a no-op on the other).
- [x] 2.3 Implement an internal `(*config).resolveTracer()` helper that picks `cfg.tracerProvider` (or `otel.GetTracerProvider()` when unset) and returns `tp.Tracer(cfg.tracerName)` with default name `"github.com/Azure/go-workflow/contrib/otel"`.

## 3. Step interceptor

- [x] 3.1 Create `contrib/otel/step.go` exporting `NewStepInterceptor(opts ...Option) flow.StepInterceptor` returning a `flow.StepInterceptorFunc`.
- [x] 3.2 In the closure: resolve span name (default `flow.String(step)`, overridable), build start attributes (`workflow.step.name` plus user-supplied), call `tracer.Start(ctx, name, trace.WithAttributes(...))`, defer `span.End()`.
- [x] 3.3 After `next(ctx)` returns: set `workflow.step.status` to `"success"` or `"error"`, and on non-nil error call `span.RecordError(err)` + `span.SetStatus(codes.Error, err.Error())`. Treat `context.Canceled` like any other error.
- [x] 3.4 Return the error unchanged.

## 4. Attempt interceptor

- [x] 4.1 Create `contrib/otel/attempt.go` exporting `NewAttemptInterceptor(opts ...Option) flow.AttemptInterceptor` returning a `flow.AttemptInterceptorFunc`.
- [x] 4.2 Resolve span name (default `fmt.Sprintf("%s (attempt %d)", flow.String(step), attempt)`, overridable), build start attributes (`workflow.step.name`, `workflow.step.attempt = int64(attempt)`, plus user-supplied), call `tracer.Start(ctx, name, trace.WithAttributes(...))`, defer `span.End()`.
- [x] 4.3 After `next(ctx)` returns, on non-nil error call `RecordError` + `SetStatus(codes.Error, …)`. Return the error unchanged.

## 5. Test scaffolding

- [x] 5.1 Add test-only dependencies in `contrib/otel/`: `go.opentelemetry.io/otel/sdk/trace` and `go.opentelemetry.io/otel/sdk/trace/tracetest` (and the SDK's transitive `go.opentelemetry.io/otel/sdk`). Confirm with `go list -deps -test=false ./...` that they are not pulled into the runtime graph.
- [x] 5.2 Create `contrib/otel/internal_test.go` (or `helpers_test.go`) with a small helper that builds a `(tp *sdktrace.TracerProvider, recorder *tracetest.SpanRecorder)` pair.

## 6. Step interceptor tests (`step_test.go`)

- [x] 6.1 Test `TestStepInterceptor_SuccessOneSpan`: single step, succeeds first try → exactly one span, name = `flow.String(step)`, attribute `workflow.step.status == "success"`, status code OK/Unset.
- [x] 6.2 Test `TestStepInterceptor_RetriesStillOneSpan`: step retries N times then succeeds → still exactly one step span.
- [x] 6.3 Test `TestStepInterceptor_FinalErrorRecorded`: step fails terminally → `workflow.step.status == "error"`, `Status.Code == codes.Error`, span events include the `RecordError` event.
- [x] 6.4 Test `TestStepInterceptor_ContextCanceled`: step returns `context.Canceled` → status = Error, error event recorded.
- [x] 6.5 Test `TestStepInterceptor_SkippedStepNoSpan`: step with Condition resolving to `Skipped` → recorder yields zero spans.
- [x] 6.6 Test `TestStepInterceptor_CustomNamer`: `WithStepSpanNamer` overrides the default.
- [x] 6.7 Test `TestStepInterceptor_CustomAttributesAppend`: `WithStepAttributes` adds extras while defaults remain.

## 7. Attempt interceptor tests (`attempt_test.go`)

- [x] 7.1 Test `TestAttemptInterceptor_OneSpanPerAttempt`: step with N+1 attempts → exactly N+1 spans, attempt indices 0..N.
- [x] 7.2 Test `TestAttemptInterceptor_DefaultName`: name format matches `"%s (attempt %d)"`.
- [x] 7.3 Test `TestAttemptInterceptor_FailingAttemptRecorded`: failed attempt span has Error status + RecordError event.
- [x] 7.4 Test `TestAttemptInterceptor_ChildOfCallerSpan`: when only attempt interceptor is registered, attempt span's `Parent.SpanID` matches an outer caller span.
- [x] 7.5 Test `TestAttemptInterceptor_CustomNamer` and `TestAttemptInterceptor_CustomAttributes`.

## 8. Combined / integration tests (`integration_test.go`)

- [x] 8.1 Test `TestBothLayers_AttemptIsChildOfStep`: register both, run a step, assert attempt span's `Parent.SpanID` equals step span's `SpanID` and `TraceID` matches.
- [x] 8.2 Test `TestBothLayers_RetryAttemptCount`: step retries M times, assert one step span and M+1 attempt spans, all sharing the trace.
- [x] 8.3 Test `TestProviderResolutionAtFactoryTime`: change global `otel.SetTracerProvider` after constructing the interceptor → spans still flow to the original provider.

## 9. Godoc Example (`example_test.go`)

- [x] 9.1 Add test dep `go.opentelemetry.io/otel/exporters/stdout/stdouttrace`.
- [x] 9.2 Write `func Example()` that builds a `TracerProvider` with `stdouttrace.New(stdouttrace.WithoutTimestamps())`, registers both interceptors on a 2-step workflow, and runs it. Use `// Output:` only if deterministic; otherwise document why output is omitted.
- [x] 9.3 Verify `go test ./... -run Example` passes inside `contrib/otel/`.

## 10. Package documentation

- [x] 10.1 Add a package-level doc comment in `contrib/otel/doc.go` covering: usage snippet, default span/attribute conventions, parent/child relation between layers, the `Skipped`/`Canceled-by-Condition` zero-span behavior, and the `context.Canceled = Error` policy.
- [x] 10.2 Add `contrib/otel/README.md` with the usage snippet, dependency policy notice, and a "must `cd contrib/otel && go test ./...`" note for contributors.

## 11. Repository-level docs and follow-ups

- [x] 11.1 Add a top-level mention of the new contrib module in the root `README.md` (one line under a "contrib" heading or a dedicated section).
- [x] 11.2 Open a tracking issue (or add a TODO note in `contrib/otel/README.md`) for: (a) adding a CI job that runs `go test ./...` inside `contrib/otel/`, and (b) the release-time checklist to drop or pin the `replace` directive before tagging `contrib/otel/v0.1.0`.

## 12. Verification before completion

- [x] 12.1 `go test ./...` from repository root passes (core unchanged).
- [x] 12.2 `go test ./...` from `contrib/otel/` passes; race detector also passes (`-race`).
- [x] 12.3 `openspec validate contrib-otel-tracing --strict` reports no errors.
- [x] 12.4 `go list -deps -test=false ./...` inside `contrib/otel/` confirms no SDK / exporter / tracetest packages in the runtime graph.
