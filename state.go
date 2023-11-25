package flow

import (
	"context"
	"sync"
)

// State is the internal state of a Step in a Workflow.
//
// It has the status and the config (dependency, input, retry option, condition, timeout, etc.) of the step.
// The status could be read / write from different goroutines, so use RWMutex to protect it.
type State struct {
	Status StepStatus
	Config *StepConfig
	sync.RWMutex
}

func (s *State) GetStatus() StepStatus {
	s.RLock()
	defer s.RUnlock()
	return s.Status
}
func (s *State) SetStatus(ss StepStatus) {
	s.Lock()
	defer s.Unlock()
	s.Status = ss
}
func (s *State) Upstreams() Set[Steper] {
	if s.Config == nil {
		return nil
	}
	return s.Config.Upstreams
}
func (s *State) Option() *StepOption {
	opt := &StepOption{}
	if s.Config != nil && s.Config.Option != nil {
		s.Config.Option(opt)
	}
	return opt
}
func (s *State) Input(ctx context.Context) error {
	if s.Config == nil || s.Config.Input == nil {
		return nil
	}
	return s.Config.Input(ctx)
}
func (s *State) MergeConfig(sc *StepConfig) {
	if s.Config == nil {
		s.Config = &StepConfig{}
	}
	s.Config.Merge(sc)
}
