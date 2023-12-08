package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// In this example, we have a composite step that contains multiple steps or even a sub-workflow.
// When building such a composite step, it can be challenging to unit test without accessing the inner step implementations.

// It is not considered a good practice to unbox the inner step implementations, as we want the step implementation names to describe the actions they perform,
// while hiding the implementation details from users. We want to treat these inner steps as black boxes.

// To address this, we can use the MockStep. The MockStep replaces the original step in the workflow without affecting dependencies, Input, and InputDependsOn callbacks.
// It hijacks the Do method of the original step, allowing us to mock its behavior for testing purposes.

// The version of the composite step in this example is testable using the power of the Workflow!

type CompositeStepTestable struct {
	Bootstrap
	SimpleStep
	Cleanup
}

func (c *CompositeStepTestable) String() string { return "CompositeStepTestable" }
func (c *CompositeStepTestable) Build() flow.Steper {
	w := new(flow.Workflow)
	w.Add(
		flow.Step(&c.Bootstrap),
		flow.Step(&c.SimpleStep).
			DependsOn(&c.Bootstrap),
	)
	w.Defer(
		flow.Step(&c.Cleanup),
	)
	return &flow.StringerNamedStep{
		Name:   c,
		Steper: w,
	}
}

func ExampleMockStep() {
	// In business logic, we can use this composite step by adding the step it built to the workflow
	// and using the values from itself in Input callbacks.
	workflow := new(flow.Workflow)
	upstream := flow.FuncO("upstream", func(ctx context.Context) (string, error) {
		fmt.Println("Upstream")
		return "Action!", nil
	})
	composite := new(CompositeStepTestable)
	compositeStep := composite.Build()
	workflow.Add(
		// Add Step built!
		flow.Step(compositeStep).
			DependsOn(upstream).
			Input(func(ctx context.Context, _ flow.Steper) error {
				// Set Input to the original composite!
				composite.SimpleStep.Value = upstream.Output
				return nil
			}),
	)

	// In unit test code, we can use MockStep to mock the behavior of the original step.
	innerWorkflow := flow.As[*flow.Workflow](compositeStep)[0]
	simpleStep := flow.As[*SimpleStep](innerWorkflow)[0]
	mockSimpleStep := &flow.MockStep{
		Step: simpleStep,
		MockDo: func(ctx context.Context) error {
			fmt.Printf("Mocked SimpleStep: %s\n", simpleStep.Value)
			return nil
		},
	}
	// Add the mock step back to the workflow
	innerWorkflow.Add(
		flow.Step(mockSimpleStep),
	)

	err := workflow.Do(context.Background())
	fmt.Println(err)
	// Output:
	// Upstream
	// Bootstrap
	// Mocked SimpleStep: Action!
	// Cleanup
	// <nil>
}
