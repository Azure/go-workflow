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
// The Steps are connected via dependency, use Step(), Steps() or Pipe() to add Steps into Workflow.
//
//	workflow.Add(
//		Step(a),
//		Steps(b, c).DependsOn(a),	// a -> b, c
//		Pipe(d, e, f),              // d -> e -> f
//	)
//
// Workflow will execute Steps in a topological order, each Step will be executed in a separate goroutine.
// Workflow guarantees that
//
//	Before a Step goroutine starts, all its Upstream Steps are terminated, and registered Input callbacks are called.
//
// Workflow supports Step-level configuration,       check Step(), Steps() and Pipe() for details.
// Workflow supports Workflow-level configuration,   check WorkflowOption for details.
// Workflow supports executing Steps phase in phase, check Phase for details.
// Workflow supports Nested Steps,				     check Is(), As() and StepTree for details.
type Workflow struct {
	tree  StepTree              // tree of Nested / Wrapped Steps, only root Steps are used in the below fields
	state map[Steper]*State     // the internal states of Steps
	steps map[Phase]Set[Steper] // all Steps grouped in phases

	err               ErrWorkflow    // errors reported from finished Steps
	errsMu            sync.RWMutex   // need this because errs are read / write from different goroutines
	leaseBucket       chan struct{}  // constraint max concurrency of running Steps
	waitGroup         sync.WaitGroup // to prevent goroutine leak
	isRunning         sync.Mutex     // indicate whether the Workflow is running
	oneStepTerminated chan struct{}  // signals for next tick
	clock             clock.Clock    // clock for unit test
	notify            []Notify       // notify before and after Step
}

// Phase clusters Steps into different execution phases.
//
// Workflow supports three phases: Init, Main and Defer.
// It derives from the below common go pattern:
//
//	func init() {}
//	func main() {
//		defer func() {}
//	}
//
// Only all Steps in previous phase terminated, the next phase will start.
// Even if the steps in previous phase are not successful, the next phase will still start.
type Phase int

const (
	PhaseUnknown Phase = iota
	PhaseInit
	PhaseMain
	PhaseDefer
)

func (w *Workflow) getPhases() []Phase { return []Phase{PhaseInit, PhaseMain, PhaseDefer} }

// Add Steps into Workflow in phase Main.
func (w *Workflow) Add(was ...WorkflowAdder) *Workflow { return w.add(PhaseMain, was...) }

// Init adds Steps into Workflow in phase Init.
func (w *Workflow) Init(was ...WorkflowAdder) *Workflow { return w.add(PhaseInit, was...) }

// Defer adds Steps into Workflow in phase Defer.
func (w *Workflow) Defer(was ...WorkflowAdder) *Workflow { return w.add(PhaseDefer, was...) }

func (w *Workflow) add(phase Phase, was ...WorkflowAdder) *Workflow {
	if w.tree == nil {
		w.tree = make(StepTree)
	}
	if w.state == nil {
		w.state = make(map[Steper]*State)
	}
	if w.steps == nil {
		w.steps = make(map[Phase]Set[Steper])
	}
	if w.steps[phase] == nil {
		w.steps[phase] = make(Set[Steper])
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
	olds := w.tree.Add(step)
	root := w.tree.RootOf(step)
	if w.state[root] == nil {
		w.state[root] = &State{Status: Pending}
	}
	w.state[root].MergeConfig(sc)
	w.steps[phase].Add(root)
	for old := range olds {
		// replace all old roots with the new root
		w.state[root].MergeConfig(w.state[old].Config)
		for _, phase := range w.getPhases() {
			if w.steps[phase] == nil {
				continue
			}
			if w.steps[phase].Has(old) {
				w.steps[phase].Add(root)
				delete(w.steps[phase], old)
			}
		}
		delete(w.state, old)
	}
}

// Tree returns the tree of all nested Steps in the Workflow.
func (w *Workflow) Tree() StepTree {
	rv := make(StepTree)
	rv.Merge(w.tree)
	return rv
}
func (w *Workflow) empty() bool { return len(w.tree) == 0 || len(w.state) == 0 || len(w.steps) == 0 }

// Steps returns all Steps in the Workflow.
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

// ErrorOf returns the error of the Step.
func (w *Workflow) ErrorOf(step Steper) error {
	if step == nil {
		return nil
	}
	ancestor := w.tree[step]
	for !w.tree.IsRoot(ancestor) {
		if a, ok := ancestor.(interface{ ErrorOf(Steper) StatusError }); ok {
			return a.ErrorOf(step)
		}
		ancestor = w.tree[ancestor]
	}
	w.errsMu.RLock()
	defer w.errsMu.RUnlock()
	return w.err[ancestor].Err
}
func (w *Workflow) setError(step Steper, statusErr StatusError) {
	w.errsMu.Lock()
	defer w.errsMu.Unlock()
	w.err[step] = statusErr
}

// StateOf returns the internal state of the Step.
//
// StateOf is relatively an internal operation, you don't need to use it normally.
func (w *Workflow) StateOf(step Steper) *State {
	if w.empty() {
		return nil
	}
	ancestor := w.tree[step]
	for !w.tree.IsRoot(ancestor) {
		if a, ok := ancestor.(interface{ StateOf(Steper) *State }); ok {
			return a.StateOf(step)
		}
		ancestor = w.tree[ancestor]
	}
	return w.state[ancestor]
}
func (w *Workflow) StatusOf(step Steper) StepStatus {
	state := w.StateOf(step)
	if state == nil {
		return Pending
	}
	return state.GetStatus()
}
func (w *Workflow) OptionOf(step Steper) *StepOption {
	state := w.StateOf(step)
	if state == nil {
		return nil
	}
	return state.Option()
}
func (w *Workflow) PhaseOf(step Steper) Phase {
	if w.empty() {
		return PhaseUnknown
	}
	root := w.tree.RootOf(step)
	for _, phase := range w.getPhases() {
		if w.steps[phase] == nil {
			continue
		}
		if w.steps[phase].Has(root) {
			return phase
		}
	}
	return PhaseUnknown
}
func (w *Workflow) UpstreamOf(step Steper) map[Steper]StatusError {
	if w.empty() {
		return nil
	}
	root := w.tree.RootOf(step)
	rv := make(map[Steper]StatusError)
	for _, phase := range w.getPhases() {
		if w.steps[phase] == nil {
			continue
		}
		if w.steps[phase].Has(root) {
			for up := range w.StateOf(root).Upstreams() {
				up = w.tree.RootOf(up)
				rv[up] = StatusError{
					Status: w.StateOf(up).GetStatus(),
					Err:    w.ErrorOf(up),
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
	root := w.tree[step]
	rv := make(map[Steper]StatusError)
	for _, phase := range w.getPhases() {
		if w.steps[phase] == nil {
			continue
		}
		for down := range w.steps[phase] {
			for up := range w.StateOf(down).Config.Upstreams {
				if w.tree.RootOf(up) == root {
					rv[down] = StatusError{
						Status: w.StatusOf(down),
						Err:    w.ErrorOf(down),
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
		if !w.StatusOf(step).IsTerminated() {
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
// tick returns true if all steps in all phases are terminated.
func (w *Workflow) tick(ctx context.Context) bool {
	var steps Set[Steper]
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
		if w.StatusOf(step) != Pending {
			continue
		}
		// continue if any Upstream is not terminated
		ups := w.UpstreamOf(step)
		if isAnyUpstreamNotTerminated(ups) {
			continue
		}
		option := w.OptionOf(step)
		when := DefaultCondition
		if option != nil && option.Condition != nil {
			when = option.Condition
		}
		if nextStatus := when(ctx, ups); nextStatus.IsTerminated() {
			w.StateOf(step).SetStatus(nextStatus)
			w.setError(step, StatusError{nextStatus, nil})
			w.signalTick()
			continue
		}
		// start the Step
		w.lease()
		w.StateOf(step).SetStatus(Running)
		w.waitGroup.Add(1)
		go func(ctx context.Context, step Steper) {
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
			w.StateOf(step).SetStatus(result)
			w.setError(step, StatusError{result, err})
		}(ctx, step)
	}
	return false
}

func (w *Workflow) runStep(ctx context.Context, step Steper) error {
	// set Step-level timeout for the Step
	var notAfter time.Time
	option := w.OptionOf(step)
	if option != nil && option.Timeout != nil {
		notAfter = w.clock.Now().Add(*option.Timeout)
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
			if ierr := catchPanicAsError(func() error {
				return w.StateOf(step).Input(ctx)
			}); ierr != nil {
				err = ierr
				return err
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
