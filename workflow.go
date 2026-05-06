package flow

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
)

// Workflow represents a collection of connected Steps that form a directed acyclic graph (DAG).
//
// The Steps are connected via dependency, use Step(), Steps() or Pipe(), BatchPipe() to add Steps into Workflow.
//
//	workflow.Add(
//		Step(a),
//		Steps(b, c).DependsOn(a),	// a -> b, c
//		Pipe(d, e, f),              // d -> e -> f
//		BatchPipe(
//			Steps(g, h),
//			Steps(i, j),
//		),                          // g, h -> i, j
//	)
//
// Workflow will execute Steps in a topological order, each Step will be executed in a separate goroutine.
//
// Workflow guarantees that
//
//	Before a Step goroutine starts, all its Upstream Steps are `terminated`.
//
// Check `StepStatus` and `Condition` for details.
//
// Workflow supports Step-level configuration,       check Step(), Steps() and Pipe() for details.
// Workflow supports Composite Steps,				 check Has(), As() and HasStep() for details.
type Workflow struct {
	MaxConcurrency int         // MaxConcurrency limits the max concurrency of running Steps
	DontPanic      bool        // DontPanic suppress panics, instead return it as error
	SkipAsError    bool        // SkipAsError marks skipped Steps as an error if true, otherwise ignore them
	Clock          clock.Clock // Clock for retry and unit test
	DefaultOption  *StepOption // DefaultOption is the default option for all Steps

	StepInterceptors    []StepInterceptor    // per-step global interceptors
	AttemptInterceptors []AttemptInterceptor // per-attempt global interceptors

	StepBuilder // StepBuilder to call BuildStep() for Steps

	steps map[Steper]*State // the internal states of Steps

	statusChange *sync.Cond     // a condition to signal the status change to proceed tick
	leaseBucket  chan struct{}  // constraint max concurrency of running Steps, nil means no limit
	waitGroup    sync.WaitGroup // to prevent goroutine leak
	isRunning    sync.Mutex     // indicate whether the Workflow is running
}

// Add Steps into Workflow in phase Main.
func (w *Workflow) Add(was ...Builder) *Workflow {
	if w.steps == nil {
		w.steps = make(map[Steper]*State)
	}
	for _, wa := range was {
		if wa != nil {
			for step, config := range wa.AddToWorkflow() {
				if w.DefaultOption != nil && config != nil {
					config.Option = slices.Insert(config.Option, 0, func(o *StepOption) {
						*o = *w.DefaultOption
					})
				}
				w.addStep(step, config)
			}
		}
	}
	return w
}

// AddStep adds a Step into Workflow with the given phase and config.
func (w *Workflow) addStep(step Steper, config *StepConfig) {
	if step == nil {
		return
	}
	w.BuildStep(step)
	if !HasStep(w, step) {
		// the step is new, it becomes a new root.
		// add the new root to the Workflow.
		// if the step embeds a previous root step,
		// we need to replace them with the new root.
		// workflow will only orchestrate the root Steps,
		// and leave the nested Steps being managed by the root Steps.
		var oldRoots Set[Steper]
		Traverse(step, func(s Steper, walked []Steper) TraverseDecision {
			if r := w.RootOf(s); r != nil {
				if r != s { // s has another root
					panic(fmt.Errorf("add step %p(%s) failed, another step %p(%s) already has %p(%s)",
						step, step, r, r, s, s))
				}
				oldRoots.Add(r)
				return TraverseEndBranch
			}
			return TraverseContinue
		})
		state := new(State)
		for old := range oldRoots {
			state.MergeConfig(w.steps[old].Config)
			delete(w.steps, old)
		}
		w.steps[step] = state
	}
	if config != nil {
		for up := range config.Upstreams {
			w.setUpstream(step, up)
		}
		config.Upstreams = nil
		// merge config to the state in the lowest workflow
		w.StateOf(step).MergeConfig(config)
	}
}

// setUpstream will put the upstream into proper state.
func (w *Workflow) setUpstream(step, up Steper) {
	if step == nil || up == nil {
		return
	}
	w.addStep(up, nil) // just add the upstream step
	var stepWalked, upWalked []Steper
	Traverse(w, func(s Steper, walked []Steper) TraverseDecision {
		if s == step {
			stepWalked = walked
		}
		if s == up {
			upWalked = walked
		}
		if len(stepWalked) > 0 && len(upWalked) > 0 {
			return TraverseStop
		}
		return TraverseContinue
	})
	i := 0
	for ; i < len(stepWalked) && i < len(upWalked); i++ {
		if stepWalked[i] != upWalked[i] {
			break
		}
	}
	i--
	for ; i >= 0; i-- {
		if s, ok := stepWalked[i].(interface {
			StateOf(Steper) *State
			RootOf(Steper) Steper
		}); ok {
			s.StateOf(s.RootOf(step)).AddUpstream(up)
		}
	}
}

// Empty returns true if the Workflow don't have any Step.
func (w *Workflow) Empty() bool { return w == nil || len(w.steps) == 0 }

// Steps returns all root Steps in the Workflow.
func (w *Workflow) Steps() []Steper { return w.Unwrap() }
func (w *Workflow) Unwrap() []Steper {
	if w.Empty() {
		return nil
	}
	return Keys(w.steps)
}

// RootOf returns the root Step of the given Step.
func (w *Workflow) RootOf(step Steper) Steper {
	if w.Empty() {
		return nil
	}
	for root := range w.steps {
		if HasStep(root, step) {
			return root
		}
	}
	return nil
}

// StateOf returns the internal state of the Step.
// State includes Step's status, error, input, dependency and config.
func (w *Workflow) StateOf(step Steper) *State {
	if w.Empty() || step == nil {
		return nil
	}
	for root := range w.steps {
		var find *State
		Traverse(root, func(s Steper, walked []Steper) TraverseDecision {
			if step == s {
				find = w.steps[root]
				return TraverseStop // found
			}
			if sub, ok := s.(interface{ StateOf(Steper) *State }); ok {
				if state := sub.StateOf(step); state != nil {
					find = state
					return TraverseStop // found in sub-workflow
				}
				return TraverseEndBranch // not found in sub-workflow
			}
			return TraverseContinue
		})
		if find != nil {
			return find
		}
	}
	return nil
}

// UpstreamOf returns all upstream Steps and their status and error.
func (w *Workflow) UpstreamOf(step Steper) map[Steper]StepResult {
	if w.Empty() {
		return nil
	}
	rv := make(map[Steper]StepResult)
	for up := range w.StateOf(step).Upstreams() {
		up = w.RootOf(up)
		rv[up] = w.StateOf(up).GetStepResult()
	}
	return rv
}

// IsTerminated returns true if all Steps terminated.
func (w *Workflow) IsTerminated() bool {
	if w.Empty() {
		return true
	}
	for _, state := range w.steps {
		if !state.GetStatus().IsTerminated() {
			return false
		}
	}
	return true
}

// Reset resets the Workflow to ready for a new run.
func (w *Workflow) Reset() error {
	if !w.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer w.isRunning.Unlock()
	w.reset()
	return nil
}

func (w *Workflow) reset() {
	for _, state := range w.steps {
		state.SetStatus(Pending)
	}
	if w.Clock == nil {
		w.Clock = clock.New()
	}
	w.statusChange = sync.NewCond(new(sync.Mutex))
	if w.MaxConcurrency > 0 {
		// use buffered channel as a sized bucket
		// a Step needs to create a lease in the bucket to run,
		// and remove the lease from the bucket when it's done.
		w.leaseBucket = make(chan struct{}, w.MaxConcurrency)
	}
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
	if w.Empty() {
		return nil
	}
	w.reset()
	// preflight check
	if err := w.preflight(); err != nil {
		return err
	}
	// each time one Step terminated, tick forward
	w.statusChange.L.Lock()
	for {
		if done := w.tick(ctx); done {
			break
		}
		w.statusChange.Wait()
	}
	w.statusChange.L.Unlock()
	// ensure all goroutines are exited
	w.waitGroup.Wait()
	// return the error
	err := make(ErrWorkflow)
	for step, state := range w.steps {
		err[step] = state.GetStepResult()
	}
	if w.SkipAsError && err.AllSucceeded() {
		return nil
	}
	if !w.SkipAsError && err.AllSucceededOrSkipped() {
		return nil
	}
	return err
}

const scanned StepStatus = "scanned" // a private status for preflight

// scheduled is a private StepStatus sentinel used by tick() to atomically
// claim a step and prevent double-spawning. Never exposed to users.
const scheduled StepStatus = "scheduled"

type stepExecution struct {
	w       *Workflow
	step    Steper
	state   *State
	attempt uint64
	onRetry func(WorkflowEvent)
}

func isAllUpstreamScanned(ups map[Steper]StepResult) bool {
	for _, up := range ups {
		if up.Status != scanned {
			return false
		}
	}
	return true
}
func isAnyUpstreamNotTerminated(ups map[Steper]StepResult) bool {
	for _, up := range ups {
		if !up.Status.IsTerminated() {
			return true
		}
	}
	return false
}
func (w *Workflow) preflight() error {
	// assert all dependency would not form a cycle
	// start scanning, mark Step as Scanned only when its all dependencies are Scanned
	for {
		hasNewScanned := false // whether a new Step being marked as Scanned this turn
		for step, state := range w.steps {
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
	for step, state := range w.steps {
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
	for _, step := range w.steps {
		step.SetStatus(Pending)
	}
	return nil
}

// tick will not block, it starts a goroutine for each runnable Step.
// tick returns true if all steps in all phases are terminated.
func (w *Workflow) tick(ctx context.Context) bool {
	if w.IsTerminated() {
		return true
	}
	for step := range w.steps {
		state := w.StateOf(step)
		// we only process pending Steps
		if state.GetStatus() != Pending {
			continue
		}
		// we only process Steps whose all upstreams are terminated
		ups := w.UpstreamOf(step)
		if isAnyUpstreamNotTerminated(ups) {
			continue
		}
		// kick off the Step
		if w.lease() {
			state.SetStatus(scheduled)
			w.waitGroup.Add(1)
			ex := &stepExecution{w: w, step: step, state: state}
			go ex.run(ctx)
		}
	}
	return false
}

func (w *Workflow) signalStatusChange() {
	w.statusChange.L.Lock()
	defer w.statusChange.L.Unlock()
	w.statusChange.Signal()
}

func (ex *stepExecution) run(ctx context.Context) {
	defer ex.w.waitGroup.Done()

	ups := ex.w.UpstreamOf(ex.step)
	option := ex.state.Option()
	cond := DefaultCondition
	if option != nil && option.Condition != nil {
		cond = option.Condition
	}

	terminalReason := Pending
	if nextStatus := cond(ctx, ups); nextStatus.IsTerminated() {
		terminalReason = nextStatus
	}

	info := StepInfo{Step: ex.step, TerminalReason: terminalReason}

	// Build StepInterceptor chain; collect retryNotifiers.
	// The innermost next is executeWithRetry for normal steps; a no-op for terminal steps
	// (interceptors that observe terminalReason should not call next).
	var retrySinks []func(WorkflowEvent)
	var stepNext func(context.Context) error
	if terminalReason == Pending {
		stepNext = ex.executeWithRetry
	} else {
		stepNext = func(ctx context.Context) error { return nil }
	}
	for i := len(ex.w.StepInterceptors) - 1; i >= 0; i-- {
		ic := ex.w.StepInterceptors[i]
		if rn, ok := ic.(retryNotifier); ok {
			retrySinks = append(retrySinks, rn.onRetry)
		}
		next := stepNext
		icLocal := ic
		stepNext = func(ctx context.Context) error {
			return icLocal.InterceptStep(ctx, info, next)
		}
	}
	ex.onRetry = func(e WorkflowEvent) {
		for _, s := range retrySinks {
			s(e)
		}
	}

	var status StepStatus
	var err error

	if terminalReason != Pending {
		err = stepNext(ctx)
		status = terminalReason
	} else {
		ex.state.SetStatus(Running)
		err = stepNext(ctx)
		status = statusFromError(err)
		if status == Failed {
			switch {
			case DefaultIsCanceled(err),
				errors.Is(err, context.Canceled),
				errors.Is(err, context.DeadlineExceeded):
				status = Canceled
			}
		}
	}

	ex.state.SetStatus(status)
	ex.state.SetError(err)
	ex.w.unlease()
	ex.w.signalStatusChange()
}

func (ex *stepExecution) executeWithRetry(ctx context.Context) error {
	option := ex.state.Option()

	// Propagate interceptors to SubWorkflow once — before the retry loop starts.
	if recv, ok := ex.step.(InterceptorReceiver); ok {
		recv.PrependInterceptors(ex.w.StepInterceptors, ex.w.AttemptInterceptors)
	}

	ex.wireNotify(option)

	attemptChain := ex.buildAttemptChain()

	var notAfter time.Time
	if option != nil && option.Timeout != nil {
		notAfter = ex.w.Clock.Now().Add(*option.Timeout)
		var cancel func()
		ctx, cancel = ex.w.Clock.WithDeadline(ctx, notAfter)
		defer cancel()
	}

	return ex.w.retry(option.RetryOption)(ctx, attemptChain, notAfter)
}

func (ex *stepExecution) buildAttemptChain() func(context.Context) error {
	chain := func(ctx context.Context) error {
		return ex.runAttempt(ctx)
	}
	for i := len(ex.w.AttemptInterceptors) - 1; i >= 0; i-- {
		ic := ex.w.AttemptInterceptors[i]
		next := chain
		icLocal := ic
		chain = func(ctx context.Context) error {
			info := AttemptInfo{
				StepInfo: StepInfo{Step: ex.step},
				Attempt:  ex.attempt,
			}
			return icLocal.InterceptAttempt(ctx, info, next)
		}
	}
	// Wrap the full attempt chain (including interceptors) so ex.attempt is always
	// incremented after each attempt regardless of whether interceptors short-circuit.
	inner := chain
	return func(ctx context.Context) error {
		defer func() { ex.attempt++ }()
		return inner(ctx)
	}
}

func (ex *stepExecution) runAttempt(ctx context.Context) error {
	do := func(fn func() error) error { return fn() }
	if ex.w.DontPanic {
		do = catchPanicAsError
	}

	var ctxStep context.Context
	err := do(func() error {
		ctxBefore, errBefore := ex.state.Before(ctx, ex.step, do)
		ctxStep = ctxBefore
		return errBefore
	})
	if err != nil {
		err = ErrBeforeStep{err}
	} else {
		err = do(func() error { return ex.step.Do(ctxStep) })
	}
	return do(func() error { return ex.state.After(ctxStep, ex.step, err) })
}

func (ex *stepExecution) wireNotify(option *StepOption) {
	if option == nil || option.RetryOption == nil {
		return
	}
	// Deep-copy RetryOption before mutating its Notify field.
	// option is a fresh StepOption from State.Option(), but its RetryOption pointer
	// may be shared (e.g. when Workflow.DefaultOption carries a RetryOption) — a
	// shallow copy of StepOption does not copy the pointed-to RetryOption.
	ro := *option.RetryOption
	option.RetryOption = &ro

	userNotify := option.RetryOption.Notify
	option.RetryOption.Notify = func(err error, d time.Duration) {
		// ex.attempt has already been incremented by the buildAttemptChain wrapper's
		// defer, so subtract 1 to get the attempt number that just failed.
		e := WorkflowEvent{
			Step:            ex.step,
			Type:            Retrying,
			Attempt:         ex.attempt - 1,
			Err:             err,
			BackoffDuration: d,
		}
		if ex.onRetry != nil {
			ex.onRetry(e)
		}
		if userNotify != nil {
			userNotify(err, d)
		}
	}
}

func statusFromError(err error) StepStatus {
	if err == nil {
		return Succeeded
	}
	if s := StatusFromError(err); s != Failed {
		return s
	}
	return Failed
}

func (w *Workflow) lease() bool {
	if w.leaseBucket == nil {
		return true
	}
	select {
	case w.leaseBucket <- struct{}{}:
		return true
	default:
		return false
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
				switch t := r.(type) {
				case error:
					*err = t
				default:
					*err = fmt.Errorf("%s", r)
				}
				*err = WithStackTraces(4, 32, func(f runtime.Frame) bool {
					return strings.HasPrefix(f.Function, "github.com/Azure/go-workflow")
				})(*err)
				*err = ErrPanic{*err}
			}
		}()
		*err = f()
	}(&returnErr)
	return returnErr
}

// SubWorkflow is a helper struct to let you create a step with a sub-workflow.
// Embed this struct to your struct definition.
//
// Usage:
//
//	type MyStep struct {
//		flow.SubWorkflow
//	}
//
//	func (s *MyStep) BuildStep() {
//		s.Reset() // reset the workflow
//		s.Add(
//			flow.Step(/* stepX */),
//		)
//	}
//
//	func main() {
//		w := &flow.Workflow{}
//		myStep := &MyStep{}
//		w.Add(flow.Step(myStep)) // BuildStep() will be called when adding the step
//		...
//		stepX := flow.As[*StepX](w) // we can get the inner stepX from the workflow
//	}
type SubWorkflow struct{ w Workflow }

func (s *SubWorkflow) Unwrap() Steper                    { return &s.w }
func (s *SubWorkflow) Add(builders ...Builder) *Workflow { return s.w.Add(builders...) }
func (s *SubWorkflow) Do(ctx context.Context) error      { return s.w.Do(ctx) }

// Reset resets the sub-workflow to ready for BuildStep()
func (s *SubWorkflow) Reset() { s.w = Workflow{} }

// PrependInterceptors implements InterceptorReceiver.
// Parent workflow interceptors are prepended so they execute outside child interceptors.
func (s *SubWorkflow) PrependInterceptors(step []StepInterceptor, attempt []AttemptInterceptor) {
	combined := make([]StepInterceptor, len(step)+len(s.w.StepInterceptors))
	copy(combined, step)
	copy(combined[len(step):], s.w.StepInterceptors)
	s.w.StepInterceptors = combined

	combinedA := make([]AttemptInterceptor, len(attempt)+len(s.w.AttemptInterceptors))
	copy(combinedA, attempt)
	copy(combinedA[len(attempt):], s.w.AttemptInterceptors)
	s.w.AttemptInterceptors = combinedA
}
