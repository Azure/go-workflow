package flow_test

import (
	"context"
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
