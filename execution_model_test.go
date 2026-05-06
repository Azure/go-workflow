package flow

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
)

func TestPreflight(t *testing.T) {
	t.Parallel()
	t.Run("WorkflowIsRunning", func(t *testing.T) {
		var (
			workflow       = new(Workflow)
			start          = make(chan struct{})
			done           = make(chan struct{})
			blockUntilDone = Func("block until done", func(ctx context.Context) error {
				start <- struct{}{}
				<-done
				return nil
			})
		)

		workflow.Add(
			Step(blockUntilDone),
		)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, workflow.Do(context.Background()))
		}()

		// ensure step is running
		<-start
		assert.ErrorIs(t, workflow.Do(context.Background()), ErrWorkflowIsRunning)

		// unblock step
		close(done)

		// wait workflow to finish
		wg.Wait()
	})
	t.Run("empty Workflow will just return nil", func(t *testing.T) {
		workflow := new(Workflow)
		assert.NoError(t, workflow.Do(context.Background()))
		assert.NoError(t, workflow.Do(context.Background()))
	})
}

func TestWorkflowErr(t *testing.T) {
	t.Run("Workflow without error, should also return nil", func(t *testing.T) {
		t.Parallel()
		workflow := new(Workflow)
		workflow.Add(
			Step(Func("A", func(ctx context.Context) error { return nil })),
		)
		err := workflow.Do(context.Background())
		assert.NoError(t, err)
	})
	t.Run("Workflow with error, return ErrWorkflow", func(t *testing.T) {
		t.Parallel()
		workflow := new(Workflow)
		workflow.Add(
			Step(Func("A", func(ctx context.Context) error { return nil })),
			Step(Func("B", func(ctx context.Context) error { return fmt.Errorf("B") })),
		)
		err := workflow.Do(context.Background())
		var errWorkflow ErrWorkflow
		assert.ErrorAs(t, err, &errWorkflow)
		for step, stepErr := range errWorkflow {
			switch fmt.Sprint(step) {
			case "A":
				assert.NoError(t, stepErr.Unwrap())
			case "B":
				assert.ErrorContains(t, stepErr, "B")
			}
		}
	})
}

func TestSkip(t *testing.T) {
	t.Parallel()
	t.Run("should skip step if return ErrSkip", func(t *testing.T) {
		w := &Workflow{SkipAsError: true}
		skipMe := Func("SkipMe", func(ctx context.Context) error {
			return Skip(fmt.Errorf("skip me"))
		})
		w.Add(Step(skipMe))
		err := w.Do(context.Background())
		var errWorkflow ErrWorkflow
		if assert.ErrorAs(t, err, &errWorkflow) {
			assert.False(t, errWorkflow.AllSucceeded())
			assert.True(t, errWorkflow.AllSucceededOrSkipped())
			assert.Equal(t, Skipped, errWorkflow[skipMe].Status)
			assert.NotErrorIs(t, errWorkflow[skipMe].Unwrap(), ErrSkip{})
			assert.ErrorContains(t, errWorkflow[skipMe].Unwrap(), "skip me")
		}
	})
	t.Run("should cancel skip if return ErrCancel", func(t *testing.T) {
		w := new(Workflow)
		cancelMe := Func("CancelMe", func(ctx context.Context) error {
			return Cancel(fmt.Errorf("cancel me"))
		})
		w.Add(Step(cancelMe))
		err := w.Do(context.Background())
		var errWorkflow ErrWorkflow
		if assert.ErrorAs(t, err, &errWorkflow) {
			assert.False(t, errWorkflow.AllSucceeded())
			assert.False(t, errWorkflow.AllSucceededOrSkipped())
			assert.Equal(t, Canceled, errWorkflow[cancelMe].Status)
			assert.NotErrorIs(t, errWorkflow[cancelMe].Unwrap(), ErrCancel{})
			assert.ErrorContains(t, errWorkflow[cancelMe].Unwrap(), "cancel me")
		}
	})
	t.Run("should succeeded when return ErrSucceed", func(t *testing.T) {
		w := new(Workflow)
		succeedMe := Func("SucceedMe", func(ctx context.Context) error {
			return Succeed(fmt.Errorf("succeed me"))
		})
		w.Add(Step(succeedMe))
		assert.NoError(t, w.Do(context.Background()))
		assert.Equal(t, Succeeded, w.StateOf(succeedMe).GetStatus())
	})
}

func TestWorkflowReset(t *testing.T) {
	t.Parallel()
	t.Run("Reset allows re-run", func(t *testing.T) {
		var calls atomic.Int32
		step := Func("step", func(ctx context.Context) error {
			calls.Add(1)
			return nil
		})
		w := new(Workflow).Add(Step(step))

		assert.NoError(t, w.Do(context.Background()))
		assert.EqualValues(t, 1, calls.Load())
		assert.Equal(t, Succeeded, w.StateOf(step).Status)

		assert.NoError(t, w.Reset())
		assert.Equal(t, Pending, w.StateOf(step).Status)

		assert.NoError(t, w.Do(context.Background()))
		assert.EqualValues(t, 2, calls.Load())
		assert.Equal(t, Succeeded, w.StateOf(step).Status)
	})

	t.Run("Reset rejected while running", func(t *testing.T) {
		started := make(chan struct{})
		unblock := make(chan struct{})
		step := Func("step", func(ctx context.Context) error {
			close(started)
			<-unblock
			return nil
		})
		w := new(Workflow).Add(Step(step))

		done := make(chan error, 1)
		go func() { done <- w.Do(context.Background()) }()

		<-started
		assert.ErrorIs(t, w.Reset(), ErrWorkflowIsRunning)
		close(unblock)
		assert.NoError(t, <-done)
	})
}

func TestConcurrentExecution(t *testing.T) {
	t.Parallel()
	t.Run("independent Steps execute concurrently", func(t *testing.T) {
		const n = 4
		var running atomic.Int32
		var maxSeen atomic.Int32

		gate := make(chan struct{})
		makeStep := func(name string) Steper {
			return Func(name, func(ctx context.Context) error {
				cur := running.Add(1)
				for {
					old := maxSeen.Load()
					if cur <= old || maxSeen.CompareAndSwap(old, cur) {
						break
					}
				}
				<-gate
				running.Add(-1)
				return nil
			})
		}

		steps := make([]Steper, n)
		for i := range steps {
			steps[i] = makeStep(string(rune('a' + i)))
		}

		w := new(Workflow)
		for _, s := range steps {
			w.Add(Step(s))
		}

		done := make(chan error, 1)
		go func() { done <- w.Do(context.Background()) }()

		time.Sleep(20 * time.Millisecond)
		close(gate)

		assert.NoError(t, <-done)
		assert.GreaterOrEqual(t, int(maxSeen.Load()), 2,
			"expected at least 2 steps to run concurrently")
	})
}

func TestStepResultFinishedAtPopulated(t *testing.T) {
	mockClock := clock.NewMock()
	step := Func("test-step", func(ctx context.Context) error { return nil })
	w := &Workflow{Clock: mockClock}
	w.Add(Step(step))

	err := w.Do(context.Background())
	assert.NoError(t, err)

	result := w.StateOf(step).GetStepResult()
	assert.False(t, result.FinishedAt.IsZero(), "FinishedAt should be set after step execution")
}
