package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// Maybe you've already noticed that, Workflow also implements Steper interface.
//
//	func (w *Workflow) Do(ctx context.Context) error {
//
// Which means, you can actually put a Workflow into another Workflow as a Step!
func ExampleWiW() {
	inner := new(flow.Workflow)
	inner.Add(
		flow.Step(new(Bar)).DependsOn(new(Foo)),
	)

	outer := new(flow.Workflow)
	before := flow.Func("before", func(ctx context.Context) error {
		fmt.Println("Before")
		return nil
	})
	after := flow.Func("after", func(ctx context.Context) error {
		fmt.Println("After")
		return nil
	})
	outer.Add(
		flow.Pipe(before, inner, after),
	)
	_ = outer.Do(context.Background())
	// Output:
	// Before
	// Foo
	// Bar
	// After
}
