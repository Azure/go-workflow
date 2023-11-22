package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// Workflow supports a concept called "Phase", which is a convient way to group steps.
// The built-in phases are:
//   - Init
//   - Run
//   - Defer
//
// We choose the above phases respectively because this common go pattern:
//
//	func init() {}
//	func main() {
//		defer func() {}
//	}
func ExamplePhase() {
	workflow := new(flow.Workflow)
	workflow.Init(flow.Step(
		flow.Func("init", func(ctx context.Context) error {
			fmt.Println("Do preparation here")
			return nil
		}),
	))
	workflow.Add(flow.Step(
		flow.Func("run", func(ctx context.Context) error {
			fmt.Println("Do main logic here")
			return nil
		}),
	))
	workflow.Defer(flow.Step(
		flow.Func("defer", func(ctx context.Context) error {
			fmt.Println("Do cleanup here")
			return nil
		}),
	))
	_ = workflow.Do(context.Background())
	// Output:
	// Do preparation here
	// Do main logic here
	// Do cleanup here
}
