package flow

import "context"

// StepInterceptor wraps the FULL lifetime of a step (from the first attempt
// up to and including the last retry). The chain is built once per step run
// in stepExecution.run, with the lowest-index interceptor on the outside.
// `next` invokes the next interceptor in the chain — eventually calling into
// executeWithRetry, which itself loops over attempts and per-attempt
// interceptors.
//
// Important: steps that are settled inline (Skipped or Canceled by their
// Condition) bypass the interceptor chain entirely. If you need observability
// for those terminal states, watch the StepResult instead.
type StepInterceptor interface {
	InterceptStep(ctx context.Context, step Steper, next func(context.Context) error) error
}

// AttemptInterceptor wraps a SINGLE attempt (Before → Do → After) inside the
// retry loop. It receives the 0-based attempt index, and the error returned
// by `next` is the error of THAT attempt — so you can inspect, transform or
// suppress it before it propagates up to the retry policy.
type AttemptInterceptor interface {
	InterceptAttempt(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error
}

// StepInterceptorFunc adapts a plain function to the StepInterceptor
// interface — same shape as http.HandlerFunc.
type StepInterceptorFunc func(ctx context.Context, step Steper, next func(context.Context) error) error

func (f StepInterceptorFunc) InterceptStep(ctx context.Context, step Steper, next func(context.Context) error) error {
	return f(ctx, step, next)
}

// AttemptInterceptorFunc adapts a plain function to the AttemptInterceptor interface.
type AttemptInterceptorFunc func(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error

func (f AttemptInterceptorFunc) InterceptAttempt(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error {
	return f(ctx, step, attempt, next)
}

// InterceptorReceiver is implemented by any Step that contains a sub-workflow
// (notably *Workflow itself and SubWorkflow). The parent's stepExecution
// calls PrependInterceptors ONCE — in executeWithRetry, just before the
// retry loop — so the parent's interceptor chain wraps the child's
// interceptor chain for the duration of the step.
//
// Implementations should be careful not to mutate the user-supplied base
// chain or accumulate inherited entries across runs (see Workflow's
// `inheritedStep` / `inheritedAttempt` design).
//
// The parent looks for an InterceptorReceiver by walking the Step tree via
// Unwrap (using the same protocol as As[T] / Has[T]). This means inheritance
// keeps working when the sub-workflow is wrapped in a Name / NamedStep / any
// other Steper wrapper that exposes Unwrap. The first receiver found in
// pre-order is used.
type InterceptorReceiver interface {
	PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor)
}

// findInterceptorReceiver returns the first InterceptorReceiver in the Step
// tree rooted at s, walking via Unwrap in pre-order. Returns nil if none of
// the unwrapped Steps satisfies InterceptorReceiver.
//
// This lets a sub-workflow be wrapped in a Steper-only wrapper (e.g.
// NamedStep, which embeds the Steper interface and therefore does not
// promote PrependInterceptors) without losing parent-interceptor inheritance.
func findInterceptorReceiver(s Steper) InterceptorReceiver {
	var found InterceptorReceiver
	Traverse(s, func(s Steper, _ []Steper) TraverseDecision {
		if r, ok := s.(InterceptorReceiver); ok {
			found = r
			return TraverseStop
		}
		return TraverseContinue
	})
	return found
}
