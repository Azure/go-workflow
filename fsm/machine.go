package fsm

import (
	"context"
	"fmt"
	"reflect"

	"github.com/benbjohnson/clock"
	"github.com/cenkalti/backoff/v4"
)

// StateMachine is a Finite-State-Machine (FSM) that starts from a given state,
// Do() the state, and then transition to the next state until terminal state is reached.
//
// Please use one struct type to represent one FiniteState.
type StateMachine struct {
	Maps map[reflect.Type]FiniteState
	clock.Clock
}

func NewStateMachine(states ...FiniteState) (*StateMachine, error) {
	sm := &StateMachine{
		Maps:  make(map[reflect.Type]FiniteState),
		Clock: clock.New(),
	}
	for _, state := range states {
		typeOfState := reflect.TypeOf(state)
		if state == nil {
			return nil, fmt.Errorf("state %s should not be nil", typeOfState)
		}
		previous, existed := sm.Maps[typeOfState]
		if existed && previous != state {
			return nil, fmt.Errorf("each state should have unique instance: %s", typeOfState)
		}
		sm.Maps[typeOfState] = state
	}
	return sm, nil
}

func MustNewStateMachine(states ...FiniteState) *StateMachine {
	sm, err := NewStateMachine(states...)
	if err != nil {
		panic(err)
	}
	return sm
}

func (sm *StateMachine) Start(ctx context.Context, state FiniteState) (FiniteState, error) {
	if state == nil {
		return nil, nil
	}
	for !state.IsTerminal() {
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		default:
			transition := state.Do(ctx)
			if transition == nil {
				return state, fmt.Errorf("state %s returned nil transition", reflect.TypeOf(state))
			}
			next, exist := sm.Maps[transition.Next()]
			if !exist {
				return state, fmt.Errorf("state %s returned unknown next transition %s", reflect.TypeOf(state), transition.Next())
			}
			if withBackOff, ok := transition.(TransitionWithBackOff); ok {
				if backOff := withBackOff.GetBackOff(); backOff != nil {
					nextBackOff := backOff.NextBackOff()
					if nextBackOff == backoff.Stop {
						return state, fmt.Errorf("state %s returned Stop", reflect.TypeOf(state))
					}
					sm.Clock.Sleep(nextBackOff)
				}
			}
			transition.FeedInputTo(ctx, next)
			state = next
		}
	}
	return state, nil
}
