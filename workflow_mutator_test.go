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

func TestWorkflow_PrependMutators(t *testing.T) {
	m1 := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })
	m2 := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })

	w := &flow.Workflow{Mutators: []flow.Mutator{m2}}
	w.PrependMutators([]flow.Mutator{m1})

	assert.Len(t, w.Mutators, 2)
}

func TestWorkflow_PrependMutatorsNilOrEmpty(t *testing.T) {
	w := &flow.Workflow{}
	w.PrependMutators(nil)
	assert.Empty(t, w.Mutators)
	w.PrependMutators([]flow.Mutator{})
	assert.Empty(t, w.Mutators)
}

func TestSubWorkflow_PrependMutators(t *testing.T) {
	type sub struct{ flow.SubWorkflow }
	s := &sub{}
	m := flow.Mutate[*wfFoo](func(ctx context.Context, f *wfFoo) flow.Builder { return nil })

	// MutatorReceiver must be implemented
	var _ flow.MutatorReceiver = s
	s.PrependMutators([]flow.Mutator{m})
	// No panic / no error → behaviour verified by integration test in Task 7
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
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, gg *wfGreet) flow.Builder {
				called++
				return flow.Step(gg).Input(func(_ context.Context, gg *wfGreet) error {
					gg.Who = "world"
					return nil
				})
			}),
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
	w := &flow.Workflow{} // Mutators == nil
	w.Add(flow.Step(g))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, "Bob", g.Who)
}

type wfComposite struct {
	flow.SubWorkflow
	Inner wfGreet
}

func (c *wfComposite) Do(ctx context.Context) error {
	// Lazy build inside Do — replaces BuildStep pattern.
	c.Add(flow.Step(&c.Inner))
	return c.SubWorkflow.Do(ctx)
}

func TestMutator_reachesIntoSubWorkflow(t *testing.T) {
	c := &wfComposite{Inner: wfGreet{Greeting: "Hello"}}
	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
				g.Who = "world"
				return nil
			}),
		},
	}
	w.Add(flow.Step(c))

	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "world", c.Inner.Who, "parent Mutator must reach inner step via PrependMutators")
}

func TestMutator_reachesIntoNestedWorkflow(t *testing.T) {
	innerG := &wfGreet{Greeting: "Hi"}
	innerW := new(flow.Workflow).Add(flow.Step(innerG))

	w := &flow.Workflow{
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
				g.Who = "world"
				return nil
			}),
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
		Mutators: []flow.Mutator{
			flow.Mutate[*wfGreet](func(ctx context.Context, g *wfGreet) flow.Builder {
				called++
				g.Who = "lazy"
				return nil
			}),
		},
	}
	w.Add(flow.Step(c))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, 1, called)
	assert.Equal(t, "lazy", c.Inner.Who)
}
