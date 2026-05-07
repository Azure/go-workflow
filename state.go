package flow

import (
	"context"
	"sync"
)

// State is the per-step bookkeeping that a Workflow keeps for every Step it
// orchestrates. It carries two things:
//
//   - StepResult — the runtime status, the error from the last attempt, and
//     the time the step finished (zero if it never ran).
//   - Config    — the dependency edges, before/after callbacks and step
//     options (retry, condition, timeout) declared via Step()/Steps()/Pipe().
//
// State is read and written from multiple goroutines (the tick loop and the
// per-step worker), so every accessor goes through the embedded RWMutex.
// Use the GetXxx / SetXxx helpers rather than touching StepResult directly.
type State struct {
	StepResult
	Config *StepConfig
	sync.RWMutex
}

// GetStatus returns the current StepStatus under read lock.
func (s *State) GetStatus() StepStatus {
	s.RLock()
	defer s.RUnlock()
	return s.Status
}

// SetStatus replaces the StepStatus under write lock. Other StepResult fields
// (Err, FinishedAt) are left as-is — use SetStepResult to write them together.
func (s *State) SetStatus(ss StepStatus) {
	s.Lock()
	defer s.Unlock()
	s.Status = ss
}

// GetStepResult returns a snapshot of the full StepResult under read lock.
func (s *State) GetStepResult() StepResult {
	s.RLock()
	defer s.RUnlock()
	return s.StepResult
}

// SetStepResult atomically replaces the entire StepResult (status, error and
// finish time). Used at terminal transitions where all three change together.
func (s *State) SetStepResult(r StepResult) {
	s.Lock()
	defer s.Unlock()
	s.StepResult = r
}

// GetError is a convenience over GetStepResult().Err.
func (s *State) GetError() error { return s.GetStepResult().Err }

// SetError replaces only the Err field under write lock.
func (s *State) SetError(err error) {
	s.Lock()
	defer s.Unlock()
	s.Err = err
}

// Upstreams returns the set of steps this step depends on, or nil if no
// dependencies have been declared yet.
func (s *State) Upstreams() Set[Steper] {
	if s.Config == nil {
		return nil
	}
	return s.Config.Upstreams
}

// Option folds every registered option function into a fresh StepOption and
// returns it. The result is always non-nil; absent options leave their fields
// at their zero value (no retry, no timeout, default Condition).
func (s *State) Option() *StepOption {
	opt := &StepOption{}
	if s.Config != nil && s.Config.Option != nil {
		for _, o := range s.Config.Option {
			o(opt)
		}
	}
	return opt
}

// Before runs every BeforeStep callback registered via Input() / BeforeStep()
// in declaration order. Each callback can swap the context.Context that is
// threaded into the next callback (and ultimately into Step.Do). The first
// callback to return an error short-circuits the chain and that error is
// returned alongside the most-recent context.
//
// The do parameter is the panic-catching wrapper supplied by the caller —
// passing each callback through `do` lets a panicking callback be turned into
// an error when Workflow.DontPanic is true.
func (s *State) Before(root context.Context, step Steper, do func(func() error) error) (context.Context, error) {
	if s.Config == nil || len(s.Config.Before) == 0 {
		return root, nil
	}
	ctx := root
	for _, b := range s.Config.Before {
		if err := do(func() error {
			ctxReturned, err := b(ctx, step)
			ctx = ctxReturned // use the context returned by Before callback
			return err
		}); err != nil {
			return ctx, err
		}
	}
	return ctx, nil
}

// After runs every AfterStep callback registered via Output() / AfterStep()
// in declaration order, threading the error from each callback into the next.
// Unlike Before, callbacks are NOT short-circuited by an error — every After
// runs and the final error is whatever the last callback returns. This lets
// AfterStep callbacks observe, transform, or even swallow the error.
func (s *State) After(ctx context.Context, step Steper, err error) error {
	if s.Config == nil || len(s.Config.After) == 0 {
		return err
	}
	for _, a := range s.Config.After {
		err = a(ctx, step, err)
	}
	return err
}

// AddUpstream declares that this step depends on `up`. Lazily allocates
// Config / Config.Upstreams. Nil upstreams are silently ignored.
func (s *State) AddUpstream(up Steper) {
	if s.Config == nil {
		s.Config = &StepConfig{}
	}
	if s.Config.Upstreams == nil {
		s.Config.Upstreams = make(Set[Steper])
	}
	if up != nil {
		s.Config.Upstreams.Add(up)
	}
}

// MergeConfig folds another StepConfig into this state's config (union of
// upstreams, concatenation of Before / After / Option). Used when the same
// step is referenced by multiple Add() calls or when a composite step is
// replaced by a new root that absorbs prior configuration.
func (s *State) MergeConfig(sc *StepConfig) {
	if s.Config == nil {
		s.Config = &StepConfig{}
	}
	s.Config.Merge(sc)
}
