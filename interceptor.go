package flow

import "context"

// StepInterceptor intercepts the full lifecycle of a step (all retry attempts).
// Skipped and Canceled steps do not enter the interceptor chain.
type StepInterceptor interface {
	InterceptStep(ctx context.Context, step Steper, next func(context.Context) error) error
}

// AttemptInterceptor intercepts each individual attempt (Before → Do → After).
// The error returned by next (if any) is the attempt's failure — it is available
// for inspection before being returned.
type AttemptInterceptor interface {
	InterceptAttempt(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error
}

// StepInterceptorFunc is a function adapter for StepInterceptor.
type StepInterceptorFunc func(ctx context.Context, step Steper, next func(context.Context) error) error

func (f StepInterceptorFunc) InterceptStep(ctx context.Context, step Steper, next func(context.Context) error) error {
	return f(ctx, step, next)
}

// AttemptInterceptorFunc is a function adapter for AttemptInterceptor.
type AttemptInterceptorFunc func(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error

func (f AttemptInterceptorFunc) InterceptAttempt(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error {
	return f(ctx, step, attempt, next)
}

// InterceptorReceiver is implemented by steps that contain a sub-workflow.
// stepExecution calls PrependInterceptors once (in executeWithRetry, before the retry loop)
// so that parent interceptors wrap child interceptors for the entire step lifetime.
type InterceptorReceiver interface {
	PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}
