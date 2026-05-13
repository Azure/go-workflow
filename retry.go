package flow

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// DefaultRetryOption is the policy used as the seed when a step calls Retry()
// with no overriding mutator. It is also the policy used when Retry(nil) is
// called explicitly.
//
// Note: each step takes its OWN copy of this option, with Backoff cleared to
// nil so that the per-run retry() allocates a fresh backoff.BackOff. This
// avoids a data race on the shared NewExponentialBackOff() instance when
// multiple steps retry concurrently.
var DefaultRetryOption = RetryOption{
	Backoff:  backoff.NewExponentialBackOff(),
	Attempts: 3,
}

// RetryOption controls how a step is retried.
//
// The semantics map onto cenkalti/backoff/v4 internals:
//
//   - Attempts: total number of attempts including the first try (so 3 means
//     "try, retry, retry"). 0 means "no attempt cap"; the run is then bounded
//     only by Backoff/ctx/notAfter.
//   - TimeoutPerTry: deadline applied to each individual attempt's context.
//     0 means "no per-try deadline"; the attempt is bounded by the step-level
//     Timeout (if any) and by ctx.
//   - Backoff: the backoff strategy. If left nil at retry time, retry()
//     allocates a fresh ExponentialBackOff for this run (see DefaultRetryOption).
//   - Notify / Timer: passed straight through to backoff.RetryNotifyWithTimer.
type RetryOption struct {
	TimeoutPerTry time.Duration
	Attempts      uint64
	// NextBackOff is invoked AFTER each failed attempt to (optionally) override
	// the next backoff duration computed by Backoff. It is NOT called when:
	//   - Attempts cap has been reached, or
	//   - ctx has fired (cancel / deadline), or
	//   - the inner Backoff returned backoff.Stop, or
	//   - the step-level Timeout (notAfter) has elapsed.
	//
	// Arguments:
	//   re          — what just happened (attempt number, total elapsed, last error).
	//   nextBackOff — the duration the inner Backoff suggests next.
	NextBackOff func(ctx context.Context, re RetryEvent, nextBackOff time.Duration) time.Duration

	Backoff backoff.BackOff
	Notify  backoff.Notify
	Timer   backoff.Timer
}

// RetryEvent is a snapshot of a single failed attempt, fed to NextBackOff.
type RetryEvent struct {
	Attempt uint64        // 0-based index of the attempt that just failed.
	Since   time.Duration // time elapsed since the first attempt started.
	Error   error         // error returned by the failed attempt.
}

// retry returns a wrapper that will run `do` with retry semantics derived from
// `opt`. If opt is nil the wrapper runs `do` exactly once.
//
// The wrapper threads through the step-level deadline (`notAfter`) so the
// retry loop can stop early if the deadline is about to elapse, and applies
// `TimeoutPerTry` (if set) by deriving a per-attempt context.
func (w *Workflow) retry(opt *RetryOption) func(
	ctx context.Context,
	do func(context.Context) error,
	notAfter time.Time, // step-level Timeout deadline; zero means "none".
) error {
	if opt == nil {
		return func(ctx context.Context, do func(context.Context) error, notAfter time.Time) error { return do(ctx) }
	}
	return func(ctx context.Context, do func(context.Context) error, notAfter time.Time) error {
		backOff := opt.Backoff
		if backOff == nil {
			// Backoff was not set (or was cleared to avoid sharing
			// DefaultRetryOption's mutable Backoff instance). Allocate a
			// fresh one so concurrent retries don't race on shared state.
			backOff = backoff.NewExponentialBackOff()
		}
		backOff = backoff.WithContext(backOff, ctx)
		if !notAfter.IsZero() {
			backOff = &backOffStopIfTimeout{BackOff: backOff, NotAfter: notAfter, Now: w.clock().Now}
		}
		if opt.Attempts > 0 {
			// WithMaxRetries counts RETRIES, not total attempts — Attempts=N
			// means "1 initial + (N-1) retries".
			backOff = backoff.WithMaxRetries(backOff, opt.Attempts-1)
		}
		retried := func(ctx context.Context, e RetryEvent) {}
		if opt.NextBackOff != nil {
			b := &backOffWithEvent{BackOff: backOff, nextBackOff: opt.NextBackOff}
			retried = b.retried
			backOff = b
		}
		e := RetryEvent{Attempt: 0}
		start := w.clock().Now()
		return backoff.RetryNotifyWithTimer(
			func() error {
				defer func() {
					retried(ctx, e)
					e.Attempt++
				}()
				ctxPerTry := ctx
				if opt.TimeoutPerTry > 0 {
					var cancel context.CancelFunc
					ctxPerTry, cancel = w.clock().WithTimeout(ctx, opt.TimeoutPerTry)
					defer cancel()
				}
				err := do(ctxPerTry)
				e.Since = w.clock().Since(start)
				e.Error = err
				return err
			},
			backOff,
			opt.Notify,
			opt.Timer,
		)
	}
}

// backOffWithEvent is a thin BackOff decorator that lets the user-supplied
// NextBackOff observe each retry event and override the next backoff.
// retried() is called from inside the retry function (not from NextBackOff
// itself) so the event reflects the attempt that just finished.
type backOffWithEvent struct {
	backoff.BackOff
	nextBackOff func(context.Context, RetryEvent, time.Duration) time.Duration

	ctx context.Context
	e   RetryEvent
}

// NextBackOff defers to the inner Backoff first; if it returned backoff.Stop
// the retry loop is finished and the user override is not called.
func (b *backOffWithEvent) NextBackOff() time.Duration {
	bkof := b.BackOff.NextBackOff()
	if b.nextBackOff == nil || bkof == backoff.Stop {
		return backoff.Stop
	}
	return b.nextBackOff(b.ctx, b.e, bkof)
}

// retried is called by the retry loop after each failed attempt to publish
// the event for the next NextBackOff() invocation.
func (b *backOffWithEvent) retried(ctx context.Context, e RetryEvent) {
	b.ctx = ctx
	b.e = e
}

// backOffStopIfTimeout fires backoff.Stop as soon as the step-level deadline
// (NotAfter) has been crossed, so the retry loop doesn't sleep into a
// deadline that's about to elapse.
type backOffStopIfTimeout struct {
	backoff.BackOff
	NotAfter time.Time
	Now      func() time.Time
}

// NextBackOff returns backoff.Stop if the step deadline has passed (or any
// supporting field is missing); otherwise the inner backoff value is used.
func (b *backOffStopIfTimeout) NextBackOff() time.Duration {
	bkof := b.BackOff.NextBackOff()
	if b.NotAfter.IsZero() || b.Now == nil || bkof == backoff.Stop || b.Now().After(b.NotAfter) {
		return backoff.Stop
	}
	return bkof
}
