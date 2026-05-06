package flow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
)

// resetBuildStep is a spy Step that records whether Reset and BuildStep are called,
// and in which order.
type resetBuildStep struct {
	calls []string
}

func (r *resetBuildStep) Do(ctx context.Context) error { return nil }

func (r *resetBuildStep) Reset() {
	r.calls = append(r.calls, "Reset")
}

func (r *resetBuildStep) BuildStep() {
	r.calls = append(r.calls, "BuildStep")
}

type wrappedStep struct{ Steper }
type multiStep struct{ steps []Steper }

func wrap(s Steper) *wrappedStep                  { return &wrappedStep{s} }
func multi(ss ...Steper) *multiStep               { return &multiStep{steps: ss} }
func (w *wrappedStep) Unwrap() Steper             { return w.Steper }
func (w *wrappedStep) String() string             { return strings.ToUpper(String(w.Steper)) }
func (m *multiStep) Unwrap() []Steper             { return m.steps }
func (m *multiStep) Do(ctx context.Context) error { return nil }

func TestHas(t *testing.T) {
	var (
		a  = NoOp("a")
		b  = NoOp("b")
		A  = wrap(a)
		ab = multi(a, b)
	)
	assert.True(t, Has[*NoOpStep](a))
	assert.True(t, Has[*NoOpStep](b))
	assert.True(t, Has[*NoOpStep](A))
	assert.True(t, Has[*NoOpStep](ab))

	assert.False(t, Has[*wrappedStep](a))
	assert.False(t, Has[*wrappedStep](b))
	assert.True(t, Has[*wrappedStep](A))
	assert.False(t, Has[*wrappedStep](ab))

	assert.False(t, Has[*multiStep](a))
	assert.False(t, Has[*multiStep](b))
	assert.False(t, Has[*multiStep](A))
	assert.True(t, Has[*multiStep](ab))

	t.Run("is nil", func(t *testing.T) {
		assert.False(t, Has[*NoOpStep](nil))
		assert.False(t, Has[*wrappedStep](nil))
		assert.False(t, Has[*multiStep](nil))
		assert.False(t, Has[*NoOpStep](wrap(nil)))
		assert.False(t, Has[*NoOpStep](multi(nil, nil)))
		assert.False(t, Has[*NoOpStep](multi()))
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

func TestHasStep(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		assert.False(t, HasStep(nil, nil))
		assert.False(t, HasStep(nil, &NoOpStep{}))
		assert.False(t, HasStep(&NoOpStep{}, nil))
	})
	t.Run("single wrap", func(t *testing.T) {
		var (
			a = NoOp("a")
			A = wrap(a)
		)
		assert.True(t, HasStep(A, a))
		assert.False(t, HasStep(a, A))
	})
	t.Run("multi wrap", func(t *testing.T) {
		var (
			a  = NoOp("a")
			b  = NoOp("b")
			ab = multi(a, b)
		)
		assert.True(t, HasStep(ab, a))
		assert.True(t, HasStep(ab, b))
		assert.False(t, HasStep(a, b))
		assert.False(t, HasStep(b, a))
		assert.False(t, HasStep(a, ab))
		assert.False(t, HasStep(b, ab))
	})
}

func TestString(t *testing.T) {
	var (
		a  = NoOp("a")
		b  = NoOp("b")
		A  = wrap(a)
		ab = multi(a, b)
	)
	assert.Equal(t, "<nil>", String(nil))
	assert.Equal(t, "a", String(a))
	assert.Equal(t, "A", String(A))
	assert.Contains(t, String(ab), "*flow.multiStep")
	assert.Contains(t, String(ab), " {\n\ta\n\tb\n}")
}

func TestBuildStep(t *testing.T) {
	t.Run("Reset called before BuildStep", func(t *testing.T) {
		s := &resetBuildStep{}
		_ = new(Workflow).Add(Step(s))
		assert.Equal(t, []string{"Reset", "BuildStep"}, s.calls)
	})
}

func TestSubWorkflow_InterceptorPropagation(t *testing.T) {
	t.Parallel()

	var events []WorkflowEvent
	mu := sync.Mutex{}
	sink := NewStepEventSink(func(e WorkflowEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	innerStep := NoOp("inner")
	type mySubStep struct{ SubWorkflow }
	sub := &mySubStep{}
	sub.Add(Step(innerStep))

	w := &Workflow{
		StepInterceptors: []StepInterceptor{sink},
	}
	w.Add(Step(sub))

	assert.NoError(t, w.Do(context.Background()))

	types := make([]EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	// At least 4 events: Scheduled+Succeeded for sub, Scheduled+Succeeded for innerStep
	assert.GreaterOrEqual(t, len(events), 4)
	assert.Contains(t, types, Scheduled)
	assert.Contains(t, types, EventSucceeded)
	for _, e := range events {
		assert.NotNil(t, e.Step)
	}
}

func TestSubWorkflow_ChildInterceptorPreserved(t *testing.T) {
	t.Parallel()

	var parentEvents []WorkflowEvent
	var childEvents []WorkflowEvent
	pmu := sync.Mutex{}
	cmu := sync.Mutex{}

	parentSink := NewStepEventSink(func(e WorkflowEvent) {
		pmu.Lock()
		parentEvents = append(parentEvents, e)
		pmu.Unlock()
	})
	childSink := NewStepEventSink(func(e WorkflowEvent) {
		cmu.Lock()
		childEvents = append(childEvents, e)
		cmu.Unlock()
	})

	innerStep := NoOp("inner")
	type mySubStep struct{ SubWorkflow }
	sub := &mySubStep{}
	sub.Add(Step(innerStep))
	sub.w.StepInterceptors = []StepInterceptor{childSink}

	w := &Workflow{
		StepInterceptors: []StepInterceptor{parentSink},
	}
	w.Add(Step(sub))

	assert.NoError(t, w.Do(context.Background()))

	// Parent sees outer step (sub) + inner step (propagated) = at least 4 events
	assert.GreaterOrEqual(t, len(parentEvents), 4)
	// Child sees inner step only = at least 2 events
	assert.GreaterOrEqual(t, len(childEvents), 2)
}

func TestSubWorkflow_InterceptorNotDuplicatedOnRetry(t *testing.T) {
	t.Parallel()

	var count atomic.Int32
	sink := StepInterceptorFunc(func(ctx context.Context, info StepInfo, next func(context.Context) error) error {
		count.Add(1)
		return next(ctx)
	})

	attempts := 0
	inner := Func("inner", func(ctx context.Context) error {
		attempts++
		if attempts < 2 {
			return errors.New("fail once")
		}
		return nil
	})

	type mySubStep struct{ SubWorkflow }
	sub := &mySubStep{}
	sub.Add(Step(inner).Retry(func(o *RetryOption) {
		o.Attempts = 3
		o.Backoff = &backoff.ZeroBackOff{}
	}))

	w := &Workflow{StepInterceptors: []StepInterceptor{sink}}
	w.Add(Step(sub))
	assert.NoError(t, w.Do(context.Background()))

	// parent interceptor must fire exactly twice:
	// once for the outer sub step, once for the inner step (regardless of retry count).
	assert.Equal(t, int32(2), count.Load())
}
