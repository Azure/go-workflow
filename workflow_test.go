package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDep(t *testing.T) {
	a := Func("A", func(ctx context.Context) error { return nil })
	b := Func("B", func(ctx context.Context) error { return nil })
	c := Func("C", func(ctx context.Context) error { return nil })
	d := Func("D", func(ctx context.Context) error { return nil })
	t.Run("(a -> b, c) (c -> d)", func(t *testing.T) {
		flow := new(Workflow)
		flow.Add(
			Step(a).DependsOn(b, c),
			Step(c).DependsOn(d),
		)
		t.Run("list all steps from dependency", func(t *testing.T) {
			t.Parallel()
			var dep []Steper
			for s := range flow.Dep() {
				dep = append(dep, s)
			}
			assert.ElementsMatch(t, []Steper{a, b, c, d}, dep)
		})
		t.Run("list all upstream of some step", func(t *testing.T) {
			t.Parallel()
			assert.ElementsMatch(t, []Steper{b, c}, flow.Dep().UpstreamOf(a))
			assert.ElementsMatch(t, []Steper{}, flow.Dep().UpstreamOf(b))
			assert.ElementsMatch(t, []Steper{d}, flow.Dep().UpstreamOf(c))
			assert.ElementsMatch(t, []Steper{}, flow.Dep().UpstreamOf(d))
		})
		t.Run("list all downstrem of some step", func(t *testing.T) {
			t.Parallel()
			assert.ElementsMatch(t, []Steper{}, flow.Dep().DownstreamOf(a))
			assert.ElementsMatch(t, []Steper{a}, flow.Dep().DownstreamOf(b))
			assert.ElementsMatch(t, []Steper{a}, flow.Dep().DownstreamOf(c))
			assert.ElementsMatch(t, []Steper{c}, flow.Dep().DownstreamOf(d))
		})
	})
	t.Run("cycle dependency", func(t *testing.T) {
		flow := new(Workflow)
		flow.Add(
			Step(a).DependsOn(b),
			Step(b).DependsOn(c),
			Step(c).DependsOn(a),
		)
		var err ErrCycleDependency
		assert.ErrorAs(t, flow.Run(context.Background()), &err)
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
		flow := new(Workflow)
		flow.Add(
			Step(blockUntilDone),
		)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			flow.Run(context.Background())
		}()

		// ensure step is running
		<-start
		assert.ErrorIs(t, flow.Run(context.Background()), ErrWorkflowIsRunning)

		// unblock step
		close(done)

		// wait workflow to finish
		wg.Wait()
	})
	t.Run("empty Workflow will just return nil", func(t *testing.T) {
		t.Parallel()
		flow := new(Workflow)
		assert.NoError(t, flow.Run(context.Background()))
		assert.NoError(t, flow.Run(context.Background()))
	})
	t.Run("Workflow has run", func(t *testing.T) {
		t.Parallel()
		flow := new(Workflow)
		flow.Add(Step(Func("A", func(ctx context.Context) error { return nil })))
		assert.NoError(t, flow.Run(context.Background()))
		assert.ErrorIs(t, flow.Run(context.Background()), ErrWorkflowHasRun)
	})
	t.Run("all steps must initialized with Pending", func(t *testing.T) {
		t.Parallel()
		a := Func("A", func(ctx context.Context) error { return nil })
		flow := new(Workflow)
		flow.Add(Step(a))
		a.setStatus(StepStatusRunning)
		var err ErrUnexpectStepInitStatus
		assert.ErrorAs(t, flow.Run(context.Background()), &err)
		assert.ElementsMatch(t, []StepReader{a}, err)
	})
}

func TestWorkflowWillRecover(t *testing.T) {
	t.Run("panic in step", func(t *testing.T) {
		t.Parallel()
		flow := new(Workflow)
		panicStep := Func("panic", func(ctx context.Context) error {
			panic("panic in step")
		})
		flow.Add(
			Step(panicStep),
		)
		err := flow.Run(context.Background())
		assert.ErrorContains(t, err, "panic in step")
	})
	t.Run("panic in flow", func(t *testing.T) {
		t.Parallel()
		flow := new(Workflow)
		answer := FuncO("answer", func(ctx context.Context) (int, error) {
			return 42, nil
		})
		print := FuncI("print", func(ctx context.Context, msg string) error {
			fmt.Println(msg)
			return nil
		})

		flow.Add(
			Step(print).
				InputDependsOn(Adapt(answer,
					func(ctx context.Context, answer *Function[struct{}, int], print *Function[string, struct{}]) error {
						panic("panic in flow")
					}),
				),
		)

		err := flow.Run(context.Background())
		assert.ErrorContains(t, err, "panic in flow")
	})
}

func TestWorkflowErr(t *testing.T) {
	t.Run("Workflow without error, Err() should also return nil", func(t *testing.T) {
		t.Parallel()
		flow := new(Workflow)
		flow.Add(
			Step(Func("A", func(ctx context.Context) error { return nil })),
		)
		err := flow.Run(context.Background())
		assert.NoError(t, err)
	})
	t.Run("Workflow with error, iterate Err() to access all errors", func(t *testing.T) {
		t.Parallel()
		flow := new(Workflow)
		flow.Add(
			Step(Func("A", func(ctx context.Context) error { return nil })),
			Step(Func("B", func(ctx context.Context) error { return fmt.Errorf("B") })),
		)
		err := flow.Run(context.Background())
		assert.Error(t, err)
		for step, stepErr := range flow.Err() {
			switch step.String() {
			case "A":
				assert.NoError(t, stepErr)
			case "B":
				assert.ErrorContains(t, stepErr, "B")
			}
		}
	})
}

func TestWorkflowContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	pend := Func("pend", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	flow := new(Workflow)
	flow.Add(
		Step(pend),
	)

	var err error

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = flow.Run(ctx)
	}()

	cancel()
	wg.Wait()

	var werr ErrWorkflow
	assert.ErrorAs(t, err, &werr)
	assert.ErrorIs(t, werr[pend], context.Canceled)
}
