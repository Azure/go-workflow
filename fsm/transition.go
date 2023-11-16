package fsm

import (
	"context"
	"reflect"

	"github.com/cenkalti/backoff/v4"
)

// Transition decides the next state to hop
type Transition interface {
	Next() reflect.Type
	FeedInputTo(context.Context, any)
}

type TransitionWithBackOff interface {
	Transition
	GetBackOff() backoff.BackOff
}

// TransitionTo returns a transition to the next state, with optional inputs feeding into next state.
func TransitionTo[S FiniteState](inputs ...func(context.Context, S)) *transition[S] {
	return &transition[S]{
		next: reflect.TypeOf(*new(S)),
		feedInputTo: func(ctx context.Context, s S) {
			for _, input := range inputs {
				input(ctx, s)
			}
		},
	}
}

type transition[S FiniteState] struct {
	next        reflect.Type
	feedInputTo func(context.Context, S)
}

func (t *transition[S]) Next() reflect.Type {
	return t.next
}

func (t *transition[S]) FeedInputTo(ctx context.Context, next any) {
	if t.feedInputTo != nil {
		s, ok := next.(S)
		if ok {
			t.feedInputTo(ctx, s)
		}
	}
}

// WithBackOff delays the transition.
func WithBackOff(t Transition, backOff backoff.BackOff) *transitionWithBackOff {
	return &transitionWithBackOff{
		Transition: t,
		backOff:    backOff,
	}
}

type transitionWithBackOff struct {
	Transition
	backOff backoff.BackOff
}

func (t *transitionWithBackOff) GetBackOff() backoff.BackOff {
	return t.backOff
}
