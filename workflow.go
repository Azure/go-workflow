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
	tree  stepTree              // tree of Steps, Steps are chained via Unwrap() method,
	state map[Steper]*state     // keys are root Steps, values are the internal state
	steps map[Phase]Set[*state] // all Steps grouped in phases

	err               ErrWorkflow    // errors reported from Steps
	errsMu            sync.RWMutex   // need this because errs are written from each Step's goroutine
	leaseBucket       chan struct{}  // constraint max concurrency of running Steps
	waitGroup         sync.WaitGroup // to prevent goroutine leak, only Add(1) when forks a goroutine
	isRunning         sync.Mutex     // indicate whether the Workflow is running
	oneStepTerminated chan struct{}  // signals for next tick
	clock             clock.Clock    // clock for unit test
	notify            []Notify       // notify before and after Step
}

// state is the internal state of a Step in Workflow.
type state struct {
	Step   Steper
	Status StepStatus
	sync.RWMutex
	StepConfig
}

func (s *state) GetStatus() StepStatus {
	s.RLock()
	defer s.RUnlock()
	return s.Status
}
func (s *state) SetStatus(ss StepStatus) {
	s.Lock()
	defer s.Unlock()
	s.Status = ss
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
func (w *Workflow) Add(was ...WorkflowAdder) *Workflow { return w.add(PhaseRun, was...) }

// Add Steps into Workflow in phase Init.
func (w *Workflow) Init(was ...WorkflowAdder) *Workflow { return w.add(PhaseInit, was...) }

// Add Steps into Workflow in phase Defer.
func (w *Workflow) Defer(was ...WorkflowAdder) *Workflow { return w.add(PhaseDefer, was...) }

func (w *Workflow) add(phase Phase, was ...WorkflowAdder) *Workflow {
	if w.tree == nil {
		w.tree = make(stepTree)
	}
	if w.state == nil {
		w.state = make(map[Steper]*state)
	}
	if w.steps == nil {
		w.steps = make(map[Phase]Set[*state])
	}
	if w.steps[phase] == nil {
		w.steps[phase] = make(Set[*state])
	}
	for _, wa := range was {
		for step, sc := range wa.Done() {
			w.addStep(phase, step, sc)
			for up := range sc.Upstreams {
				w.addStep(phase, up, nil)
			}
		}
	}
	return w
}
func (w *Workflow) addStep(phase Phase, step Steper, sc *StepConfig) {
	step, olds := w.tree.Add(step)
	if w.state[step] == nil {
		w.state[step] = &state{
			Step:       step,
			Status:     Pending,
			StepConfig: StepConfig{},
		}
	}
	w.state[step].StepConfig.Merge(sc)
	w.steps[phase].Add(w.state[step])
	for old := range olds {
		w.state[step].Merge(&w.state[old].StepConfig)
		// need replace all old roots with the new root
		for _, phase := range w.getPhases() {
			if w.steps[phase] == nil {
				continue
			}
			if w.steps[phase].Has(w.state[old]) {
				if !w.steps[phase].Has(w.state[step]) {
					w.steps[phase].Add(w.state[step])
				}
				delete(w.steps[phase], w.state[old])
			}
		}
		delete(w.state, old)
	}
}

func (w *Workflow) String() string {
	return fmt.Sprintf("Workflow(%d-%d-%d)", len(w.steps[PhaseInit]), len(w.steps[PhaseRun]), len(w.steps[PhaseDefer]))
}
func (w *Workflow) empty() bool     { return len(w.tree) == 0 || len(w.state) == 0 || len(w.steps) == 0 }
func (w *Workflow) Steps() []Steper { return w.Unwrap() }
func (w *Workflow) Unwrap() []Steper {
	if w.empty() {
		return nil
	}
	rv := []Steper{}
	for step := range w.state {
		rv = append(rv, step)
	}
	return rv
}
func (w *Workflow) setError(step Steper, statusErr StatusError) {
	w.errsMu.Lock()
	defer w.errsMu.Unlock()
	w.err[step] = statusErr
}
func (w *Workflow) StatusOf(step Steper) StepStatus {
	if w.empty() {
		return Pending
	}
	step = w.tree.RootOf(step)
	return w.state[step].GetStatus()
}
func (w *Workflow) OptionOf(step Steper) *StepOption {
	if w.empty() {
		return nil
	}
	step = w.tree.RootOf(step)
	opt := &StepOption{}
	if w.state[step].Option != nil {
		w.state[step].Option(opt)
	}
	return opt
}
func (w *Workflow) PhaseOf(step Steper) Phase {
	if w.empty() {
		return PhaseUnknown
	}
	step = w.tree.RootOf(step)
	for _, phase := range w.getPhases() {
		if w.steps[phase] == nil {
			continue
		}
		if w.steps[phase].Has(w.state[step]) {
			return phase
		}
	}
	return PhaseUnknown
}
func (w *Workflow) UpstreamOf(step Steper) map[Steper]StatusError {
	if w.empty() {
		return nil
	}
	step = w.tree.RootOf(step)
	rv := make(map[Steper]StatusError)
	for _, phase := range w.getPhases() {
		if w.steps[phase] == nil {
			continue
		}
		if w.steps[phase].Has(w.state[step]) {
			for up := range w.state[step].Upstreams {
				up = w.tree.RootOf(up)
				rv[up] = StatusError{
					Status: w.state[up].GetStatus(),
					Err:    w.err[up].Err,
				}
			}
		}
	}
	return rv
}
func (w *Workflow) DownstreamOf(step Steper) map[Steper]StatusError {
	if w.empty() {
		return nil
	}
	step = w.tree.RootOf(step)
	rv := make(map[Steper]StatusError)
	for _, phase := range w.getPhases() {
		if w.steps[phase] == nil {
			continue
		}
		for down := range w.steps[phase] {
			for up := range down.Upstreams {
				if w.tree.RootOf(up) == step {
					rv[down.Step] = StatusError{
						Status: down.GetStatus(),
						Err:    w.err[down.Step].Err,
					}
				}
			}
		}
	}
	return rv
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
	if w.empty() {
		return true
	}
	for step := range w.steps[phase] {
		if !step.GetStatus().IsTerminated() {
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
	if w.empty() {
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
	w.err = make(ErrWorkflow)
	w.oneStepTerminated = make(chan struct{}, len(w.state)+1) // need one more for the first tick
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
	if w.err.IsNil() {
		return nil
	}
	return w.err
}

func (w *Workflow) Reset() {
	w.isRunning.Lock()
	defer w.isRunning.Unlock()
	w.err = nil
	close(w.oneStepTerminated)
	w.oneStepTerminated = nil
	for _, step := range w.state {
		step.SetStatus(Pending)
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
	for step, state := range w.state {
		if status := state.GetStatus(); status != Pending {
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
		for step, state := range w.state {
			if state.GetStatus() == scanned {
				continue
			}
			if isAllUpstreamScanned(w.UpstreamOf(step)) {
				hasNewScanned = true
				state.SetStatus(scanned)
			}
		}
		if !hasNewScanned { // break when no new Step being Scanned
			break
		}
	}
	// check whether still have Steps not Scanned,
	// not Scanned Steps are in a cycle.
	stepsInCycle := make(ErrCycleDependency)
	for step, state := range w.state {
		if state.GetStatus() == scanned {
			continue
		}
		for up, statusErr := range w.UpstreamOf(step) {
			if statusErr.Status != scanned {
				stepsInCycle[step] = append(stepsInCycle[step], up)
			}
		}
	}
	if len(stepsInCycle) > 0 {
		return stepsInCycle
	}
	// reset all Steps' status to Pending
	for _, step := range w.state {
		step.SetStatus(Pending)
	}
	return nil
}

func (w *Workflow) signalTick() { w.oneStepTerminated <- struct{}{} }

// tick will not block, it starts a goroutine for each runnable Step.
// tick returns true if all steps are terminated.
func (w *Workflow) tick(ctx context.Context) bool {
	var steps Set[*state]
	for _, phase := range w.getPhases() {
		if !w.IsPhaseTerminated(phase) {
			steps = w.steps[phase]
			break
		}
	}
	if steps == nil {
		return true
	}
	for step := range steps {
		// continue if the Step is not Pending
		if step.GetStatus() != Pending {
			continue
		}
		option := w.OptionOf(step.Step)
		// continue if any Upstream is not terminated
		ups := w.UpstreamOf(step.Step)
		if isAnyUpstreamNotTerminated(ups) {
			continue
		}
		when := DefaultWhen
		if option.When != nil {
			when = option.When
		}
		if nextStatus := when(ctx, ups); nextStatus.IsTerminated() {
			step.SetStatus(nextStatus)
			w.setError(step.Step, StatusError{nextStatus, nil})
			w.signalTick()
			continue
		}
		// start the Step
		w.lease()
		step.SetStatus(Running)
		w.waitGroup.Add(1)
		go func(ctx context.Context, step *state) {
			defer w.waitGroup.Done()
			defer w.signalTick()
			defer w.unlease()

			err := w.runStep(ctx, step)
			var result StepStatus
			switch {
			case err == nil:
				result = Succeeded
			case DefaultIsCanceled(err):
				result = Canceled
			case errors.Is(err, &ErrSkip{}):
				result = Skipped
			default:
				result = Failed
			}
			step.SetStatus(result)
			w.setError(step.Step, StatusError{result, err})
		}(ctx, step)
	}
	return false
}

func (w *Workflow) runStep(ctx context.Context, step *state) error {
	// set Step-level timeout for the Step
	var notAfter time.Time
	option := w.OptionOf(step.Step)
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
func (w *Workflow) makeDoForStep(step *state) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return catchPanicAsError(func() error {
			var err error
			ctx, afterStep := w.notifyStep(ctx, step.Step)
			defer func() {
				afterStep(ctx, step.Step, err)
			}()
			// apply up's output to current Step's input
			if step.Input != nil {
				if ierr := catchPanicAsError(func() error {
					return step.Input(ctx)
				}); ierr != nil {
					err = ierr
					return err
				}
			}
			err = step.Step.Do(ctx)
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
