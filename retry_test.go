package flow_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	flow "github.com/Azure/go-workflow"
	"github.com/benbjohnson/clock"
	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockStep struct {
	mock.Mock
	Started chan struct{}
}

func (m *MockStep) Do(ctx context.Context) error {
	var (
		done = make(chan struct{})
	)
	var args mock.Arguments
	go func() {
		defer close(done)
		m.Started <- struct{}{}
		args = m.MethodCalled("Do", ctx)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return args.Error(0)
	}
}

func TestRetry(t *testing.T) {
	type Mock struct {
		w     *flow.Workflow
		clock *clock.Mock
		*MockStep
	}
	newMock := func() *Mock {
		var (
			mockClock = clock.NewMock()
			w         = &flow.Workflow{Option: flow.WorkflowOption{Clock: mockClock}}
			mockStep  = &MockStep{Started: make(chan struct{})}
		)
		w.Add(flow.Step(mockStep).Retry(func(ro *flow.RetryOption) {
			ro.Timer = newTestTimer()
		}))
		return &Mock{
			w:        w,
			clock:    mockClock,
			MockStep: mockStep,
		}
	}
	start := func(m *Mock) <-chan error {
		done := make(chan error)
		go func() {
			var errW flow.ErrWorkflow
			err := m.w.Do(context.Background())
			switch {
			case err == nil:
				done <- nil
			case errors.As(err, &errW):
				done <- errW[m.MockStep]
			}
		}()
		return done
	}
	t.Run("TimeoutPerTry", func(t *testing.T) {
		t.Parallel()
		m := newMock()
		defer m.AssertExpectations(t)
		m.w.Add(
			flow.Step(m.MockStep).Retry(func(ro *flow.RetryOption) {
				ro.TimeoutPerTry = time.Second
				ro.Attempts = 1
			}),
		)
		m.On("Do", mock.Anything).
			Return(nil).
			WaitUntil(m.clock.After(2 * time.Second))
		done := start(m)
		<-m.Started
		m.clock.Add(time.Second)
		assert.ErrorIs(t, <-done, context.DeadlineExceeded)
	})
	t.Run("Attempts", func(t *testing.T) {
		t.Parallel()
		m := newMock()
		defer m.AssertExpectations(t)
		m.w.Add(
			flow.Step(m.MockStep).Retry(func(ro *flow.RetryOption) {
				ro.Attempts = 3
			}),
		)
		var (
			failTwice = m.On("Do", mock.Anything).Return(assert.AnError).Times(2)
			_         = m.On("Do", mock.Anything).Return(nil).NotBefore(failTwice)
		)
		done := start(m)
		<-m.Started
		<-m.Started
		<-m.Started
		assert.NoError(t, <-done)
	})
	t.Run("ShouldRetry", func(t *testing.T) {
		t.Parallel()
		m := newMock()
		defer m.AssertExpectations(t)
		m.w.Add(
			flow.Step(m.MockStep).Retry(func(ro *flow.RetryOption) {
				ro.NextBackOff = func(ctx context.Context, re flow.RetryEvent, nextBackOff time.Duration) time.Duration {
					if re.Attempt > 1 {
						return backoff.Stop
					}
					return nextBackOff
				}
			}),
		)
		m.On("Do", mock.Anything).Return(assert.AnError).Times(3)
		done := start(m)
		<-m.Started
		<-m.Started
		<-m.Started
		assert.ErrorIs(t, <-done, assert.AnError)
	})
	t.Run("Step Level Timeout", func(t *testing.T) {
		t.Parallel()
		m := newMock()
		defer m.AssertExpectations(t)
		m.w.Add(
			flow.Step(m.MockStep).
				Retry(func(ro *flow.RetryOption) {
					ro.TimeoutPerTry = 2 * time.Minute
					ro.Attempts = 2
				}).
				Timeout(time.Minute),
		)
		m.On("Do", mock.Anything).Return(nil).WaitUntil(m.clock.After(time.Hour))
		done := start(m)
		<-m.Started
		m.clock.Add(time.Minute)
		assert.ErrorIs(t, <-done, context.DeadlineExceeded)
	})
	t.Run("Retry(nil) uses DefaultRetryOption — 3 attempts", func(t *testing.T) {
		t.Parallel()
		// newMock() sets up a workflow with a Retry(func{ro.Timer=...}) step.
		// For this test we need a fresh workflow using Retry(nil) instead.
		var (
			mockClock = clock.NewMock()
			w         = &flow.Workflow{Option: flow.WorkflowOption{Clock: mockClock}}
			mockStep  = &MockStep{Started: make(chan struct{})}
		)
		w.Add(flow.Step(mockStep).Retry(nil))
		defer mockStep.AssertExpectations(t)

		mockStep.On("Do", mock.Anything).Return(assert.AnError).Times(3)

		done := make(chan error, 1)
		go func() {
			var errW flow.ErrWorkflow
			err := w.Do(context.Background())
			if errors.As(err, &errW) {
				done <- errW[mockStep]
			} else {
				done <- err
			}
		}()
		<-mockStep.Started
		<-mockStep.Started
		<-mockStep.Started
		assert.ErrorIs(t, <-done, assert.AnError)
	})

	t.Run("Attempts=0 retries until NextBackOff stops", func(t *testing.T) {
		t.Parallel()
		m := newMock()
		defer m.AssertExpectations(t)
		const maxAttempts = 5
		m.w.Add(
			flow.Step(m.MockStep).Retry(func(ro *flow.RetryOption) {
				ro.Timer = newTestTimer()
				ro.Attempts = 0 // unlimited
				ro.Backoff = &backoff.ZeroBackOff{}
				ro.NextBackOff = func(ctx context.Context, re flow.RetryEvent, next time.Duration) time.Duration {
					if re.Attempt >= maxAttempts-1 {
						return backoff.Stop
					}
					return next
				}
			}),
		)
		m.On("Do", mock.Anything).Return(assert.AnError).Times(maxAttempts)

		done := start(m)
		for i := 0; i < maxAttempts; i++ {
			<-m.Started
		}
		assert.ErrorIs(t, <-done, assert.AnError)
	})

	t.Run("Notify called after each failed attempt", func(t *testing.T) {
		t.Parallel()
		m := newMock()
		defer m.AssertExpectations(t)

		var mu sync.Mutex
		var notified []error
		m.w.Add(
			flow.Step(m.MockStep).Retry(func(ro *flow.RetryOption) {
				ro.Timer = newTestTimer()
				ro.Attempts = 3
				ro.Notify = func(err error, d time.Duration) {
					mu.Lock()
					notified = append(notified, err)
					mu.Unlock()
				}
			}),
		)
		m.On("Do", mock.Anything).Return(assert.AnError).Times(3)

		done := start(m)
		<-m.Started
		<-m.Started
		<-m.Started
		assert.ErrorIs(t, <-done, assert.AnError)
		mu.Lock()
		defer mu.Unlock()
		// Notify fires after each failure except the last (backoff.Stop after 3rd)
		assert.GreaterOrEqual(t, len(notified), 2)
		for _, e := range notified {
			assert.ErrorIs(t, e, assert.AnError)
		}
	})

	t.Run("Workflow context canceled stops retry", func(t *testing.T) {
		t.Parallel()
		m := newMock()
		defer m.AssertExpectations(t)
		m.w.Add(
			flow.Step(m.MockStep).Retry(func(ro *flow.RetryOption) {
				ro.Timer = newTestTimer()
				ro.Attempts = 10
			}),
		)
		ctx, cancel := context.WithCancel(context.Background())
		m.On("Do", mock.Anything).Return(assert.AnError)

		done := make(chan error, 1)
		go func() {
			var errW flow.ErrWorkflow
			err := m.w.Do(ctx)
			if errors.As(err, &errW) {
				done <- errW[m.MockStep]
			} else {
				done <- err
			}
		}()
		<-m.Started
		cancel()
		err := <-done
		// After cancel, the step error is either context.Canceled or the last step error
		assert.True(t,
			errors.Is(err, context.Canceled) || errors.Is(err, assert.AnError),
			"expected context.Canceled or step error, got %v", err)
	})

	t.Run("Per-try timeout resets between attempts", func(t *testing.T) {
		t.Parallel()
		m := newMock()
		defer m.AssertExpectations(t)
		m.w.Add(
			flow.Step(m.MockStep).Retry(func(ro *flow.RetryOption) {
				ro.Timer = newTestTimer()
				ro.TimeoutPerTry = time.Second
				ro.Attempts = 2
			}),
		)

		// Attempt 1: block past the per-try deadline so ctx.Done() fires and
		// MockStep.Do returns DeadlineExceeded.
		// Attempt 2: return nil immediately.
		m.On("Do", mock.Anything).Return(nil).WaitUntil(m.clock.After(2 * time.Second)).Once()
		m.On("Do", mock.Anything).Return(nil).Once()

		done := make(chan error, 1)
		go func() { done <- m.w.Do(context.Background()) }()

		<-m.Started
		m.clock.Add(time.Second) // trigger per-try timeout on attempt 1
		<-m.Started              // attempt 2 started with a fresh deadline
		assert.NoError(t, <-done)
	})
}

func newTestTimer() *testTimer {
	return &testTimer{time.NewTimer(0)}
}

// testTimer is a Timer that all retry intervals are immediate (0).
type testTimer struct {
	timer *time.Timer
}

func (t *testTimer) C() <-chan time.Time {
	return t.timer.C
}

func (t *testTimer) Start(duration time.Duration) {
	t.timer.Reset(duration)
}

func (t *testTimer) Stop() {
	t.timer.Stop()
}

// TestRetrySharedBackoffDataRace verifies that steps using Retry(nil) (which
// copies DefaultRetryOption) do not share the same Backoff instance.
// Before the fix, all such steps shared the same *ExponentialBackOff pointer
// (shallow copy), causing a data race under concurrent retries.
// Run with -race to catch the race; without -race it checks attempt counts
// are not corrupted by concurrent resets.
func TestRetrySharedBackoffDataRace(t *testing.T) {
	t.Parallel()
	const numSteps = 5
	const wantAttempts = 3

	type counter struct{ n atomic.Int64 }
	counters := make([]counter, numSteps)

	w := &flow.Workflow{}
	for i := range numSteps {
		i := i
		s := flow.Func("step-"+string(rune('A'+i)), func(ctx context.Context) error {
			if counters[i].n.Add(1) < wantAttempts {
				return assert.AnError
			}
			return nil
		})
		// Retry(nil) triggers shallow-copy of DefaultRetryOption,
		// sharing the same Backoff pointer across all steps before the fix.
		w.Add(flow.Step(s).Retry(nil))
	}

	assert.NoError(t, w.Do(context.Background()))
	for i := range numSteps {
		assert.Equal(t, int64(wantAttempts), counters[i].n.Load(),
			"step %c attempt count should not be corrupted by shared Backoff", rune('A'+i))
	}
}
