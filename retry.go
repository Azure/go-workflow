package flow

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
)

var DefaultRetryOption = RetryOption{
	Backoff:  backoff.NewExponentialBackOff(),
	Attempts: 3,
}

// RetryOption customizes retry behavior of a Step in Workflow.
type RetryOption struct {
	Timeout  time.Duration // 0 means no timeout, it's per-retry timeout
	Attempts uint64        // 0 means no limit
	StopIf   func(ctx context.Context, attempt uint64, since time.Duration, err error) bool
	Backoff  backoff.BackOff
	Notify   backoff.Notify
	Timer    backoff.Timer
}

// retry constructs a do function with retry enabled according to the option.
func (w *Workflow) retry(opt *RetryOption) func(
	ctx context.Context,
	fn func(context.Context) error,
	notAfter time.Time, // the Step level timeout ddl
) error {
	if opt == nil {
		return func(ctx context.Context, fn func(context.Context) error, notAfter time.Time) error {
			return fn(ctx)
		}
	}
	return func(ctx context.Context, fn func(context.Context) error, notAfter time.Time) error {
		backOff := opt.Backoff
		backOff = backoff.WithContext(backOff, ctx)
		if opt.Attempts > 0 {
			backOff = backoff.WithMaxRetries(backOff, opt.Attempts)
		}
		attempt := uint64(0)
		start := w.clock.Now()
		return backoff.RetryNotifyWithTimer(
			func() error {
				defer func() { attempt++ }()
				ctx := ctx
				var cancel func()

				if opt.Timeout > 0 {
					ctx, cancel = w.clock.WithTimeout(ctx, opt.Timeout)
				} else {
					ctx, cancel = context.WithCancel(ctx)
				}
				defer cancel()
				err := fn(ctx)
				if err == nil {
					return nil
				}
				if !notAfter.IsZero() && w.clock.Now().After(notAfter) { // Step level timeouted
					err = backoff.Permanent(err)
				}
				if opt.StopIf != nil && opt.StopIf(ctx, attempt, w.clock.Since(start), err) {
					err = backoff.Permanent(err)
				}
				return err
			},
			backOff,
			opt.Notify,
			opt.Timer,
		)
	}
}
