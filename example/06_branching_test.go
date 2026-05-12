package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Branching: If / Switch
//
// **What you'll learn**
//   - `If` and `Switch` are not Steps — they are *control branches* that
//     evaluate a predicate and decide which downstream Step(s) run.
//   - The predicate sees the producer Step's typed Output, so the branch
//     can react to runtime data.
//
// **Mental model**
//
// `If(producer, predicate).Then(thenStep).Else(elseStep)` adds three Steps
// to the Workflow: producer, thenStep, and elseStep — wired so that exactly
// one of (thenStep, elseStep) runs and the other is Skipped.
//
//	  producer ── predicate(producer.Output) ──► true  ──► thenStep
//	                                       └──► false ──► elseStep
//
// `Switch(producer)` is the multi-way version: each `.Case(step, predicate)`
// declares one branch. Unlike a Go `switch`, cases are NOT exclusive:
// every case whose predicate returns true runs. Make your predicates
// mutually exclusive if you want only one branch to fire.

// ExampleIf shows a typical "load or create" pattern: load an item from a
// store, and if it doesn't exist yet, create it.
func ExampleIf() {
	var item string

	var (
		// Producer: returns true if `item` already exists.
		hasItem = flow.FuncO("HasItem", func(ctx context.Context) (bool, error) {
			return item != "", nil
		})
		create = flow.Func("Create", func(ctx context.Context) error {
			item = "new"
			fmt.Println("created")
			return nil
		})
		update = flow.Func("Update", func(ctx context.Context) error {
			item += " (updated)"
			fmt.Println("updated")
			return nil
		})
	)

	w := new(flow.Workflow).Add(
		flow.If(hasItem, func(ctx context.Context, f *flow.Function[struct{}, bool]) (bool, error) {
			return f.Output, nil
		}).
			Then(update). // run if hasItem.Output == true
			Else(create), // run if hasItem.Output == false
	)

	_ = w.Do(context.Background()) // first run: item is empty → Create
	_ = w.Do(context.Background()) // second run: item exists → Update
	fmt.Println("final:", item)
	// Output:
	// created
	// updated
	// final: new (updated)
}

// ExampleSwitch shows a multi-way branch. Cases are NOT exclusive — every
// predicate that returns true causes its Step to run. Use mutually
// exclusive predicates (or a chain of `.Case(...)` with disjoint ranges)
// when you want only one to fire.
func ExampleSwitch() {
	getAge := flow.FuncO("GetAge", func(ctx context.Context) (int, error) {
		return 25, nil
	})

	var (
		minor  = flow.Func("Minor", func(ctx context.Context) error { fmt.Println("minor"); return nil })
		adult  = flow.Func("Adult", func(ctx context.Context) error { fmt.Println("adult"); return nil })
		senior = flow.Func("Senior", func(ctx context.Context) error { fmt.Println("senior"); return nil })
	)

	// Mutually exclusive bands: 0–17, 18–64, 65+. Exactly one matches.
	band := func(min, max int) func(context.Context, *flow.Function[struct{}, int]) (bool, error) {
		return func(ctx context.Context, f *flow.Function[struct{}, int]) (bool, error) {
			return f.Output >= min && f.Output <= max, nil
		}
	}

	w := new(flow.Workflow).Add(
		flow.Switch(getAge).
			Case(minor, band(0, 17)).
			Case(adult, band(18, 64)).
			Case(senior, band(65, 200)),
	)

	_ = w.Do(context.Background())
	// Output:
	// adult
}
