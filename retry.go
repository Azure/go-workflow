package workflow

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
)

var DefaultRetryOption = RetryOption{
	Backoff:  backoff.NewExponentialBackOff(),
	Attempts: 10,
}

type RetryOption struct {
	Timeout  time.Duration // 0 means no timeout, it's per-Retry timeout
	Attempts uint64        // 0 means no limit
	StopIf   func(ctx context.Context, attempt uint64, since time.Duration, err error) bool
	Backoff  backoff.BackOff
	Notify   backoff.Notify
	Timer    backoff.Timer
}

func (s *Workflow) retry(opt *RetryOption) func(
	ctx context.Context,
	fn func(context.Context) error,
	notAfter time.Time, // the Step level timeout ddl
) error {
	return func(ctx context.Context, fn func(context.Context) error, notAfter time.Time) error {
		if opt.Attempts > 0 {
			opt.Backoff = backoff.WithMaxRetries(opt.Backoff, opt.Attempts)
		}
		opt.Backoff = backoff.WithContext(opt.Backoff, ctx)
		attempt := uint64(0)
		start := s.clock.Now()
		return backoff.RetryNotifyWithTimer(
			func() error {
				defer func() { attempt++ }()
				ctx, cancel := s.clock.WithTimeout(ctx, opt.Timeout)
				defer cancel()
				err := fn(ctx)
				if err == nil {
					return nil
				}
				if !notAfter.IsZero() && s.clock.Now().After(notAfter) { // Step level timeouted
					err = backoff.Permanent(err)
				}
				if opt.StopIf != nil && opt.StopIf(ctx, attempt, s.clock.Since(start), err) {
					err = backoff.Permanent(err)
				}
				return err
			},
			opt.Backoff,
			opt.Notify,
			opt.Timer,
		)
	}
}
