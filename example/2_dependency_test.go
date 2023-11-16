package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
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
//   - Step is a generic builder accepting `Input` and `InputDependsOn`, check next session about I/O for more details.
func ExampleDeclareDependency() {
	workflow := new(flow.Workflow)

	// Besides, `workflow` also provides a convenient way to create a Step implementation without declaring type.
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

	dep := workflow.Dep()
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

func getUpstreamNames(ups []flow.Link) []string {
	rv := []string{}
	for _, up := range ups {
		rv = append(rv, up.Upstream.String())
	}
	return rv
}
