package flowotel_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
	"github.com/Azure/go-workflow/contrib/otel"

	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Example demonstrates registering the contrib/otel step and attempt
// interceptors on a Workflow, with spans exported to stdout for inspection.
//
// We intentionally omit the `// Output:` directive: TraceIDs, SpanIDs and
// other span fields are non-deterministic, so verifying string output
// would force the example to mock the SDK and obscure the integration
// pattern. The Example still compiles and runs as part of `go test`.
func Example() {
	// 1. Build a TracerProvider with a stdout exporter (any exporter works:
	//    OTLP, Jaeger, Zipkin, etc.).
	exporter, err := stdouttrace.New(stdouttrace.WithoutTimestamps())
	if err != nil {
		panic(err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	// Shutdown errors are intentionally ignored in this example;
	// production code should log or surface them.
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// 2. Register both interceptors on a Workflow.
	w := &flow.Workflow{}
	w.Option.StepInterceptors = []flow.StepInterceptor{
		flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)),
	}
	w.Option.AttemptInterceptors = []flow.AttemptInterceptor{
		flowotel.NewAttemptInterceptor(flowotel.WithTracerProvider(tp)),
	}

	// 3. Build a tiny 2-step pipeline: A → B. flow.Step(b).DependsOn(a)
	//    registers BOTH steps and the dependency in one call.
	a := flow.NoOp("A")
	b := flow.NoOp("B")
	w.Add(flow.Step(b).DependsOn(a))

	// 4. Run.
	if err := w.Do(context.Background()); err != nil {
		fmt.Println("workflow error:", err)
	}
}
