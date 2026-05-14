package otel_test

import (
	"context"
	"fmt"
	"testing"

	flow "github.com/Azure/go-workflow"
	otelflow "github.com/Azure/go-workflow/contrib/otel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// newAttemptWorkflow builds a Workflow with only the given AttemptInterceptor
// registered (no StepInterceptor) so attempt-layer tests stay isolated.
func newAttemptWorkflow(ic flow.AttemptInterceptor) *flow.Workflow {
	return &flow.Workflow{Option: flow.WorkflowOption{
		AttemptInterceptors: []flow.AttemptInterceptor{ic},
	}}
}

func TestAttemptInterceptor_OneSpanPerAttempt(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := &retryStep{Name: "S", NeedAttempts: 4} // succeeds on the 4th try
	w := newAttemptWorkflow(otelflow.NewAttemptInterceptor(otelflow.WithTracerProvider(tp)))
	w.Add(flow.Step(step).Retry(noBackoff(5)))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 4, "expected one span per attempt (0..3)")
	for i, s := range spans {
		a, ok := findAttr(s.Attributes(), "workflow.step.attempt")
		require.True(t, ok, "span %d missing workflow.step.attempt", i)
		assert.Equal(t, int64(i), a.Value.AsInt64(), "span %d attempt index mismatch", i)
		assertAttr(t, s.Attributes(), "workflow.step.name", "S")
	}
}

func TestAttemptInterceptor_DefaultName(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := flow.NoOp("MyStep")
	w := newAttemptWorkflow(otelflow.NewAttemptInterceptor(otelflow.WithTracerProvider(tp)))
	w.Add(flow.Step(step))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "MyStep (attempt 0)", spans[0].Name())
}

func TestAttemptInterceptor_FailingAttemptRecorded(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := &retryStep{Name: "Flaky", NeedAttempts: 2} // fails once, then succeeds
	w := newAttemptWorkflow(otelflow.NewAttemptInterceptor(otelflow.WithTracerProvider(tp)))
	w.Add(flow.Step(step).Retry(noBackoff(2)))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 2, "expected one span per attempt (failure + success)")

	// First attempt span: failure with RecordError + codes.Error.
	first := spans[0]
	assert.Equal(t, codes.Error, first.Status().Code, "first attempt span should be Error")
	var sawException bool
	for _, ev := range first.Events() {
		if ev.Name == exceptionEventName {
			sawException = true
			break
		}
	}
	assert.True(t, sawException, "first attempt should record an exception event")

	// Second attempt span: success leaves status Unset.
	second := spans[1]
	assert.Equal(t, codes.Unset, second.Status().Code, "successful attempt span should be Unset")
}

func TestAttemptInterceptor_ChildOfCallerSpan(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	tracer := tp.Tracer("test")
	ctx, outer := tracer.Start(context.Background(), "OUTER")
	defer outer.End()

	step := flow.NoOp("S")
	w := newAttemptWorkflow(otelflow.NewAttemptInterceptor(otelflow.WithTracerProvider(tp)))
	w.Add(flow.Step(step))
	require.NoError(t, w.Do(ctx))

	// OUTER is still open (it ends via defer); only the attempt span should be Ended.
	spans := rec.Ended()
	var attempt sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() != "OUTER" {
			attempt = s
			break
		}
	}
	require.NotNil(t, attempt, "expected an attempt span among %d ended spans", len(spans))
	assert.Equal(t, outer.SpanContext().SpanID(), attempt.Parent().SpanID(),
		"attempt span must be a child of the caller-supplied OUTER span")
}

func TestAttemptInterceptor_CustomNamer(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := flow.NoOp("Original")
	namer := func(s flow.Steper, n uint64) string {
		return fmt.Sprintf("X-%s-%d", flow.String(s), n)
	}
	w := newAttemptWorkflow(otelflow.NewAttemptInterceptor(
		otelflow.WithTracerProvider(tp),
		otelflow.WithAttemptSpanNamer(namer),
	))
	w.Add(flow.Step(step))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "X-Original-0", s.Name(), "custom attempt namer should win")
	// Canonical attributes still present despite the custom name.
	assertAttr(t, s.Attributes(), "workflow.step.name", "Original")
	a, ok := findAttr(s.Attributes(), "workflow.step.attempt")
	require.True(t, ok)
	assert.Equal(t, int64(0), a.Value.AsInt64())
}

func TestAttemptInterceptor_CustomAttributes(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := flow.NoOp("Hello")
	extras := func(flow.Steper, uint64) []attribute.KeyValue {
		return []attribute.KeyValue{
			attribute.String("env", "test"),
			attribute.Int("answer", 42),
			// Regression: a malicious user attribute trying to override the
			// canonical workflow.step.attempt key MUST be superseded by the
			// interceptor's own value.
			attribute.Int64("workflow.step.attempt", 999),
		}
	}
	w := newAttemptWorkflow(otelflow.NewAttemptInterceptor(
		otelflow.WithTracerProvider(tp),
		otelflow.WithAttemptAttributes(extras),
	))
	w.Add(flow.Step(step))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 1)
	attrs := spans[0].Attributes()

	// Defaults still present.
	assertAttr(t, attrs, "workflow.step.name", "Hello")
	canonical, ok := findAttr(attrs, "workflow.step.attempt")
	require.True(t, ok)
	assert.Equal(t, int64(0), canonical.Value.AsInt64(),
		"canonical workflow.step.attempt must win over user-supplied override")

	// User attributes appended.
	assertAttr(t, attrs, "env", "test")
	a, ok := findAttr(attrs, "answer")
	require.True(t, ok, "custom int attribute missing")
	assert.Equal(t, int64(42), a.Value.AsInt64())
}
