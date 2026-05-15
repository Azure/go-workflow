package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Mutators: cross-cutting configuration by Step type
//
// **What you'll learn**
//   - Register a `flow.Mutator` on a `Workflow` to contribute configuration
//     to every Step of a chosen Go type, without naming each Step by hand.
//   - A Mutator can mutate fields on the typed Step pointer, return a
//     `flow.Builder` to register `Input` / `BeforeStep` / `AfterStep` /
//     `Retry` / `Timeout` / ..., or both.
//   - Parent-Workflow Mutators reach into sub-Workflows automatically — the
//     same WorkflowOptionReceiver mechanism described in 11_observability for
//     interceptors.
//
// **Where they fit**
//
//	Mutator   (workflow-level, by Go type — this file)
//	  runs once per Step, at first schedule, BEFORE any callbacks.
//	  ── plan-declared Input / BeforeStep callbacks
//	      ── Mutator-contributed Input / BeforeStep callbacks (appended)
//	          ── step.Do(ctx)
//
// **When to reach for which mechanism**
//
//	Need behaviour for one specific Step?            → BeforeStep / AfterStep
//	                                                   (05_callbacks).
//	Need behaviour for every Step in the Workflow?   → Interceptor
//	                                                   (11_observability).
//	Need behaviour for every Step of a given type,
//	even Steps added later or inside sub-workflows?  → Mutator (this file).
//
// **Use case in this file**
//
// We're building a release pipeline whose individual Steps post structured
// notifications (`*Notify`) to a chat system. The notification target
// (channel, on-call rotation) and retry policy are environment-specific,
// and we don't want every Step author to remember to set them. A single
// `Mutate[*Notify](…)` on the Workflow takes care of that.

// Notify is a plain Step. Authors fill in only the message-shaped fields
// (Title, Body); cross-cutting fields (Channel, RetryPolicy) are populated
// by a Mutator at run time.
type Notify struct {
	Title   string
	Body    string
	Channel string // "" by default; filled in by the Mutator
}

func (n *Notify) String() string { return "Notify(" + n.Title + ")" }
func (n *Notify) Do(ctx context.Context) error {
	fmt.Printf("[%s] %s — %s\n", n.Channel, n.Title, n.Body)
	return nil
}

// ExampleMutator_fieldDefaults is the simplest form: a Mutator that mutates
// fields on the typed Step pointer and returns nil. Use this when the only
// thing you need is "fill in a default if the author didn't set one".
//
// The Mutator is invoked exactly once per matched Step, just before its
// first attempt — so it can read fields that earlier steps set, but the
// changes are applied before any Input / BeforeStep / Do runs.
func ExampleMutator_fieldDefaults() {
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				// Anywhere a *Notify shows up, default Channel if it's empty.
				flow.Mutate[*Notify](func(_ context.Context, n *Notify) flow.Builder {
					if n.Channel == "" {
						n.Channel = "#release"
					}
					return nil // pure field mutation; no Builder needed
				}),
			},
		},
	}
	w.Add(
		flow.Pipe(
			&Notify{Title: "deploy started", Body: "v1.2.3"},
			&Notify{Title: "smoke ok", Body: "all green"},
			// This one overrides the default.
			&Notify{Title: "rollback!", Body: "v1.2.3 → v1.2.2", Channel: "#oncall"},
		),
	)
	_ = w.Do(context.Background())
	// Output:
	// [#release] deploy started — v1.2.3
	// [#release] smoke ok — all green
	// [#oncall] rollback! — v1.2.3 → v1.2.2
}

// ExampleMutator_contributeConfig shows the more powerful form: the Mutator
// returns a `flow.Builder` (the same builder you get from `flow.Step(…)`),
// and any configuration on that Builder is merged into the matched Step.
//
// Here we tag every *Notify with a uniform "[prod]" prefix via a BeforeStep
// callback. Step authors don't have to remember it, and Ops can change the
// tag in one place. The same shape works for Retry, Timeout, Input,
// AfterStep, etc.
func ExampleMutator_contributeConfig() {
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*Notify](func(_ context.Context, n *Notify) flow.Builder {
					if n.Channel == "" {
						n.Channel = "#release"
					}
					// Same builder methods you use at Add() time — BeforeStep,
					// AfterStep, Input, Retry, Timeout, ... — all work here.
					return flow.Step(n).BeforeStep(func(ctx context.Context, s flow.Steper) (context.Context, error) {
						s.(*Notify).Title = "[prod] " + s.(*Notify).Title
						return ctx, nil
					})
				}),
			},
		},
	}
	w.Add(flow.Step(&Notify{Title: "promo live", Body: "v2"}))
	_ = w.Do(context.Background())
	// Output:
	// [#release] [prod] promo live — v2
}

// ExampleMutator_ctxValue shows that the Mutator's ctx is the same ctx
// passed to `Workflow.Do(ctx)`. This is how a Mutator can pull
// environment-specific configuration off the context: a per-test
// scenario name, a build ID, an on-call channel resolved at runtime, ...
func ExampleMutator_ctxValue() {
	type envKey struct{}

	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*Notify](func(ctx context.Context, n *Notify) flow.Builder {
					if env, ok := ctx.Value(envKey{}).(string); ok && n.Channel == "" {
						// e.g. #release-prod vs #release-dev
						n.Channel = "#release-" + env
					}
					return nil
				}),
			},
		},
	}
	w.Add(flow.Step(&Notify{Title: "deploy started", Body: "v1.2.3"}))

	ctx := context.WithValue(context.Background(), envKey{}, "prod")
	_ = w.Do(ctx)
	// Output:
	// [#release-prod] deploy started — v1.2.3
}

// ExampleMutator_subWorkflow shows that parent-Workflow Mutators reach
// inner Steps automatically — even when those Steps live inside a
// sub-Workflow, and even when the sub-Workflow adds them lazily at run
// time.
//
// The propagation mechanism is the same `WorkflowOptionReceiver` interface
// used by interceptors in 11_observability: any Step that contains a
// sub-Workflow (a `*Workflow` used as a Step, or a struct embedding the
// deprecated `flow.SubWorkflow`) automatically receives the parent's Option
// (Mutators included) before the inner Workflow starts scheduling.
func ExampleMutator_subWorkflow() {
	// A "release stage" sub-workflow that posts a start and an end notice.
	stage := new(flow.Workflow).Add(
		flow.Pipe(
			&Notify{Title: "stage:start", Body: "build"},
			&Notify{Title: "stage:end", Body: "build"},
		),
	)

	outer := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*Notify](func(_ context.Context, n *Notify) flow.Builder {
					if n.Channel == "" {
						n.Channel = "#release"
					}
					return nil
				}),
			},
		},
	}
	outer.Add(
		flow.Pipe(
			&Notify{Title: "pipeline start", Body: "v1"},
			stage, // a sub-Workflow used as a Step
			&Notify{Title: "pipeline end", Body: "v1"},
		),
	)
	_ = outer.Do(context.Background())
	// Output:
	// [#release] pipeline start — v1
	// [#release] stage:start — build
	// [#release] stage:end — build
	// [#release] pipeline end — v1
}

// ExampleMutator_multipleInOrder shows two boundaries it's useful to know
// about:
//
//   - Multiple Mutators registered for the same type are applied in the
//     order they appear in `Workflow.Mutators`. Their Input callbacks are
//     appended to the Step's chain in that same order.
//   - Plan-declared Input callbacks (the ones you write at `Add()` time)
//     ALWAYS run before any Mutator-contributed Input callbacks.
//
// This means: a Mutator can rely on plan Inputs having already run, and
// later Mutators can rely on earlier Mutators having already run.
func ExampleMutator_multipleInOrder() {
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*Notify](func(_ context.Context, n *Notify) flow.Builder {
					return flow.Step(n).Input(func(_ context.Context, n *Notify) error {
						n.Body += " [m1]"
						return nil
					})
				}),
				flow.Mutate[*Notify](func(_ context.Context, n *Notify) flow.Builder {
					return flow.Step(n).Input(func(_ context.Context, n *Notify) error {
						n.Body += " [m2]"
						return nil
					})
				}),
			},
		},
	}
	w.Add(
		flow.Step(&Notify{Title: "deploy", Body: "v1", Channel: "#release"}).
			Input(func(_ context.Context, n *Notify) error {
				n.Body += " [plan]"
				return nil
			}),
	)
	_ = w.Do(context.Background())
	// Output:
	// [#release] deploy — v1 [plan] [m1] [m2]
}
