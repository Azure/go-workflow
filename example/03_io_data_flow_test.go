package flow_test

import (
	"context"
	"fmt"
	"slices"
	"strings"

	flow "github.com/Azure/go-workflow"
)

// After connected Steps into Workflow via dependencies,
// there is a very common scenarios that passing value / data through dependency.
//
// `flow` is designed with the support of flowing data between Steps.
// In order to connect the Steps with data flow among them, use InputDependsOn().
// In order to add callbacks to modify the Input of the Step at runtime, use Input().
//
//	Step(someTask).
//		Input(func(_ context.Context, i *SomeTask) error { ... }).
//		InputDependsOn(
//			Adapt(upstream, func(_ context.Context, o *Upstream, i *SomeTask) error { ... }),
//		)
//
// Notice the callbacks declares in Input() and InputDependsOn() are executed at runtime and per-retry.
//
// Q: why not just pass the values outside, but rather use Input callbacks?
// A: if the values are assigned to Step outside of Input callbacks, the values are assigned only once at the beginning,
// and moreover, at the building workflow stage, the values could be not available yet.

type SayHello struct {
	Who    string
	Output string
}

func (s *SayHello) Do(context.Context) error {
	s.Output = "Hello " + s.Who
	fmt.Println(s.Output)
	return nil
}

type ImBob struct {
	Output string
}

func (i *ImBob) Do(context.Context) error {
	i.Output = "Bob"
	return nil
}

type ReverseOrder struct {
	Slice []string
}

func (r *ReverseOrder) Do(context.Context) error {
	slices.Reverse(r.Slice)
	return nil
}

func ExampleDataFlow() {
	// Now, let's connect the Steps into Workflow with data flow.
	workflow := new(flow.Workflow)

	imBob := new(ImBob)
	sayHello := &SayHello{
		// initialize fields in variable declaration is not encouraged, please use Input() or InputDependsOn() callback.
		// workflow will respect the callbacks in Input() and InputDependsOn() before each retry,
		// such you can guarantee the fields you care are always initialized before each retry.
		Who: "do not set value here",
	}

	workflow.Add(
		// use InputDependsOn() with Adapt() to connect the Steps
		flow.Step(sayHello).
			InputDependsOn(
				flow.Adapt(imBob, func(_ context.Context, imBob *ImBob, sayHello *SayHello) error {
					sayHello.Who = imBob.Output // imBob's Output will be passed to sayHello's Input
					return nil
				}),
			).
			// use Input() to modify the Input of the Step at runtime.
			Input(func(ctx context.Context, sayHello *SayHello) error {
				// This callback will be executed at runtime and per-retry.
				// And the order of execution is respected to the order of declaration,
				// means that,
				// sayHello.Who = imBob.Output is already executed
				// then
				sayHello.Who += " and Alice"
				return nil
			}),
	)

	reverseOrder := new(ReverseOrder)
	workflow.Add(
		flow.Step(reverseOrder).InputDependsOn(
			flow.Adapt(sayHello, func(_ context.Context, sayHello *SayHello, reverseOrder *ReverseOrder) error {
				// In this adapt function, you can transform the Upstream to Downstream's Input
				// "Hello Bob and Alice" => []string{"Hello", "Bob", "and", "Alice"}
				reverseOrder.Slice = strings.Split(sayHello.Output, " ")
				return nil
			}),
		),
	)

	_ = workflow.Do(context.TODO())

	// After the Workflow is finished, you can get the result from the Output of the last Step.
	fmt.Println(reverseOrder.Slice)

	// Output:
	// Hello Bob and Alice
	// [Alice and Bob Hello]
}
