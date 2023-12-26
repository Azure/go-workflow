package flow

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNil(t *testing.T) {
	workflow := new(Workflow)
	t.Run("nil step", func(t *testing.T) {
		assert.Nil(t, workflow.Steps())
		assert.Nil(t, workflow.StateOf(nil))
		assert.Equal(t, PhaseUnknown, workflow.PhaseOf(nil))
		assert.Nil(t, workflow.UpstreamOf(nil))
		assert.True(t, workflow.IsTerminated())
	})
	t.Run("step not in workflow", func(t *testing.T) {
		step := Func("step", func(ctx context.Context) error { return nil })
		assert.Nil(t, workflow.Steps())
		assert.Nil(t, workflow.StateOf(step))
		assert.Equal(t, PhaseUnknown, workflow.PhaseOf(step))
		assert.Nil(t, workflow.UpstreamOf(step))
	})
}

func TestAdd(t *testing.T) {
	t.Run("add nil WorkflowAdder", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(nil)
		assert.Nil(t, workflow.Steps())
	})
	t.Run("add nil step", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(Steps(nil))
		assert.Nil(t, workflow.Steps())
	})
	t.Run("add new step", func(t *testing.T) {
		workflow := new(Workflow)
		a := &someStep{value: "a"}
		workflow.Add(Step(a))
		assert.Len(t, workflow.Steps(), 1)
		assert.Equal(t, a, workflow.Steps()[0])
	})
	t.Run("nested workflow with input", func(t *testing.T) {
		inner := new(Workflow)
		step := &someStep{value: "inner step"}
		inner.Add(Step(step))
		outer := new(Workflow)
		outer.Add(Step(inner))
		for _, step := range As[*someStep](outer) {
			outer.Add(Step(step).Input(func(ctx context.Context, ss *someStep) error {
				ss.value = "modified"
				return nil
			}))
		}
		outerState := outer.StateOf(step)
		innerState := inner.StateOf(step)
		assert.ObjectsAreEqual(outerState, innerState)
		assert.NoError(t, innerState.Input(context.Background()))
		assert.Equal(t, "modified", step.value)
	})
	t.Run("nested multi step in nested workflow", func(t *testing.T) {
		inner, outer := new(Workflow), new(Workflow)
		a, b := &someStep{value: "a"}, &someStep{value: "b"}
		multi := &multiStep{steps: []Steper{a, b}}
		inner.Add(Step(multi))
		outer.Add(Step(inner))
		outer.Add(Step(a).Input(func(ctx context.Context, ss *someStep) error {
			ss.value += "_updated"
			return nil
		}))
		outerState := outer.StateOf(a)
		innerState := inner.StateOf(a)
		assert.ObjectsAreEqual(outerState, innerState)
		assert.NoError(t, innerState.Input(context.TODO()))
		assert.Equal(t, "a_updated", a.value)

	})
	t.Run("inner depends on new", func(t *testing.T) {
		inner := new(Workflow)
		outer := new(Workflow)
		{
			a := &someStep{value: "a"}
			inner.Add(Step(a))
			outer.Add(Step(inner))
		}

		var a *someStep
		for _, step := range As[*someStep](outer) {
			a = step
		}
		b := &someStep{value: "b"}
		outer.Add(Step(a).DependsOn(b))
		assert.Contains(t, outer.state[inner].Config.Upstreams, b,
			"b is new, so the dependency should be added to root of a")
		assert.NotContains(t, inner.state[a].Config.Upstreams, b,
			"inner workflow doesn't know the existing of b")
	})
	t.Run("inner depends on existing inner", func(t *testing.T) {
		inner := new(Workflow)
		outer := new(Workflow)
		{
			a := &someStep{value: "a"}
			b := &someStep{value: "b"}
			inner.Add(Steps(a, b))
			outer.Add(Step(inner))
		}

		var b *someStep
		for _, step := range As[*someStep](outer) {
			if step.value == "b" {
				b = step
			}
		}
		var a *someStep
		for _, step := range As[*someStep](outer) {
			if step.value == "a" {
				a = step
			}
		}
		outer.Add(Step(a).DependsOn(b))
		assert.NotContains(t, outer.UpstreamOf(a), b)
		assert.Contains(t, inner.state[a].Config.Upstreams, b,
			"b is known by inner, so it should be added to inner")
	})
}

func TestDep(t *testing.T) {
	a := Func("A", func(ctx context.Context) error { return nil })
	b := Func("B", func(ctx context.Context) error { return nil })
	c := Func("C", func(ctx context.Context) error { return nil })
	d := Func("D", func(ctx context.Context) error { return nil })
	t.Run("(a -> b, c) (c -> d)", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(
			Step(a).DependsOn(b, c),
			Step(c).DependsOn(d),
		)
		t.Run("list all steps from stepsendency", func(t *testing.T) {
			t.Parallel()
			var steps []Steper
			for _, s := range workflow.Steps() {
				steps = append(steps, s)
			}
			assert.ElementsMatch(t, []Steper{a, b, c, d}, steps)
		})
		t.Run("list all upstream of some step", func(t *testing.T) {
			t.Parallel()
			assert.ElementsMatch(t, []Steper{b, c}, keys(workflow.UpstreamOf(a)))
			assert.ElementsMatch(t, []Steper{}, keys(workflow.UpstreamOf(b)))
			assert.ElementsMatch(t, []Steper{d}, keys(workflow.UpstreamOf(c)))
			assert.ElementsMatch(t, []Steper{}, keys(workflow.UpstreamOf(d)))
		})
	})
	t.Run("cycle stepsendency", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(
			Step(a).DependsOn(b),
			Step(b).DependsOn(c),
			Step(c).DependsOn(a),
		)
		var err ErrCycleDependency
		assert.ErrorAs(t, workflow.Do(context.Background()), &err)
		assert.Len(t, err, 3)
	})
}

func TestPreflight(t *testing.T) {
	t.Run("WorkflowIsRunning", func(t *testing.T) {
		t.Parallel()
		start := make(chan struct{})
		done := make(chan struct{})
		blockUntilDone := Func("block until done", func(ctx context.Context) error {
			start <- struct{}{}
			<-done
			return nil
		})
		workflow := new(Workflow)
		workflow.Add(
			Step(blockUntilDone),
		)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			workflow.Do(context.Background())
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
		t.Parallel()
		workflow := new(Workflow)
		assert.NoError(t, workflow.Do(context.Background()))
		assert.NoError(t, workflow.Do(context.Background()))
	})
}

func TestWorkflowWillRecover(t *testing.T) {
	t.Run("panic in step", func(t *testing.T) {
		t.Parallel()
		workflow := new(Workflow).Options(DontPanic)
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
		workflow := new(Workflow).Options(DontPanic)
		answer := FuncO("answer", func(ctx context.Context) (int, error) {
			return 42, nil
		})
		print := FuncI("print", func(ctx context.Context, msg string) error {
			fmt.Println(msg)
			return nil
		})

		workflow.Add(
			Step(print).
				InputDependsOn(Adapt(answer,
					func(ctx context.Context, answer *Function[struct{}, int], print *Function[string, struct{}]) error {
						panic("panic in flow")
					}),
				),
		)

		err := workflow.Do(context.Background())
		assert.ErrorContains(t, err, "panic in flow")
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
				assert.NoError(t, stepErr.Err)
			case "B":
				assert.ErrorContains(t, stepErr, "B")
			}
		}
	})
}

func ExampleNotify() {
	workflow := new(Workflow)
	workflow.Add(
		Step(Func("dummy step", func(ctx context.Context) error {
			fmt.Println("inside step")
			return fmt.Errorf("step error")
		})),
	).Options(
		WithNotify(Notify{
			BeforeStep: func(ctx context.Context, step Steper) context.Context {
				fmt.Printf("before step: %s\n", step)
				return ctx
			},
			AfterStep: func(ctx context.Context, step Steper, err error) {
				fmt.Printf("after step: %s error: %s\n", step, err)
			},
		}),
	)
	_ = workflow.Do(context.Background())
	// Output:
	// before step: dummy step
	// inside step
	// after step: dummy step error: step error
}

type MockOrder struct{ mock.Mock }

func (m *MockOrder) Do(s string) { m.Called(s) }

func TestInitDefer(t *testing.T) {
	t.Run("should in order of init -> step -> defer", func(t *testing.T) {
		workflow := new(Workflow)
		mockOrder := new(MockOrder)
		makeStep := func(s string) Steper {
			return Func(s, func(ctx context.Context) error {
				mockOrder.Do(s)
				return nil
			})
		}
		var (
			a = makeStep("A")
			b = makeStep("B")
			c = makeStep("C")
			d = makeStep("D")
		)

		// Init:  B -> A
		// Run:   C -> A
		// Defer: D
		workflow.Init(
			Step(b).DependsOn(a),
		).Add(
			Step(c).DependsOn(a),
		).Defer(
			Step(d),
		)

		// order should be a -> b -> c -> d
		var (
			mA = mockOrder.On("Do", "A")
			mB = mockOrder.On("Do", "B")
			mC = mockOrder.On("Do", "C")
			mD = mockOrder.On("Do", "D")
		)
		mB.NotBefore(mA)
		mC.NotBefore(mB)
		mD.NotBefore(mC)
		_ = workflow.Do(context.Background())
	})
}

func keys[K comparable, V any](m map[K]V) []K {
	var keys []K
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestWorkflowTree(t *testing.T) {
	step1 := &someStep{value: "1"}
	step2 := &someStep{value: "2"}
	wStep1 := &wrappedStep{step1}
	mStep := &multiStep{steps: []Steper{wStep1, step2}}

	t.Run("add from leaf to root", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(Step(step1))
		assert.Len(t, workflow.tree, 1)
		assert.Len(t, workflow.steps[PhaseMain], 1)
		assert.Len(t, workflow.state, 1)

		workflow.Add(Step(wStep1))
		assert.Len(t, workflow.tree, 2)
		assert.Len(t, workflow.steps[PhaseMain], 1, "the previous root should be replaced")
		assert.Len(t, workflow.state, 1)

		workflow.Add(Step(mStep))
		assert.Len(t, workflow.tree, 4)
		assert.Len(t, workflow.steps[PhaseMain], 1, "the previous root should be replaced")
		assert.Len(t, workflow.state, 1)
	})
	t.Run("add from root to leaf", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(Step(mStep))
		assert.Len(t, workflow.tree, 4)
		assert.Len(t, workflow.steps[PhaseMain], 1)
		assert.Len(t, workflow.state, 1)
	})
}

func TestWorkflowTreeWithPhase(t *testing.T) {
	step1 := &someStep{value: "1"}
	step2 := &someStep{value: "2"}
	wStep1 := &wrappedStep{step1}

	w := new(Workflow)
	w.Init(Step(step1))
	w.Add(Step(step2).DependsOn(step1))
	w.Init(Step(wStep1))

	assert.Len(t, w.tree, 3)
	assert.Len(t, w.steps[PhaseInit], 1)
	assert.Contains(t, w.steps[PhaseInit], wStep1)
	assert.NotContains(t, w.steps[PhaseInit], w.state[step1])
	assert.Len(t, w.steps[PhaseMain], 2)
	assert.Contains(t, w.steps[PhaseMain], wStep1)
	assert.NotContains(t, w.steps[PhaseMain], w.state[step1])
}
