package flow

import (
	"context"
	"time"
)

// EventType identifies a step lifecycle event.
// It reuses the same underlying type as StepStatus so that StepStatus constants
// (Succeeded, Failed, Canceled, Skipped) are directly usable as EventType values.
type EventType = StepStatus

const (
	Scheduled EventType = "Scheduled"
	Started   EventType = "Started"
	Retrying  EventType = "Retrying"
	// Succeeded, Failed, Canceled, Skipped are inherited from StepStatus in condition.go
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
// stepExecution calls PrependInterceptors before each attempt so that
// parent interceptors wrap child interceptors.
type InterceptorReceiver interface {
	PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}

// retryNotifier is a package-private interface implemented by the concrete
// type returned by NewStepEventSink. stepExecution uses it to deliver
// Retrying events (which bypass the interceptor chain) to the sink.
type retryNotifier interface {
	onRetry(WorkflowEvent)
}
