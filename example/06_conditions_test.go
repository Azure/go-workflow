package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Conditions: deciding whether a Step runs
//
// **What you'll learn**
//   - Every Step has a Condition that decides — based on its upstreams'
//     terminal status — whether the Step `Running` or settles inline as
//     `Skipped` / `Canceled`.
//   - Built-in conditions: `AllSucceeded` (default), `AllSucceededOrSkipped`,
//     `Always`, `AnyFailed`, `BeCanceled`.
//   - Use `flow.Skip(err)` / `flow.Cancel(err)` inside `Do` to settle a Step
//     yourself.
//
// **Status state machine**
//
// Steps move through these states. Only `Pending` is queueable; the four
// terminal states are mutually exclusive.
//
//	Pending ─► Running ─► Succeeded | Failed
//	  │
//	  └──► Skipped | Canceled (decided by Condition before Running)
//
// **Why this matters**
//
// Steps that are settled inline (Skipped/Canceled by Condition) DO NOT enter
// the interceptor chain or consume a MaxConcurrency lease. So conditions are
// the right place to short-circuit work — not the wrong-fitting AfterStep
// or a bail-out inside Do.

// ExampleCondition_default shows the default condition: a Step runs only if
// every upstream Succeeded. If anything else terminated upstream, the Step
// is Skipped.
func ExampleCondition_default() {
	var (
		ok       = flow.Func("ok", func(ctx context.Context) error { return nil })
		boom     = flow.Func("boom", func(ctx context.Context) error { return fmt.Errorf("boom") })
		downstream = flow.Func("downstream", func(ctx context.Context) error {
			fmt.Println("downstream ran")
			return nil
		})
	)

	w := new(flow.Workflow)
	w.Add(
		flow.Step(downstream).DependsOn(ok, boom), // default = AllSucceeded
	)
	_ = w.Do(context.Background())

	fmt.Println("downstream:", w.StateOf(downstream).GetStatus())
	// Output:
	// downstream: Skipped
}

// ExampleCondition_anyFailed shows a recovery / cleanup pattern: a Step
// that runs *because* something upstream failed.
func ExampleCondition_anyFailed() {
	var (
		ok      = flow.Func("ok", func(ctx context.Context) error { return nil })
		boom    = flow.Func("boom", func(ctx context.Context) error { return fmt.Errorf("boom") })
		recover = flow.Func("recover", func(ctx context.Context) error {
			fmt.Println("recover ran")
			return nil
		})
	)

	w := new(flow.Workflow)
	w.Add(
		flow.Step(recover).
			DependsOn(ok, boom).
			When(flow.AnyFailed),
	)
	_ = w.Do(context.Background())
	// Output:
	// recover ran
}

// ExampleCondition_skipFromDo shows how to settle a Step as Skipped from
// inside `Do` (for example, "this run has nothing to do for these inputs").
// Wrap the cause with `flow.Skip(err)`. Use `flow.Cancel(err)` similarly to
// mark a Step as Canceled.
//
// Skipped is the polite way to say "I had nothing to do" — it is NOT a
// failure, and downstreams with the default condition (AllSucceeded) will
// also Skip rather than Run. Set Workflow.SkipAsError = true if you want
// Skipped to count as a workflow error.
func ExampleCondition_skipFromDo() {
	var nothing = flow.Func("nothing", func(ctx context.Context) error {
		return flow.Skip(fmt.Errorf("no work for now"))
	})

	w := new(flow.Workflow)
	w.Add(flow.Step(nothing))

	err := w.Do(context.Background()) // returns nil; Skipped is not surfaced.
	fmt.Println("err:", err)
	fmt.Println("status:", w.StateOf(nothing).GetStatus())
	// Output:
	// err: <nil>
	// status: Skipped
}
