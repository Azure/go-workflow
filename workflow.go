package flow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
)

// Workflow represents a collection of connected Steps that form a directed acyclic graph (DAG).
//
// Workflow executes Steps in a topological order, and flow the data from Upstream(s) to Downstream(s).
type Workflow struct {
	dep    map[Phase]Dependency                     // Dependency is the graph of connected Steps
	states map[Steper]*StepState                    // States saves the internal state(s) of Step(s)
	inputs map[Steper][]func(context.Context) error // Inputs callbacks that will be executed before Step.Do per retry
	tree   StepTree                                 // tree of Steps, Step(s) are chained with Unwrap() method

	err               map[Steper]error // errors reported from all Steps
	errsMu            sync.RWMutex     // need this because errs are written from each Step's goroutine
	leaseBucket       chan struct{}    // constraint max concurrency of running Steps
	waitGroup         sync.WaitGroup   // to prevent goroutine leak, only Add(1) when a Step start running
	isRunning         sync.Mutex       // indicate whether the Workflow is running
	oneStepTerminated chan struct{}    // signals for next tick
	clock             clock.Clock      // clock for unit test
	notify            []Notify         // notify before and after Step
}

// StepState saves the internal state of a Step in Workflow,
// including its status, timeout, retry options, etc.
type StepState struct {
	Status      StepStatus
	RetryOption *RetryOption
	When        When
	Timeout     time.Duration
}

// Phase indicates the phase to run of a Step in Workflow.
type Phase string

const (
	PhaseUnknown Phase = ""
	PhaseInit    Phase = "Init"
	PhaseRun     Phase = "Run"
	PhaseDefer   Phase = "Defer"
)

// Workflow executes in following phases:
func (w *Workflow) getPhases() []Phase { return []Phase{PhaseInit, PhaseRun, PhaseDefer} }

// Add appends Steps into Workflow.
func (w *Workflow) Add(was ...WorkflowAdder) *Workflow   { return w.add(PhaseRun, was...) }
func (w *Workflow) Init(was ...WorkflowAdder) *Workflow  { return w.add(PhaseInit, was...) }
func (w *Workflow) Defer(was ...WorkflowAdder) *Workflow { return w.add(PhaseDefer, was...) }
func (w *Workflow) add(phase Phase, was ...WorkflowAdder) *Workflow {
	if w.dep == nil {
		w.dep = map[Phase]Dependency{}
	}
	if w.states == nil {
		w.states = make(map[Steper]*StepState)
	}
	if w.inputs == nil {
		w.inputs = make(map[Steper][]func(context.Context) error)
	}
	if w.dep[phase] == nil {
		w.dep[phase] = make(Dependency)
	}
	if w.tree == nil {
		w.tree = make(StepTree)
	}
	for _, wa := range was {
		for step, records := range wa.Done() {
			root := w.addStep(phase, step)
			for _, r := range records {
				if r.Upstream != nil {
					w.addStep(phase, r.Upstream)
					w.dep[phase][root].Add(r.Upstream)
				}
				if r.State != nil {
					r.State(w.states[root])
				}
				if r.Input != nil {
					w.inputs[root] = append(w.inputs[root], r.Input)
				}
			}
		}
	}
	return w
}
func (w *Workflow) addStep(phase Phase, step Steper) (root Steper) {
	root = w.tree.Add(step)
	if _, ok := w.states[root]; !ok {
		w.states[root] = &StepState{}
	}
	if _, ok := w.dep[phase][root]; !ok {
		w.dep[phase][root] = make(Set[Steper])
	}
	return root
}

func (w *Workflow) Steps() Set[Steper] {
	if w.tree == nil {
		return nil
	}
	return w.tree.Roots()
}
func (w *Workflow) Unwrap() []Steper {
	if w.tree == nil {
		return nil
	}
	rv := []Steper{}
	for step := range w.tree.Roots() {
		rv = append(rv, step)
	}
	return rv
}
func (w *Workflow) StateOf(step Steper) *StepState {
	if w.states == nil || w.tree == nil {
		return nil
	}
	return w.states[w.tree.RootOf(step)]
}
func (w *Workflow) PhaseOf(step Steper) Phase {
	for _, phase := range w.getPhases() {
		if w.dep != nil && w.dep[phase] != nil && w.tree != nil {
			if _, ok := w.dep[phase][w.tree.RootOf(step)]; ok {
				return phase
			}
		}
	}
	return PhaseUnknown
}
func (w *Workflow) UpstreamOf(step Steper) map[Steper]StatusErr {
	if w.tree == nil {
		return nil
	}
	return w.listStatusErr(w.tree.RootOf(step), func(d Dependency) func(Steper) Set[Steper] { return d.UpstreamOf })
}
func (w *Workflow) DownstreamOf(step Steper) map[Steper]StatusErr {
	if w.tree == nil {
		return nil
	}
	return w.listStatusErr(w.tree.RootOf(step), func(d Dependency) func(Steper) Set[Steper] { return d.DownstreamOf })
}

// IsTerminated returns true if all Steps terminated.
func (w *Workflow) IsTerminated() bool {
	for _, phase := range w.getPhases() {
		if !w.IsPhaseTerminated(phase) {
			return false
		}
	}
	return true
}
func (w *Workflow) IsPhaseTerminated(phase Phase) bool {
	for step := range w.dep[phase] {
		if w.states != nil && w.states[step] != nil && !w.states[step].Status.IsTerminated() {
			return false
		}
	}
	return true
}

// Do starts the Step execution in topological order,
// and waits until all Steps terminated.
//
// Do will block the current goroutine.
func (w *Workflow) Do(ctx context.Context) error {
	// assert the Workflow is not running
	if !w.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer w.isRunning.Unlock()
	// if no steps to run
	if len(w.states) == 0 {
		return nil
	}
	// preflight check
	if err := w.preflight(); err != nil {
		return err
	}
	// new fields for ready to tick
	if w.clock == nil {
		w.clock = clock.New()
	}
	w.err = make(map[Steper]error)
	w.oneStepTerminated = make(chan struct{}, len(w.states)+1) // need one more for the first tick
	// signal for the first tick
	w.signalTick()
	// each time one Step terminated, tick forward
	for range w.oneStepTerminated {
		if done := w.tick(ctx); done {
			break
		}
	}
	// ensure all goroutines are exited
	w.waitGroup.Wait()
	// return the error
	wErr := make(ErrWorkflow)
	for step, err := range w.err {
		wErr[step] = &StatusErr{w.states[step].Status, err}
	}
	if wErr.IsNil() {
		return nil
	}
	return wErr
}

func (w *Workflow) Reset() {
	w.isRunning.Lock()
	defer w.isRunning.Unlock()
	w.err = nil
	close(w.oneStepTerminated)
	w.oneStepTerminated = nil
	for _, state := range w.states {
		state.Status = Pending
	}
}

const scanned StepStatus = "scanned" // a private status for preflight

func isAllUpstreamScanned(ups map[Steper]StatusErr) bool {
	for _, up := range ups {
		if up.Status != scanned {
			return false
		}
	}
	return true
}

func isAnyUpstreamNotTerminated(ups map[Steper]StatusErr) bool {
	for _, up := range ups {
		if !up.Status.IsTerminated() {
			return true
		}
	}
	return false
}

func (w *Workflow) preflight() error {
	// check whether the workflow has been run
	if w.err != nil {
		return ErrWorkflowHasRun
	}
	// assert all Steps' status start with Pending
	unexpectStatusSteps := make(ErrUnexpectStepInitStatus)
	for step, state := range w.states {
		if state.Status != Pending {
			unexpectStatusSteps[step] = state.Status
		}
	}
	if len(unexpectStatusSteps) > 0 {
		return unexpectStatusSteps
	}
	// assert all dependency would not form a cycle
	// start scanning, mark Step as Scanned only when its all depdencies are Scanned
	for {
		hasNewScanned := false // whether a new Step being marked as Scanned this turn
		for step, state := range w.states {
			if state.Status == scanned {
				continue
			}
			if isAllUpstreamScanned(w.UpstreamOf(step)) {
				hasNewScanned = true
				state.Status = scanned
			}
		}
		if !hasNewScanned { // break when no new Step being Scanned
			break
		}
	}
	// check whether still have Steps not Scanned,
	// not Scanned Steps are in a cycle.
	stepsInCycle := make(ErrCycleDependency)
	for step, state := range w.states {
		if state.Status != scanned {
			for up := range w.UpstreamOf(step) {
				if w.states[up].Status != scanned {
					stepsInCycle[step] = append(stepsInCycle[step], up)
				}
			}
		}
	}
	if len(stepsInCycle) > 0 {
		return stepsInCycle
	}
	// reset all Steps' status to Pending
	for _, state := range w.states {
		state.Status = Pending
	}
	return nil
}

func (w *Workflow) signalTick() { w.oneStepTerminated <- struct{}{} }

// tick will not block, it starts a goroutine for each runnable Step.
// tick returns true if all steps are terminated.
func (w *Workflow) tick(ctx context.Context) bool {
	var dep Dependency
	for _, phase := range w.getPhases() {
		if !w.IsPhaseTerminated(phase) {
			dep = w.dep[phase]
			break
		}
	}
	if dep == nil {
		return true
	}
	for step := range dep {
		state := w.states[step]
		// continue if the Step is not Pending
		if state.Status != Pending {
			continue
		}
		// continue if any Upstream is not terminated
		ups := w.UpstreamOf(step)
		if isAnyUpstreamNotTerminated(ups) {
			continue
		}
		when := DefaultWhen
		if state.When != nil {
			when = state.When
		}
		if nextStatus := when(ctx, ups); nextStatus.IsTerminated() {
			state.Status = nextStatus
			w.signalTick()
			continue
		}
		// start the Step
		w.lease()
		state.Status = Running
		w.waitGroup.Add(1)
		go func(ctx context.Context, step Steper) {
			defer w.waitGroup.Done()
			defer w.signalTick()
			defer w.unlease()

			err := w.runStep(ctx, step)
			switch {
			case err == nil:
				state.Status = Succeeded
			case DefaultIsCanceled(err):
				state.Status = Canceled
			case errors.Is(err, &ErrSkip{}):
				state.Status = Skipped
			default:
				state.Status = Failed
			}
			// use mutex to guard errs
			w.errsMu.Lock()
			w.err[step] = err
			w.errsMu.Unlock()
		}(ctx, step)
	}
	return false
}

func (w *Workflow) runStep(ctx context.Context, step Steper) error {
	// set Step-level timeout for the Step
	var notAfter time.Time
	timeout := w.states[step].Timeout
	if timeout > 0 {
		notAfter = w.clock.Now().Add(timeout)
		var cancel func()
		ctx, cancel = w.clock.WithDeadline(ctx, notAfter)
		defer cancel()
	}
	// run the Step with or without retry
	do := w.makeDoForStep(step)
	if retryOpt := w.states[step].RetryOption; retryOpt != nil {
		return w.retry(retryOpt)(ctx, do, notAfter)
	}
	return do(ctx)
}

// makeDoForStep is panic-free from Step's Do and Input.
func (w *Workflow) makeDoForStep(step Steper) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return catchPanicAsError(func() error {
			var err error
			ctx, afterStep := w.notifyStep(ctx, step)
			defer func() {
				afterStep(ctx, step, err)
			}()
			// apply up's output to current Step's input
			for _, input := range w.inputs[step] {
				if input == nil {
					continue
				}
				if ferr := catchPanicAsError(func() error {
					return input(ctx)
				}); ferr != nil {
					err = &ErrInput{ferr, step}
					return err
				}
			}
			err = step.Do(ctx)
			return err
		})
	}
}
func (w *Workflow) notifyStep(ctx context.Context, step Steper) (context.Context, func(context.Context, Steper, error)) {
	afterStep := []func(context.Context, Steper, error){}
	for _, notify := range w.notify {
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
func (w *Workflow) listStatusErr(step Steper, of func(Dependency) func(Steper) Set[Steper]) map[Steper]StatusErr {
	w.errsMu.RLock()
	defer w.errsMu.RUnlock()
	rv := make(map[Steper]StatusErr)
	for _, phase := range w.getPhases() {
		for other := range of(w.dep[phase])(step) {
			rv[other] = StatusErr{
				Status: w.states[other].Status,
				Err:    w.err[other],
			}
		}
	}
	return rv
}
func (w *Workflow) lease() {
	if w.leaseBucket != nil {
		w.leaseBucket <- struct{}{}
	}
}
func (w *Workflow) unlease() {
	if w.leaseBucket != nil {
		<-w.leaseBucket
	}
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
