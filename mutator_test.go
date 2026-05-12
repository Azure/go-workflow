package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mutFoo struct{ Field string }

func (*mutFoo) Do(context.Context) error { return nil }

type mutBar struct{}

func (*mutBar) Do(context.Context) error { return nil }

func TestMutate_matchesExactType(t *testing.T) {
	called := 0
	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		called++
		return nil
	})
	matched, target, b := m.applyTo(context.Background(), &mutFoo{})
	assert.True(t, matched)
	assert.NotNil(t, target)
	assert.Nil(t, b)
	assert.Equal(t, 1, called)
}

func TestMutate_skipsNonMatchingType(t *testing.T) {
	called := 0
	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		called++
		return nil
	})
	matched, target, b := m.applyTo(context.Background(), &mutBar{})
	assert.False(t, matched)
	assert.Nil(t, target)
	assert.Nil(t, b)
	assert.Equal(t, 0, called)
}

type mutWrapper struct{ inner Steper }

func (w *mutWrapper) Do(context.Context) error { return nil }
func (w *mutWrapper) Unwrap() Steper           { return w.inner }

func TestMutate_matchesInnerViaUnwrap(t *testing.T) {
	inner := &mutFoo{Field: "before"}
	wrapper := &mutWrapper{inner: inner}

	called := 0
	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		called++
		assert.Same(t, inner, f, "should receive the inner *mutFoo, not the wrapper")
		return nil
	})
	matched, target, _ := m.applyTo(context.Background(), wrapper)
	assert.True(t, matched)
	assert.Same(t, inner, target)
	assert.Equal(t, 1, called)
}

func TestMutate_outerWrapperWinsWhenItIsTheTarget(t *testing.T) {
	inner := &mutFoo{}
	wrapper := &mutWrapper{inner: inner}

	called := 0
	m := Mutate[*mutWrapper](func(ctx context.Context, w *mutWrapper) Builder {
		called++
		assert.Same(t, wrapper, w)
		return nil
	})
	matched, _, _ := m.applyTo(context.Background(), wrapper)
	assert.True(t, matched)
	assert.Equal(t, 1, called)
}

func TestMutate_doesNotCrossWorkflowBoundary(t *testing.T) {
	// A *Workflow sits between the outer step and the inner *mutFoo.
	// applyTo must NOT descend into it; that's what PrependMutators is for.
	innerFoo := &mutFoo{}
	innerWf := new(Workflow).Add(Step(innerFoo))

	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		t.Fatalf("mutator must not descend into nested workflow")
		return nil
	})
	matched, _, _ := m.applyTo(context.Background(), innerWf)
	assert.False(t, matched)
}

func TestMutate_doesNotCrossWorkflowBoundaryInsideWrapper(t *testing.T) {
	// wrapper -> *Workflow -> *mutFoo. The wrapper itself is not a workflow,
	// so Traverse will descend past it; the *Workflow must then halt the
	// descent so the inner *mutFoo is NOT reached.
	innerFoo := &mutFoo{}
	innerWf := new(Workflow).Add(Step(innerFoo))
	wrapper := &mutWrapper{inner: innerWf}

	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		t.Fatalf("mutator must not descend into nested workflow even when wrapped")
		return nil
	})
	matched, _, _ := m.applyTo(context.Background(), wrapper)
	assert.False(t, matched)
}

func TestState_MutatorsAppliedDefault(t *testing.T) {
	s := &State{}
	assert.False(t, s.MutatorsApplied())
}

func TestState_SetMutatorsApplied(t *testing.T) {
	s := &State{}
	s.SetMutatorsApplied()
	assert.True(t, s.MutatorsApplied())
}
