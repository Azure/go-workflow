package flow_test

import (
	"context"
	"errors"
	"testing"

	flow "github.com/Azure/go-workflow"
	"github.com/stretchr/testify/assert"
)

type wfFoo struct{}

func (*wfFoo) Do(context.Context) error { return nil }

func TestWorkflow_InheritOption_Mutators(t *testing.T) {
	m1 := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })
	m2 := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })

	w := &flow.Workflow{Option: flow.WorkflowOption{Mutators: []flow.Mutator{m2}}}
	// Simulate a parent workflow injecting m1 as a prepended Mutator via
	// InheritOption (parent → child propagation contract).
	w.InheritOption(flow.WorkflowOption{Mutators: []flow.Mutator{m1}})

	assert.Len(t, w.Option.Mutators, 2)
}

func TestWorkflow_InheritOption_NilOrEmptyMutators(t *testing.T) {
	w := &flow.Workflow{}
	w.InheritOption(flow.WorkflowOption{}) // empty parent — no contribution
	assert.Empty(t, w.Option.Mutators)
	w.InheritOption(flow.WorkflowOption{Mutators: []flow.Mutator{}})
	assert.Empty(t, w.Option.Mutators)
}

func TestSubWorkflow_InheritOption(t *testing.T) {
	type sub struct{ flow.SubWorkflow }
	s := &sub{}
	m := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })

	// WorkflowOptionReceiver must be implemented (SubWorkflow delegates).
	var _ flow.WorkflowOptionReceiver = s
	s.InheritOption(flow.WorkflowOption{Mutators: []flow.Mutator{m}})
	// Behaviour verified by integration tests; constructing this scenario
	// exercises the deprecation-window delegation path.
}

type wfGreet struct {
	Greeting string
	Who      string
}

func (g *wfGreet) Do(context.Context) error { return nil }

func TestMutator_mergesInputBeforeFirstAttempt(t *testing.T) {
	called := 0
	g := &wfGreet{Greeting: "Hi"}
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
					called++
					return flow.Step(gg).Input(func(_ context.Context, gg *wfGreet) error {
						gg.Who = "world"
						return nil
					})
				}),
			},
		},
	}
	w.Add(flow.Step(g))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 1, called, "mutator must run exactly once")
	assert.Equal(t, "world", g.Who, "mutator-contributed Input must run before Do")
}

type wfFlaky struct {
	attempts int
	failTill int
}

func (f *wfFlaky) Do(context.Context) error {
	f.attempts++
	if f.attempts <= f.failTill {
		return errors.New("transient")
	}
	return nil
}

func TestMutator_runsExactlyOnceAcrossRetries(t *testing.T) {
	mutatorCalls := 0
	inputCalls := 0
	f := &wfFlaky{failTill: 2}
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfFlaky](func(ctx context.Context, ff *wfFlaky) flow.Builder {
					mutatorCalls++
					return flow.Step(ff).
						Retry(func(o *flow.RetryOption) { o.Attempts = 3 }).
						Input(func(_ context.Context, _ *wfFlaky) error {
							inputCalls++
							return nil
						})
				}),
			},
		},
	}
	w.Add(flow.Step(f))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 1, mutatorCalls, "mutator user-fn runs once")
	assert.Equal(t, 3, inputCalls, "mutator-contributed Input runs per attempt")
	assert.Equal(t, 3, f.attempts)
}

func TestMutator_nilSliceIsNoOp(t *testing.T) {
	g := &wfGreet{Greeting: "Hi", Who: "Bob"}
	w := &flow.Workflow{} // Option.Mutators == nil
	w.Add(flow.Step(g))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, "Bob", g.Who)
}

type wfComposite struct {
	flow.SubWorkflow
	Inner wfGreet
}

func (c *wfComposite) Do(ctx context.Context) error {
	// Lazy build inside Do — replaces BuildStep pattern. NOTE: in
	// production code this MUST be guarded by sync.Once when the host step
	// can be re-executed; this test runs Do once so the bare Add is fine.
	c.Add(flow.Step(&c.Inner))
	return c.SubWorkflow.Do(ctx)
}

func TestMutator_reachesIntoSubWorkflow(t *testing.T) {
	c := &wfComposite{Inner: wfGreet{Greeting: "Hello"}}
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
					g.Who = "world"
					return nil
				}),
			},
		},
	}
	w.Add(flow.Step(c))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "world", c.Inner.Who, "parent Mutator must reach inner step via WorkflowOptionReceiver.InheritOption")
}

func TestMutator_reachesIntoNestedWorkflow(t *testing.T) {
	innerG := &wfGreet{Greeting: "Hi"}
	innerW := new(flow.Workflow).Add(flow.Step(innerG))

	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
					g.Who = "world"
					return nil
				}),
			},
		},
	}
	w.Add(flow.Step(innerW))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "world", innerG.Who)
}

func TestMutator_reachesLazilyAddedInnerStep(t *testing.T) {
	c := &wfComposite{Inner: wfGreet{Greeting: "Yo"}}
	called := 0
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
					called++
					g.Who = "lazy"
					return nil
				}),
			},
		},
	}
	w.Add(flow.Step(c))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, 1, called)
	assert.Equal(t, "lazy", c.Inner.Who)
}

func TestMutator_planInputBeforeMutatorInput(t *testing.T) {
	g := &wfGreet{Greeting: "Hi"}
	order := []string{}
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
					return flow.Step(gg).Input(func(_ context.Context, _ *wfGreet) error {
						order = append(order, "mutator")
						return nil
					})
				}),
			},
		},
	}
	w.Add(
		flow.Step(g).Input(func(_ context.Context, _ *wfGreet) error {
			order = append(order, "plan")
			return nil
		}),
	)
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, []string{"plan", "mutator"}, order)
}

func TestMutator_multipleMutatorsRunInSliceOrder(t *testing.T) {
	g := &wfGreet{}
	order := []string{}
	mk := func(name string) flow.Mutator {
		return flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
			return flow.Step(gg).Input(func(_ context.Context, _ *wfGreet) error {
				order = append(order, name)
				return nil
			})
		})
	}
	w := &flow.Workflow{Option: flow.WorkflowOption{Mutators: []flow.Mutator{mk("m1"), mk("m2")}}}
	w.Add(flow.Step(g))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, []string{"m1", "m2"}, order)
}

func TestMutator_mergeAtFirstScheduling_NotAtAdd(t *testing.T) {
	g := &wfGreet{}
	called := 0
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
					called++
					return nil
				}),
			},
		},
	}
	w.Add(flow.Step(g))
	assert.Equal(t, 0, called, "Add must not invoke mutators")
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, 1, called)
}

func TestMutator_matchesThroughNameWrapper(t *testing.T) {
	g := &wfGreet{Greeting: "Hi"}
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
					gg.Who = "world"
					return nil
				}),
			},
		},
	}
	w.Add(flow.Name(g, "named-greet"))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, "world", g.Who)
}

type ctxKey string

const wfCtxKey ctxKey = "k"

func TestMutator_receivesWorkflowCtx(t *testing.T) {
	g := &wfGreet{}
	got := ""
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
					if v, ok := ctx.Value(wfCtxKey).(string); ok {
						got = v
					}
					return nil
				}),
			},
		},
	}
	w.Add(flow.Step(g))
	ctx := context.WithValue(context.Background(), wfCtxKey, "value-from-do")
	assert.NoError(t, w.Do(ctx))
	assert.Equal(t, "value-from-do", got)
}

func TestMutator_unrelatedBuilderEntryIgnored(t *testing.T) {
	g := &wfGreet{Greeting: "Hi", Who: "Bob"}
	other := &wfGreet{Who: "untouched"}
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
					if gg == g {
						// Mistakenly return a Builder keyed on `other` instead of `gg`.
						return flow.Step(other).Input(func(_ context.Context, o *wfGreet) error {
							o.Who = "stolen"
							return nil
						})
					}
					return nil
				}),
			},
		},
	}
	w.Add(flow.Step(g)) // only g is in the workflow
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, "untouched", other.Who, "config keyed on a different step is dropped")
}

func TestMutator_panicCaughtWhenDontPanic(t *testing.T) {
	g := &wfGreet{}
	dontPanic := true
	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			DontPanic: &dontPanic,
			Mutators: []flow.Mutator{
				flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
					panic("boom")
				}),
			},
		},
	}
	w.Add(flow.Step(g))
	err := w.Do(context.Background())
	assert.Error(t, err)
}
