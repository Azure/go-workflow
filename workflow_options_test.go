package flow

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
)

func TestDontPanic(t *testing.T) {
	t.Parallel()
	dontPanic := true
	t.Run("panic in step", func(t *testing.T) {
		t.Parallel()
		workflow := &Workflow{Option: WorkflowOption{DontPanic: &dontPanic}}
		panicStep := Func("panic", func(ctx context.Context) error {
			panic("panic in step")
		})
		workflow.Add(
			Step(panicStep),
		)
		err := workflow.Do(context.Background())
		assert.ErrorContains(t, err, "panic in step")
	})
	t.Run("panic in flow", func(t *testing.T) {
		t.Parallel()
		workflow := &Workflow{Option: WorkflowOption{DontPanic: &dontPanic}}
		answer := FuncO("answer", func(ctx context.Context) (int, error) {
			return 42, nil
		})
		print := FuncI("print", func(ctx context.Context, msg string) error {
			fmt.Println(msg)
			return nil
		})

		workflow.Add(
			Step(print).DependsOn(answer).Input(func(ctx context.Context, print *Function[string, struct{}]) error {
				panic("panic in flow")
			}),
		)

		err := workflow.Do(context.Background())
		assert.ErrorContains(t, err, "panic in flow")
	})
	t.Run("panic will have stack traces", func(t *testing.T) {
		t.Parallel()
		workflow := &Workflow{Option: WorkflowOption{DontPanic: &dontPanic}}
		panicStep := Func("panic", func(ctx context.Context) error {
			panic("panic in step")
		})
		workflow.Add(
			Step(panicStep),
		)
		err := workflow.Do(context.Background())
		assert.ErrorContains(t, err, "panic in step")
	})
}

func TestMaxConcurrency(t *testing.T) {
	t.Parallel()
	t.Run("MaxConcurrency=2 allows at most 2 concurrent Steps", func(t *testing.T) {
		t.Parallel()
		mc := 2
		w := &Workflow{Option: WorkflowOption{MaxConcurrency: &mc}}
		var running atomic.Int32
		var maxSeen atomic.Int32
		gate := make(chan struct{})

		for _, name := range []string{"a", "b", "c", "d"} {
			name := name
			w.Add(Step(Func(name, func(ctx context.Context) error {
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
			})))
		}

		done := make(chan error, 1)
		go func() { done <- w.Do(context.Background()) }()

		time.Sleep(20 * time.Millisecond)
		close(gate)

		assert.NoError(t, <-done)
		assert.LessOrEqual(t, int(maxSeen.Load()), 2,
			"expected at most 2 steps to run concurrently")
	})

	t.Run("MaxConcurrency=0 imposes no limit", func(t *testing.T) {
		t.Parallel()
		const n = 4
		mc := 0
		w := &Workflow{Option: WorkflowOption{MaxConcurrency: &mc}}
		var running atomic.Int32
		var maxSeen atomic.Int32
		gate := make(chan struct{})

		for i := 0; i < n; i++ {
			name := string(rune('a' + i))
			w.Add(Step(Func(name, func(ctx context.Context) error {
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
			})))
		}

		done := make(chan error, 1)
		go func() { done <- w.Do(context.Background()) }()

		time.Sleep(20 * time.Millisecond)
		close(gate)

		assert.NoError(t, <-done)
		assert.EqualValues(t, n, maxSeen.Load(),
			"expected all steps to run concurrently with MaxConcurrency=0")
	})
}

func TestSkipAsError(t *testing.T) {
	t.Parallel()
	skipAsError := true
	t.Run("Skipped is acceptable by default", func(t *testing.T) {
		step := Func("step", func(ctx context.Context) error { return Skip(nil) })
		w := new(Workflow).Add(Step(step))
		assert.NoError(t, w.Do(context.Background()))
	})

	t.Run("Skipped counts as error when SkipAsError=true", func(t *testing.T) {
		step := Func("step", func(ctx context.Context) error { return Skip(nil) })
		w := &Workflow{Option: WorkflowOption{SkipAsError: &skipAsError}}
		w.Add(Step(step))
		assert.Error(t, w.Do(context.Background()))
	})
}

func TestClock(t *testing.T) {
	t.Parallel()
	t.Run("Nil Clock uses wall clock via accessor", func(t *testing.T) {
		step := Func("step", func(ctx context.Context) error { return nil })
		w := &Workflow{}
		w.Add(Step(step))
		assert.Nil(t, w.Option.Clock, "Clock field starts unset")
		assert.NotNil(t, w.clock(), "w.clock() returns a real clock when field is nil")
		assert.NoError(t, w.Do(context.Background()))
		// New contract: w.Option.Clock is NOT written by Do() — the accessor
		// returns a fresh clock.New() on each call when the field is unset.
		assert.Nil(t, w.Option.Clock, "Do() must not mutate Option.Clock")
	})

	t.Run("Mock clock controls Step timeout", func(t *testing.T) {
		t.Parallel()
		mockClock := clock.NewMock()
		blocker := make(chan struct{})
		step := Func("step", func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-blocker:
				return nil
			}
		})
		w := &Workflow{Option: WorkflowOption{Clock: mockClock}}
		w.Add(Step(step).Timeout(time.Minute))

		done := make(chan error, 1)
		go func() { done <- w.Do(context.Background()) }()

		time.Sleep(10 * time.Millisecond)
		mockClock.Add(time.Minute + time.Second)

		assert.ErrorIs(t, <-done, context.DeadlineExceeded)
	})
}
