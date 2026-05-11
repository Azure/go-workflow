package flow_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	flow "github.com/Azure/go-workflow"
	"github.com/benbjohnson/clock"
)

// # Step Timeout and Per-Try Timeout
//
// Workflow can manages the timeout of each Step in different granularity.
//
//	       ┌────────────────Step Timeout──────────────┐
//	       │                                          │
//	       │ ┌─────────┐       ┌─────────┐            │
//	START ─┴─► Step.Do ├─retry┌► Step.Do ├┐retry─►...─┴─► EXIT
//	         └─────────┘      │┬─────────┼│
//	                          └───────────┘
//	                         Per-Try Timeout
//
//	workflow.Add(
//		Step(a).
//		Timeout(/* Step Timeout */).
//		Retry(func(ro *flow.RetryOption) {
//			ro.TimeoutPerTry = /* Per-Try Timeout */
//		}),
//	)
func ExampleAddSteps_Timeout() {
	var (
		mock     = clock.NewMock() // use mock clock
		workflow = &flow.Workflow{Clock: mock}
		started  = make(chan struct{})
		waitDone = &WaitDone{StartDo: started}
	)

	workflow.Add(
		flow.Steps(waitDone).
			Timeout(15 * time.Minute).
			Retry(func(ro *flow.RetryOption) {
				ro.TimeoutPerTry = 10 * time.Minute
				ro.Attempts = 2
				ro.Timer = new(testTimer)
			}),
	)

	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// you can, actually, pass a context with timeout to set a Workflow level timeout
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
		defer cancel()
		err = workflow.Do(ctx)
	}()
	go func() {
		for range started {
			mock.Add(10 * time.Minute) // tick forward 10 minute
		}
	}()
	wg.Wait()

	fmt.Println(err)
	// Output:
	// done
	// done
	// WaitDone: [Canceled]
	//	context deadline exceeded
}

// WaitDone will be pending until the context is done.
type WaitDone struct {
	StartDo chan<- struct{} // signal it each time start Do()
}

func (p *WaitDone) String() string { return "WaitDone" }
func (p *WaitDone) Do(ctx context.Context) error {
	p.StartDo <- struct{}{}
	<-ctx.Done()
	fmt.Println("done")
	return ctx.Err()
}

// testTimer is a Timer that all retry intervals are immediate (0).
type testTimer struct {
	timer *time.Timer
}

func (t *testTimer) C() <-chan time.Time {
	return t.timer.C
}

func (t *testTimer) Start(duration time.Duration) {
	// Ignore the requested duration and fire immediately so examples / unit
	// tests do not have to wait through real backoff intervals.
	t.timer = time.NewTimer(0)
}

func (t *testTimer) Stop() {
	t.timer.Stop()
}
