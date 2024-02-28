package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Phase
//
// Workflow supports grouping Steps into different phases.
//
// The built-in phases are:
//   - Init
//   - Run
//   - Defer
//
// You can customize the phases by add / remove / reorder new Phase to WorkflowPhases.
//
//	var WorkflowPhases = []Phase{PhaseInit, PhaseMain, /* YOUR NEW PHASE */, PhaseDefer}
//
// We choose the above phases respectively because the below common go pattern:
//
//	func init() {}
//	func main() {
//		defer func() {}
//	}
func ExamplePhase() {
	var (
		workflow = new(flow.Workflow)

		preparation = flow.Func("init", func(ctx context.Context) error {
			fmt.Println("Do preparation here")
			return nil
		})
		main = flow.Func("run", func(ctx context.Context) error {
			fmt.Println("Do main logic here")
			return nil
		})
		failed  = new(FailedStep)
		cleanup = flow.Func("defer", func(ctx context.Context) error {
			fmt.Println("Do cleanup here")
			return nil
		})
	)
	workflow.Init(flow.Step(
		preparation,
	))
	workflow.Add(flow.Steps(
		main,
		failed, // even one phase failed, the next phase will still be executed
	))
	workflow.Defer(flow.Step(
		cleanup,
	))
	_ = workflow.Do(context.Background())
	// Output:
	// Do preparation here
	// Do main logic here
	// Do cleanup here
}
