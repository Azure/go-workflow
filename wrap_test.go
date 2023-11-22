package flow

import (
	"context"
	"fmt"
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
		assert.Len(t, steps, 1)
		assert.True(t, step1 == steps[0])
	})
	t.Run("multi wrap", func(t *testing.T) {
		steps := As[*someStep](mStep)
		assert.Len(t, steps, 2)
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
	step1 := &someStep{value: "1"}
	step2 := &someStep{value: "2"}
	wStep1 := &wrappedStep{Steper: step1}
	mStep := &multiStep{steps: []Steper{step1, step2}}

	t.Run("single wrap", func(t *testing.T) {
		tree := make(StepTree)
		tree.Add(wStep1)
		assert.Len(t, tree, 2)
		assert.Equal(t, wStep1, tree.RootOf(step1))
		assert.Equal(t, wStep1, tree.RootOf(wStep1))
	})
	t.Run("multi wrap", func(t *testing.T) {
		tree := make(StepTree)
		tree.Add(mStep)
		assert.Len(t, tree, 3)
		assert.Equal(t, mStep, tree.RootOf(step1))
		assert.Equal(t, mStep, tree.RootOf(step2))
		assert.Equal(t, mStep, tree.RootOf(mStep))
	})
	t.Run("child then parent", func(t *testing.T) {
		tree := make(StepTree)
		tree.Add(step1)
		assert.Len(t, tree, 1)
		assert.Equal(t, step1, tree.RootOf(step1))
		tree.Add(wStep1)
		assert.Len(t, tree, 2)
		assert.Equal(t, wStep1, tree.RootOf(step1))
		assert.Equal(t, wStep1, tree.RootOf(wStep1))
	})
	t.Run("parent then child", func(t *testing.T) {
		tree := make(StepTree)
		tree.Add(wStep1)
		assert.Len(t, tree, 2)
		assert.Equal(t, wStep1, tree.RootOf(step1))
		assert.Equal(t, wStep1, tree.RootOf(wStep1))
		tree.Add(step1)
		assert.Len(t, tree, 2)
		assert.Equal(t, wStep1, tree.RootOf(step1))
		assert.Equal(t, wStep1, tree.RootOf(wStep1))
	})
	t.Run("nil", func(t *testing.T) {
		t.Run("add nil", func(t *testing.T) {
			tree := make(StepTree)
			new, olds := tree.Add(nil)
			assert.Nil(t, new)
			assert.Nil(t, olds)
		})
		t.Run("single wrap", func(t *testing.T) {
			tree := make(StepTree)
			wStepNil := &wrappedStep{nil}
			new, olds := tree.Add(wStepNil)
			assert.True(t, new == wStepNil)
			assert.Empty(t, olds)
			assert.Len(t, tree, 1)
		})
		t.Run("multi wrap", func(t *testing.T) {
			tree := make(StepTree)
			mStepNil := &multiStep{nil}
			new, olds := tree.Add(mStepNil)
			assert.True(t, new == mStepNil)
			assert.Empty(t, olds)
			assert.Len(t, tree, 1)
		})
		t.Run("multi wrap with nil", func(t *testing.T) {
			tree := make(StepTree)
			mStepNil := &multiStep{steps: []Steper{nil, step1}}
			new, olds := tree.Add(mStepNil)
			assert.True(t, new == mStepNil)
			assert.Empty(t, olds)
			assert.Len(t, tree, 2)
			assert.True(t, tree.RootOf(step1) == mStepNil)
		})
	})
	t.Run("escalate", func(t *testing.T) {
		t.Run("single wrap", func(t *testing.T) {
			tree := make(StepTree)
			new, olds := tree.Add(step1)
			assert.Len(t, tree, 1)
			assert.True(t, new == step1)
			assert.Empty(t, olds)
			new, olds = tree.Add(wStep1)
			assert.Len(t, tree, 2)
			assert.True(t, new == wStep1)
			assert.Contains(t, olds, step1)
		})
		t.Run("multi wrap", func(t *testing.T) {
			tree := make(StepTree)
			new, olds := tree.Add(step1)
			assert.Len(t, tree, 1)
			assert.Equal(t, step1, tree.RootOf(step1))
			assert.True(t, new == step1)
			assert.Empty(t, olds)
			new, olds = tree.Add(step2)
			assert.Len(t, tree, 2)
			assert.Equal(t, step2, tree.RootOf(step2))
			assert.True(t, new == step2)
			assert.Empty(t, olds)
			new, olds = tree.Add(mStep)
			assert.Len(t, tree, 3)
			assert.Equal(t, mStep, tree.RootOf(step1))
			assert.Equal(t, mStep, tree.RootOf(step2))
			assert.True(t, new == mStep)
			assert.Len(t, olds, 2)
			assert.Contains(t, olds, step1)
			assert.Contains(t, olds, step2)
		})
	})
}

func ExampleNamedStep() {
	step := &NamedStep{
		Name:   "hello",
		Steper: &someStep{value: "1"},
	}
	wStep := &wrappedStep{step}
	fmt.Println(String(nil))
	fmt.Println(String(wStep))
	fmt.Println(String(step))
	// Output:
	// <nil>
	// hello
	// hello
}
