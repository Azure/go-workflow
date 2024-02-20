package flow

import (
	"context"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
)

func TestRetry(t *testing.T) {
	t.Run("Attempts until succeeded", func(t *testing.T) {
		mockClock := clock.NewMock()
		workflow := &Workflow{
			Clock: mockClock,
		}
		retryOpt := &RetryOption{
			Backoff:  backoff.NewExponentialBackOff(),
			Attempts: 3,
		}
		retryOpt.Timer = new(testTimer)
		retryFunc := workflow.retry(retryOpt)
		retryTimes := 0
		var notAfter time.Time
		err := retryFunc(
			context.Background(),
			func(ctx context.Context) error {
				if retryTimes < 3 {
					retryTimes++
					return assert.AnError
				}
				return nil
			},
			notAfter,
		)
		assert.NoError(t, err)
	})
	t.Run("Attempt but still fail", func(t *testing.T) {
		w := new(Workflow)
		w.Add(
			Step(Func("", func(ctx context.Context) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					return assert.AnError
				}
			})).Retry(func(ro *RetryOption) {
				ro.Timer = new(testTimer)
				ro.Attempts = 3
			}),
		)
		assert.ErrorIs(t, w.Do(context.Background()), assert.AnError)
	})
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
