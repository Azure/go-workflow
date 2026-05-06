package flow

import (
	"context"
	"time"
)

// EventType identifies a step lifecycle event.
type EventType string

const (
	EventScheduled EventType = "Scheduled"
	EventStarted   EventType = "Started"
	EventRetrying  EventType = "Retrying"
	EventSucceeded EventType = "Succeeded"
	EventFailed    EventType = "Failed"
	EventCanceled  EventType = "Canceled"
	EventSkipped   EventType = "Skipped"
)

// WorkflowEvent carries information about a step lifecycle event.
type WorkflowEvent struct {
	Step            Steper
	Type            EventType
	Attempt         uint64
	Err             error
	Duration        time.Duration
	BackoffDuration time.Duration // non-zero only for Retrying
}

// StepInfo is passed to StepInterceptor.
// Step is the canonical identifier — same pointer used as map key in Workflow.
// Callers that need a human-readable name can call flow.String(info.Step).
type StepInfo struct {
	Step           Steper
	TerminalReason StepStatus // Pending = will execute; Skipped/Canceled = will not execute
}

// AttemptInfo is passed to AttemptInterceptor.
// Interceptors that need timing should record time.Now() at the top of InterceptAttempt.
type AttemptInfo struct {
	StepInfo
	Attempt uint64
}

// StepInterceptor intercepts the full lifecycle of a step (all retry attempts).
// If info.TerminalReason != Pending, next must not be called — the step will not execute.
// Return nil in that case after observing the event.
type StepInterceptor interface {
	InterceptStep(ctx context.Context, info StepInfo, next func(context.Context) error) error
}

// AttemptInterceptor intercepts each individual attempt (Before → Do → After).
type AttemptInterceptor interface {
	InterceptAttempt(ctx context.Context, info AttemptInfo, next func(context.Context) error) error
}

// StepInterceptorFunc is a function adapter for StepInterceptor.
type StepInterceptorFunc func(ctx context.Context, info StepInfo, next func(context.Context) error) error

func (f StepInterceptorFunc) InterceptStep(ctx context.Context, info StepInfo, next func(context.Context) error) error {
	return f(ctx, info, next)
}

// AttemptInterceptorFunc is a function adapter for AttemptInterceptor.
type AttemptInterceptorFunc func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error

func (f AttemptInterceptorFunc) InterceptAttempt(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
	return f(ctx, info, next)
}

// InterceptorReceiver is implemented by steps that contain a sub-workflow.
// stepExecution calls PrependInterceptors once (in executeWithRetry, before the retry loop)
// so that parent interceptors wrap child interceptors for the entire step lifetime.
type InterceptorReceiver interface {
	PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}

// retryNotifier is a package-private interface implemented by the concrete
// type returned by NewAttemptEventSink. stepExecution uses it to deliver
// Retrying events (which bypass the interceptor chain) to the sink.
type retryNotifier interface {
	onRetry(WorkflowEvent)
}

// terminalEventType maps an error to the corresponding terminal EventType.
func terminalEventType(err error) EventType {
	if err == nil {
		return EventSucceeded
	}
	switch StatusFromError(err) {
	case Canceled:
		return EventCanceled
	case Skipped:
		return EventSkipped
	default:
		return EventFailed
	}
}

// terminalStepStatusToEventType converts a terminal StepStatus to its EventType counterpart.
func terminalStepStatusToEventType(s StepStatus) EventType {
	switch s {
	case Succeeded:
		return EventSucceeded
	case Failed:
		return EventFailed
	case Canceled:
		return EventCanceled
	case Skipped:
		return EventSkipped
	default:
		return EventFailed
	}
}

// stepEventSink is the concrete type returned by NewStepEventSink.
type stepEventSink struct {
	sink func(WorkflowEvent)
}

// NewStepEventSink returns a StepInterceptor that emits Scheduled then a terminal
// event (Succeeded/Failed/Canceled/Skipped) for every step.
// It is not aware of individual retry attempts; use NewAttemptEventSink for that.
func NewStepEventSink(sink func(WorkflowEvent)) StepInterceptor {
	return &stepEventSink{sink: sink}
}

func (s *stepEventSink) InterceptStep(ctx context.Context, info StepInfo, next func(context.Context) error) error {
	s.sink(WorkflowEvent{Step: info.Step, Type: EventScheduled})

	if info.TerminalReason != Pending {
		s.sink(WorkflowEvent{Step: info.Step, Type: terminalStepStatusToEventType(info.TerminalReason)})
		return nil
	}

	start := time.Now()
	err := next(ctx)
	s.sink(WorkflowEvent{
		Step:     info.Step,
		Type:     terminalEventType(err),
		Err:      err,
		Duration: time.Since(start),
	})
	return err
}

// attemptEventSink is the concrete type returned by NewAttemptEventSink.
// It implements both AttemptInterceptor and retryNotifier so that Started and
// Retrying events are delivered to the same sink function.
type attemptEventSink struct {
	sink func(WorkflowEvent)
}

// NewAttemptEventSink returns an AttemptInterceptor that emits a Started event
// for each attempt and a Retrying event after each failed attempt (before backoff).
// Retrying carries the failure error and the backoff duration.
func NewAttemptEventSink(sink func(WorkflowEvent)) AttemptInterceptor {
	return &attemptEventSink{sink: sink}
}

func (s *attemptEventSink) InterceptAttempt(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
	s.sink(WorkflowEvent{
		Step:    info.Step,
		Type:    EventStarted,
		Attempt: info.Attempt,
	})
	return next(ctx)
}

func (s *attemptEventSink) onRetry(e WorkflowEvent) { s.sink(e) }
