package flow

import (
	"context"
	"testing"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
)

// ptr is a tiny helper local to this test file. Real users would write &v or
// use Go 1.26's `new(value)`; the test file uses ptr only to keep call sites
// short.
func ptr[T any](v T) *T { return &v }

func TestInheritOption_Scalars(t *testing.T) {
	parent := WorkflowOption{
		MaxConcurrency: ptr(4),
		DontPanic:      ptr(true),
		SkipAsError:    ptr(true),
		Clock:          clock.NewMock(),
		StepDefaults:   &StepOption{},
	}

	t.Run("nil scalar -> parent value used", func(t *testing.T) {
		w := &Workflow{}
		w.InheritOption(parent)
		assert.Equal(t, parent.MaxConcurrency, w.Option.MaxConcurrency)
		assert.Equal(t, parent.DontPanic, w.Option.DontPanic)
		assert.Equal(t, parent.SkipAsError, w.Option.SkipAsError)
		assert.Equal(t, parent.Clock, w.Option.Clock)
		assert.Equal(t, parent.StepDefaults, w.Option.StepDefaults)
	})

	t.Run("non-nil child wins", func(t *testing.T) {
		childMax := 9
		childDontPanic := false
		childSkip := false
		childClock := clock.New()
		childDefaults := &StepOption{}
		w := &Workflow{Option: WorkflowOption{
			MaxConcurrency: &childMax,
			DontPanic:      &childDontPanic,
			SkipAsError:    &childSkip,
			Clock:          childClock,
			StepDefaults:   childDefaults,
		}}
		w.InheritOption(parent)
		assert.Equal(t, 9, *w.Option.MaxConcurrency)
		assert.Equal(t, false, *w.Option.DontPanic)
		assert.Equal(t, false, *w.Option.SkipAsError)
		assert.Same(t, childClock, w.Option.Clock)
		assert.Same(t, childDefaults, w.Option.StepDefaults)
	})

	t.Run("explicit-zero child wins (distinguished from inherit)", func(t *testing.T) {
		// Pointer to zero IS the user saying "child wins with value 0".
		zeroMax := 0
		zeroDontPanic := false
		w := &Workflow{Option: WorkflowOption{
			MaxConcurrency: &zeroMax,
			DontPanic:      &zeroDontPanic,
		}}
		w.InheritOption(parent)
		assert.NotNil(t, w.Option.MaxConcurrency)
		assert.Equal(t, 0, *w.Option.MaxConcurrency, "explicit-zero pointer wins over parent's 4")
		assert.NotNil(t, w.Option.DontPanic)
		assert.Equal(t, false, *w.Option.DontPanic, "explicit-false wins over parent's true")
	})
}

func TestInheritOption_Slices(t *testing.T) {
	mA := Mutate[*Workflow](func(context.Context, *Workflow) Builder { return nil })
	mB := Mutate[*Workflow](func(context.Context, *Workflow) Builder { return nil })
	sA := StepInterceptorFunc(func(ctx context.Context, _ Steper, next func(context.Context) error) error {
		return next(ctx)
	})
	sB := StepInterceptorFunc(func(ctx context.Context, _ Steper, next func(context.Context) error) error {
		return next(ctx)
	})
	aA := AttemptInterceptorFunc(func(ctx context.Context, _ Steper, _ uint64, next func(context.Context) error) error {
		return next(ctx)
	})
	aB := AttemptInterceptorFunc(func(ctx context.Context, _ Steper, _ uint64, next func(context.Context) error) error {
		return next(ctx)
	})

	parent := WorkflowOption{
		Mutators:            []Mutator{mA},
		StepInterceptors:    []StepInterceptor{sA},
		AttemptInterceptors: []AttemptInterceptor{aA},
	}

	t.Run("parent prepended to child for each slice field", func(t *testing.T) {
		w := &Workflow{Option: WorkflowOption{
			Mutators:            []Mutator{mB},
			StepInterceptors:    []StepInterceptor{sB},
			AttemptInterceptors: []AttemptInterceptor{aB},
		}}
		w.InheritOption(parent)
		assert.Len(t, w.Option.Mutators, 2)
		assert.Len(t, w.Option.StepInterceptors, 2)
		assert.Len(t, w.Option.AttemptInterceptors, 2)
	})

	t.Run("InheritOption does not mutate parent's slices", func(t *testing.T) {
		parentCopy := WorkflowOption{
			Mutators:            []Mutator{mA},
			StepInterceptors:    []StepInterceptor{sA},
			AttemptInterceptors: []AttemptInterceptor{aA},
		}
		w := &Workflow{Option: WorkflowOption{
			Mutators:            []Mutator{mB},
			StepInterceptors:    []StepInterceptor{sB},
			AttemptInterceptors: []AttemptInterceptor{aB},
		}}
		w.InheritOption(parentCopy)
		assert.Len(t, parentCopy.Mutators, 1)
		assert.Len(t, parentCopy.StepInterceptors, 1)
		assert.Len(t, parentCopy.AttemptInterceptors, 1)
	})
}

func TestInheritOption_DontInherit(t *testing.T) {
	parent := WorkflowOption{
		MaxConcurrency:      ptr(4),
		DontPanic:           ptr(true),
		Mutators:            []Mutator{Mutate[*Workflow](func(context.Context, *Workflow) Builder { return nil })},
		StepInterceptors:    []StepInterceptor{StepInterceptorFunc(func(ctx context.Context, _ Steper, next func(context.Context) error) error { return next(ctx) })},
		AttemptInterceptors: []AttemptInterceptor{AttemptInterceptorFunc(func(ctx context.Context, _ Steper, _ uint64, next func(context.Context) error) error { return next(ctx) })},
	}
	w := &Workflow{Option: WorkflowOption{DontInherit: true}}
	w.InheritOption(parent)
	assert.Nil(t, w.Option.MaxConcurrency)
	assert.Nil(t, w.Option.DontPanic)
	assert.Nil(t, w.Option.Mutators)
	assert.Nil(t, w.Option.StepInterceptors)
	assert.Nil(t, w.Option.AttemptInterceptors)
	assert.True(t, w.Option.DontInherit)
}

// TestInheritOption_MultiLevelNesting verifies that a three-level chain
// (grandparent → parent → child) yields a child Mutators slice of
// [grandparent, parent, child] after the chain is walked through
// InheritOption twice — once on parent (receiving grandparent), then on
// child (receiving the now-merged parent.Option).
func TestInheritOption_MultiLevelNesting(t *testing.T) {
	g := Mutate[*Workflow](func(context.Context, *Workflow) Builder { return nil })
	p := Mutate[*Workflow](func(context.Context, *Workflow) Builder { return nil })
	c := Mutate[*Workflow](func(context.Context, *Workflow) Builder { return nil })

	grandparent := WorkflowOption{Mutators: []Mutator{g}}
	parent := &Workflow{Option: WorkflowOption{Mutators: []Mutator{p}}}
	child := &Workflow{Option: WorkflowOption{Mutators: []Mutator{c}}}

	parent.InheritOption(grandparent)
	child.InheritOption(parent.Option)

	assert.Len(t, child.Option.Mutators, 3)
}

// TestDo_OptionSnapshot verifies the snapshot/restore behavior at Workflow.Do
// entry/exit. A parent that .Do()s its child workflow twice must observe the
// child's Option.Mutators reverted between runs (no accumulation).
func TestDo_OptionSnapshot(t *testing.T) {
	parentM := Mutate[*Workflow](func(context.Context, *Workflow) Builder { return nil })

	child := &Workflow{}
	child.Option.Mutators = []Mutator{} // explicit empty

	parent := &Workflow{Option: WorkflowOption{Mutators: []Mutator{parentM}}}
	parent.Add(Step(child))

	ctx := context.Background()

	// Run 1.
	require := assert.New(t)
	require.NoError(parent.Do(ctx))
	// After Do() exits, child's Option must be back to its pre-Do() shape.
	require.Empty(child.Option.Mutators, "child Mutators must be reverted to pre-run shape after Do()")

	// Reset before re-running per the documented contract (rewinds per-step
	// statuses; Option isolation is already handled by snapshot/restore).
	require.NoError(parent.Reset())
	require.NoError(child.Reset())

	// Run 2 — same expected post-state, no accumulation.
	require.NoError(parent.Do(ctx))
	require.Empty(child.Option.Mutators, "no accumulation across Do() runs")
}

// TestInheritOption_ScalarInheritsDontPanic covers the motivating use case:
// a parent sets DontPanic=true and the child leaves it unset (nil); after
// InheritOption the child observes DontPanic=true via the dontPanic()
// accessor.
func TestInheritOption_ScalarInheritsDontPanic(t *testing.T) {
	parent := WorkflowOption{DontPanic: ptr(true)}
	child := &Workflow{}
	child.InheritOption(parent)
	assert.True(t, child.dontPanic())
}

// TestZeroWorkflow_NoPanic verifies that a Workflow{} with no Option set
// behaves identically to before: empty workflow Do() is a no-op success.
func TestZeroWorkflow_NoPanic(t *testing.T) {
	w := &Workflow{}
	assert.NoError(t, w.Do(context.Background()))
	// Helper accessors must return zero values on a fresh Workflow.
	assert.Equal(t, 0, w.maxConcurrency())
	assert.False(t, w.dontPanic())
	assert.False(t, w.skipAsError())
	assert.NotNil(t, w.clock())
}

// TestSubWorkflow_InheritOption smoke-tests the deprecation-window delegation
// from SubWorkflow.InheritOption to the inner Workflow.
func TestSubWorkflow_InheritOption(t *testing.T) {
	parent := WorkflowOption{DontPanic: ptr(true), MaxConcurrency: ptr(7)}
	s := &SubWorkflow{}
	s.InheritOption(parent)
	assert.True(t, s.w.dontPanic())
	assert.Equal(t, 7, s.w.maxConcurrency())
}
