package flowotel_test

import (
	"context"
	"errors"
	"testing"

	flow "github.com/Azure/go-workflow"
	"github.com/Azure/go-workflow/contrib/otel"

	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// exceptionEventName is the OTel SDK convention for span events emitted by
// RecordError.
const exceptionEventName = "exception"

// retryStep counts attempts and only succeeds on the Nth try.
type retryStep struct {
	Name         string
	NeedAttempts int
	Attempts     int
}

func (s *retryStep) String() string { return s.Name }
func (s *retryStep) Do(_ context.Context) error {
	s.Attempts++
	if s.Attempts < s.NeedAttempts {
		return errors.New("transient")
	}
	return nil
}

// alwaysFail is a Step whose Do always returns the configured error.
type alwaysFail struct {
	Name string
	Err  error
}

func (s *alwaysFail) String() string             { return s.Name }
func (s *alwaysFail) Do(_ context.Context) error { return s.Err }

// findAttr looks up the named attribute key in the slice. Returns the
// attribute and whether it was found.
func findAttr(attrs []attribute.KeyValue, key string) (attribute.KeyValue, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a, true
		}
	}
	return attribute.KeyValue{}, false
}

func assertAttr(t *testing.T, attrs []attribute.KeyValue, key, want string) {
	t.Helper()
	a, ok := findAttr(attrs, key)
	if !assert.True(t, ok, "attribute %q not found", key) {
		return
	}
	assert.Equal(t, want, a.Value.AsString(), "attribute %q value mismatch", key)
}

// noBackoff returns a RetryOption mutator that disables real backoff sleeps
// so retry tests run instantly.
func noBackoff(attempts uint64) func(*flow.RetryOption) {
	return func(ro *flow.RetryOption) {
		ro.Attempts = attempts
		ro.Backoff = &backoff.ZeroBackOff{}
	}
}

func TestStepInterceptor_SuccessOneSpan(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := flow.NoOp("MyStep")
	w := newTestWorkflow(flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)), nil)
	w.Add(flow.Step(step))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "MyStep", s.Name())
	assertAttr(t, s.Attributes(), "workflow.step.name", "MyStep")
	assertAttr(t, s.Attributes(), "workflow.step.status", "success")
	assert.Equal(t, codes.Unset, s.Status().Code, "no SetStatus on success")
}

func TestStepInterceptor_RetriesStillOneSpan(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := &retryStep{Name: "Flaky", NeedAttempts: 3}
	w := newTestWorkflow(flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)), nil)
	w.Add(flow.Step(step).Retry(noBackoff(5)))
	require.NoError(t, w.Do(context.Background()))

	assert.Equal(t, 3, step.Attempts, "step should have been attempted 3 times")
	spans := rec.Ended()
	require.Len(t, spans, 1, "step interceptor must emit exactly one span across retries")
	s := spans[0]
	assertAttr(t, s.Attributes(), "workflow.step.status", "success")
}

func TestStepInterceptor_FinalErrorRecorded(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	boom := errors.New("boom")
	step := &alwaysFail{Name: "Fail", Err: boom}
	w := newTestWorkflow(flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)), nil)
	w.Add(flow.Step(step).Retry(noBackoff(2)))
	err := w.Do(context.Background())
	require.Error(t, err)

	spans := rec.Ended()
	require.Len(t, spans, 1)
	s := spans[0]
	assertAttr(t, s.Attributes(), "workflow.step.status", "error")
	assert.Equal(t, codes.Error, s.Status().Code)

	events := s.Events()
	var sawException bool
	for _, ev := range events {
		if ev.Name == exceptionEventName {
			sawException = true
			break
		}
	}
	assert.True(t, sawException, "expected RecordError to add an 'exception' event; got %+v", events)
}

func TestStepInterceptor_ContextCanceled(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := &alwaysFail{Name: "Cancel", Err: context.Canceled}
	w := newTestWorkflow(flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)), nil)
	w.Add(flow.Step(step))
	_ = w.Do(context.Background())

	spans := rec.Ended()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, codes.Error, s.Status().Code)
	var sawException bool
	for _, ev := range s.Events() {
		if ev.Name == exceptionEventName {
			sawException = true
			break
		}
	}
	assert.True(t, sawException, "context.Canceled should record an exception event")
}

func TestStepInterceptor_SkippedStepNoSpan(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	skipMe := flow.NoOp("Skipped")
	w := newTestWorkflow(flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)), nil)
	w.Add(flow.Step(skipMe).When(func(context.Context, map[flow.Steper]flow.StepResult) flow.StepStatus {
		return flow.Skipped
	}))
	require.NoError(t, w.Do(context.Background()))

	assert.Empty(t, rec.Ended(), "Skipped steps must bypass the interceptor chain")
	// neither Started() nor Ended() should fire for a Skipped step.
	assert.Empty(t, rec.Started())
}

func TestStepInterceptor_CustomNamer(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := flow.NoOp("Original")
	namer := func(s flow.Steper) string { return "custom:" + flow.String(s) }
	w := newTestWorkflow(flowotel.NewStepInterceptor(
		flowotel.WithTracerProvider(tp),
		flowotel.WithStepSpanNamer(namer),
	), nil)
	w.Add(flow.Step(step))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "custom:Original", s.Name(), "custom span namer should win")
	// workflow.step.name attribute still uses flow.String(step) per spec.
	assertAttr(t, s.Attributes(), "workflow.step.name", "Original")
}

func TestStepInterceptor_CustomAttributesAppend(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := flow.NoOp("Hello")
	extras := func(flow.Steper) []attribute.KeyValue {
		return []attribute.KeyValue{
			attribute.String("env", "test"),
			attribute.Int("answer", 42),
		}
	}
	w := newTestWorkflow(flowotel.NewStepInterceptor(
		flowotel.WithTracerProvider(tp),
		flowotel.WithStepAttributes(extras),
	), nil)
	w.Add(flow.Step(step))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 1)
	attrs := spans[0].Attributes()
	assertAttr(t, attrs, "workflow.step.name", "Hello")     // default still present
	assertAttr(t, attrs, "workflow.step.status", "success") // default still present
	assertAttr(t, attrs, "env", "test")                     // user-supplied
	a, ok := findAttr(attrs, "answer")
	require.True(t, ok, "custom int attribute missing")
	assert.Equal(t, int64(42), a.Value.AsInt64())
}

func TestStepInterceptor_UserAttributeCannotOverrideCanonicalName(t *testing.T) {
	t.Parallel()
	tp, rec := newRecorderTracerProvider()
	step := flow.NoOp("Real")
	hijack := func(flow.Steper) []attribute.KeyValue {
		return []attribute.KeyValue{attribute.String("workflow.step.name", "HACKED")}
	}
	w := newTestWorkflow(flowotel.NewStepInterceptor(
		flowotel.WithTracerProvider(tp),
		flowotel.WithStepAttributes(hijack),
	), nil)
	w.Add(flow.Step(step))
	require.NoError(t, w.Do(context.Background()))

	spans := rec.Ended()
	require.Len(t, spans, 1)
	// Canonical attribute must win over user-supplied override.
	assertAttr(t, spans[0].Attributes(), "workflow.step.name", flow.String(step))
}
