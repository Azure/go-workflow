package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// Writing a Step with only a few operations is easy, but writing a Step that contains multiple steps
// (where inner Steps could even have dependencies) is a real challenge.

// In the real world, it is a good practice to reuse implemented Steps. We can build a composite step
// by combining a set of steps to achieve complex goals.

// BTW, this composite step is feasible in the this framework, but it has a few drawbacks:
//	- it's not testable
//	- the inner steps are invisible to the workflow if not implement Unwrap() method
//	- only one error returned in Do() will be showed in the final error message
//	- when add input callbacks to the inner steps, the callbacks will be called before the composite step's Do()
//
// Thus, we recommend to use the another pattern to build a composite step, see example 13.

type Bootstrap struct{}
type Cleanup struct{}
type SimpleStep struct{ Value string }
type CompositeStep struct {
	Bootstrap
	SimpleStep
	Cleanup
}

func (b *Bootstrap) Do(ctx context.Context) error {
	fmt.Println("Bootstrap")
	return nil
}
func (c *Cleanup) Do(ctx context.Context) error {
	fmt.Println("Cleanup")
	return nil
}
func (s *SimpleStep) Do(ctx context.Context) error {
	fmt.Printf("SimpleStep: %s\n", s.Value)
	return fmt.Errorf("SimpleStep Failed!")
}
func (c *CompositeStep) String() string { return "CompositeStep" }
func (c *CompositeStep) Unwrap() []flow.Steper {
	return []flow.Steper{&c.Bootstrap, &c.SimpleStep, &c.Cleanup}
}
func (c *CompositeStep) Do(ctx context.Context) error {
	if err := c.Bootstrap.Do(ctx); err != nil {
		return err
	}
	defer c.Cleanup.Do(ctx)
	return c.SimpleStep.Do(ctx)
}

func ExampleCompositeStep() {
	workflow := new(flow.Workflow)
	workflow.Add(
		flow.Step(new(CompositeStep)).
			Input(func(ctx context.Context, cs *CompositeStep) error {
				cs.SimpleStep.Value = "Action!"
				return nil
			}),
	)
	err := workflow.Do(context.Background())
	fmt.Println(err)
	// Output:
	// Bootstrap
	// SimpleStep: Action!
	// Cleanup
	// CompositeStep: [Failed]
	// 	SimpleStep Failed!
}
