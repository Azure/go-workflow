package otel_test

import (
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
