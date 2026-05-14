package otel_test

import (
	flow "github.com/Azure/go-workflow"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newRecorderTracerProvider returns a TracerProvider wired to a synchronous
// in-memory SpanRecorder, suitable for asserting the spans emitted by the
// step / attempt interceptors. SpanRecorder implements SpanProcessor, so no
// extra batching or simple processor is needed.
func newRecorderTracerProvider() (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	return tp, rec
}

// newTestWorkflow builds an empty Workflow registered with the given step
// interceptor and/or attempt interceptor; nil arguments are not registered.
func newTestWorkflow(stepIC flow.StepInterceptor, attemptIC flow.AttemptInterceptor) *flow.Workflow {
	w := &flow.Workflow{}
	if stepIC != nil {
		w.Option.StepInterceptors = []flow.StepInterceptor{stepIC}
	}
	if attemptIC != nil {
		w.Option.AttemptInterceptors = []flow.AttemptInterceptor{attemptIC}
	}
	return w
}
