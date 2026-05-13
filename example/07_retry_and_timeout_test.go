package flow_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	flow "github.com/Azure/go-workflow"
	"github.com/benbjohnson/clock"
)

// # Retry & Timeout: surviving flaky work
//
// **What you'll learn**
//   - `Retry` re-runs a Step until it Succeeds or runs out of attempts.
//   - `Timeout` (per-step) bounds the entire Step including all retries.
//   - `Retry.TimeoutPerTry` bounds *each individual attempt* — much more
//     useful than a global step timeout when retries are involved.
//   - Both timeouts compose:
//
//	    ┌────────────── Step Timeout (Timeout) ──────────────┐
//	    │  ┌── attempt ──┐  ┌── attempt ──┐  ┌── attempt ──┐ │
//	    │  │ TimeoutPerTry│  │ TimeoutPerTry│  │ TimeoutPerTry│ │
//	    │  └─────────────┘  └─────────────┘  └─────────────┘ │
//	    └────────────────────────────────────────────────────┘
//
// **Defaults**
//   - No retry. A failing Step's error is published as-is.
//   - No timeout (per-step or per-try). The Step runs until ctx is done
//     (which is what `context.WithTimeout` on the workflow ctx achieves).

// ExampleAddStep_Retry shows the most common form: succeed eventually.
//
// `passAfter{n}` fails n times then succeeds. With Attempts = 5 the retry
// loop has plenty of headroom.
func ExampleAddStep_Retry() {
	pa := &passAfter{n: 2}

	w := new(flow.Workflow)
	w.Add(
		flow.Step(pa).Retry(func(ro *flow.RetryOption) {
			ro.Attempts = 5
			ro.Timer = new(zeroTimer) // suppress real backoff sleep in the example
		}),
	)
	_ = w.Do(context.Background())
	// Output:
	// fail #0
	// fail #1
	// pass #2
}

// ExampleAddStep_Retry_perTryTimeout shows TimeoutPerTry. The Step itself
// runs forever (waits on ctx); each attempt is killed at the per-try
// deadline; after `Attempts` killings, the Step is finally marked Failed.
//
// We use a mock clock so the example is fast and deterministic.
func ExampleAddStep_Retry_perTryTimeout() {
	mock := clock.NewMock()
	w := &flow.Workflow{Option: flow.WorkflowOption{Clock: mock}}

	startedAttempt := make(chan struct{}, 16)
	hangForever := flow.Func("hang", func(ctx context.Context) error {
		startedAttempt <- struct{}{} // signal "I'm running"
		<-ctx.Done()                 // wait until killed by per-try timeout
		return ctx.Err()
	})

	w.Add(
		flow.Step(hangForever).Retry(func(ro *flow.RetryOption) {
			ro.Attempts = 2
			ro.TimeoutPerTry = 5 * time.Minute
			ro.Timer = new(zeroTimer)
		}),
	)

	// Run the workflow in the background. As each attempt starts, advance
	// the mock clock past the per-try deadline so the attempt's ctx fires.
	var wg sync.WaitGroup
	wg.Add(1)
	var workflowErr error
	go func() {
		defer wg.Done()
		workflowErr = w.Do(context.Background())
	}()
	go func() {
		for range startedAttempt {
			mock.Add(6 * time.Minute) // tick past TimeoutPerTry
		}
	}()
	wg.Wait()

	var ew flow.ErrWorkflow
	fmt.Println("ErrWorkflow?", errors.As(workflowErr, &ew))
	fmt.Println("status:", w.StateOf(hangForever).GetStatus())
	// Output:
	// ErrWorkflow? true
	// status: Canceled
}

// passAfter is a Step that fails n times then succeeds.
type passAfter struct {
	n      int
	tryNum int
}

func (p *passAfter) String() string { return "passAfter" }
func (p *passAfter) Do(ctx context.Context) error {
	defer func() { p.tryNum++ }()
	if p.tryNum < p.n {
		fmt.Printf("fail #%d\n", p.tryNum)
		return fmt.Errorf("transient")
	}
	fmt.Printf("pass #%d\n", p.tryNum)
	return nil
}

// zeroTimer is a backoff Timer that fires immediately. Use in examples /
// tests to skip real backoff sleeps.
type zeroTimer struct{ t *time.Timer }

func (z *zeroTimer) C() <-chan time.Time   { return z.t.C }
func (z *zeroTimer) Start(d time.Duration) { z.t = time.NewTimer(0) }
func (z *zeroTimer) Stop()                 { z.t.Stop() }
