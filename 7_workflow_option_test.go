package workflow_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.goms.io/aks/rp/test/v3/workflow"
)

// `workflow` provides Workflow Options that configures the Workflow behavior.
//
//   - WithMaxConcurrency: set the maximum concurrency of the Workflow
//   - WithWhen:           set the When condition of the Workflow level
//
// WorkflowOption can be passed to Workflow via `(*Workflow).Options()`

func ExampleWorkflowWithMaxConcurrency() {
	flow := workflow.New()

	start := make(chan struct{})
	counter := new(atomic.Int32)
	done := make(chan struct{})

	countOneThenWaitDone := func(context.Context) error {
		counter.Add(1)
		start <- struct{}{}
		<-done
		return nil
	}

	var (
		a = workflow.Func("a", countOneThenWaitDone)
		b = workflow.Func("b", countOneThenWaitDone)
		c = workflow.Func("c", countOneThenWaitDone)
	)

	flow.Add(
		workflow.Steps(a, b, c),
	).Options(
		workflow.WithMaxConcurrency(2), // only 2 Steps can run concurrently
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = flow.Run(context.TODO())
	}()

	// should only two Steps are running concurrently
	<-start
	<-start
	// <-start // this will block
	fmt.Println(counter.Load())

	// unblock one Step
	done <- struct{}{}
	<-start
	fmt.Println(counter.Load())

	// unblock all Step
	close(done)

	// wait the Workflow to finish
	wg.Wait()

	// Output:
	// 2
	// 3
}

func ExampleWorkflowWithWhen() {
	flow := workflow.New()

	a := new(ArbitraryTask)

	flow.Add(
		workflow.Step(a),
	)

	ctx := context.WithValue(context.Background(), "key", "value")

	flow.Options(
		workflow.WithWhen(func(ctx context.Context) bool {
			// you can access the context here
			value := ctx.Value("key").(string)
			fmt.Println(value)
			// true  -> run the Workflow
			// false -> skip the Workflow
			return value != "value"
		}),
	)

	_ = flow.Run(ctx)
	fmt.Println(a.GetStatus())
	// Output:
	// value
	// Skipped
}
