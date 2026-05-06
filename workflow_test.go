package flow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
)

func TestNil(t *testing.T) {
	t.Parallel()
	workflow := new(Workflow)
	t.Run("nil step", func(t *testing.T) {
		assert.Nil(t, workflow.Steps())
		assert.Nil(t, workflow.StateOf(nil))
		assert.Nil(t, workflow.UpstreamOf(nil))
		assert.True(t, workflow.IsTerminated())
	})
	t.Run("step not in workflow", func(t *testing.T) {
		step := Func("step", func(ctx context.Context) error { return nil })
		assert.Nil(t, workflow.Steps())
		assert.Nil(t, workflow.StateOf(step))
		assert.Nil(t, workflow.UpstreamOf(step))
	})
}

func TestAdd(t *testing.T) {
	t.Parallel()
	t.Run("add nil Builder", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(nil)
		assert.Nil(t, workflow.Steps())
	})
	t.Run("add nil step", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(Steps(nil))
		assert.Nil(t, workflow.Steps())
	})
	t.Run("add nil step should not break HasStep", func(t *testing.T) {
		a := NoOp("a")
		w := new(Workflow).Add(
			Step(a),
			Name(nil, "nil step"),
		)
		for i := 0; i < 100; i++ {
			assert.True(t, HasStep(w, a))
		}
	})
	t.Run("add new step", func(t *testing.T) {
		workflow := new(Workflow)
		a := NoOp("a")
		workflow.Add(Step(a))
		assert.Len(t, workflow.Steps(), 1)
		assert.Equal(t, a, workflow.Steps()[0])
	})
	do := func(fn func() error) error { return fn() }
	t.Run("nested workflow with input", func(t *testing.T) {
		inner := new(Workflow)
		step := NoOp("inner step")
		inner.Add(Step(step))
		outer := new(Workflow)
		outer.Add(Step(inner))
		for _, step := range As[*NoOpStep](outer) {
			outer.Add(Step(step).Input(func(ctx context.Context, ss *NoOpStep) error {
				ss.Name = "modified"
				return nil
			}))
		}
		outerState := outer.StateOf(step)
		innerState := inner.StateOf(step)
		assert.ObjectsAreEqual(outerState, innerState)
		_, err := innerState.Before(context.Background(), inner, do)
		assert.NoError(t, err)
		assert.Equal(t, "modified", step.Name)
	})
	t.Run("nested multi step in nested workflow", func(t *testing.T) {
		inner, outer := new(Workflow), new(Workflow)
		a, b := NoOp("a"), NoOp("b")
		ab := multi(a, b)
		inner.Add(Step(ab))
		outer.Add(Step(inner))
		outer.Add(Step(a).Input(func(ctx context.Context, ss *NoOpStep) error {
			ss.Name += "_updated"
			return nil
		}))
		outerState := outer.StateOf(a)
		innerState := inner.StateOf(a)
		assert.ObjectsAreEqual(outerState, innerState)
		_, err := innerState.Before(context.TODO(), inner, do)
		assert.NoError(t, err)
		assert.Equal(t, "a_updated", a.Name)

	})
	t.Run("inner depends on new", func(t *testing.T) {
		inner := new(Workflow)
		outer := new(Workflow)
		{
			a := NoOp("a")
			inner.Add(Step(a))
			outer.Add(Step(inner))
		}

		var a *NoOpStep
		for _, step := range As[*NoOpStep](outer) {
			a = step
		}
		b := NoOp("b")
		outer.Add(Step(a).DependsOn(b))
		assert.Contains(t, outer.steps[inner].Config.Upstreams, b,
			"b is new, so the dependency should be added to root of a")
		assert.NotContains(t, inner.steps[a].Config.Upstreams, b,
			"inner workflow doesn't know the existing of b")
	})
	t.Run("inner depends on existing inner", func(t *testing.T) {
		inner := new(Workflow)
		outer := new(Workflow)
		{
			a := NoOp("a")
			b := NoOp("b")
			inner.Add(Steps(a, b))
			outer.Add(Step(inner))
		}

		var b *NoOpStep
		for _, step := range As[*NoOpStep](outer) {
			if step.Name == "b" {
				b = step
			}
		}
		var a *NoOpStep
		for _, step := range As[*NoOpStep](outer) {
			if step.Name == "a" {
				a = step
			}
		}
		outer.Add(Step(a).DependsOn(b))
		assert.NotContains(t, outer.UpstreamOf(a), b)
		assert.Contains(t, inner.steps[a].Config.Upstreams, b,
			"b is known by inner, so it should be added to inner")
	})
	t.Run("add twice should not call BuildStep twice", func(t *testing.T) {
		var i atomic.Int32
		step := &stepWithBuilder{
			Builder: func(s *stepWithBuilder) {
				s.Add(Step(NoOp(fmt.Sprintf("%d", i.Add(1)))))
			},
		}
		_ = new(Workflow).Add(
			Step(step),
			Step(step),
		)
		assert.EqualValues(t, 1, i.Load())
	})
}

type stepWithBuilder struct {
	Workflow
	Builder func(*stepWithBuilder)
}

func (s *stepWithBuilder) BuildStep() { s.Builder(s) }

func TestWorkflowTree(t *testing.T) {
	var (
		a  = NoOp("a")
		b  = NoOp("b")
		A  = wrap(a)
		Ab = multi(A, b)
	)
	t.Run("nil", func(t *testing.T) {
		w := new(Workflow)
		assert.Nil(t, w.RootOf(nil))
	})
	t.Run("", func(t *testing.T) {})
	t.Run("add from leaf to root", func(t *testing.T) {
		w := new(Workflow)
		w.Add(Step(a))
		assert.Len(t, w.steps, 1)

		w.Add(Step(A))
		assert.Len(t, w.steps, 1)

		w.Add(Step(Ab))
		assert.Len(t, w.steps, 1)
	})
	t.Run("add from root to leaf", func(t *testing.T) {
		w := new(Workflow)
		w.Add(Step(Ab))
		assert.Len(t, w.steps, 1)

		w.Add(Step(A))
		assert.Len(t, w.steps, 1)

		w.Add(Step(a))
		assert.Len(t, w.steps, 1)
	})
}

func BenchmarkStatusChange(b *testing.B) {
	// statusChange.Wait could be blocked when it's after all Signals fired
	//
	//	w.statusChange.L.Lock()
	//	for {
	//		if done := w.tick(ctx); done {	// A: kick step goroutines here
	//			break
	//		}
	//		w.statusChange.Wait()			// B: wait for step goroutines here
	//	}
	//	w.statusChange.L.Unlock()
	//
	//	====================================
	//
	//	go func(ctx context.Context, step Steper, state *State) {
	//		...
	//		defer func() {
	//			state.SetStatus(status)
	//			w.statusChange.Signal()		// C: signal statusChange here
	//			state.SetError(err)
	//		}()
	//
	// The deadlock condition is when
	//	A ----> C ----> B
	for range b.N {
		w := new(Workflow)
		w.Add(Step(NoOp("step")))
		w.Do(context.Background())
	}
}

type StepSubWorkflow struct{ SubWorkflow }

func (s *StepSubWorkflow) BuildStep() {
	s.Reset()
	s.Add(Step(NoOp("inner")))
}

func TestSubWorkflow(t *testing.T) {
	w := new(Workflow).Add(
		Step(&StepSubWorkflow{}),
	)
	assert.NoError(t, w.Do(context.Background()))
	assert.True(t, Has[*NoOpStep](w))
	assert.Equal(t, "inner", As[*NoOpStep](w)[0].Name)
}

// TestMaxConcurrencyDeadlock verifies that a workflow with MaxConcurrency=1
// and a dependency chain (a → b → c) completes without deadlock.
//
// Before the fix, a step's goroutine called signalStatusChange() *before*
// unlease(), so the main loop could wake up, fail to acquire the lease, go
// back to Wait(), and then never be woken again after the lease was released.
func TestMaxConcurrencyDeadlock(t *testing.T) {
	t.Parallel()
	a, b, c := NoOp("a"), NoOp("b"), NoOp("c")
	w := &Workflow{MaxConcurrency: 1}
	w.Add(
		Step(a),
		Step(b).DependsOn(a),
		Step(c).DependsOn(b),
	)

	done := make(chan error, 1)
	go func() { done <- w.Do(context.Background()) }()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: workflow with MaxConcurrency=1 did not complete within 5s")
	}
}

// TestMaxConcurrencyDeadlockStress runs many concurrent workflow chains to
// shake out the race between lease release and status-change signalling.
func TestMaxConcurrencyDeadlockStress(t *testing.T) {
	t.Parallel()
	const rounds = 100
	var wg sync.WaitGroup
	for range rounds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, b, c := NoOp("a"), NoOp("b"), NoOp("c")
			w := &Workflow{MaxConcurrency: 1}
			w.Add(
				Step(a),
				Step(b).DependsOn(a),
				Step(c).DependsOn(b),
			)
			done := make(chan error, 1)
			go func() { done <- w.Do(context.Background()) }()
			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Errorf("deadlock detected in stress round")
			}
		}()
	}
	wg.Wait()
}

func TestStepExecution_BasicSuccess(t *testing.T) {
	t.Parallel()
	var events []WorkflowEvent
	step := NoOp("a")
	w := &Workflow{
		StepInterceptors: []StepInterceptor{
			NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) }),
		},
	}
	w.Add(Step(step))
	err := w.Do(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, []EventType{Scheduled, Succeeded}, eventTypes(events))
}

func TestStepExecution_StepInterceptorOrder(t *testing.T) {
	t.Parallel()
	var order []string
	makeIC := func(name string) StepInterceptor {
		return StepInterceptorFunc(func(ctx context.Context, info StepInfo, next func(context.Context) error) error {
			order = append(order, name+":before")
			err := next(ctx)
			order = append(order, name+":after")
			return err
		})
	}
	w := &Workflow{
		StepInterceptors: []StepInterceptor{makeIC("A"), makeIC("B")},
	}
	w.Add(Step(NoOp("s")))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, []string{"A:before", "B:before", "B:after", "A:after"}, order)
}

func TestStepExecution_AttemptInterceptorOrder(t *testing.T) {
	t.Parallel()
	var order []string
	makeIC := func(name string) AttemptInterceptor {
		return AttemptInterceptorFunc(func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
			order = append(order, name+":before")
			err := next(ctx)
			order = append(order, name+":after")
			return err
		})
	}
	w := &Workflow{
		AttemptInterceptors: []AttemptInterceptor{makeIC("X"), makeIC("Y")},
	}
	w.Add(Step(NoOp("s")))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, []string{"X:before", "Y:before", "Y:after", "X:after"}, order)
}

func TestStepExecution_SkippedStep(t *testing.T) {
	t.Parallel()
	var events []WorkflowEvent
	step := NoOp("a")
	w := &Workflow{
		StepInterceptors: []StepInterceptor{
			NewStepEventSink(func(e WorkflowEvent) { events = append(events, e) }),
		},
	}
	w.Add(Step(step).When(func(_ context.Context, _ map[Steper]StepResult) StepStatus {
		return Skipped
	}))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, []EventType{Scheduled, Skipped}, eventTypes(events))
}

func TestStepExecution_RetryingEvent(t *testing.T) {
	t.Parallel()
	var events []WorkflowEvent
	mu := sync.Mutex{}
	record := func(e WorkflowEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}
	boom := errors.New("boom")
	attempts := 0
	step := Func("s", func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return boom
		}
		return nil
	})
	w := &Workflow{
		StepInterceptors: []StepInterceptor{
			NewStepEventSink(record),
		},
		AttemptInterceptors: []AttemptInterceptor{
			NewAttemptEventSink(record),
		},
	}
	w.Add(Step(step).Retry(func(o *RetryOption) {
		o.Attempts = 3
		o.Backoff = &backoff.ZeroBackOff{}
	}))
	assert.NoError(t, w.Do(context.Background()))
	assert.Equal(t, []EventType{
		Scheduled,
		Started, Retrying,
		Started, Retrying,
		Started, Succeeded,
	}, eventTypes(events))
}

func eventTypes(events []WorkflowEvent) []EventType {
	types := make([]EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

func TestStepExecution_RetryingEventAttemptNumbers(t *testing.T) {
	t.Parallel()

	var events []WorkflowEvent
	mu := sync.Mutex{}
	record := func(e WorkflowEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	callCount := 0
	step := Func("flaky", func(ctx context.Context) error {
		callCount++
		if callCount < 3 {
			return errors.New("not yet")
		}
		return nil
	})

	w := &Workflow{
		StepInterceptors:    []StepInterceptor{NewStepEventSink(record)},
		AttemptInterceptors: []AttemptInterceptor{NewAttemptEventSink(record)},
	}
	w.Add(Step(step).Retry(func(o *RetryOption) {
		o.Attempts = 5
		o.Backoff = &backoff.ZeroBackOff{}
	}))

	assert.NoError(t, w.Do(context.Background()))

	assert.Equal(t, []EventType{
		Scheduled,
		Started,   // attempt 0
		Retrying,  // attempt 0 failed
		Started,   // attempt 1
		Retrying,  // attempt 1 failed
		Started,   // attempt 2 succeeds
		Succeeded,
	}, eventTypes(events))

	retryingEvents := filterEvents(events, Retrying)
	assert.Equal(t, uint64(0), retryingEvents[0].Attempt)
	assert.Equal(t, uint64(1), retryingEvents[1].Attempt)

	startedEvents := filterEvents(events, Started)
	assert.Equal(t, uint64(0), startedEvents[0].Attempt)
	assert.Equal(t, uint64(1), startedEvents[1].Attempt)
	assert.Equal(t, uint64(2), startedEvents[2].Attempt)
}

func filterEvents(events []WorkflowEvent, et EventType) []WorkflowEvent {
	var rv []WorkflowEvent
	for _, e := range events {
		if e.Type == et {
			rv = append(rv, e)
		}
	}
	return rv
}
