package flow

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDep(t *testing.T) {
	t.Parallel()
	a := Func("A", func(ctx context.Context) error { return nil })
	b := Func("B", func(ctx context.Context) error { return nil })
	c := Func("C", func(ctx context.Context) error { return nil })
	d := Func("D", func(ctx context.Context) error { return nil })
	t.Run("(a -> b, c) (c -> d)", func(t *testing.T) {
		w := new(Workflow)
		w.Add(
			Step(a).DependsOn(b, c),
			Step(c).DependsOn(d),
		)
		t.Run("list all steps from steps", func(t *testing.T) {
			var steps []Steper
			for _, s := range w.Steps() {
				steps = append(steps, s)
			}
			assert.ElementsMatch(t, []Steper{a, b, c, d}, steps)
		})
		t.Run("list all upstream of some step", func(t *testing.T) {
			assert.ElementsMatch(t, []Steper{b, c}, Keys(w.UpstreamOf(a)))
			assert.ElementsMatch(t, []Steper{}, Keys(w.UpstreamOf(b)))
			assert.ElementsMatch(t, []Steper{d}, Keys(w.UpstreamOf(c)))
			assert.ElementsMatch(t, []Steper{}, Keys(w.UpstreamOf(d)))
		})
	})
	t.Run("cycle dependency", func(t *testing.T) {
		w := new(Workflow)
		w.Add(
			Step(a).DependsOn(b),
			Step(b).DependsOn(c),
			Step(c).DependsOn(a),
		)
		var err ErrCycleDependency
		assert.ErrorAs(t, w.Do(context.Background()), &err)
		assert.Len(t, err, 3)
	})
	t.Run("Pipe", func(t *testing.T) {
		w := new(Workflow)
		w.Add(
			Pipe(a, b, c),
		)
		assert.ElementsMatch(t, []Steper{}, Keys(w.UpstreamOf(a)))
		assert.ElementsMatch(t, []Steper{a}, Keys(w.UpstreamOf(b)))
		assert.ElementsMatch(t, []Steper{b}, Keys(w.UpstreamOf(c)))
	})
	t.Run("BatchPipe", func(t *testing.T) {
		w := new(Workflow)
		w.Add(
			BatchPipe(
				Steps(a, b),
				Steps(c, d),
			),
		)
		assert.ElementsMatch(t, []Steper{}, Keys(w.UpstreamOf(a)))
		assert.ElementsMatch(t, []Steper{}, Keys(w.UpstreamOf(b)))
		assert.ElementsMatch(t, []Steper{a, b}, Keys(w.UpstreamOf(c)))
		assert.ElementsMatch(t, []Steper{a, b}, Keys(w.UpstreamOf(d)))
	})
}

func TestBeforeAfter(t *testing.T) {
	t.Parallel()
	var (
		i    atomic.Int32
		step = Func("step", func(ctx context.Context) error {
			assert.EqualValues(t, 1, i.Load())
			i.Add(1)
			return nil
		})
		beforeContext = func(ctx context.Context, _ Steper) (context.Context, error) {
			assert.Equal(t, "context.TODO", fmt.Sprint(ctx))
			return context.Background(), nil
		}
		beforeInc = func(ctx context.Context, _ Steper) (context.Context, error) {
			i.Add(1)
			return ctx, nil
		}
		beforeAssertContext = func(ctx context.Context, _ Steper) (context.Context, error) {
			assert.Equal(t, "context.Background", fmt.Sprint(ctx))
			return ctx, nil
		}
		beforeFail = func(ctx context.Context, _ Steper) (context.Context, error) {
			return ctx, assert.AnError
		}
		afterAssert = func(ctx context.Context, _ Steper, err error) error {
			assert.EqualValues(t, 2, i.Load())
			return nil
		}
		afterFail = func(ctx context.Context, _ Steper, err error) error {
			return assert.AnError
		}
		reset = func() {
			i.Store(0)
		}
	)
	t.Run("should call Before then Step then After", func(t *testing.T) {
		defer reset()
		w := new(Workflow)
		w.Add(
			Step(step).
				BeforeStep(beforeInc).
				AfterStep(afterAssert),
		)
		assert.NoError(t, w.Do(context.TODO()))
	})
	t.Run("should fail if Before failed", func(t *testing.T) {
		defer reset()
		w := new(Workflow)
		w.Add(
			Step(step).
				BeforeStep(beforeFail),
		)
		assert.Error(t, w.Do(context.TODO()))
		assert.EqualValues(t, 0, i.Load())
	})
	t.Run("before callbacks are executed in order", func(t *testing.T) {
		defer reset()
		w := new(Workflow)
		w.Add(
			Step(step).
				BeforeStep(beforeContext, beforeAssertContext, beforeInc).
				AfterStep(afterAssert, afterFail),
		)
		assert.Error(t, w.Do(context.TODO()))
	})
	t.Run("input should also respect the order", func(t *testing.T) {
		defer reset()
		w := new(Workflow).Add(
			Step(NoOp("step")).Input(
				func(ctx context.Context, nos *NoOpStep) error {
					assert.EqualValues(t, 1, i.Add(1))
					return nil
				},
				func(ctx context.Context, nos *NoOpStep) error {
					assert.EqualValues(t, 2, i.Add(1))
					return nil
				},
			),
		)
		assert.NoError(t, w.Do(context.Background()))
	})
	t.Run("output", func(t *testing.T) {
		defer reset()
		step := FuncO("step", func(ctx context.Context) (string, error) {
			return "hello, world", nil
		})
		w := new(Workflow).Add(
			Step(step).Output(func(ctx context.Context, f *Function[struct{}, string]) error {
				assert.Equal(t, "hello, world", f.Output)
				return nil
			}),
		)
		assert.NoError(t, w.Do(context.Background()))
	})
	t.Run("output only called when step is successful", func(t *testing.T) {
		defer reset()
		step := FuncO("step", func(ctx context.Context) (string, error) {
			return "hello, world", assert.AnError
		})
		w := new(Workflow).Add(
			Step(step).Output(func(ctx context.Context, f *Function[struct{}, string]) error {
				assert.FailNow(t, "output should not be called")
				return nil
			}),
		)
		assert.ErrorIs(t, w.Do(context.Background()), assert.AnError)
	})
	t.Run("should call AfterStep even step panics", func(t *testing.T) {
		dontPanic := true
		w := &Workflow{Option: WorkflowOption{DontPanic: &dontPanic}}
		w.Add(
			Step(Func("step", func(ctx context.Context) error {
				panic("panic!")
			})).AfterStep(func(ctx context.Context, s Steper, err error) error {
				assert.ErrorContains(t, err, "panic!")
				return nil
			}),
		)
		assert.NoError(t, w.Do(context.Background()))
	})
	t.Run("should call AfterStep even BeforeStep fails", func(t *testing.T) {
		w := &Workflow{}
		afterRan := false
		w.Add(
			Step(NoOp("step")).
				Input(func(ctx context.Context, nos *NoOpStep) error {
					return assert.AnError
				}).
				AfterStep(func(ctx context.Context, s Steper, err error) error {
					assert.ErrorIs(t, err, assert.AnError)
					afterRan = true
					return nil
				}),
		)
		assert.NoError(t, w.Do(context.Background()))
		assert.True(t, afterRan)
	})
	t.Run("modified context from BeforeStep should still be used even panic happens", func(t *testing.T) {
		dontPanic := true
		w := &Workflow{Option: WorkflowOption{DontPanic: &dontPanic}}
		noop := NoOp("NoOp")
		ctx := context.Background()
		w.Add(Step(noop).
			BeforeStep(func(ctx context.Context, s Steper) (context.Context, error) {
				return context.WithValue(ctx, "key", "value"), nil // save a modified context
			}).
			Input(func(ctx context.Context, nos *NoOpStep) error {
				panic("panic in input")
			}).
			AfterStep(func(ctx context.Context, s Steper, err error) error {
				assert.Equal(t, "value", ctx.Value("key")) // assert modified context is still used
				return nil
			}),
		)
		assert.NoError(t, w.Do(ctx))
	})
	t.Run("BeforeStep can modify context", func(t *testing.T) {
		w := &Workflow{}
		step := Func("step", func(ctx context.Context) error {
			assert.Equal(t, "value", ctx.Value("key"))
			return nil
		})
		w.Add(Step(step).
			BeforeStep(func(ctx context.Context, _ Steper) (context.Context, error) {
				return context.WithValue(ctx, "key", "value"), nil // save a modified context
			}).
			Input(func(ctx context.Context, _ *Function[struct{}, struct{}]) error {
				assert.Equal(t, "value", ctx.Value("key")) // assert modified context is used
				return nil
			}).
			Output(func(ctx context.Context, _ *Function[struct{}, struct{}]) error {
				assert.Equal(t, "value", ctx.Value("key"))
				return nil
			}),
		)
		assert.NoError(t, w.Do(context.Background()))
	})
}

func TestDefaultOption(t *testing.T) {
	t.Parallel()
	t.Run("StepDefaults applies to all Steps", func(t *testing.T) {
		w := &Workflow{
			Option: WorkflowOption{StepDefaults: &StepOption{Timeout: durationPtr(10 * time.Minute)}},
		}
		step := NoOp("step")
		w.Add(Step(step))
		assert.Equal(t, 10*time.Minute, *w.StateOf(step).Option().Timeout)
	})

	t.Run("Step-level option overrides StepDefaults", func(t *testing.T) {
		w := &Workflow{
			Option: WorkflowOption{StepDefaults: &StepOption{Timeout: durationPtr(10 * time.Minute)}},
		}
		step := NoOp("step")
		w.Add(Step(step).Timeout(5 * time.Minute))
		assert.Equal(t, 5*time.Minute, *w.StateOf(step).Option().Timeout)
	})

	t.Run("Timeout last-write wins", func(t *testing.T) {
		w := new(Workflow)
		step := NoOp("step")
		w.Add(Step(step).Timeout(3 * time.Minute))
		w.Add(Step(step).Timeout(7 * time.Minute))
		assert.Equal(t, 7*time.Minute, *w.StateOf(step).Option().Timeout)
	})
}
