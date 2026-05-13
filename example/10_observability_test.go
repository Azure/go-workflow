package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Observability: Interceptors
//
// **What you'll learn**
//   - Use `StepInterceptor` to observe every Step (full lifetime, including
//     retries) — perfect for logs and tracing spans.
//   - Use `AttemptInterceptor` to observe each individual attempt — perfect
//     for per-try metrics or transforming an attempt's error.
//   - Parent → child workflows inherit interceptors automatically; opt out
//     with `Option.DontInherit` (which now opts out of the entire
//     `WorkflowOption`, not just interceptors).
//
// **Two layers, deliberately separated**
//
//	StepInterceptor    wraps the FULL lifetime of a Step (across all retries).
//	                   Sees one Begin / End per Step.
//
//	AttemptInterceptor wraps a SINGLE attempt (Before → Do → After) inside the
//	                   retry loop. Sees one call per attempt.
//
// Both are middleware: each receives a `next` callback and is free to
// short-circuit, wrap, or transform around it.
//
// **When to reach for which mechanism**
//
//	Need to log/trace every Step?               → Interceptor (this file).
//	Need to react to upstream's terminal status → Condition (05_conditions).
//	Need behaviour for one specific Step?       → BeforeStep / AfterStep
//	                                              (04_callbacks).
//
// **Caveats**
//   - Steps settled inline as Skipped/Canceled by their Condition bypass
//     the interceptor chain. Inspect `StepResult` if you need those.

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
		foo = flow.Func("Foo", func(ctx context.Context) error { fmt.Println("Foo"); return nil })
		bar = flow.Func("Bar", func(ctx context.Context) error { fmt.Println("Bar"); return nil })
	)
	workflow := &flow.Workflow{
		Option: flow.WorkflowOption{StepInterceptors: []flow.StepInterceptor{logger}},
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

	passAfter2 := &flow.NamedStep{Name: "PassAfter2", Steper: &passAfter{n: 2}}
	workflow := &flow.Workflow{
		Option: flow.WorkflowOption{
			StepInterceptors:    []flow.StepInterceptor{stepLog},
			AttemptInterceptors: []flow.AttemptInterceptor{attemptLog},
		},
	}
	workflow.Add(
		flow.Step(passAfter2).
			Retry(func(ro *flow.RetryOption) {
				ro.Attempts = 5
				ro.Timer = new(zeroTimer)
			}),
	)

	_ = workflow.Do(context.Background())
	// Output:
	// [step ] begin PassAfter2
	// fail #0
	// [try=0] PassAfter2 err=transient
	// fail #1
	// [try=1] PassAfter2 err=transient
	// pass #2
	// [try=2] PassAfter2 err=<nil>
	// [step ] end   PassAfter2 (err=<nil>)
}

// ExampleWorkflowOptionReceiver shows that when a Workflow is used as a
// Step inside another Workflow, the outer Workflow's interceptors
// automatically wrap every Step in the inner Workflow.
//
// The mechanism is the WorkflowOptionReceiver interface: any Step that
// contains a sub-Workflow (Workflow itself, the deprecated SubWorkflow)
// implements InheritOption, and the parent walks the Step tree (via Unwrap)
// to find a receiver. So you can wrap a sub-Workflow in flow.Name (or any
// other Steper wrapper) without losing inheritance.
//
// To opt out of inheritance and run an inner Workflow with only its own
// interceptors (and other Option fields), set Option.DontInherit: true on
// the inner.
func ExampleWorkflowOptionReceiver() {
	outerLogger := flow.StepInterceptorFunc(func(ctx context.Context, step flow.Steper, next func(context.Context) error) error {
		fmt.Printf("[outer] %s\n", step)
		return next(ctx)
	})

	// inner has no interceptors of its own — it inherits from outer.
	inner := new(flow.Workflow)
	inner.Add(
		flow.Pipe(
			flow.Func("inner-B", func(ctx context.Context) error { fmt.Println("inner-B"); return nil }),
			flow.Func("inner-A", func(ctx context.Context) error { fmt.Println("inner-A"); return nil }),
		),
	)

	// isolated has the same shape but opts out of inheritance.
	isolated := &flow.Workflow{Option: flow.WorkflowOption{DontInherit: true}}
	isolated.Add(
		flow.Step(flow.Func("isolated-X", func(ctx context.Context) error { fmt.Println("isolated-X"); return nil })),
	)

	outer := &flow.Workflow{
		Option: flow.WorkflowOption{StepInterceptors: []flow.StepInterceptor{outerLogger}},
	}
	// Naming the sub-workflows is purely cosmetic — it just makes the log
	// readable. flow.Name wraps each sub-workflow in a NamedStep, and the
	// outer interceptor finds the WorkflowOptionReceiver by walking through
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
