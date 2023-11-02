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
//
// SteperIO[I, O any] interface extends Steper to have the following methods:
//
//	Input() *I : returning a reference to Input, such that Upstreams can fill data into it
//	Output(*O) : accepting a reference to Output, such that the Step can fill data into it
//
// Then in order to connect the Steps with I/O, use generic function Step() instead of Steps()
//
//	Step(someTask).
//		Input(func(_ context.Context, i *I) error { ... }).
//		InputDependsOn(
//			upstream1,
//			Adapt(upstream2, func(_ context.Context, o O, i *I) error { ... }),
//		)

type SayHello struct {
	workflow.Base
	Who string
}

func (s *SayHello) Input() *string   { return &s.Who }
func (s *SayHello) Output(o *string) { *o = "Hello " + s.Who }
func (s *SayHello) Do(context.Context) error {
	fmt.Println("Hello " + s.Who)
	return nil
}

// Base has a variant BaseIO[I, O any] that support default Input / Output methods.
//
//	type BaseIO[I, O] struct {
//		Base
//		In I
//		Out O
//	}
//	func (i *BaseIO[I, O]) Input() *I     { return &i.In }
//	func (i *BaseIO[I, O]) Output(out *O) { *out = i.Out }
//
// and an alias for empty Input / Output:
//
//	type BaseEmptyIO = BaseIO[struct{}, struct{}]
//
// Choose the one that fits your Step I/O scenario.
type ImBob struct {
	workflow.BaseIO[struct{}, string]
}

func (i *ImBob) Do(context.Context) error {
	i.Out = "Bob"
	return nil
}

type ReverseOrder struct {
	workflow.BaseIO[[]string, []string]
}

func (r *ReverseOrder) Do(context.Context) error {
	// accessing the Input via `In` field
	// and fill the result into the Output via `Out` field.
	r.Out = slices.Clone(r.In)
	slices.Reverse(r.Out)
	return nil
}

func ExampleInOut() {
	// Now, let's connect the Steps into a Workflow!
	flow := new(workflow.Workflow)

	// create Steps
	imBob := new(ImBob)
	sayHello := new(SayHello)

	flow.Add(
		// use InputDependsOn() to connect the Steps with I/O.
		workflow.Step(sayHello).InputDependsOn(imBob), // imBob's Output will be passed to sayHello's Input
		// use Input() to modify the Input of the Step.
		workflow.Step(sayHello).Input(func(ctx context.Context, i *string) error {
			// i is already filled by `imBob`.
			*i = *i + " and Alice"
			return nil
		}),
	)

	// However, in most real world scenarios, the Upstream's Output and Downstream's Input are not the same type.
	// In this case, we need to use an Adapter to connect them.
	reverseOrder := new(ReverseOrder)
	flow.Add(
		workflow.Step(reverseOrder).InputDependsOn(
			workflow.Adapt(sayHello,
				func(_ context.Context, o string /* o is sayHello output */, i *[]string /* i is reverseOrder input */) error {
					// in Adapter, you can transform the Upstream's Output into Downstream's Input
					// o: "Hello Bob and Alice" => i: []string{"Hello", "Bob", "and", "Alice"}
					*i = strings.Split(o, " ")
					return nil
				},
			)),
	)

	_ = flow.Run(context.TODO())

	// After the Workflow is finished, you can get the result from the Output of the last Step.
	fmt.Println(workflow.GetOutput(reverseOrder))

	// Output:
	// Hello Bob and Alice
	// [Alice and Bob Hello]
}
