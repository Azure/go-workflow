package flow

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

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
	t.Run("cycle stepsendency", func(t *testing.T) {
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

func TestWorkflowWillRecover(t *testing.T) {
	t.Parallel()
	t.Run("panic in step", func(t *testing.T) {
		t.Parallel()
		workflow := &Workflow{DontPanic: true}
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
		workflow := &Workflow{DontPanic: true}
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
		workflow := &Workflow{DontPanic: true}
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
		w := &Workflow{DontPanic: true}
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
		w := &Workflow{DontPanic: true}
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
