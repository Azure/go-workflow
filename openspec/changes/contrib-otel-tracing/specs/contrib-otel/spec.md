## ADDED Requirements

### Requirement: Independent submodule

The `contrib/otel` package SHALL be delivered as an independent Go module at module path `github.com/Azure/go-workflow/contrib/otel`, located at the repository directory `contrib/otel/`. The module SHALL declare Go 1.23 (matching the core module) and SHALL NOT cause the core module `github.com/Azure/go-workflow` to acquire any new direct or transitive dependency on OpenTelemetry packages.

#### Scenario: Core go.mod is unchanged
- **GIVEN** the change has been applied
- **WHEN** the root `go.mod` of `github.com/Azure/go-workflow` is inspected
- **THEN** it contains no `require`, `replace`, or `exclude` line referencing any `go.opentelemetry.io/...` module
- **AND** `go mod tidy` at the repository root produces no diff caused by this change

#### Scenario: Contrib module builds standalone
- **GIVEN** a checkout of the repository
- **WHEN** the developer runs `go build ./...` and `go test ./...` from `contrib/otel/`
- **THEN** the commands succeed using only the contrib module's `go.mod`
- **AND** the contrib `go.mod` declares the module path `github.com/Azure/go-workflow/contrib/otel`
- **AND** the contrib `go.mod` carries `go 1.23`

#### Scenario: Contrib module resolves core via replace during development
- **GIVEN** the repository is checked out at the change branch
- **WHEN** `contrib/otel/go.mod` is inspected
- **THEN** it contains a `replace github.com/Azure/go-workflow => ../..` directive (or an equivalent local replace) so the contrib module builds against in-tree core sources

---

### Requirement: Dependency policy

The `contrib/otel` module's runtime (non-test) dependencies SHALL be limited to the OpenTelemetry **API** packages — `go.opentelemetry.io/otel` and `go.opentelemetry.io/otel/trace` (and their transitive requirements) — and `github.com/Azure/go-workflow`. The OpenTelemetry **SDK**, exporter packages, and `tracetest` SHALL appear only as test dependencies.

#### Scenario: SDK is test-only
- **GIVEN** the contrib module
- **WHEN** `go list -deps -test=false ./...` is run inside `contrib/otel/`
- **THEN** the output contains no package under `go.opentelemetry.io/otel/sdk/...`
- **AND** the output contains no `stdouttrace` or other exporter packages

#### Scenario: API packages available at runtime
- **WHEN** a consumer imports `github.com/Azure/go-workflow/contrib/otel`
- **THEN** the import graph includes `go.opentelemetry.io/otel/trace` and `go.opentelemetry.io/otel/attribute`

---

### Requirement: Step interceptor factory

`contrib/otel` SHALL export a function `NewStepInterceptor(opts ...Option) flow.StepInterceptor` that returns a value satisfying the `flow.StepInterceptor` interface. Each invocation of the returned `InterceptStep` method SHALL start exactly one span at the beginning of `next` and end it after `next` returns, regardless of how many retry attempts occur inside `next`.

#### Scenario: One span per step on success
- **GIVEN** a Workflow with a single step `s` registered with `flow.WithStepInterceptor(flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)))` where `tp` records spans
- **WHEN** the Workflow runs and `s` succeeds on the first attempt
- **THEN** exactly one span is recorded with the step span name
- **AND** the span has status `OK` (or unset, the OTel default for success)

#### Scenario: One span per step despite retries
- **GIVEN** a Workflow whose step `s` is configured to retry up to 3 times and finally succeeds on attempt 2
- **AND** only `NewStepInterceptor` is registered (no AttemptInterceptor)
- **WHEN** the Workflow runs
- **THEN** exactly one span is recorded for `s` (not three)

#### Scenario: Step span records terminal failure
- **GIVEN** a step `s` whose final attempt returns a non-nil error `err`
- **WHEN** the step interceptor's outer `next` returns
- **THEN** the recorded span calls `RecordError(err)` and `SetStatus(codes.Error, err.Error())`
- **AND** the span attribute `workflow.step.status` equals `"error"`

---

### Requirement: Attempt interceptor factory

`contrib/otel` SHALL export a function `NewAttemptInterceptor(opts ...Option) flow.AttemptInterceptor` that returns a value satisfying the `flow.AttemptInterceptor` interface. Each invocation of `InterceptAttempt` SHALL start one span before calling `next` and end it after `next` returns. Each per-attempt span SHALL carry the attempt index as the OTel attribute `workflow.step.attempt`.

#### Scenario: One attempt span per attempt
- **GIVEN** a Workflow whose step `s` is configured to retry up to 3 times and ultimately succeeds on attempt 2
- **AND** only `NewAttemptInterceptor` is registered (no StepInterceptor)
- **WHEN** the Workflow runs
- **THEN** exactly three spans are recorded (attempt 0, attempt 1, attempt 2)
- **AND** each span carries attribute `workflow.step.attempt` with the matching 0-based index

#### Scenario: Failing attempt records its error
- **GIVEN** an attempt that returns error `err` from `next`
- **WHEN** that attempt's span is ended
- **THEN** the span calls `RecordError(err)` and `SetStatus(codes.Error, err.Error())`

---

### Requirement: Independent registration of the two layers

The two interceptor factories SHALL be usable independently or together. When both are registered on the same Workflow, the attempt span SHALL be a child of the step span (i.e. the attempt's `Span.Parent.SpanID` equals the step span's `SpanID`). When only one is registered, that span SHALL be a child of whatever span (if any) is on the caller-provided context.

#### Scenario: Both layers registered → parent/child relation
- **GIVEN** a Workflow registered with both `NewStepInterceptor` and `NewAttemptInterceptor`
- **WHEN** a single step runs once
- **THEN** the step span and the attempt span share the same `TraceID`
- **AND** the attempt span's `Parent.SpanID` equals the step span's `SpanID`

#### Scenario: Only attempt layer registered → no orphan
- **GIVEN** a Workflow registered with only `NewAttemptInterceptor` and a caller-supplied context that already contains an outer span `outer`
- **WHEN** a step runs
- **THEN** the attempt span's `Parent.SpanID` equals `outer`'s `SpanID`

---

### Requirement: Default span naming

When no custom namer is supplied, the default step span name SHALL equal `flow.String(step)`, and the default attempt span name SHALL equal `fmt.Sprintf("%s (attempt %d)", flow.String(step), attempt)`.

#### Scenario: Default step span name
- **GIVEN** a step `s` for which `flow.String(s) == "MyStep"`
- **WHEN** the step interceptor produces a span without a custom `WithStepSpanNamer`
- **THEN** the span's name is `"MyStep"`

#### Scenario: Default attempt span name carries attempt index
- **GIVEN** a step `s` for which `flow.String(s) == "MyStep"` running its second attempt (`attempt == 1`)
- **WHEN** the attempt interceptor produces a span without a custom `WithAttemptSpanNamer`
- **THEN** the span's name is `"MyStep (attempt 1)"`

---

### Requirement: Default span attributes

The step span SHALL be created with attribute `workflow.step.name = flow.String(step)`, and on `End` it SHALL receive attribute `workflow.step.status` equal to `"success"` when `next` returned nil and `"error"` otherwise. The attempt span SHALL be created with attributes `workflow.step.name = flow.String(step)` and `workflow.step.attempt = attempt` (as an `Int64` attribute).

#### Scenario: Step span carries name and status
- **GIVEN** a step `s` that succeeds
- **WHEN** its span is ended
- **THEN** the span's attribute set contains `workflow.step.name = flow.String(s)`
- **AND** the attribute `workflow.step.status` equals `"success"`

#### Scenario: Attempt span carries name and attempt index
- **GIVEN** an attempt at index 2 of step `s`
- **WHEN** its span is created
- **THEN** the span's attribute set contains `workflow.step.name = flow.String(s)`
- **AND** the attribute `workflow.step.attempt` equals the int64 `2`

---

### Requirement: Functional options

`contrib/otel` SHALL expose a single exported `Option` type and the following functional option constructors. Both factories SHALL accept the same `Option` values. Options that target one layer SHALL be no-ops on the other layer (no error, no panic).

| Option | Effect |
|---|---|
| `WithTracerProvider(tp trace.TracerProvider)` | Sets the provider. If `nil` or unset, defaults to `otel.GetTracerProvider()` resolved at factory-call time. |
| `WithTracerName(name string)` | Sets the tracer instrumentation name. Default `"github.com/Azure/go-workflow/contrib/otel"`. |
| `WithStepSpanNamer(fn func(flow.Steper) string)` | Overrides default step span naming. |
| `WithAttemptSpanNamer(fn func(flow.Steper, uint64) string)` | Overrides default attempt span naming. |
| `WithStepAttributes(fn func(flow.Steper) []attribute.KeyValue)` | Adds extra attributes to the step span at start. Defaults still apply. |
| `WithAttemptAttributes(fn func(flow.Steper, uint64) []attribute.KeyValue)` | Adds extra attributes to the attempt span at start. Defaults still apply. |

#### Scenario: Custom step namer overrides default
- **GIVEN** a `NewStepInterceptor(WithStepSpanNamer(func(flow.Steper) string { return "custom-name" }))`
- **WHEN** any step runs through the interceptor
- **THEN** its step span name is `"custom-name"`

#### Scenario: Custom attempt attributes are appended, not replaced
- **GIVEN** a `NewAttemptInterceptor(WithAttemptAttributes(func(flow.Steper, uint64) []attribute.KeyValue { return []attribute.KeyValue{attribute.String("custom.k", "v")} }))`
- **WHEN** an attempt span is created
- **THEN** it carries the default attributes (`workflow.step.name`, `workflow.step.attempt`) AND the custom attribute `custom.k = "v"`

#### Scenario: Provider defaults to global
- **GIVEN** no `WithTracerProvider` option
- **WHEN** `NewStepInterceptor()` is called
- **THEN** the constructed interceptor obtains its tracer from `otel.GetTracerProvider()` at the moment of the call (not lazily on every interception)

---

### Requirement: Cancellation is recorded as error

When the error returned by `next` satisfies `errors.Is(err, context.Canceled)`, the span produced by either interceptor SHALL be ended with `RecordError(err)` and `SetStatus(codes.Error, err.Error())` — the same as any other non-nil error. No special-case suppression SHALL be applied in v0.1.

#### Scenario: Cancelled step is Error
- **GIVEN** a step that observes context cancellation and returns `context.Canceled`
- **WHEN** the span is ended
- **THEN** the span has status code `codes.Error`
- **AND** the span's events include the recorded `context.Canceled` error

---

### Requirement: Skipped and Canceled-by-Condition steps produce no spans

The contrib package SHALL document, and tests SHALL verify, that a step whose `Condition` resolves to `Skipped` or `Canceled` produces zero spans through either interceptor — by virtue of the core capability `step-interceptor` bypassing the chain entirely for such steps. `contrib/otel` SHALL NOT implement any compensating behavior.

#### Scenario: Skipped step contributes no span
- **GIVEN** a Workflow with a step `s` whose `Condition` returns `Skipped`
- **AND** both `NewStepInterceptor` and `NewAttemptInterceptor` are registered
- **WHEN** the Workflow runs
- **THEN** the span recorder contains zero spans referencing `s`

---

### Requirement: Runnable godoc Example

The `contrib/otel` module SHALL include a runnable godoc Example (`Example*` function in `example_test.go`) that constructs a `TracerProvider` with a stdout exporter, registers both interceptors on a minimal Workflow, and runs the Workflow. The Example SHALL compile and execute under `go test ./...` inside the contrib module without external network access.

#### Scenario: Example builds and runs
- **WHEN** `go test ./... -run Example` is executed inside `contrib/otel/`
- **THEN** the command exits with status 0
- **AND** the Example function appears in `go doc -all github.com/Azure/go-workflow/contrib/otel`
