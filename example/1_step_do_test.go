package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// `workflow` has two core concepts:
//
//   - Step
//   - Workflow
//
// Where Step is the unit of a Workflow,
// and Steps are connected with dependencies to form a Workflow (actually a Directed-Acyclic-Graph).
//
// They cooperate to provide features:
//
//   - Steps are easy to implement
//   - Declare dependencies between Steps to form a Workflow
//   - Workflow executes Steps in a topological order
//
// Let's start with implementing a Step:
//
// To satisfy the interface of Steper,
// it's required to embed Base struct into your Step implement struct.
type Foo struct {
	// Base inherits methods that required by a Step interface.
	// Read the document of Base for more details.
	flow.Base
}

// Besides the Base struct, user also needs to implement the Do() method.
//
//	type Steper interface {
//		Do(context.Context) error	// the main logic
//		String() string				// [optional] give this step a name
//		...
//	}
func (f *Foo) Do(ctx context.Context) error {
	fmt.Println("Foo")
	return nil
}

type Bar struct{ flow.Base }

func (b *Bar) Do(context.Context) error {
	fmt.Println("Bar")
	return nil
}

func ExampleSimple() {
	// Create a Workflow
	workflow := new(flow.Workflow)

	// Create Steps
	foo := new(Foo)
	bar := new(Bar)

	// Connect the Steps into the Workflow
	workflow.Add(
		flow.Step(foo).DependsOn(bar),
	)

	// As the code says, step `foo` depends on step `bar`, or `bar` happens-before `foo`.
	// In `fl` terms, we call `foo` as Downstream, `bar` as Upstream, since the flow is from Up to Down.
	// We'll cover dependency in next session.

	_ = workflow.Run(context.TODO())
	// Output:
	// Bar
	// Foo
}
