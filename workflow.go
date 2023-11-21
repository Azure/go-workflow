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
// Workflow executes Steps in a topological order, and flow the data from Upstream to Downstream.
type Workflow struct {
	// tree of Steps, Steps are chained via Unwrap() method,
	// following fields will only know the existence of root Steps
	tree   StepTree
	dep    map[Phase]dependency                     // dep tracks the dependencies between Steps
	status map[Steper]StepStatus                    // status of Steps
	option map[Steper][]func(*StepOption)           // internal options for Steps, including status, timeout, retry options, etc.
	input  map[Steper][]func(context.Context) error // inputs callbacks that will be executed before Step.Do per retry

	err               map[Steper]error // errors reported from Steps
	errsMu            sync.RWMutex     // need this because errs are written from each Step's goroutine
	leaseBucket       chan struct{}    // constraint max concurrency of running Steps
	waitGroup         sync.WaitGroup   // to prevent goroutine leak, only Add(1) when forks a goroutine
	isRunning         sync.Mutex       // indicate whether the Workflow is running
	oneStepTerminated chan struct{}    // signals for next tick
	clock             clock.Clock      // clock for unit test
	notify            []Notify         // notify before and after Step
}

func New() *Workflow {
	return &Workflow{
		tree:   make(StepTree),
		dep:    make(map[Phase]dependency),
		status: make(map[Steper]StepStatus),
		option: make(map[Steper][]func(*StepOption)),
		input:  make(map[Steper][]func(context.Context) error),
	}
}

// Phase indicates the phase to run of a Step in Workflow.
type Phase string

const (
	PhaseUnknown Phase = ""
	PhaseInit    Phase = "Init"
	PhaseRun     Phase = "Run"
	PhaseDefer   Phase = "Defer"
)

// Workflow executes in following phases: Init, Run, Defer
//
// Only all Steps in the previous phase terminated, the next phase will start.
func (w *Workflow) getPhases() []Phase { return []Phase{PhaseInit, PhaseRun, PhaseDefer} }

// Add Steps into Workflow in phase Run.
func (w *Workflow) Add(was ...WorkflowAdd) *Workflow { return w.add(PhaseRun, was...) }

// Add Steps into Workflow in phase Init.
func (w *Workflow) Init(was ...WorkflowAdd) *Workflow { return w.add(PhaseInit, was...) }

// Add Steps into Workflow in phase Defer.
func (w *Workflow) Defer(was ...WorkflowAdd) *Workflow { return w.add(PhaseDefer, was...) }

func (w *Workflow) add(phase Phase, was ...WorkflowAdd) *Workflow {
	if w.tree == nil {
		w.tree = make(StepTree)
	}
	if w.dep == nil {
		w.dep = map[Phase]dependency{}
	}
	if w.dep[phase] == nil {
		w.dep[phase] = make(dependency)
	}
	if w.status == nil {
		w.status = make(map[Steper]StepStatus)
	}
	if w.option == nil {
		w.option = make(map[Steper][]func(*StepOption))
	}
	if w.input == nil {
		w.input = make(map[Steper][]func(context.Context) error)
	}
	for _, wa := range was {
		for step, records := range wa.Done() {
			root := w.addStep(phase, step)
			for _, r := range records {
				if r.Upstream != nil {
					w.dep[phase][root].Add(r.Upstream)
					w.addStep(phase, r.Upstream)
				}
				if r.Option != nil {
					w.option[root] = append(w.option[root], r.Option)
				}
				if r.Input != nil {
					w.input[root] = append(w.input[root], r.Input)
				}
			}
		}
	}
	return w
}
func (w *Workflow) addStep(phase Phase, step Steper) (root Steper) {
	var olds []Steper
	root, olds = w.tree.Add(step)
	if _, ok := w.dep[phase][root]; !ok {
		w.dep[phase][root] = make(set[Steper])
	}
	if _, ok := w.status[root]; !ok {
		w.status[root] = Pending
	}
	for _, old := range olds {
		if old != root {
			w.dep[phase][root].Union(w.dep[phase][old])
			delete(w.dep[phase], old)
			delete(w.status, old)
			w.option[root] = append(w.option[root], w.option[old]...)
			delete(w.option, old)
			w.input[root] = append(w.input[root], w.input[old]...)
			delete(w.input, old)
		}
	}
	return root
}

func (w *Workflow) Steps() []Steper { return w.Unwrap() }
func (w *Workflow) Unwrap() []Steper {
	if w.status == nil {
		return nil
	}
	rv := []Steper{}
	for step := range w.status {
		rv = append(rv, step)
	}
	return rv
}
func (w *Workflow) StatusOf(step Steper) StepStatus {
	if w.status == nil || w.tree == nil {
		return Pending
	}
	return w.status[w.tree.RootOf(step)]
}
func (w *Workflow) OptionOf(step Steper) *StepOption {
	if w.option == nil || w.tree == nil {
		return nil
	}
	opt := &StepOption{}
	for _, optFn := range w.option[w.tree.RootOf(step)] {
		optFn(opt)
	}
	return opt
}
func (w *Workflow) PhaseOf(step Steper) Phase {
	if w.dep == nil || w.tree == nil {
		return PhaseUnknown
	}
	for _, phase := range w.getPhases() {
		if w.dep[phase] != nil {
			if _, ok := w.dep[phase][w.tree.RootOf(step)]; ok {
				return phase
			}
		}
	}
	return PhaseUnknown
}
func (w *Workflow) UpstreamOf(step Steper) map[Steper]StatusError {
	return w.listStatusErr(step, func(d dependency) func(Steper) set[Steper] { return d.UpstreamOf })
}
func (w *Workflow) DownstreamOf(step Steper) map[Steper]StatusError {
	return w.listStatusErr(step, func(d dependency) func(Steper) set[Steper] { return d.DownstreamOf })
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
		if w.status != nil && !w.status[step].IsTerminated() {
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
	if len(w.status) == 0 {
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
	w.oneStepTerminated = make(chan struct{}, len(w.status)+1) // need one more for the first tick
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
		wErr[step] = &StatusError{w.status[step], err}
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
	for step := range w.status {
		w.status[step] = Pending
	}
}

const scanned StepStatus = "scanned" // a private status for preflight
func isAllUpstreamScanned(ups map[Steper]StatusError) bool {
	for _, up := range ups {
		if up.Status != scanned {
			return false
		}
	}
	return true
}
func isAnyUpstreamNotTerminated(ups map[Steper]StatusError) bool {
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
	for step, status := range w.status {
		if status != Pending {
			unexpectStatusSteps[step] = status
		}
	}
	if len(unexpectStatusSteps) > 0 {
		return unexpectStatusSteps
	}
	// assert all dependency would not form a cycle
	// start scanning, mark Step as Scanned only when its all depdencies are Scanned
	for {
		hasNewScanned := false // whether a new Step being marked as Scanned this turn
		for step, status := range w.status {
			if status == scanned {
				continue
			}
			if isAllUpstreamScanned(w.UpstreamOf(step)) {
				hasNewScanned = true
				w.status[step] = scanned
			}
		}
		if !hasNewScanned { // break when no new Step being Scanned
			break
		}
	}
	// check whether still have Steps not Scanned,
	// not Scanned Steps are in a cycle.
	stepsInCycle := make(ErrCycleDependency)
	for step, status := range w.status {
		if status != scanned {
			for up := range w.UpstreamOf(step) {
				if w.status[up] != scanned {
					stepsInCycle[step] = append(stepsInCycle[step], up)
				}
			}
		}
	}
	if len(stepsInCycle) > 0 {
		return stepsInCycle
	}
	// reset all Steps' status to Pending
	for step := range w.status {
		w.status[step] = Pending
	}
	return nil
}

func (w *Workflow) signalTick() { w.oneStepTerminated <- struct{}{} }

// tick will not block, it starts a goroutine for each runnable Step.
// tick returns true if all steps are terminated.
func (w *Workflow) tick(ctx context.Context) bool {
	var dep dependency
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
		// continue if the Step is not Pending
		if w.status[step] != Pending {
			continue
		}
		option := w.OptionOf(step)
		// continue if any Upstream is not terminated
		ups := w.UpstreamOf(step)
		if isAnyUpstreamNotTerminated(ups) {
			continue
		}
		when := DefaultWhen
		if option.When != nil {
			when = option.When
		}
		if nextStatus := when(ctx, ups); nextStatus.IsTerminated() {
			w.status[step] = nextStatus
			w.signalTick()
			continue
		}
		// start the Step
		w.lease()
		w.status[step] = Running
		w.waitGroup.Add(1)
		go func(ctx context.Context, step Steper) {
			defer w.waitGroup.Done()
			defer w.signalTick()
			defer w.unlease()

			err := w.runStep(ctx, step)
			switch {
			case err == nil:
				w.status[step] = Succeeded
			case DefaultIsCanceled(err):
				w.status[step] = Canceled
			case errors.Is(err, &ErrSkip{}):
				w.status[step] = Skipped
			default:
				w.status[step] = Failed
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
	option := w.OptionOf(step)
	timeout := option.Timeout
	if timeout > 0 {
		notAfter = w.clock.Now().Add(timeout)
		var cancel func()
		ctx, cancel = w.clock.WithDeadline(ctx, notAfter)
		defer cancel()
	}
	// run the Step with or without retry
	do := w.makeDoForStep(step)
	return w.retry(option.RetryOption)(ctx, do, notAfter)
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
			for _, input := range w.input[step] {
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
func (w *Workflow) listStatusErr(step Steper, of func(dependency) func(Steper) set[Steper]) map[Steper]StatusError {
	if w.tree == nil {
		return nil
	}
	w.errsMu.RLock()
	defer w.errsMu.RUnlock()
	rv := make(map[Steper]StatusError)
	for _, phase := range w.getPhases() {
		for other := range of(w.dep[phase])(w.tree.RootOf(step)) {
			other = w.tree.RootOf(other)
			rv[other] = StatusError{
				Status: w.StatusOf(other),
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
