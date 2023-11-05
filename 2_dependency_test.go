package workflow_test

import (
	"context"
	"fmt"

	"go.goms.io/aks/rp/test/v3/workflow"
)

// Steps are connected with dependencies to form a Workflow.
//
// `workflow` provides rich featured Step dependency builders,
// and the syntax is pretty close to plain English:
//
//	Step(someTask).DependsOn(upstreamTask)
//	Steps(taskA, taskB).DependsOn(taskC, taskD)
//
// Most time, `Step` and `Steps` are mutually exchangeable.
// The only difference is that:
//   - Step is a generic builder accepting `Input` and `InputDepends`, check next session about I/O for more details.
func ExampleDeclareDependency() {
	flow := new(workflow.Workflow)

	// Besides, `workflow` also provides a convenient way to create a Step implementation without declaring type.
	// Use `Func` to wrap any arbitrary function into a Step.
	doNothing := func(context.Context) error { return nil }
	var (
		a = workflow.Func("a", doNothing)
		b = workflow.Func("b", doNothing)
		c = workflow.Func("c", doNothing)
		d = workflow.Func("d", doNothing)
	)

	flow.Add(
		workflow.Step(a).DependsOn(b, c),
		workflow.Steps(b, c).DependsOn(d),
	)

	dep := flow.Dep()
	fmt.Println(getUpstreamNames(dep[a]))
	fmt.Println(getUpstreamNames(dep[b]))
	fmt.Println(getUpstreamNames(dep[c]))
	fmt.Println(getUpstreamNames(dep[d]))
	// Output:
	// [b c]
	// [d]
	// [d]
	// []
}

func getUpstreamNames(ups []workflow.Link) []string {
	rv := []string{}
	for _, up := range ups {
		rv = append(rv, up.Upstream.String())
	}
	return rv
}
