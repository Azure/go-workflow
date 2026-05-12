// Examples of the flow.Mutator API.

package flow_test

import (
	"context"
	"fmt"
	"time"

	flow "github.com/Azure/go-workflow"
)

// ---------------------------------------------------------------------------
// Shared fixtures used by the examples below.
// ---------------------------------------------------------------------------

type Greet struct {
	Greeting string
	Who      string
	out      string
}

func (g *Greet) String() string { return "Greet" }
func (g *Greet) Do(ctx context.Context) error {
	g.out = fmt.Sprintf("%s, %s!", g.Greeting, g.Who)
	fmt.Println(g.out)
	return nil
}

type Farewell struct {
	Who string
}

func (f *Farewell) String() string { return "Farewell" }
func (f *Farewell) Do(ctx context.Context) error {
	fmt.Printf("Bye, %s!\n", f.Who)
	return nil
}

// ---------------------------------------------------------------------------
// Example 1 — a Mutator that fills in a default field across all *Greet steps.
//
// Equivalent legacy code would have been:
//
//   for _, g := range flow.As[*Greet](w) {
//       w.Add(flow.Step(g).Input(func(_ context.Context, g *Greet) error {
//           if g.Who == "" { g.Who = "world" }
//           return nil
//       }))
//   }
//
// With Mutate this becomes a single registration and works for steps added
// later (including inside sub-workflows) without a tree walk at registration
// time.
// ---------------------------------------------------------------------------

func ExampleMutate_input() {
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*Greet](func(_ context.Context, g *Greet) flow.Builder {
				return flow.Step(g).Input(func(_ context.Context, g *Greet) error {
					if g.Who == "" {
						g.Who = "world"
					}
					return nil
				})
			}),
		},
	}
	hello := &Greet{Greeting: "Hello"}
	bob := &Greet{Greeting: "Hi", Who: "Bob"}
	w.Add(
		flow.Pipe(hello, bob), // sequential so output order is deterministic
	)
	_ = w.Do(context.Background())
	// Output:
	// Hello, world!
	// Hi, Bob!
}

// ---------------------------------------------------------------------------
// Example 2 — A Mutator that overrides Retry and Timeout for every *Greet
// step. Demonstrates that a Mutator can return a Builder carrying *any* step
// configuration, not just Input.
// ---------------------------------------------------------------------------

func ExampleMutate_retryOverride() {
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*Greet](func(_ context.Context, g *Greet) flow.Builder {
				return flow.Step(g).
					Retry(func(o *flow.RetryOption) { o.Attempts = 3 }).
					Timeout(2 * time.Second)
			}),
		},
	}
	w.Add(flow.Step(&Greet{Greeting: "Hi", Who: "world"}))
	_ = w.Do(context.Background())
	// Output:
	// Hi, world!
}

// ---------------------------------------------------------------------------
// Example 3 — Mutator-as-pure-field-mutation (return nil Builder).
//
// When the only thing you need is to mutate fields on the typed step pointer
// (no callback registration), return nil. This is the cheapest form.
// ---------------------------------------------------------------------------

func ExampleMutate_nilBuilder() {
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*Greet](func(_ context.Context, g *Greet) flow.Builder {
				g.Greeting = "Hola" // direct field mutation
				return nil
			}),
		},
	}
	w.Add(flow.Step(&Greet{Greeting: "Hi", Who: "world"}))
	_ = w.Do(context.Background())
	// Output:
	// Hola, world!
}

// ---------------------------------------------------------------------------
// Example 4 — ctx received by the Mutator is the workflow-scoped ctx.
//
// Demonstrates the documented invariant that the Mutator's `ctx` is the same
// `ctx` passed to Workflow.Do. This is how production helpers fetch a logger,
// scenario name, test session ID, etc.
// ---------------------------------------------------------------------------

type ctxKey string

const greetingKey ctxKey = "greeting"

func ExampleMutate_ctx() {
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*Greet](func(ctx context.Context, g *Greet) flow.Builder {
				if v, ok := ctx.Value(greetingKey).(string); ok {
					g.Greeting = v
				}
				return nil
			}),
		},
	}
	w.Add(flow.Step(&Greet{Who: "world"}))

	ctx := context.WithValue(context.Background(), greetingKey, "Bonjour")
	_ = w.Do(ctx)
	// Output:
	// Bonjour, world!
}

// ---------------------------------------------------------------------------
// Example 5 — Multiple Mutators registered for the same type run in slice
// order; their Input callbacks are appended in that order and all run after
// any plan-declared Inputs.
// ---------------------------------------------------------------------------

func ExampleMutate_multipleInOrder() {
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*Greet](func(_ context.Context, g *Greet) flow.Builder {
				return flow.Step(g).Input(func(_ context.Context, g *Greet) error {
					g.Greeting += " (m1)"
					return nil
				})
			}),
			flow.Mutate[*Greet](func(_ context.Context, g *Greet) flow.Builder {
				return flow.Step(g).Input(func(_ context.Context, g *Greet) error {
					g.Greeting += " (m2)"
					return nil
				})
			}),
		},
	}
	w.Add(
		flow.Step(&Greet{Greeting: "Hi", Who: "world"}).
			Input(func(_ context.Context, g *Greet) error {
				g.Greeting += " (plan)"
				return nil
			}),
	)
	_ = w.Do(context.Background())
	// Plan callback runs first, then m1, then m2 — in declared order.
	// Output:
	// Hi (plan) (m1) (m2), world!
}

// ---------------------------------------------------------------------------
// Example 6 — Sub-workflow construction inside Do().
//
// This is the new pattern that replaces BuildStep. The composite step embeds
// flow.SubWorkflow (which gives it a stable inner *Workflow handle for
// PrependMutators propagation) but constructs its inner steps inside Do(),
// not at Add time. Parent Mutators still reach inner steps via PrependMutators.
// ---------------------------------------------------------------------------

type CompositeViaDo struct {
	flow.SubWorkflow // gives stable inner-workflow handle for Mutator propagation
	Hello            Greet
	Bye              Farewell
}

func (c *CompositeViaDo) String() string { return "CompositeViaDo" }
func (c *CompositeViaDo) Do(ctx context.Context) error {
	// Build the inner DAG lazily, here, inside Do — no BuildStep needed.
	c.Add(flow.Pipe(&c.Hello, &c.Bye))
	return c.SubWorkflow.Do(ctx)
}

func ExampleMutate_subWorkflow() {
	composite := &CompositeViaDo{
		Hello: Greet{Greeting: "Hello"},
		Bye:   Farewell{},
	}

	w := &flow.Workflow{
		// Parent-level Mutator. Reaches *Greet AND *Farewell inside the
		// composite step via the MutatorReceiver propagation mechanism:
		// before parent invokes composite.Do, runtime calls
		// composite.PrependMutators(parent.Mutators).
		Mutators: []flow.Mutator{
			flow.Mutate[*Greet](func(_ context.Context, g *Greet) flow.Builder {
				if g.Who == "" {
					g.Who = "world"
				}
				return nil
			}),
			flow.Mutate[*Farewell](func(_ context.Context, f *Farewell) flow.Builder {
				if f.Who == "" {
					f.Who = "world"
				}
				return nil
			}),
		},
	}
	w.Add(flow.Step(composite))
	_ = w.Do(context.Background())
	// Output:
	// Hello, world!
	// Bye, world!
}

// ---------------------------------------------------------------------------
// Example 7 — Mutator matches an inner type through the Unwrap chain.
//
// flow.Name("...", greet) returns a wrapper whose Unwrap() returns greet.
// flow.Mutate[*Greet] matches the inner *Greet via unwrap traversal; the
// resulting Builder's config is merged onto the wrapper's state entry, but
// the user function receives the typed *Greet pointer.
// ---------------------------------------------------------------------------

func ExampleMutate_unwrap() {
	greet := &Greet{Greeting: "Hi"}
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*Greet](func(_ context.Context, g *Greet) flow.Builder {
				g.Who = "world"
				return nil
			}),
		},
	}
	w.Add(flow.Name(greet, "named-greet"))
	_ = w.Do(context.Background())
	// Output:
	// Hi, world!
}
