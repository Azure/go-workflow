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
	dep               map[Phase]Dependency // Dependency is the graph of connected Steps
	depAll            Dependency           // a copy of all Steps and its Upstreams to decrease copy times
	errs              ErrWorkflow          // errors reported from all Steps
	errsMu            sync.RWMutex         // need this because errs are written from each Step's goroutine
	when              When                 // Workflow level When
	leaseBucket       chan struct{}        // constraint max concurrency of running Steps
	waitGroup         sync.WaitGroup       // to prevent goroutine leak, only Add(1) when a Step start running
	isRunning         sync.Mutex           // indicate whether the Workflow is running
	oneStepTerminated chan struct{}        // signals for next tick
	clock             clock.Clock          // clock for unit test
	notify            []Notify             // notify before and after Step
}

func New() *Workflow { return new(Workflow) }

// Add appends Steps into Workflow.
func (s *Workflow) Add(dbs ...WorkflowStep) *Workflow   { return s.add(PhaseRun, dbs...) }
func (s *Workflow) Init(dbs ...WorkflowStep) *Workflow  { return s.add(PhaseInit, dbs...) }
func (s *Workflow) Defer(dbs ...WorkflowStep) *Workflow { return s.add(PhaseDefer, dbs...) }
func (s *Workflow) add(phase Phase, dbs ...WorkflowStep) *Workflow {
	if s.dep == nil {
		s.dep = map[Phase]Dependency{}
	}
	if s.dep[phase] == nil {
		s.dep[phase] = make(Dependency)
	}
	for _, db := range dbs {
		s.dep[phase].Merge(db.done())
	}
	return s
}

// DepPhased returns the Init / Run / Deferred Steps and dependencies in this Workflow.
func (s *Workflow) DepPhased() (Dependency, Dependency, Dependency) {
	// make a copy to prevent origin being modified
	init, dep, defer_ := make(Dependency), make(Dependency), make(Dependency)
	init.Merge(s.dep[PhaseInit])
	dep.Merge(s.dep[PhaseRun])
	defer_.Merge(s.dep[PhaseDefer])
	return init, dep, defer_
}

// Dep returns the Steps and dependencies in this Workflow.
//
// Modify the returned Dependency will not affect the graph in Workflow,
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
	all := make(Dependency)
	all.Merge(s.dep[PhaseInit])
	all.Merge(s.dep[PhaseRun])
	all.Merge(s.dep[PhaseDefer])
	return all
}

// Run starts the Step execution in topological order,
// and waits until all Steps terminated.
//
// Run will block the current goroutine.
func (s *Workflow) Run(ctx context.Context) error {
	// assert the Workflow is not running
	if !s.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer s.isRunning.Unlock()
	// assert the Workflow has Steps
	s.depAll = s.Dep()
	if len(s.depAll) == 0 {
		return nil
	}
	// check whether the Workflow when is satisfied
	if s.when != nil && !s.when(ctx) {
		for step := range s.depAll {
			step.setStatus(StepStatusSkipped)
		}
		return nil
	}
	// preflight check the initial state of workflow
	if err := s.preflight(); err != nil {
		return err
	}
	// new Workflow clock
	if s.clock == nil {
		s.clock = clock.New()
	}
	s.errs = make(ErrWorkflow)
	s.oneStepTerminated = make(chan struct{}, len(s.depAll))
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

func isAllUpstreamScanned(ups []Steper) bool {
	for _, up := range ups {
		if up.GetStatus() != scanned {
			return false
		}
	}
	return true
}

func isAnyUpstreamNotTerminated(ups []Steper) bool {
	for _, up := range ups {
		if !up.GetStatus().IsTerminated() {
			return true
		}
	}
	return false
}

func isAnyUpstreamSkipped(ups []Steper) bool {
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
	unexpectStatusSteps := []Steper{}
	for step := range s.depAll {
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
		for step := range s.depAll {
			if step.GetStatus() == scanned {
				continue
			}
			if isAllUpstreamScanned(s.depAll.UpstreamOf(step)) {
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
	stepsInCycle := map[Steper][]Steper{}
	for step := range s.depAll {
		if step.GetStatus() != scanned {
			for _, up := range s.depAll.UpstreamOf(step) {
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
	for step := range s.depAll {
		step.setStatus(StepStatusPending)
	}
	return nil
}

func (s *Workflow) signalTick() {
	s.oneStepTerminated <- struct{}{}
}

// tick will not block, it starts a goroutine for each runnable Step.
func (s *Workflow) tick(ctx context.Context) {
	var dep Dependency
	for _, phase := range s.getPhases() {
		if !s.isTerminated(phase) {
			dep = s.dep[phase]
			break
		}
	}
	for step := range dep {
		// continue if the Step is not Pending
		if step.GetStatus() != StepStatusPending {
			continue
		}
		// continue if any Upstream is not terminated
		ups := dep.UpstreamOf(step)
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
			ctx, afterStep := s.notifyStep(ctx, step)
			// apply up's output to current Step's input
			for _, l := range s.depAll[step] {
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
			err := step.Do(ctx)
			afterStep(ctx, step, err)
			return err
		})
	}
}

func (s *Workflow) notifyStep(ctx context.Context, step Steper) (context.Context, func(context.Context, Steper, error)) {
	afterStep := []func(context.Context, Steper, error){}
	for _, notify := range s.notify {
		if notify.BeforeStep != nil {
			ctx = notify.BeforeStep(ctx, step)
		}
		if notify.AfterStep != nil {
			afterStep = append(afterStep, notify.AfterStep)
		}
	}
	return ctx, func(ctx context.Context, sr Steper, err error) {
		for _, notify := range afterStep {
			notify(ctx, sr, err)
		}
	}
}

func (s *Workflow) getPhases() []Phase {
	return []Phase{PhaseInit, PhaseRun, PhaseDefer}
}

// IsTerminated returns true if all Steps terminated.
func (s *Workflow) IsTerminated() bool {
	for _, phase := range s.getPhases() {
		if !s.isTerminated(phase) {
			return false
		}
	}
	return true
}

func (s *Workflow) isTerminated(phase Phase) bool {
	for step := range s.dep[phase] {
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
