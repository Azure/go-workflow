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
		var step *multiStep
		assert.False(t, As(step1, &step))
	})
	t.Run("single wrap", func(t *testing.T) {
		var step *someStep
		assert.True(t, As(wStep1, &step))
		assert.True(t, step1 == step)
	})
	t.Run("multi wrap", func(t *testing.T) {
		var step *someStep
		assert.True(t, As(mStep, &step))
		assert.True(t, step1 == step)
	})
	t.Run("nil step", func(t *testing.T) {
		var step *someStep
		assert.False(t, As(nil, &step))
		assert.Nil(t, step)
	})
	t.Run("nil target", func(t *testing.T) {
		assert.Panics(t, func() { As(step1, nil) })
	})
	t.Run("target must non-nil", func(t *testing.T) {
		var step *someStep
		assert.Panics(t, func() { As(step1, step) })
	})
	t.Run("target not ptr", func(t *testing.T) {
		assert.Panics(t, func() { As(step1, step1) })
	})
	t.Run("unwrap nil", func(t *testing.T) {
		var step *someStep
		assert.False(t, As(&wrappedStep{nil}, &step))
		assert.Nil(t, step)
	})
	t.Run("multi unwrap nil", func(t *testing.T) {
		var step *someStep
		assert.False(t, As(&multiStep{nil}, &step))
		assert.Nil(t, step)
		assert.False(t, As(&multiStep{steps: []Steper{nil}}, &step))
		assert.Nil(t, step)
	})
}

func TestStepTree(t *testing.T) {
	step1 := &someStep{value: "1"}
	step2 := &someStep{value: "2"}
	wStep1 := &wrappedStep{Steper: step1}
	mStep := &multiStep{steps: []Steper{step1, step2}}

	t.Run("nil", func(t *testing.T) {
		t.Run("add nil", func(t *testing.T) {
			tree := make(StepTree)
			assert.Nil(t, tree.Add(nil))
		})
		t.Run("single wrap", func(t *testing.T) {
			tree := make(StepTree)
			wStepNil := &wrappedStep{nil}
			assert.True(t, tree.Add(wStepNil) == wStepNil)
			assert.Len(t, tree, 1)
		})
		t.Run("multi wrap", func(t *testing.T) {
			tree := make(StepTree)
			mStepNil := &multiStep{nil}
			assert.True(t, tree.Add(mStepNil) == mStepNil)
			assert.Len(t, tree, 1)
		})
		t.Run("multi wrap with nil", func(t *testing.T) {
			tree := make(StepTree)
			mStepNil := &multiStep{steps: []Steper{nil, step1}}
			assert.True(t, tree.Add(mStepNil) == mStepNil)
			assert.Len(t, tree, 2)
			assert.True(t, tree.RootOf(step1) == mStepNil)
		})
	})
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
}
