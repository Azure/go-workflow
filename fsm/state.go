package fsm

import (
	"context"
)

// FiniteState is a state in Finite-State-Machine
type FiniteState interface {
	IsTerminal() bool              // IsTerminal returns true if the state is a terminal (end) state
	Do(context.Context) Transition // Do returns the next transition
}

type State struct{}

func (s *State) IsTerminal() bool { return false }

type EndState struct{}

func (e *EndState) IsTerminal() bool              { return true }
func (e *EndState) Do(context.Context) Transition { return nil }
