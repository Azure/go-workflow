package reconcile

import (
	"context"

	"github.com/Azure/go-workflow/fsm"
	"github.com/cenkalti/backoff/v4"
)

type Decision struct {
	fsm.Transition
	BackOff backoff.BackOff
	Err     error
	// IsRetry and IsContinue are two special transition,
	// state will make its own transition decision based on these two.
	IsRetry    bool
	IsContinue bool

	retryOption *RetryOption
}

// GetBackOff implements fsm.TransitionWithBackOff interface
func (dec Decision) GetBackOff() backoff.BackOff {
	if dec.BackOff != nil {
		return dec.BackOff
	}
	if dec.retryOption != nil {
		return dec.retryOption
	}
	return new(backoff.ZeroBackOff)
}

func (dec Decision) Error() string {
	return dec.Err.Error()
}

func (dec Decision) Unwarp() error {
	return dec.Err
}

func Fail(err error) (dec Decision) {
	dec.Transition = fsm.TransitionTo[*Failed](func(ctx context.Context, f *Failed) {
		f.Err = err
	})
	dec.Err = err
	return
}

func Succeed[T any](value *T) (dec Decision) {
	dec.Transition = fsm.TransitionTo[*Succeeded[T]](func(ctx context.Context, s *Succeeded[T]) {
		s.FinalState = value
	})
	return
}

func Continue() (dec Decision) {
	dec.IsContinue = true
	return
}

func Retry(err error) (dec Decision) {
	dec.Err = err
	dec.IsRetry = true
	return
}

func RetryAfter(err error, b backoff.BackOff) (dec Decision) {
	dec.Err = err
	dec.BackOff = b
	dec.IsRetry = true
	return
}
