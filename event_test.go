package flow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEventTypeConstants(t *testing.T) {
	// Verify all constants exist and are distinct
	types := []EventType{EventScheduled, EventStarted, EventRetrying, EventSucceeded, EventFailed, EventCanceled, EventSkipped}
	seen := map[EventType]bool{}
	for _, et := range types {
		assert.False(t, seen[et], "duplicate EventType: %q", et)
		seen[et] = true
	}
}

func TestStepInterceptorFunc(t *testing.T) {
	called := false
	var ic StepInterceptor = StepInterceptorFunc(func(ctx context.Context, info StepInfo, next func(context.Context) error) error {
		called = true
		return next(ctx)
	})
	_ = ic.InterceptStep(context.Background(), StepInfo{}, func(ctx context.Context) error { return nil })
	assert.True(t, called)
}

func TestAttemptInterceptorFunc(t *testing.T) {
	called := false
	var ic AttemptInterceptor = AttemptInterceptorFunc(func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
		called = true
		return next(ctx)
	})
	_ = ic.InterceptAttempt(context.Background(), AttemptInfo{}, func(ctx context.Context) error { return nil })
	assert.True(t, called)
}

func TestNewStepEventSink_SucceededStep(t *testing.T) {
	var events []WorkflowEvent
	sink := NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) })

	step := NoOp("a")
	info := StepInfo{Step: step, TerminalReason: Pending}
	err := sink.InterceptStep(context.Background(), info, func(ctx context.Context) error {
		return nil
	})

	assert.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, EventScheduled, events[0].Type)
	assert.Equal(t, step, events[0].Step)
	assert.Equal(t, EventSucceeded, events[1].Type)
	assert.NotZero(t, events[1].Duration)
}

func TestNewStepEventSink_FailedStep(t *testing.T) {
	var events []WorkflowEvent
	sink := NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) })

	step := NoOp("a")
	boom := errors.New("boom")
	info := StepInfo{Step: step, TerminalReason: Pending}
	err := sink.InterceptStep(context.Background(), info, func(ctx context.Context) error {
		return boom
	})

	assert.Equal(t, boom, err)
	assert.Len(t, events, 2)
	assert.Equal(t, EventScheduled, events[0].Type)
	assert.Equal(t, EventFailed, events[1].Type)
	assert.Equal(t, boom, events[1].Err)
}

func TestNewStepEventSink_SkippedStep(t *testing.T) {
	var events []WorkflowEvent
	sink := NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) })

	step := NoOp("a")
	info := StepInfo{Step: step, TerminalReason: Skipped}
	nextCalled := false
	err := sink.InterceptStep(context.Background(), info, func(ctx context.Context) error {
		nextCalled = true
		return nil
	})

	assert.NoError(t, err)
	assert.False(t, nextCalled, "next must not be called for Skipped")
	assert.Len(t, events, 2)
	assert.Equal(t, EventScheduled, events[0].Type)
	assert.Equal(t, EventSkipped, events[1].Type)
}

func TestNewStepEventSink_OnRetry_NotImplemented(t *testing.T) {
	sink := NewStepEventSink(func(e WorkflowEvent) {})
	_, ok := sink.(retryNotifier)
	assert.False(t, ok, "NewStepEventSink must NOT implement retryNotifier")
}

func TestNewAttemptEventSink_OnRetry(t *testing.T) {
	var events []WorkflowEvent
	sink := NewAttemptEventSink(func(e WorkflowEvent) { events = append(events, e) })

	rn, ok := sink.(retryNotifier)
	assert.True(t, ok, "NewAttemptEventSink should implement retryNotifier")

	boom := errors.New("boom")
	rn.onRetry(WorkflowEvent{Type: EventRetrying, Attempt: 0, Err: boom, BackoffDuration: time.Second})

	assert.Len(t, events, 1)
	assert.Equal(t, EventRetrying, events[0].Type)
	assert.Equal(t, boom, events[0].Err)
}

func TestNewAttemptEventSink_EmitsStarted(t *testing.T) {
	var events []WorkflowEvent
	sink := NewAttemptEventSink(func(e WorkflowEvent) { events = append(events, e) })

	step := NoOp("a")
	info := AttemptInfo{StepInfo: StepInfo{Step: step}, Attempt: 2}
	err := sink.InterceptAttempt(context.Background(), info, func(ctx context.Context) error {
		return nil
	})

	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, EventStarted, events[0].Type)
	assert.Equal(t, uint64(2), events[0].Attempt)
	assert.Equal(t, step, events[0].Step)
}
