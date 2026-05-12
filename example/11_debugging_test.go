package flow_test

import (
	"context"
	"errors"
	"fmt"
	"sort"

	flow "github.com/Azure/go-workflow"
)

// # Debugging: figuring out what failed and why
//
// **What you'll learn**
//   - `Workflow.Do` returns `flow.ErrWorkflow` — a `map[Steper]StepResult`
//     keyed by failed Step. Iterate to print them all, or use `errors.As`.
//   - Use `Workflow.StateOf(step).GetStatus()` to inspect any Step's
//     terminal status post-run.
//   - For per-Step structured logging, prefer an interceptor (10_observability)
//     over `AfterStep` so the same logger applies to every Step.
//
// **The two questions you'll typically ask**
//
//	1. Which Steps failed?  ─► iterate ErrWorkflow.
//	2. What status did Step X end in?  ─► Workflow.StateOf(X).GetStatus().

// ExampleErrWorkflow shows how to inspect the Workflow's error after Do.
// Several Steps fail in different ways; iterating ErrWorkflow gives you
// the per-Step error breakdown.
//
// Note: ErrWorkflow contains an entry for *every* Step in the Workflow,
// including the ones that succeeded. Filter on `result.Err != nil` (or
// `result.Status != flow.Succeeded`) to focus on the failures.
func ExampleErrWorkflow() {
	w := new(flow.Workflow)
	w.Add(
		flow.Step(flow.Func("a", func(context.Context) error { return errors.New("disk full") })),
		flow.Step(flow.Func("b", func(context.Context) error { return errors.New("403 forbidden") })),
		flow.Step(flow.Func("c", func(context.Context) error { return nil })), // succeeds
	)

	err := w.Do(context.Background())

	var ew flow.ErrWorkflow
	if errors.As(err, &ew) {
		// Sort keys so the godoc output is deterministic.
		var names []string
		byName := map[string]flow.StepResult{}
		for step, result := range ew {
			if result.Err == nil {
				continue
			}
			name := fmt.Sprint(step)
			names = append(names, name)
			byName[name] = result
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("%s: %s — %v\n", name, byName[name].Status, byName[name].Err)
		}
	}
	// Output:
	// a: Failed — disk full
	// b: Failed — 403 forbidden
}

// ExampleWorkflow_StateOf shows how to inspect any individual Step's state
// after the Workflow has run, without going through the error.
func ExampleWorkflow_StateOf() {
	var (
		ok      = flow.Func("ok",      func(context.Context) error { return nil })
		boom    = flow.Func("boom",    func(context.Context) error { return errors.New("boom") })
		downstream = flow.Func("downstream", func(context.Context) error { return nil })
	)

	w := new(flow.Workflow)
	w.Add(
		flow.Step(downstream).DependsOn(ok, boom), // default condition: skipped because boom failed
	)
	_ = w.Do(context.Background())

	for _, step := range []flow.Steper{ok, boom, downstream} {
		fmt.Printf("%s: %s\n", step, w.StateOf(step).GetStatus())
	}
	// Output:
	// ok: Succeeded
	// boom: Failed
	// downstream: Skipped
}
