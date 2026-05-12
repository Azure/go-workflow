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
//   - `MaxConcurrency` caps how many Steps run at the same time.
//   - `DontPanic` recovers panics inside Step bodies and converts them to
//     errors instead of crashing the program.
//
// **Other options (see godoc on `Workflow`)**
//   - `Clock`         — inject a deterministic clock for testing.
//   - `DefaultOption` — apply a `StepOption` (timeout, retry, …) to every
//                       Step. Per-Step options still win.
//   - `SkipAsError`   — treat `Skipped` Steps as workflow errors.

// ExampleWorkflow_MaxConcurrency caps parallelism. Steps that are eligible
// to run beyond the cap wait for a slot to free up.
func ExampleWorkflow_MaxConcurrency() {
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

	w := &flow.Workflow{MaxConcurrency: cap}
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
func ExampleWorkflow_DontPanic() {
	w := &flow.Workflow{DontPanic: true}
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
