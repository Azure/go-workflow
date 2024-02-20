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
	TimeoutPerTry time.Duration // 0 means no timeout
	Attempts      uint64        // 0 means no limit
	StopIf        func(ctx context.Context, attempt uint64, since time.Duration, err error) bool
	Backoff       backoff.BackOff
	Notify        backoff.Notify
	Timer         backoff.Timer
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
		start := w.Clock.Now()
		return backoff.RetryNotifyWithTimer(
			func() error {
				defer func() { attempt++ }()
				if opt.TimeoutPerTry > 0 {
					var cancel func()
					ctx, cancel = w.Clock.WithTimeout(ctx, opt.TimeoutPerTry)
					defer cancel()
				}
				if err := fn(ctx); err != nil {
					switch {
					case !notAfter.IsZero() && w.Clock.Now().After(notAfter), // Step level timeout
						opt.StopIf != nil && opt.StopIf(ctx, attempt, w.Clock.Since(start), err):
						return backoff.Permanent(err)
					default:
						return err
					}
				}
				return nil
			},
			backOff,
			opt.Notify,
			opt.Timer,
		)
	}
}
