package flow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
)

func TestRetry(t *testing.T) {
	mockClock := clock.NewMock()
	workflow := new(Workflow).
		Options(WithClock(mockClock))
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
				retryTimes += 1
				return errors.New("failed")
			}
			return nil
		},
		notAfter,
	)
	assert.Nil(t, err)
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
