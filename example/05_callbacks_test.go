package flow_test

import (
	"context"
	"errors"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Callbacks: BeforeStep / AfterStep
//
// **What you'll learn**
//   - Use `BeforeStep` to mutate context (or short-circuit) right before Do.
//   - Use `AfterStep` to inspect / transform the error right after Do.
//   - Where these callbacks sit in the execution stack vs `Input` /
//     Interceptors.
//
// **Where they fit**
//
//	StepInterceptor (workflow-level, see 11_observability_test.go)
//	  └── retry loop (one iteration per attempt)
//	      └── AttemptInterceptor (workflow-level)
//	          └── BeforeStep callbacks   ← runs once PER ATTEMPT
//	              └── Input callbacks    (a special BeforeStep)
//	                  └── step.Do(ctx)
//	              └── AfterStep callbacks ← runs once PER ATTEMPT
//
// `BeforeStep` and `AfterStep` are step-level (configured per Step). Use
// them when behaviour applies to one Step. Reach for an Interceptor when
// it applies to every Step in the Workflow.

// ExampleAddStep_BeforeStep adds Before/After callbacks to a Step. The
// Step is just a plain struct with Do — same shape as in 01 and 03.
func ExampleAddStep_BeforeStep() {
	greet := &greeter{Name: "world"}

	w := new(flow.Workflow)
	w.Add(
		flow.Step(greet).
			// BeforeStep can read/modify ctx, or return an error to skip Do.
			// The returned ctx is forwarded to subsequent BeforeStep
			// callbacks and ultimately to Do.
			BeforeStep(func(ctx context.Context, _ flow.Steper) (context.Context, error) {
				fmt.Println("(before)")
				return ctx, nil
			}).
			// AfterStep can inspect or transform Do's error. Return nil to
			// suppress; return a different error to replace it.
			AfterStep(func(ctx context.Context, _ flow.Steper, err error) error {
				fmt.Println("(after) err=", err)
				return err
			}),
	)

	_ = w.Do(context.Background())
	// Output:
	// (before)
	// hello, world
	// (after) err= <nil>
}

type greeter struct {
	Name string
}

func (g *greeter) Do(ctx context.Context) error {
	fmt.Printf("hello, %s\n", g.Name)
	return nil
}

// ExampleAddStep_AfterStep_transformError shows the most common AfterStep
// idiom: catch a known error and convert it to nil (suppress) or to a
// domain-specific error.
func ExampleAddStep_AfterStep_transformError() {
	lookup := &lookupItem{}

	w := new(flow.Workflow)
	w.Add(
		flow.Step(lookup).
			AfterStep(func(ctx context.Context, _ flow.Steper, err error) error {
				if errors.Is(err, errNotFound) {
					// "Not found" is fine for this workflow — treat as success.
					fmt.Println("nothing to do")
					return nil
				}
				return err
			}),
	)

	if err := w.Do(context.Background()); err != nil {
		fmt.Println("workflow failed:", err)
	}
	// Output:
	// nothing to do
}

var errNotFound = errors.New("not found")

type lookupItem struct{}

func (l *lookupItem) Do(ctx context.Context) error {
	return errNotFound
}
