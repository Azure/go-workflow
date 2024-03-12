package flow

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type wrappedStep struct{ Steper }
type multiStep struct{ steps []Steper }

func wrap(s Steper) *wrappedStep                  { return &wrappedStep{s} }
func multi(ss ...Steper) *multiStep               { return &multiStep{steps: ss} }
func (w *wrappedStep) Unwrap() Steper             { return w.Steper }
func (w *wrappedStep) String() string             { return strings.ToUpper(String(w.Steper)) }
func (m *multiStep) Unwrap() []Steper             { return m.steps }
func (m *multiStep) Do(ctx context.Context) error { return nil }

func TestIs(t *testing.T) {
	var (
		a  = NoOp("a")
		b  = NoOp("b")
		A  = wrap(a)
		ab = multi(a, b)
	)
	assert.True(t, Is[*NoOpStep](a))
	assert.True(t, Is[*NoOpStep](b))
	assert.True(t, Is[*NoOpStep](A))
	assert.True(t, Is[*NoOpStep](ab))

	assert.False(t, Is[*wrappedStep](a))
	assert.False(t, Is[*wrappedStep](b))
	assert.True(t, Is[*wrappedStep](A))
	assert.False(t, Is[*wrappedStep](ab))

	assert.False(t, Is[*multiStep](a))
	assert.False(t, Is[*multiStep](b))
	assert.False(t, Is[*multiStep](A))
	assert.True(t, Is[*multiStep](ab))

	t.Run("is nil", func(t *testing.T) {
		assert.False(t, Is[*NoOpStep](nil))
		assert.False(t, Is[*wrappedStep](nil))
		assert.False(t, Is[*multiStep](nil))
		assert.False(t, Is[*NoOpStep](wrap(nil)))
		assert.False(t, Is[*NoOpStep](multi(nil, nil)))
		assert.False(t, Is[*NoOpStep](multi()))
	})
}

func TestAs(t *testing.T) {
	var (
		a  = NoOp("a")
		b  = NoOp("b")
		A  = wrap(a)
		ab = multi(a, b)
	)

	t.Run("no wrap", func(t *testing.T) {
		assert.Nil(t, As[*multiStep](a))
	})
	t.Run("single wrap", func(t *testing.T) {
		steps := As[*NoOpStep](A)
		if assert.Len(t, steps, 1) {
			assert.True(t, a == steps[0])
		}
	})
	t.Run("multi wrap", func(t *testing.T) {
		steps := As[*NoOpStep](ab)
		assert.ElementsMatch(t, []Steper{a, b}, steps)
	})
	t.Run("nil step", func(t *testing.T) {
		assert.Nil(t, As[*NoOpStep](nil))
	})
	t.Run("unwrap nil", func(t *testing.T) {
		steps := As[*NoOpStep](&wrappedStep{nil})
		assert.Nil(t, steps)
	})
	t.Run("multi unwrap nil", func(t *testing.T) {
		assert.Nil(t, As[*NoOpStep](&multiStep{nil}))
		assert.Nil(t, As[*NoOpStep](&multiStep{steps: []Steper{nil}}))
	})
}

func TestStepTree(t *testing.T) {
	var (
		a  = NoOp("a")
		b  = NoOp("b")
		A  = wrap(a)
		ab = multi(a, b)
		Ab = multi(A, b)
	)

	t.Run("nil", func(t *testing.T) {
		tree := make(StepTree)
		assert.False(t, tree.IsRoot(nil))
		assert.Nil(t, tree.RootOf(nil))
	})
	t.Run("no wrap", func(t *testing.T) {
		t.Run("add one step", func(t *testing.T) {
			tree := make(StepTree)
			assert.Empty(t, tree.Add(a))
			assert.Equal(t, a, tree[a])
		})
		t.Run("add an existing step", func(t *testing.T) {
			tree := make(StepTree)
			tree.Add(a)
			assert.Empty(t, tree.Add(a))
			assert.Len(t, tree, 1)
			assert.Equal(t, a, tree[a])
		})
		t.Run("add two different steps", func(t *testing.T) {
			tree := make(StepTree)
			tree.Add(a)
			assert.Empty(t, tree.Add(b))
			assert.Equal(t, a, tree[a])
			assert.Equal(t, b, tree[b])
		})
	})
	t.Run("single wrap", func(t *testing.T) {
		t.Run("add wrap step", func(t *testing.T) {
			tree := make(StepTree)
			assert.Empty(t, tree.Add(A))
			assert.Len(t, tree, 2)
			assert.Equal(t, A, tree[A])
			assert.Equal(t, A, tree[a])
		})
		t.Run("add inner then wrap", func(t *testing.T) {
			tree := make(StepTree)
			tree.Add(a)
			olds := tree.Add(A)
			assert.Len(t, olds, 1)
			assert.Contains(t, olds, a)
			assert.Len(t, tree, 2)
			assert.Equal(t, A, tree[A])
			assert.Equal(t, A, tree[a])
		})
		t.Run("add wrap then inner", func(t *testing.T) {
			tree := make(StepTree)
			assert.Empty(t, tree.Add(A))
			assert.Len(t, tree, 2)
			assert.Empty(t, tree.Add(a))
			assert.Len(t, tree, 2)
			assert.Equal(t, A, tree[A])
			assert.Equal(t, A, tree[a])
			assert.Len(t, tree.Roots(), 1)
		})
		t.Run("long chain", func(t *testing.T) {
			tree := make(StepTree)
			w := wrap(wrap(wrap(a)))
			tree.Add(w)
			assert.Len(t, tree, 4)
			assert.Equal(t, w, tree[w])
			assert.Equal(t, w, tree[w.Steper])
			assert.Equal(t, w.Steper, tree[w.Steper.(*wrappedStep).Steper])
			assert.Equal(t, w.Steper.(*wrappedStep).Steper, tree[a])
		})
	})
	t.Run("multi wrap", func(t *testing.T) {
		t.Run("add multi wrap", func(t *testing.T) {
			tree := make(StepTree)
			assert.Empty(t, tree.Add(ab))
			assert.Len(t, tree, 3)
			assert.Equal(t, ab, tree[a])
			assert.Equal(t, ab, tree[b])
			assert.Equal(t, ab, tree[ab])
		})
		t.Run("first inner then multi wrap", func(t *testing.T) {
			tree := make(StepTree)
			tree.Add(a)
			olds := tree.Add(ab)
			assert.Len(t, olds, 1)
			assert.Contains(t, olds, a)
			assert.Len(t, tree, 3)
			assert.Equal(t, ab, tree[a])
			assert.Equal(t, ab, tree[b])
			assert.Equal(t, ab, tree[ab])
		})
		t.Run("first multi then inner", func(t *testing.T) {
			tree := make(StepTree)
			tree.Add(ab)
			assert.Empty(t, tree.Add(a))
			assert.Empty(t, tree.Add(b))
			assert.Len(t, tree, 3)
			assert.Equal(t, ab, tree[a])
			assert.Equal(t, ab, tree[b])
			assert.Equal(t, ab, tree[ab])
		})
		t.Run("single wrap multi", func(t *testing.T) {
			wab := wrap(ab)
			tree := make(StepTree)
			tree.Add(ab)
			tree.Add(wab)
			assert.Len(t, tree, 4)
			assert.Equal(t, wab, tree[wab])
			assert.Equal(t, wab, tree[ab])
			assert.Equal(t, ab, tree[a])
			assert.Equal(t, ab, tree[b])
		})
	})
	t.Run("conflict", func(t *testing.T) {
		B := wrap(b)
		aB := multi(a, B)
		t.Run("add", func(t *testing.T) {
			tree := make(StepTree)
			tree.Add(Ab)
			err := ErrWrappedStepAlreadyInTree{
				StepAlreadyThere: b,
				NewAncestor:      B,
				OldAncestor:      Ab,
			}
			assert.PanicsWithValue(t, err, func() {
				tree.Add(B)
			})
			assert.ErrorContains(t, err,
				`add step "B" failed: inner step "b" already has an ancestor "[A, b]"`)
		})
		t.Run("add multi", func(t *testing.T) {
			tree := make(StepTree)
			tree.Add(Ab)
			assert.Panics(t, func() {
				tree.Add(aB)
			})
		})
	})
}

func TestIsStep(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		assert.True(t, IsStep(nil, nil))
		assert.False(t, IsStep(nil, &NoOpStep{}))
		assert.False(t, IsStep(&NoOpStep{}, nil))
	})
	t.Run("single wrap", func(t *testing.T) {
		var (
			a = NoOp("a")
			A = wrap(a)
		)
		assert.True(t, IsStep(A, a))
		assert.False(t, IsStep(a, A))
	})
	t.Run("multi wrap", func(t *testing.T) {
		var (
			a  = NoOp("a")
			b  = NoOp("b")
			ab = multi(a, b)
		)
		assert.True(t, IsStep(ab, a))
		assert.True(t, IsStep(ab, b))
		assert.False(t, IsStep(a, b))
		assert.False(t, IsStep(b, a))
		assert.False(t, IsStep(a, ab))
		assert.False(t, IsStep(b, ab))
	})
}

func TestString(t *testing.T) {
	var (
		a  = NoOp("a")
		b  = NoOp("b")
		ab = multi(a, b)
	)
	assert.Equal(t, "<nil>", String(nil))
	assert.Equal(t, "a", String(a))
	assert.Equal(t, "[a, b]", String(ab))
}
