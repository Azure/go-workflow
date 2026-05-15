package otel_test

import (
	"context"
	"testing"

	flow "github.com/Azure/go-workflow"
	otelflow "github.com/Azure/go-workflow/contrib/otel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// isAttemptSpan identifies attempt spans by the canonical attribute that
// NewAttemptInterceptor always writes (workflow.step.attempt). Using the
// attribute rather than the default span name keeps the discriminator
// robust against WithAttemptSpanNamer overrides and default-name changes.
func isAttemptSpan(sp sdktrace.ReadOnlySpan) bool {
	for _, kv := range sp.Attributes() {
		if string(kv.Key) == "workflow.step.attempt" {
			return true
		}
	}
	return false
}

func TestBothLayers_AttemptIsChildOfStep(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	s := flow.NoOp("OnlyStep")
	w := newTestWorkflow(
		otelflow.NewStepInterceptor(otelflow.WithTracerProvider(tp)),
		otelflow.NewAttemptInterceptor(otelflow.WithTracerProvider(tp)),
	)
	w.Add(flow.Step(s))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 2)

	var stepSpan, attemptSpan sdktrace.ReadOnlySpan
	for _, sp := range spans {
		if isAttemptSpan(sp) {
			attemptSpan = sp
		} else {
			stepSpan = sp
		}
	}
	require.NotNil(t, stepSpan, "step span must be present")
	require.NotNil(t, attemptSpan, "attempt span must be present")

	assert.Equal(t, stepSpan.SpanContext().TraceID(), attemptSpan.SpanContext().TraceID(),
		"step and attempt spans must share TraceID")
	assert.Equal(t, stepSpan.SpanContext().SpanID(), attemptSpan.Parent().SpanID(),
		"attempt span must be child of step span")
}

func TestBothLayers_RetryAttemptCount(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := &retryStep{Name: "Flaky", NeedAttempts: 3} // succeeds on 3rd attempt
	w := newTestWorkflow(
		otelflow.NewStepInterceptor(otelflow.WithTracerProvider(tp)),
		otelflow.NewAttemptInterceptor(otelflow.WithTracerProvider(tp)),
	)
	w.Add(flow.Step(step).Retry(noBackoff(5)))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	// 3 attempt spans + 1 step span = 4 total
	require.Len(t, spans, 4)

	var stepSpan sdktrace.ReadOnlySpan
	var attemptSpans []sdktrace.ReadOnlySpan
	for _, sp := range spans {
		if isAttemptSpan(sp) {
			attemptSpans = append(attemptSpans, sp)
		} else {
			stepSpan = sp
		}
	}
	require.NotNil(t, stepSpan)
	require.Len(t, attemptSpans, 3)

	for _, a := range attemptSpans {
		assert.Equal(t, stepSpan.SpanContext().TraceID(), a.SpanContext().TraceID(),
			"every attempt span must share the step's TraceID")
		assert.Equal(t, stepSpan.SpanContext().SpanID(), a.Parent().SpanID(),
			"every attempt span must be a child of the step span")
	}
}

func TestProviderResolutionAtFactoryTime(t *testing.T) {
	// CANNOT t.Parallel(): mutates global TracerProvider via otel.SetTracerProvider.
	// All other tests in this package inject TracerProvider explicitly via
	// WithTracerProvider, so flipping the global here does not affect them.
	original := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(original) })

	// Provider A is what's global at factory call time.
	tpA, recA := newRecorderTracerProvider()
	otel.SetTracerProvider(tpA)

	// Construct interceptors WITHOUT WithTracerProvider — they should snapshot
	// the current global (tpA) once.
	stepIC := otelflow.NewStepInterceptor()
	attemptIC := otelflow.NewAttemptInterceptor()

	// Now swap the global to provider B.
	tpB, recB := newRecorderTracerProvider()
	otel.SetTracerProvider(tpB)

	// Run the workflow. Spans MUST land on provider A, not B.
	s := flow.NoOp("ProviderTest")
	w := newTestWorkflow(stepIC, attemptIC)
	w.Add(flow.Step(s))
	require.NoError(t, w.Do(context.Background()))

	assert.Len(t, recA.Ended(), 2, "interceptors should still write to original provider (snapshot at factory time)")
	assert.Len(t, recB.Ended(), 0, "swapped-in provider should NOT receive spans")
}
