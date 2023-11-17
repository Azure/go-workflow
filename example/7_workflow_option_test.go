package flow_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	flow "github.com/Azure/go-workflow"
)

// `workflow` provides Workflow Options that configures the Workflow behavior.
//
//   - WithMaxConcurrency: set the maximum concurrency of the Workflow
//   - WithWhen:           set the When condition of the Workflow level
//
// WorkflowOption can be passed to Workflow via `(*Workflow).Options()`

func ExampleWorkflowWithMaxConcurrency() {
	workflow := &flow.Workflow{}

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
		a = flow.Func("a", countOneThenWaitDone)
		b = flow.Func("b", countOneThenWaitDone)
		c = flow.Func("c", countOneThenWaitDone)
	)

	workflow.Add(
		flow.Steps(a, b, c),
	).Options(
		flow.WithMaxConcurrency(2), // only 2 Steps can run concurrently
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = workflow.Do(context.TODO())
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
