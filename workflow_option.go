package flow

import "github.com/benbjohnson/clock"

// WorkflowOption alters the behavior of a Workflow.
type WorkflowOption func(*Workflow)

func (s *Workflow) Options(opts ...WorkflowOption) *Workflow {
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithMaxConcurrency limits the max concurrency of Steps in StepStatusRunning.
func WithMaxConcurrency(n int) WorkflowOption {
	return func(s *Workflow) {
		// use buffered channel as a sized bucket
		// a Step needs to create a lease in the bucket to run,
		// and remove the lease from the bucket when it's done.
		s.leaseBucket = make(chan struct{}, n)
	}
}

func WithClock(clock clock.Clock) WorkflowOption {
	return func(s *Workflow) {
		s.clock = clock
	}
}

func WithNotify(notify Notify) WorkflowOption {
	return func(w *Workflow) {
		w.notify = append(w.notify, notify)
	}
}
