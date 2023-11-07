package workflow_test

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"go.goms.io/aks/rp/test/v3/workflow"
)

// After connected Steps into Workflow via dependencies,
// there is a very common scenarios that passing value / data through dependency.
//
// `workflow` is designed with the support of flowing data between Steps.
// In order to connect the Steps with I/O, use generic function Step() instead of Steps()
//
//	Step(someTask).
//		Input(func(_ context.Context, i *SomeTask) error { ... }).
//		InputDependsOn(
//			Adapt(upstream, func(_ context.Context, o *Upstream, i *SomeTask) error { ... }),
//		)

type SayHello struct {
	workflow.Base
	Who    string
	Output string
}

func (s *SayHello) Do(context.Context) error {
	s.Output = "Hello " + s.Who
	fmt.Println(s.Output)
	return nil
}

type ImBob struct {
	workflow.Base
	Output string
}

func (i *ImBob) Do(context.Context) error {
	i.Output = "Bob"
	return nil
}

type ReverseOrder struct {
	workflow.Base
	Slice []string
}

func (r *ReverseOrder) Do(context.Context) error {
	slices.Reverse(r.Slice)
	return nil
}

func ExampleInOut() {
	// Now, let's connect the Steps into a Workflow!
	flow := new(workflow.Workflow)

	// create Steps
	imBob := new(ImBob)
	sayHello := &SayHello{
		// initialize fields in variable declaration is not encouraged, please use Input() or InputDependsOn() callback.
		// workflow will respect the callbacks in Input() and InputDependsOn() before each retry,
		// such you can guarantee the fields you care are always initialized before each retry.
		Who: "do not set value here",
	}

	flow.Add(
		// use InputDependsOn() with Adapt() to connect the Steps with I/O.
		workflow.Step(sayHello).
			InputDependsOn(
				workflow.Adapt(imBob, func(_ context.Context, imBob *ImBob, sayHello *SayHello) error {
					// This callback will be executed at runtime and per-retry.
					sayHello.Who = imBob.Output // imBob's Output will be passed to sayHello's Input
					return nil
				}),
			).
			// use Input() to modify the Input of the Step at runtime.
			Input(func(ctx context.Context, sayHello *SayHello) error {
				// This callback will be executed at runtime and per-retry.
				// And the order of execution is respected to the order of declaration,
				// means here,
				// sayHello.Who = imBob.Output is already executed
				// then
				sayHello.Who += " and Alice"
				return nil
			}),
	)

	// However, in most real world scenarios, the Upstream's Output and Downstream's Input are not the same type.
	// In this case, we need to use an Adapter to connect them.
	reverseOrder := new(ReverseOrder)
	flow.Add(
		workflow.Step(reverseOrder).InputDependsOn(
			workflow.Adapt(sayHello, func(_ context.Context, sayHello *SayHello, reverseOrder *ReverseOrder) error {
				// In this adapt function, you can transform the Upstream as Output to Downstream's Input
				// "Hello Bob and Alice" => []string{"Hello", "Bob", "and", "Alice"}
				reverseOrder.Slice = strings.Split(sayHello.Output, " ")
				return nil
			}),
		),
	)

	_ = flow.Run(context.TODO())

	// After the Workflow is finished, you can get the result from the Output of the last Step.
	fmt.Println(reverseOrder.Slice)

	// Output:
	// Hello Bob and Alice
	// [Alice and Bob Hello]
}
