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
//   - Use `Input` / `Output` callbacks to flow data between Steps.
//   - Use `Func` / `FuncIO` / `FuncI` / `FuncO` to define Steps with typed
//     Input and Output without writing your own struct.
//   - Why Input runs at runtime (not at Add time) — and what that buys you.
//
// **When Input runs**
//
// `Step(d).Input(fn)` registers `fn` to run after every upstream of `d`
// has terminated and before `d.Do` is called. It can read any upstream's
// Output safely, because the runtime has already published those values
// when fn fires.
//
// **The scenario**
//
// A 3-step pipeline that reads a feed, counts items, and announces the
// count. Each step's Output is the next step's Input.
func ExampleFunction_inputOutput() {
	var (
		// FuncO: no input, returns a string. Pretend this is an HTTP fetch.
		fetchFeed = flow.FuncO("FetchFeed", func(ctx context.Context) (string, error) {
			return "item\nitem\nitem\nfooter", nil
		})

		// FuncIO: takes one typed input, returns one typed output.
		countItems = flow.FuncIO("CountItems", func(ctx context.Context, body string) (int, error) {
			n := 0
			for _, line := range strings.Split(body, "\n") {
				if line == "item" {
					n++
				}
			}
			return n, nil
		})

		// FuncI: takes one typed input, returns no output (just an error).
		announce = flow.FuncI("Announce", func(ctx context.Context, n int) error {
			fmt.Printf("found %d items\n", n)
			return nil
		})
	)

	w := new(flow.Workflow)
	w.Add(
		// countItems consumes fetchFeed's Output. The Input callback runs
		// after fetchFeed finishes, so .Output is ready to read.
		flow.Step(countItems).
			DependsOn(fetchFeed).
			Input(func(ctx context.Context, f *flow.Function[string, int]) error {
				f.Input = fetchFeed.Output
				return nil
			}),
		flow.Step(announce).
			DependsOn(countItems).
			Input(func(ctx context.Context, f *flow.Function[int, struct{}]) error {
				f.Input = countItems.Output
				return nil
			}),
	)

	_ = w.Do(context.Background())
	// Output:
	// found 3 items
}

// ExampleFunction_inputOutput_multipleUpstreams shows that Input is the
// natural place to *combine* values from several upstreams. The downstream
// Step depends on two producers, and its Input callback merges both into a
// single typed Input struct.
func ExampleFunction_inputOutput_multipleUpstreams() {
	var (
		fetchUser = flow.FuncO("FetchUser", func(ctx context.Context) (string, error) {
			return "Alice", nil
		})
		fetchOrg = flow.FuncO("FetchOrg", func(ctx context.Context) (string, error) {
			return "Acme", nil
		})

		// The downstream takes a struct that combines both upstreams.
		introduce = flow.FuncI("Introduce", func(ctx context.Context, in person) error {
			fmt.Printf("%s @ %s\n", in.Name, in.Org)
			return nil
		})
	)

	w := new(flow.Workflow)
	w.Add(
		flow.Step(introduce).
			DependsOn(fetchUser, fetchOrg).
			Input(func(ctx context.Context, f *flow.Function[person, struct{}]) error {
				f.Input = person{Name: fetchUser.Output, Org: fetchOrg.Output}
				return nil
			}),
	)

	_ = w.Do(context.Background())
	// Output:
	// Alice @ Acme
}

// person is the typed Input for the introduce step. Naming the struct keeps
// the generics signature short and the data-flow obvious in the body.
type person struct {
	Name string
	Org  string
}
