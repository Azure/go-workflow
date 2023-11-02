package workflow_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	fl "go.goms.io/aks/rp/test/v3/workflow"
)

// `workflow` provides different levels of timeout:
//
//   - Retry level timeout
//   - Step level timeout

// WaitDone will be pending until the context is done.
type WaitDone struct {
	fl.Base
	StartDo chan<- struct{} // signal it everytime start Do()
}

func (p *WaitDone) Do(ctx context.Context) error {
	p.StartDo <- struct{}{}
	<-ctx.Done()
	fmt.Println("done")
	return ctx.Err()
}

func ExampleTimeout() {
	// use mock clock
	mock := clock.NewMock()

	flow := new(fl.Workflow).
		Options(fl.WithClock(mock))

	started := make(chan struct{})

	flow.Add(
		fl.Steps(&WaitDone{
			Base:    fl.Base{Name: "WaitDone"},
			StartDo: started,
		}).Retry(func(ro *fl.RetryOption) {
			ro.Attempts = 5
			ro.Timer = new(testTimer)
			ro.Timeout = 1000 * time.Millisecond // Retry level timeout
		}).Timeout(1500 * time.Millisecond), // Step level timeout
	)

	// Step level timeout is checked after retry returned.
	// |---------|----|----|
	// 0         1   1.5   2
	// done      done canceled

	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		// you can, actually, pass a context with timeout to set a Workflow level timeout
		err = flow.Run(context.Background())
		wg.Done()
	}()
	go func() {
		for range started {
			mock.Add(time.Second) // tick forward 1 second
		}
	}()
	wg.Wait()

	fmt.Println(err)
	// Output:
	// done
	// done
	// WaitDone [Failed]: context deadline exceeded
}

// testTimer is a Timer that all retry intervals are immediate (0).
type testTimer struct {
	timer *time.Timer
}

func (t *testTimer) C() <-chan time.Time {
	return t.timer.C
}

func (t *testTimer) Start(duration time.Duration) {
	t.timer = time.NewTimer(0)
}

func (t *testTimer) Stop() {
	t.timer.Stop()
}
