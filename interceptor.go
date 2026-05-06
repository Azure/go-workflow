package flow

import "context"

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
// The error returned by next (if any) is the attempt's failure — it is available
// for inspection before being returned.
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
