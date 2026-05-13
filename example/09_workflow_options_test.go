package flow_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	flow "github.com/Azure/go-workflow"
)

// # Workflow options: tuning execution
//
// **What you'll learn**
//   - `Option.MaxConcurrency` caps how many Steps run at the same time.
//   - `Option.DontPanic` recovers panics inside Step bodies and converts
//     them to errors instead of crashing the program.
//   - All workflow-level options live under the named `Option` field
//     (type `WorkflowOption`). Scalar fields are pointer-typed so unset /
//     explicit-zero are distinguishable.
//
// **Other options (see godoc on `WorkflowOption`)**
//   - `Option.Clock`         — inject a deterministic clock for testing.
//   - `Option.StepDefaults`  — apply a `StepOption` (timeout, retry, …) to
//                              every Step. Per-Step options still win.
//   - `Option.SkipAsError`   — treat `Skipped` Steps as workflow errors.

// ExampleWorkflow_MaxConcurrency caps parallelism. Steps that are eligible
// to run beyond the cap wait for a slot to free up.
func ExampleWorkflowOption_MaxConcurrency() {
	const cap = 2

	var live atomic.Int32
	var maxObserved atomic.Int32
	gate := make(chan struct{}) // released when we observe `cap` running

	work := func(name string) *flow.Function[struct{}, struct{}] {
		return flow.Func(name, func(ctx context.Context) error {
			n := live.Add(1)
			defer live.Add(-1)
			for {
				cur := maxObserved.Load()
				if n <= cur || maxObserved.CompareAndSwap(cur, n) {
					break
				}
			}
			// First `cap` Steps block until the gate is released; this
			// proves the limiter actually serializes the rest.
			if n <= cap {
				<-gate
			}
			return nil
		})
	}

	mc := cap
	w := &flow.Workflow{Option: flow.WorkflowOption{MaxConcurrency: &mc}}
	w.Add(flow.Steps(work("a"), work("b"), work("c"), work("d"), work("e")))

	go func() {
		// Wait until we've actually seen `cap` live, then release everyone.
		for maxObserved.Load() < cap {
		}
		close(gate)
	}()

	_ = w.Do(context.Background())
	fmt.Println("max concurrent:", maxObserved.Load())
	// Output:
	// max concurrent: 2
}

// ExampleWorkflow_DontPanic enables panic recovery. A panicking Step is
// reported as a normal `Failed` Step instead of crashing the program.
func ExampleWorkflowOption_DontPanic() {
	dontPanic := true
	w := &flow.Workflow{Option: flow.WorkflowOption{DontPanic: &dontPanic}}
	w.Add(
		flow.Step(flow.Func("oops", func(context.Context) error {
			panic("boom")
		})),
	)

	err := w.Do(context.Background())

	var ew flow.ErrWorkflow
	fmt.Println("ErrWorkflow?", errors.As(err, &ew))
	// Output:
	// ErrWorkflow? true
}

// ExampleWorkflowOption_inheritance shows how scalar Option fields
// propagate from a parent Workflow into a sub-Workflow that left them
// unset (nil pointer). Once you set DontPanic on the parent, every nested
// child workflow recovers panics too — no need to restate the option on
// every sub-workflow.
//
// To opt out of inheritance entirely, set Option.DontInherit = true on
// the child. To override just one field, set that field's pointer on the
// child (an explicit `&zero` pointer is distinguishable from "unset").
func ExampleWorkflowOption_inheritance() {
	// Inner workflow: leaves DontPanic unset.
	inner := new(flow.Workflow)
	inner.Add(flow.Step(flow.Func("boom", func(ctx context.Context) error {
		panic("inner panic")
	})))

	// Outer workflow sets DontPanic at the top level. The inner sub-workflow
	// inherits it via WorkflowOptionReceiver.InheritOption.
	dontPanic := true
	outer := &flow.Workflow{Option: flow.WorkflowOption{DontPanic: &dontPanic}}
	outer.Add(flow.Step(inner))

	err := outer.Do(context.Background())
	fmt.Println("recovered as error:", err != nil)
	// Output:
	// recovered as error: true
}
