package flow_test

import (
	"context"
	"fmt"
	"strings"

	flow "github.com/Azure/go-workflow"
)

// # Data flow: passing values between Steps
//
// **What you'll learn**
//   - The standard pattern: each Step exposes its inputs and outputs as
//     plain fields; `Input` callbacks copy upstream outputs into the
//     downstream's input fields right before `Do` runs.
//   - When you don't want to define a struct per Step, `flow.Func` /
//     `FuncIO` / `FuncI` / `FuncO` produce ready-made generic Steps
//     (`*flow.Function[I, O]`) that work with `Input` the same way.
//   - When `Input` callbacks run (after upstreams terminate, before `Do`).

// ExampleAddStep_Input shows the standard pattern, expanded into a 3-step
// pipeline that reads a feed, counts items, and announces the count.
//
// Each Step is a plain struct: inputs are fields filled by `Input`,
// outputs are fields written by `Do`. The downstream's `Input` callback
// reads from upstreams (captured by closure) and writes into the
// downstream's input fields.
//
// Why use Input rather than holding direct pointers to upstreams (as in
// 01_quickstart)? Two reasons:
//   - The Step stays self-contained: you can construct one and call its
//     `Do` directly in a unit test by setting the input fields yourself.
//   - The wiring lives next to `DependsOn`, so the data flow and the
//     dependency are declared together where you read the workflow.
func ExampleAddStep_Input() {
	feed := &fetchFeed{}
	count := &countItems{}
	announce := &announceCount{}

	w := new(flow.Workflow)
	w.Add(
		flow.Step(count).
			DependsOn(feed).
			Input(func(ctx context.Context, c *countItems) error {
				c.Body = feed.Body
				return nil
			}),
		flow.Step(announce).
			DependsOn(count).
			Input(func(ctx context.Context, a *announceCount) error {
				a.N = count.N
				return nil
			}),
	)

	_ = w.Do(context.Background())
	// Output:
	// found 3 items
}

type fetchFeed struct {
	Body string // output
}

func (f *fetchFeed) Do(ctx context.Context) error {
	f.Body = "item\nitem\nitem\nfooter" // pretend this is an HTTP fetch.
	return nil
}

type countItems struct {
	Body string // input — copied in by Input callback
	N    int    // output
}

func (c *countItems) Do(ctx context.Context) error {
	for _, line := range strings.Split(c.Body, "\n") {
		if line == "item" {
			c.N++
		}
	}
	return nil
}

type announceCount struct {
	N int // input — copied in by Input callback
}

func (a *announceCount) Do(ctx context.Context) error {
	fmt.Printf("found %d items\n", a.N)
	return nil
}

// ExampleFunction_inputOutput shows the convenience variant — when you
// don't want to declare a struct just to define a Step body. `flow.Func`
// and friends produce a generic `*flow.Function[I, O]` whose `Input`
// field is the typed input and `Output` field is the typed output:
//
//	flow.Func    — no input, no output (just a Do function)
//	flow.FuncO   — no input, typed output
//	flow.FuncI   — typed input, no output
//	flow.FuncIO  — typed input, typed output
//
// Mechanics are exactly the same as the struct version above: the
// `Input` callback runs after upstreams terminate, and you copy the
// values across.
func ExampleFunction_inputOutput() {
	var (
		fetch = flow.FuncO("FetchFeed", func(ctx context.Context) (string, error) {
			return "item\nitem\nfooter", nil
		})
		count = flow.FuncIO("CountItems", func(ctx context.Context, body string) (int, error) {
			n := 0
			for _, line := range strings.Split(body, "\n") {
				if line == "item" {
					n++
				}
			}
			return n, nil
		})
		announce = flow.FuncI("Announce", func(ctx context.Context, n int) error {
			fmt.Printf("found %d items\n", n)
			return nil
		})
	)

	w := new(flow.Workflow)
	w.Add(
		flow.Step(count).
			DependsOn(fetch).
			Input(func(ctx context.Context, f *flow.Function[string, int]) error {
				f.Input = fetch.Output
				return nil
			}),
		flow.Step(announce).
			DependsOn(count).
			Input(func(ctx context.Context, f *flow.Function[int, struct{}]) error {
				f.Input = count.Output
				return nil
			}),
	)

	_ = w.Do(context.Background())
	// Output:
	// found 2 items
}
