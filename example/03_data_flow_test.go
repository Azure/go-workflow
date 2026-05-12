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
//   - Two ways to flow data between Steps:
//     1. **Direct field access** (recommended when you can): the downstream
//        Step holds a pointer to the upstream Step; in `Do` it reads the
//        upstream's exported fields.
//     2. **`Input` / `Output` callbacks**: useful when the downstream
//        cannot or should not hold a typed reference to its upstreams
//        (e.g. plug-in pipelines wired at runtime, or Steps configured
//        without compile-time type knowledge of each other).
//   - When `Input` callbacks run (after upstreams terminate, before `Do`).

// ExampleSteper_directFields shows the recommended pattern: data flows
// through plain struct fields. The downstream Step holds pointers to its
// upstreams; when its `Do` runs, the upstreams have already terminated and
// their result fields are populated.
//
// This is the same pattern you saw in 01_quickstart, expanded into a
// 3-step pipeline that reads a feed, counts items, and announces the
// count.
func ExampleSteper_directFields() {
	// Construct each step. Downstream steps take pointers to upstream
	// steps; data will flow through those pointers, not through callbacks.
	feed := &fetchFeed{}
	count := &countItems{Feed: feed}
	announce := &announceCount{Count: count}

	w := new(flow.Workflow)
	w.Add(
		flow.Pipe(feed, count, announce),
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
	Feed *fetchFeed // upstream — we read from Feed.Body in Do.
	N    int        // output
}

func (c *countItems) Do(ctx context.Context) error {
	for _, line := range strings.Split(c.Feed.Body, "\n") {
		if line == "item" {
			c.N++
		}
	}
	return nil
}

type announceCount struct {
	Count *countItems // upstream
}

func (a *announceCount) Do(ctx context.Context) error {
	fmt.Printf("found %d items\n", a.Count.N)
	return nil
}

// ExampleAddStep_Input shows the alternative — `Input` callbacks. Reach
// for this when:
//
//   - the downstream Step is a generic helper that doesn't know its
//     upstreams' concrete types,
//   - the wiring is built at runtime by a different layer of code,
//   - or you simply prefer keeping the data wiring next to the
//     `DependsOn` declaration.
//
// `Input(fn)` registers `fn` to run after every upstream has terminated
// and before the Step's `Do` is called, so the upstream values are safe
// to read inside fn.
//
// `flow.Func / FuncIO / FuncI / FuncO` are convenience wrappers that
// produce a generic `*flow.Function[I, O]` step — they pair naturally
// with `Input` because the callback can mutate `f.Input` directly.
func ExampleAddStep_Input() {
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
				f.Input = fetch.Output // upstream output is ready when Input runs
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
