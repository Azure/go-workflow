package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// In a workflow, it's common to have debug callbacks that you want to
// execute only when some steps are failed.
//
// The debug step needs the result of the previous steps, it can be achieved by hack When.

type DebugStep struct {
	Upstreams map[flow.Steper]flow.StatusError
}

func (d *DebugStep) When(ctx context.Context, ups map[flow.Steper]flow.StatusError) flow.StepStatus {
	// save the upstreams for Do
	d.Upstreams = ups
	return flow.AnyFailed(ctx, ups)
}
func (d *DebugStep) Do(ctx context.Context) error {
	for up, statusErr := range d.Upstreams {
		switch {
		case flow.Is[*FailedStep](up):
			// handle the error
			fmt.Println(statusErr.Status, statusErr.Err)
		}
	}
	return nil
}

func ExampleDebugStep() {
	workflow := new(flow.Workflow)

	debug := new(DebugStep)
	failed := new(FailedStep)

	workflow.Add(flow.Step(failed))

	// register the debug step
	workflow.Add(flow.Step(debug).
		DependsOn(failed).
		When(debug.When),
	)

	_ = workflow.Do(context.Background())
	// Output:
	// Failed failed!
}
