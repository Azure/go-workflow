package workflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
)

// Workflow represents a collection of connected Steps that form a directed acyclic graph (DAG).
//
// Workflow executes Steps in a topological order,
// and flow the Output(s) from Upstream(s) to Input(s) of Downstream(s).
type Workflow struct {
	dep               Dependency     // Dependency is the graph of connected Steps
	errs              ErrWorkflow    // errors reported from all Steps
	errsMu            sync.RWMutex   // need this because errs are written from each Step's goroutine
	when              When           // Workflow level When
	leaseBucket       chan struct{}  // constraint max concurrency of running Steps
	waitGroup         sync.WaitGroup // to prevent goroutine leak, only Add(1) when a Step start running
	isRunning         sync.Mutex     // indicate whether the Workflow is running
	oneStepTerminated chan struct{}  // signals for next tick
	clock             clock.Clock    // clock for unit test
}

func New() *Workflow {
	return new(Workflow)
}

// Add appends Steps into Workflow.
func (s *Workflow) Add(dbs ...WorkflowStep) *Workflow {
	if s.dep == nil {
		s.dep = make(Dependency)
	}
	for _, db := range dbs {
		s.dep.Merge(db.done())
	}
	return s
}

// Dep returns the Steps and its dependencies in this Workflow.
//
// Modify the returned Dependency will not affect the graph in Workflow,
// use WorkflowInjector if you want to modify the graph.
//
// Iterate all Steps and its Upstreams:
//
//	for step, ups := range workflow.Dep() {
//		// do something with step
//		for _, link := range ups {
//			link.Upstream // do something with step's Upstream
//			link.Flow()   // this will send Output from Upstream to Downstream (step in this case)
//		}
//	}
func (s *Workflow) Dep() Dependency {
	// make a copy to prevent w.deps being modified
	d := make(Dependency)
	d.Merge(s.dep)
	return d
}

// Run starts the Step execution in topological order,
// and waits until all Steps terminated.
//
// Run will block the current goroutine.
func (s *Workflow) Run(ctx context.Context) error {
	if !s.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer s.isRunning.Unlock()
	if len(s.dep) == 0 {
		return nil
	}
	if s.when != nil && !s.when(ctx) {
		for step := range s.dep {
			step.setStatus(StepStatusSkipped)
		}
		return nil
	}
	// preflight check the initial state of workflow
	if err := s.preflight(); err != nil {
		return err
	}
	if s.clock == nil {
		s.clock = clock.New()
	}
	s.errs = make(ErrWorkflow)
	s.oneStepTerminated = make(chan struct{}, len(s.dep))
	// first tick
	s.tick(ctx)
	// each time one Step terminated, tick forward
	for range s.oneStepTerminated {
		if s.IsTerminated() {
			break
		}
		s.tick(ctx)
	}
	// consume all the following signals cooperataed with waitGroup
	s.waitGroup.Wait()
	close(s.oneStepTerminated)
	if err := s.Err(); err != nil {
		return err
	}
	return nil
}

const scanned StepStatus = "scanned" // a private status for preflight

func toStepReader(steps []Steper) []StepReader {
	var rv []StepReader
	for _, step := range steps {
		rv = append(rv, step)
	}
	return rv
}

func isAllUpstreamScanned(ups []StepReader) bool {
	for _, up := range ups {
		if up.GetStatus() != scanned {
			return false
		}
	}
	return true
}

func isAnyUpstreamNotTerminated(ups []StepReader) bool {
	for _, up := range ups {
		if !up.GetStatus().IsTerminated() {
			return true
		}
	}
	return false
}

func isAnyUpstreamSkipped(ups []StepReader) bool {
	for _, up := range ups {
		if up.GetStatus() == StepStatusSkipped {
			return true
		}
	}
	return false
}

func (s *Workflow) preflight() error {
	// check whether the workflow has been run
	if s.errs != nil {
		return ErrWorkflowHasRun
	}
	// assert all Steps' status start with Pending
	unexpectStatusSteps := []StepReader{}
	for step := range s.dep {
		if step.GetStatus() != StepStatusPending {
			unexpectStatusSteps = append(unexpectStatusSteps, step)
		}
	}
	if len(unexpectStatusSteps) > 0 {
		return ErrUnexpectStepInitStatus(unexpectStatusSteps)
	}
	// assert all dependency would not form a cycle
	// start scanning, mark Step as Scanned only when its all depdencies are Scanned
	for {
		hasNewScanned := false // whether a new Step being marked as Scanned this turn
		for step := range s.dep {
			if step.GetStatus() == scanned {
				continue
			}
			if isAllUpstreamScanned(toStepReader(s.dep.UpstreamOf(step))) {
				hasNewScanned = true
				step.setStatus(scanned)
			}
		}
		if !hasNewScanned { // break when no new Step being Scanned
			break
		}
	}
	// check whether still have Steps not Scanned,
	// not Scanned Steps are in a cycle.
	stepsInCycle := map[StepReader][]StepReader{}
	for step := range s.dep {
		if step.GetStatus() != scanned {
			for _, up := range s.dep.UpstreamOf(step) {
				if up.GetStatus() != scanned {
					stepsInCycle[step] = append(stepsInCycle[step], up)
				}
			}
		}
	}
	if len(stepsInCycle) > 0 {
		return ErrCycleDependency(stepsInCycle)
	}
	// reset all Steps' status to Pending
	for step := range s.dep {
		step.setStatus(StepStatusPending)
	}
	return nil
}

func (s *Workflow) signalTick() {
	s.oneStepTerminated <- struct{}{}
}

// tick will not block, it starts a goroutine for each runnable Step.
func (s *Workflow) tick(ctx context.Context) {
	for step := range s.dep {
		// continue if the Step is not Pending
		if step.GetStatus() != StepStatusPending {
			continue
		}
		// continue if any Upstream is not terminated
		ups := toStepReader(s.dep.UpstreamOf(step))
		if isAnyUpstreamNotTerminated(ups) {
			continue
		}
		// check whether the Step should be Skipped via When
		when := step.GetWhen()
		if when == nil {
			when = DefaultWhenFunc
		}
		if isAnyUpstreamSkipped(ups) || !when(ctx) {
			step.setStatus(StepStatusSkipped)
			s.signalTick()
			continue
		}
		// check whether the Step should be Canceled via Condition
		cond := step.GetCondition()
		if cond == nil {
			cond = DefaultCondition
		}
		if !cond(ups) {
			step.setStatus(StepStatusCanceled)
			s.signalTick()
			continue
		}
		// if WithMaxConcurrency is set
		if s.leaseBucket != nil {
			s.leaseBucket <- struct{}{} // lease
		}
		// start the Step
		step.setStatus(StepStatusRunning)
		s.waitGroup.Add(1)
		go func(ctx context.Context, step Steper) {
			defer s.waitGroup.Done()
			err := s.runStep(ctx, step)
			// mark the Step as succeeded or failed
			if err != nil {
				step.setStatus(StepStatusFailed)
			} else {
				step.setStatus(StepStatusSucceeded)
			}
			if s.leaseBucket != nil {
				<-s.leaseBucket // unlease
			}
			s.signalTick()
		}(ctx, step)
	}
}

func (s *Workflow) runStep(ctx context.Context, step Steper) error {
	// set Step-level timeout for the Step
	var notAfter time.Time
	timeout := step.GetTimeout()
	if timeout > 0 {
		notAfter = s.clock.Now().Add(timeout)
		var cancel func()
		ctx, cancel = s.clock.WithDeadline(ctx, notAfter)
		defer cancel()
	}
	// run the Step with or without retry
	do := s.makeDoForStep(step)
	var err error
	if retryOpt := step.GetRetry(); retryOpt == nil {
		err = do(ctx)
	} else {
		err = s.retry(retryOpt)(ctx, do, notAfter)
	}
	// use mutex to guard errs
	s.errsMu.Lock()
	s.errs[step] = err
	s.errsMu.Unlock()
	return err
}

// makeDoForStep is panic-free from Step's Do and Input.
func (s *Workflow) makeDoForStep(step Steper) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return catchPanicAsError(func() error {
			// apply up's output to current Step's input
			for _, l := range s.dep[step] {
				if l.Upstream == nil || // it's Input
					l.Upstream.GetStatus() == StepStatusSucceeded { // or Succeeded Upstream
					if l.Flow != nil {
						if ferr := catchPanicAsError(func() error {
							return l.Flow(ctx)
						}); ferr != nil {
							return &ErrFlow{
								Err:  ferr,
								From: l.Upstream,
							}
						}
					}
				}
			}
			return step.Do(ctx)
		})
	}
}

// IsTerminated returns true if all Steps terminated.
func (s *Workflow) IsTerminated() bool {
	for step := range s.dep {
		if !step.GetStatus().IsTerminated() {
			return false
		}
	}
	return true
}

// Err returns the errors of all Steps in Workflow.
//
// Usage:
//
//	flowErr := flow.Err()
//	if flowErr.IsNil() {
//	    // all Steps succeeded or workflow has not run
//	} else {
//	    stepErr, ok := flowErr[StepA]
//	    switch {
//	    case !ok:
//	        // StepA has not finished or StepA is not in Workflow
//	    case stepErr == nil:
//	        // StepA succeeded
//	    case stepErr != nil:
//	        // StepA failed
//	    }
//	}
func (s *Workflow) Err() ErrWorkflow {
	s.errsMu.RLock()
	defer s.errsMu.RUnlock()
	if s.errs.IsNil() {
		return nil
	}
	werr := make(ErrWorkflow)
	for step, err := range s.errs {
		werr[step] = err
	}
	return werr
}

// catchPanicAsError catches panic from f and return it as error.
func catchPanicAsError(f func() error) error {
	var returnErr error
	func(err *error) {
		defer func() {
			if r := recover(); r != nil {
				*err = fmt.Errorf("%s", r)
			}
		}()
		*err = f()
	}(&returnErr)
	return returnErr
}
