package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Steps & Dependencies: how to wire a Workflow
//
// **What you'll learn**
//   - Define a Step inline with `flow.Func` — no need to declare a type.
//   - Three ways to express a dependency graph: `DependsOn`, `Pipe`, and `BatchPipe`.
//   - Give a Step a friendly display name with `flow.Name`.
//
// **Mental model**
//
// A Workflow is a directed acyclic graph (DAG). Each Step is a node; each
// dependency is an edge from upstream to downstream. The Workflow runs
// every Step exactly once, respecting topological order: a Step starts as
// soon as all its upstreams are terminated, and Steps with no path between
// them may run in parallel.
//
// We'll wire the same toy CI/CD pipeline in three different ways so you
// can pick the style that fits your code:
//
//	    clone ──► build ──► test ──► publish
//	         \─► lint ────────┘
//
// build and lint both need clone; test needs both build and lint;
// publish needs test.

// stage is a tiny helper for these examples: a Step that just records its
// own name when it runs, so the example output proves the order. Real
// Steps would do real work.
func stage(name string) *flow.Function[struct{}, struct{}] {
	return flow.Func(name, func(context.Context) error {
		fmt.Println(name)
		return nil
	})
}

// ExampleWorkflow_dependsOn shows the most explicit style: every edge is
// declared with DependsOn. Verbose but unambiguous, and it works for any
// shape of graph.
func ExampleWorkflow_dependsOn() {
	var (
		clone   = stage("clone")
		build   = stage("build")
		lint    = stage("lint")
		test    = stage("test")
		publish = stage("publish")
	)

	w := new(flow.Workflow)
	w.Add(
		flow.Steps(build, lint).DependsOn(clone), // fan-out: both depend on clone
		flow.Step(test).DependsOn(build, lint),   // fan-in: waits for both
		flow.Step(publish).DependsOn(test),
	)

	_ = w.Do(context.Background())
	// Unordered output:
	// clone
	// build
	// lint
	// test
	// publish
}

// ExampleWorkflow_pipe shows the shorthand for *linear* chains. Pipe(a, b, c)
// is exactly Step(b).DependsOn(a) + Step(c).DependsOn(b). Use Pipe when the
// graph is a straight line; it reads top-to-bottom like a script.
func ExampleWorkflow_pipe() {
	var (
		clone   = stage("clone")
		build   = stage("build")
		test    = stage("test")
		publish = stage("publish")
	)

	w := new(flow.Workflow)
	w.Add(
		// Pure linear pipeline. Equivalent to three DependsOn calls.
		flow.Pipe(clone, build, test, publish),
	)

	_ = w.Do(context.Background())
	// Output:
	// clone
	// build
	// test
	// publish
}

// ExampleWorkflow_batchPipe shows BatchPipe — a shorthand for "every step
// in the next batch depends on every step in the previous one". This is
// the cleanest way to describe a fan-out / fan-in topology.
//
// Compare with ExampleWorkflow_dependsOn above: same graph, fewer edges to
// type out.
func ExampleWorkflow_batchPipe() {
	var (
		clone   = stage("clone")
		build   = stage("build")
		lint    = stage("lint")
		test    = stage("test")
		publish = stage("publish")
	)

	w := new(flow.Workflow)
	w.Add(
		flow.BatchPipe(
			flow.Steps(clone),
			flow.Steps(build, lint), // both depend on clone (in parallel)
			flow.Steps(test),        // waits for build AND lint
			flow.Steps(publish),
		),
	)

	_ = w.Do(context.Background())
	// Unordered output:
	// clone
	// build
	// lint
	// test
	// publish
}

// ExampleName shows how to give a Step a friendly display name. The name
// is what gets printed by `String()` — so it shows up in error messages
// (`ErrWorkflow`), in interceptor logs, and anywhere the library prints
// the Step.
//
// Useful when:
//   - your Step is an anonymous struct or third-party type with no good name;
//   - you want to disambiguate two instances of the same struct type;
//   - your scenario is auto-generated and you want to inject runtime context
//     into the name (use NameFunc / NameStringer for dynamic names).
func ExampleName() {
	// A bare struct without a String() method prints like *flow_test.compile.
	type compile struct{ flow.NoOpStep }
	step := &compile{}

	w := new(flow.Workflow)
	w.Add(
		// Wrap step in a NamedStep that prints "compile (release)" instead.
		flow.Name(step, "compile (release)"),
	)

	_ = w.Do(context.Background())
	// Reach back through the wrapper to print the registered Step's name.
	for _, s := range w.Steps() {
		fmt.Println(s)
	}
	// Output:
	// compile (release)
}
