package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Interceptor
//
// Interceptors let you observe, time, log, inject context, or transform errors
// for every Step in a Workflow — without touching Step implementations.
//
// There are TWO layers, deliberately separated:
//
//	StepInterceptor    wraps the FULL lifetime of a Step (across all retries).
//	                   Sees one Begin / End per Step.
//
//	AttemptInterceptor wraps a SINGLE attempt (Before → Do → After) inside the
//	                   retry loop. Sees one call per attempt — useful for
//	                   per-try metrics or transforming an attempt's error
//	                   before it reaches the retry policy.
//
// Both are middleware: each interceptor receives a `next` callback and is free
// to short-circuit, wrap, or transform around it.
//
// Configure them on the Workflow:
//
//	workflow := &flow.Workflow{
//	    StepInterceptors:    []flow.StepInterceptor{logger},
//	    AttemptInterceptors: []flow.AttemptInterceptor{metrics},
//	}
//
// A few things worth knowing:
//
//   - Steps that are settled inline as Skipped or Canceled by their Condition
//     bypass the interceptor chain. Use StepResult to observe those.
//   - When a Workflow is used as a child Step, the parent's interceptors are
//     automatically prepended to the child's chain — set IsolateInterceptors
//     on the child to opt out.

// ExampleStepInterceptor shows the simplest, most common use: a logger that
// prints when each Step starts and ends. The interceptor wraps the Step's
// FULL lifetime, so retries / per-attempt detail are invisible at this layer.
func ExampleStepInterceptor() {
	logger := flow.StepInterceptorFunc(func(ctx context.Context, step flow.Steper, next func(context.Context) error) error {
		fmt.Printf(">>> START %s\n", step)
		err := next(ctx)
		fmt.Printf("<<< END   %s (err=%v)\n", step, err)
		return err
	})

	var (
		foo = &flow.NamedStep{Name: "Foo", Steper: new(Foo)}
		bar = &flow.NamedStep{Name: "Bar", Steper: new(Bar)}
	)
	workflow := &flow.Workflow{
		StepInterceptors: []flow.StepInterceptor{logger},
	}
	workflow.Add(
		flow.Step(foo).DependsOn(bar),
	)

	_ = workflow.Do(context.Background())
	// Output:
	// >>> START Bar
	// Bar
	// <<< END   Bar (err=<nil>)
	// >>> START Foo
	// Foo
	// <<< END   Foo (err=<nil>)
}

// ExampleAttemptInterceptor shows the per-attempt layer. Combined with Retry,
// the StepInterceptor sees a single Begin/End for the whole Step while the
// AttemptInterceptor is invoked once per attempt — exactly what you want for
// per-try metrics, tracing spans, or attempt-scoped error inspection.
func ExampleAttemptInterceptor() {
	stepLog := flow.StepInterceptorFunc(func(ctx context.Context, step flow.Steper, next func(context.Context) error) error {
		fmt.Printf("[step ] begin %s\n", step)
		err := next(ctx)
		fmt.Printf("[step ] end   %s (err=%v)\n", step, err)
		return err
	})
	attemptLog := flow.AttemptInterceptorFunc(func(ctx context.Context, step flow.Steper, attempt uint64, next func(context.Context) error) error {
		err := next(ctx)
		fmt.Printf("[try=%d] %s err=%v\n", attempt, step, err)
		return err
	})

	passAfter2 := &flow.NamedStep{Name: "PassAfter2", Steper: &PassAfter{Attempt: 2}}
	workflow := &flow.Workflow{
		StepInterceptors:    []flow.StepInterceptor{stepLog},
		AttemptInterceptors: []flow.AttemptInterceptor{attemptLog},
	}
	workflow.Add(
		flow.Step(passAfter2).
			Retry(func(ro *flow.RetryOption) {
				ro.Attempts = 5
				ro.Timer = new(testTimer)
			}),
	)

	_ = workflow.Do(context.Background())
	// Output:
	// [step ] begin PassAfter2
	// failed at attempt 0
	// [try=0] PassAfter2 err=failed at attempt 0
	// failed at attempt 1
	// [try=1] PassAfter2 err=failed at attempt 1
	// succeed at attempt 2
	// [try=2] PassAfter2 err=<nil>
	// [step ] end   PassAfter2 (err=<nil>)
}

// ExampleInterceptorReceiver shows that when a Workflow is used as a Step
// inside another Workflow, the outer Workflow's interceptors automatically
// wrap every Step in the inner Workflow.
//
// The mechanism is the InterceptorReceiver interface: any Step that contains
// a sub-Workflow (Workflow itself, SubWorkflow) implements
// PrependInterceptors, and the parent walks the Step tree (via Unwrap) to
// find a receiver. So you can wrap a sub-Workflow in flow.Name (or any other
// Steper wrapper) without losing inheritance.
//
// To opt out of inheritance and run an inner Workflow with only its own
// interceptors, set IsolateInterceptors: true on the inner.
func ExampleInterceptorReceiver() {
	outerLogger := flow.StepInterceptorFunc(func(ctx context.Context, step flow.Steper, next func(context.Context) error) error {
		fmt.Printf("[outer] %s\n", step)
		return next(ctx)
	})

	// inner has no interceptors of its own — it inherits from outer.
	inner := new(flow.Workflow)
	inner.Add(
		flow.Step(Print("inner-A")).DependsOn(Print("inner-B")),
	)

	// isolated has the same shape but opts out of inheritance.
	isolated := &flow.Workflow{IsolateInterceptors: true}
	isolated.Add(
		flow.Step(Print("isolated-X")),
	)

	outer := &flow.Workflow{
		StepInterceptors: []flow.StepInterceptor{outerLogger},
	}
	// Naming the sub-workflows is purely cosmetic — it just makes the log
	// readable. flow.Name wraps each sub-workflow in a NamedStep, and the
	// outer interceptor finds the InterceptorReceiver by walking through
	// the wrapper via Unwrap.
	outer.Add(
		flow.Name(inner, "inner"),
		flow.Name(isolated, "isolated"),
		// Wire the dependency on the underlying sub-workflows. Add() merges
		// configs by step identity, so the wrapped (named) versions and
		// these dependency edges land on the same step.
		flow.Step(isolated).DependsOn(inner),
	)

	_ = outer.Do(context.Background())
	// Output:
	// [outer] inner
	// [outer] inner-B
	// inner-B
	// [outer] inner-A
	// inner-A
	// [outer] isolated
	// isolated-X
}
