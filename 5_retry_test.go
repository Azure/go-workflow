package workflow_test

import (
	"context"
	"fmt"

	fl "go.goms.io/aks/rp/test/v3/workflow"
)

// Step dependency builders also supports adding retry for Step.
//
//	type RetryOption struct {
//		Timeout  time.Duration // 0 means no timeout, it's per-Retry timeout
//		Attempts uint64        // 0 means no limit
//		StopIf   func(ctx context.Context, attempt uint64, since time.Duration, err error) bool
//		Backoff  backoff.BackOff
//		Notify   backoff.Notify
//		Timer    backoff.Timer
//	}

// PassAfter keeps failing until the attempt reaches the given number.
type PassAfter struct {
	fl.BaseEmptyIO
	Attempt int
	count   int
}

func (p *PassAfter) Do(ctx context.Context) error {
	defer func() { p.count++ }()
	if p.count >= p.Attempt {
		fmt.Printf("succeed at attempt %d\n", p.count)
		return nil
	}
	err := fmt.Errorf("failed at attempt %d", p.count)
	fmt.Println(err)
	return err
}

func ExampleRetry() {
	flow := new(fl.Workflow)

	flow.Add(
		fl.Step(&PassAfter{
			Attempt: 2,
		}).Retry(func(ro *fl.RetryOption) {
			ro.Attempts = 5 // retry 5 times
			ro.Timer = new(testTimer)
		}),
	)

	_ = flow.Run(context.TODO())
	// Output:
	// failed at attempt 0
	// failed at attempt 1
	// succeed at attempt 2
}
