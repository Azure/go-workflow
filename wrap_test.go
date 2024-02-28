package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type someStep struct{ value string }
type wrappedStep struct{ Steper }
type multiStep struct{ steps []Steper }

func (s *someStep) Do(ctx context.Context) error  { return nil }
func (w *wrappedStep) Unwrap() Steper             { return w.Steper }
func (m *multiStep) Unwrap() []Steper             { return m.steps }
func (m *multiStep) Do(ctx context.Context) error { return nil }

func TestIs(t *testing.T) {
	step1 := &someStep{value: "1"}
	step2 := &someStep{value: "2"}
	wStep1 := &wrappedStep{Steper: step1}
	mStep := &multiStep{steps: []Steper{step1, step2}}
	assert.True(t, Is[*someStep](step1))
	assert.True(t, Is[*someStep](step2))
	assert.True(t, Is[*someStep](wStep1))
	assert.True(t, Is[*someStep](mStep))

	assert.False(t, Is[*wrappedStep](step1))
	assert.False(t, Is[*wrappedStep](step2))
	assert.True(t, Is[*wrappedStep](wStep1))
	assert.False(t, Is[*wrappedStep](mStep))

	assert.False(t, Is[*multiStep](step1))
	assert.False(t, Is[*multiStep](step2))
	assert.False(t, Is[*multiStep](wStep1))
	assert.True(t, Is[*multiStep](mStep))

	t.Run("is nil", func(t *testing.T) {
		assert.False(t, Is[*someStep](nil))
		assert.False(t, Is[*wrappedStep](nil))
		assert.False(t, Is[*multiStep](nil))
		assert.False(t, Is[*someStep](&wrappedStep{nil}))
		assert.False(t, Is[*someStep](&multiStep{nil}))
		assert.False(t, Is[*someStep](&multiStep{steps: []Steper{nil}}))
	})
}

func TestAs(t *testing.T) {
	step1 := &someStep{value: "1"}
	step2 := &someStep{value: "2"}
	wStep1 := &wrappedStep{Steper: step1}
	mStep := &multiStep{steps: []Steper{step1, step2}}

	t.Run("no wrap", func(t *testing.T) {
		assert.Nil(t, As[*multiStep](step1))
	})
	t.Run("single wrap", func(t *testing.T) {
		steps := As[*someStep](wStep1)
		if assert.Len(t, steps, 1) {
			assert.True(t, step1 == steps[0])
		}
	})
	t.Run("multi wrap", func(t *testing.T) {
		steps := As[*someStep](mStep)
		assert.ElementsMatch(t, []Steper{step1, step2}, steps)
	})
	t.Run("nil step", func(t *testing.T) {
		assert.Nil(t, As[*someStep](nil))
	})
	t.Run("unwrap nil", func(t *testing.T) {
		steps := As[*someStep](&wrappedStep{nil})
		assert.Nil(t, steps)
	})
	t.Run("multi unwrap nil", func(t *testing.T) {
		assert.Nil(t, As[*someStep](&multiStep{nil}))
		assert.Nil(t, As[*someStep](&multiStep{steps: []Steper{nil}}))
	})
}

func TestStepTree(t *testing.T) {
	a := &someStep{value: "a"}
	b := &someStep{value: "b"}
	A := &wrappedStep{Steper: a}
	ab := &multiStep{steps: []Steper{a, b}}
	Ab := &multiStep{steps: []Steper{A, b}}

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
		})
		t.Run("long chain", func(t *testing.T) {
			tree := make(StepTree)
			w := &wrappedStep{
				&wrappedStep{
					&wrappedStep{
						a,
					},
				},
			}
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
			wab := &wrappedStep{ab}
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
		B := &wrappedStep{b}
		aB := &multiStep{steps: []Steper{a, B}}
		t.Run("add", func(t *testing.T) {
			tree := make(StepTree)
			tree.Add(Ab)
			assert.Panics(t, func() {
				tree.Add(B)
			})
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
		assert.False(t, IsStep(nil, &someStep{}))
		assert.False(t, IsStep(&someStep{}, nil))
	})
	t.Run("single wrap", func(t *testing.T) {
		var (
			s = &someStep{value: "1"}
			w = &wrappedStep{s}
		)
		assert.True(t, IsStep(w, s))
		assert.False(t, IsStep(s, w))
	})
	t.Run("multi wrap", func(t *testing.T) {
		var (
			s1 = &someStep{value: "1"}
			s2 = &someStep{value: "2"}
			m  = &multiStep{steps: []Steper{s1, s2}}
		)
		assert.True(t, IsStep(m, s1))
		assert.True(t, IsStep(m, s2))
		assert.False(t, IsStep(s1, s2))
		assert.False(t, IsStep(s2, s1))
		assert.False(t, IsStep(s1, m))
		assert.False(t, IsStep(s2, m))
	})
}
