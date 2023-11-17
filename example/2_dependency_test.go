package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// Steps are connected with dependencies to form a Workflow.
//
// `flow` provides rich featured Step dependency builders,
// and the syntax is pretty close to plain English:
//
//	Step(someTask).DependsOn(upstreamTask)
//	Steps(taskA, taskB).DependsOn(taskC, taskD)
//
// Most time, `Step` and `Steps` are mutually exchangeable.
// The only difference is that:
//
//	Step is a generic builder accepting method `Input` and `InputDependsOn`, check next session about I/O for more details.
func ExampleDeclareDependency() {
	workflow := new(flow.Workflow)

	// Besides, `flow` also provides a convenient way to create a Step implementation without declaring type,
	// (since you need a type to implement interface `Steper`).
	// Use `Func` to wrap any arbitrary function into a Step.
	doNothing := func(context.Context) error { return nil }
	var (
		a = flow.Func("a", doNothing)
		b = flow.Func("b", doNothing)
		c = flow.Func("c", doNothing)
		d = flow.Func("d", doNothing)
	)

	workflow.Add(
		flow.Step(a).DependsOn(b, c),
		flow.Steps(b, c).DependsOn(d),
	)

	fmt.Println(workflow.UpstreamOf(a))
	fmt.Println(workflow.UpstreamOf(b))
	fmt.Println(workflow.UpstreamOf(c))
	fmt.Println(workflow.UpstreamOf(d))
	// Output:
	// map[b:{Pending <nil>} c:{Pending <nil>}]
	// map[d:{Pending <nil>}]
	// map[d:{Pending <nil>}]
	// map[]
}
