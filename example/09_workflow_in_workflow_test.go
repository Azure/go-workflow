package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Workflow inside a Workflow: composing complex pipelines
//
// **What you'll learn**
//   - `*Workflow` itself satisfies `Steper`, so a Workflow can be used as
//     a Step inside another Workflow. This is the recommended way to
//     compose multiple operations into a "compound" Step.
//   - The naive alternative — a struct whose `Do` calls a fixed sequence
//     of inner Steps directly — looks simpler but loses observability,
//     per-inner-step retry, parallelism, and Mock-ability.
//
// **Rule of thumb**
//
//	Need 1 atomic action?              ─► implement Steper directly.
//	Need a sequence of N actions?      ─► put them in a sub-Workflow.
//	Don't roll your own composite by   ─► (see ExampleWorkflow_compositeAntipattern below).
//	chaining Do() calls inside Do().

// ExampleWorkflow_asStep shows the recommended pattern: build the inner
// pipeline as its own Workflow, then plug it into the outer Workflow as
// a Step. Every benefit of go-workflow (interceptors, retry, conditions,
// MaxConcurrency) applies to the inner Steps too.
func ExampleWorkflow_asStep() {
	var (
		clean   = flow.Func("Clean",   func(ctx context.Context) error { fmt.Println("clean"); return nil })
		compile = flow.Func("Compile", func(ctx context.Context) error { fmt.Println("compile"); return nil })
		test    = flow.Func("Test",    func(ctx context.Context) error { fmt.Println("test"); return nil })
	)

	// Inner workflow: the "build" sub-pipeline.
	build := new(flow.Workflow).Add(
		flow.Pipe(clean, compile, test),
	)

	var (
		fetch   = flow.Func("Fetch",   func(ctx context.Context) error { fmt.Println("fetch"); return nil })
		publish = flow.Func("Publish", func(ctx context.Context) error { fmt.Println("publish"); return nil })
	)

	// Outer workflow: fetch ─► build ─► publish.
	outer := new(flow.Workflow).Add(
		flow.Pipe(fetch, build, publish),
	)

	_ = outer.Do(context.Background())
	// Output:
	// fetch
	// clean
	// compile
	// test
	// publish
}

// ExampleWorkflow_compositeAntipattern shows what NOT to do, and why.
// `compositeStep` runs three inner Steps from inside its own Do(). It
// works — but the Workflow has no idea those inner Steps exist:
//   - Workflow.MaxConcurrency does NOT apply to inner Steps.
//   - Per-inner Retry / Timeout / When are not configurable.
//   - Interceptors only see one outer Step, not the three inner ones.
//   - flow.Mock can't mock individual inner Steps for tests.
//
// Use a sub-Workflow (above) instead.
func ExampleWorkflow_compositeAntipattern() {
	w := new(flow.Workflow).Add(
		flow.Step(&compositeStep{label: "build"}),
	)
	_ = w.Do(context.Background())
	// Output:
	// build: clean
	// build: compile
	// build: test
}

// compositeStep is the antipattern: a single Step that internally chains
// several actions. Don't do this for production pipelines — see
// ExampleWorkflow_asStep above for the right way.
type compositeStep struct {
	label string
}

func (c *compositeStep) String() string { return c.label }
func (c *compositeStep) Do(ctx context.Context) error {
	for _, action := range []string{"clean", "compile", "test"} {
		fmt.Printf("%s: %s\n", c.label, action)
	}
	return nil
}
