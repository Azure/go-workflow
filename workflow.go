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

// Workflow orchestrates a collection of Steps connected by dependency edges
// into a Directed Acyclic Graph (DAG).
//
// You declare the graph with the helpers in step.go: Step / Steps / Pipe /
// BatchPipe (and branching helpers If / Switch from branch.go), then hand
// them to Workflow.Add:
//
//	workflow.Add(
//	    Step(a),
//	    Steps(b, c).DependsOn(a),    // a runs first, then b and c in parallel.
//	    Pipe(d, e, f),               // d -> e -> f.
//	    BatchPipe(
//	        Steps(g, h),
//	        Steps(i, j),
//	    ),                           // g, h finish, then i, j run in parallel.
//	)
//
// Workflow.Do executes the graph in topological order. Each step that becomes
// runnable runs in its own goroutine, with the following guarantee:
//
//	When a step's worker goroutine starts, every upstream step is already
//	in a terminal status (Succeeded / Failed / Canceled / Skipped). The
//	step's Condition then decides whether it actually runs (Running) or is
//	settled inline as Skipped / Canceled.
//
// All workflow-level configuration lives in [WorkflowOption], exposed via
// Workflow.Option. See [WorkflowOption] for the available fields and the
// parent → child inheritance rules used when a Workflow is run as a step
// inside another Workflow.
//
// # Building sub-workflows
//
// A Workflow can itself be used as a Step inside another Workflow. Three
// patterns, in order of preference:
//
//  1. **Embed flow.Workflow at construction time (recommended).**
//     Build the sub-workflow's DAG once, before passing it as a Step. This
//     makes the inner Steps visible to introspection (Has / As / HasStep)
//     and lets the parent's [WorkflowOption] inherit cleanly via
//     [WorkflowOptionReceiver]:
//
//	type MyComposite struct{ flow.Workflow }
//	func New() *MyComposite {
//	    c := &MyComposite{}
//	    c.Add(flow.Step(/* inner steps */))
//	    return c
//	}
//
//  2. **Build inside Do() with a sync.Once guard.**
//     If the sub-workflow needs context only available at run time, build
//     it lazily on first Do(). Guard the Add() call with sync.Once so
//     re-execution (retries, multiple Do()s) does not re-Add the same Step:
//
//	type MyLazy struct{ flow.Workflow; once sync.Once }
//	func (m *MyLazy) Do(ctx context.Context) error {
//	    m.once.Do(func() { m.Add(/* inner steps */) })
//	    return m.Workflow.Do(ctx)
//	}
//
//  3. **Construct &flow.Workflow{} inline inside Do() (last resort).**
//     A throwaway sub-workflow is opaque to the parent: it does not
//     participate in [WorkflowOptionReceiver] inheritance unless the host
//     step itself implements [WorkflowOptionReceiver] and forwards the
//     parent's Option.
//
// **DO NOT** call [Workflow.Add] from inside Do() (or any method
// transitively reachable from Do()) without a sync.Once guard. Doing so
// produces undefined behavior: callbacks accumulate across re-executions,
// duplicate Steps multiply, introspection helpers report multiple matches
// for what should be a single Step, and Mutator dispatch becomes stale.
// See the warning on [Workflow.Add].
//
// Per-step configuration: use Step / Steps / Pipe (see step.go).
// Composite steps:        use Has / As / HasStep (see wrap.go).
type Workflow struct {
	// Option groups all workflow-level configuration (concurrency cap,
	// panic policy, skip-as-error, clock, default step options, mutators,
	// interceptors, and inheritance opt-out). See [WorkflowOption].
	Option WorkflowOption

	StepBuilder // embeds the BuildStep memo so Workflow.Add can call BuildStep on new steps once.

	steps map[Steper]*State // root step → its State (status + StepConfig).

	statusChange *sync.Cond     // signals to the tick loop when a worker terminates.
	leaseBucket  chan struct{}  // bounded-channel "permit pool" enforcing Option.MaxConcurrency; nil means unlimited.
	waitGroup    sync.WaitGroup // tracks worker goroutines so Do() can wait for them on exit.
	isRunning    sync.Mutex     // single-runner guard: TryLock fails fast if Do/Reset is re-entered.
}

// Scalar accessors: handle nil-pointer dereference and runtime defaults.
// All in-code reads of these scalars MUST go through these accessors.

func (w *Workflow) maxConcurrency() int {
	if w.Option.MaxConcurrency == nil {
		return 0
	}
	return *w.Option.MaxConcurrency
}

func (w *Workflow) dontPanic() bool {
	return w.Option.DontPanic != nil && *w.Option.DontPanic
}

func (w *Workflow) skipAsError() bool {
	return w.Option.SkipAsError != nil && *w.Option.SkipAsError
}

func (w *Workflow) clock() clock.Clock {
	if w.Option.Clock == nil {
		return clock.New()
	}
	return w.Option.Clock
}

// Add wires Builders (Step / Steps / Pipe / BatchPipe / If / Switch / …) into
// this Workflow. Repeated calls are additive: a step that appears in multiple
// Add() calls has its config merged (upstreams unioned, callbacks/options
// concatenated). Returns the Workflow for chaining.
//
// If Option.StepDefaults is set, it is prepended to every step's Option
// list as a SEED — so per-step Option calls (Retry, Timeout, When, …) still
// win.
//
// **WARNING — re-entry is forbidden.** Calling Add from inside the same
// Workflow's Do (or any method transitively reachable from Do) is
// FORBIDDEN unless guarded by a sync.Once that ensures Add fires at most
// once across the lifetime of the host step. Unguarded re-entry produces
// undefined behavior, including (a) callbacks accumulating across
// re-executions (BeforeStep / AfterStep / Input fire N times after N
// runs), (b) duplicate Steps registered under the same identity,
// (c) introspection helpers (Has / As / HasStep) reporting multiple
// matches for what should be one Step, and (d) Mutator dispatch becoming
// stale because per-step state was already marked applied. The framework
// does NOT detect this misuse at runtime: a host step's isRunning lock is
// released between retry attempts, so there is no stable signal to
// distinguish legitimate sync.Once-guarded lazy init from accidental
// re-Add. See [Workflow] godoc for the recommended construction patterns.
func (w *Workflow) Add(was ...Builder) *Workflow {
	if w.steps == nil {
		w.steps = make(map[Steper]*State)
	}
	for _, wa := range was {
		if wa != nil {
			for step, config := range wa.AddToWorkflow() {
				if w.Option.StepDefaults != nil && config != nil {
					config.Option = slices.Insert(config.Option, 0, func(o *StepOption) {
						*o = *w.Option.StepDefaults
					})
				}
				w.addStep(step, config)
			}
		}
	}
	return w
}

// addStep registers `step` as a root in this workflow if it isn't already
// reachable from one. If the new step embeds previously-registered roots, those
// roots are demoted (their state is folded into the new root's state) so the
// scheduler always operates on a single ROOT per composite tree.
//
// Then, if config is non-nil, declared upstreams are wired and the rest of the
// config is merged into the resolved State (typically the lowest-level
// containing workflow's State).
func (w *Workflow) addStep(step Steper, config *StepConfig) {
	if step == nil {
		return
	}
	w.BuildStep(step)
	if !HasStep(w, step) {
		// New root: scan its tree for any previously-registered roots that are
		// now nested inside it, and absorb their config so the scheduler sees a
		// single root per composite. Panic if the new step would clash with a
		// step that already belongs to a *different* root tree (that would be
		// double-ownership and we have no way to resolve it).
		var oldRoots Set[Steper]
		Traverse(step, func(s Steper, walked []Steper) TraverseDecision {
			if r := w.RootOf(s); r != nil {
				if r != s { // s already belongs to another root in this workflow.
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

// applyMutators runs each Mutator in w.Option.Mutators against step. For every match,
// the returned Builder's config keyed on the matched layer is merged into the
// state of step (the wrapper key in this workflow). Called once per step,
// just before its first attempt.
func (w *Workflow) applyMutators(ctx context.Context, step Steper, state *State) {
	if len(w.Option.Mutators) == 0 {
		return
	}
	do := func(fn func() error) error { return fn() }
	if w.dontPanic() {
		do = catchPanicAsError
	}
	for _, m := range w.Option.Mutators {
		err := do(func() error {
			matched, target, b := m.applyTo(ctx, step)
			if !matched || b == nil {
				return nil
			}
			for s, cfg := range b.AddToWorkflow() {
				if s == target {
					state.MergeConfig(cfg)
				}
				// configs keyed on other steps are silently dropped — Mutator
				// scope is the matched layer only.
			}
			return nil
		})
		if err != nil {
			state.SetError(err)
			return // a panicking mutator is fatal for this step
		}
	}
}

// setUpstream records `up` as an upstream of `step`, ensuring `up` itself is
// registered as a step first. The dependency edge is added at every workflow
// level whose tree contains both `step` and `up` — this keeps nested
// SubWorkflows in sync so their tick loops see the same dependency.
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

// Empty reports whether the Workflow has no steps. Nil-safe.
func (w *Workflow) Empty() bool { return w == nil || len(w.steps) == 0 }

// Steps returns the workflow's root steps. (Composite-step internals are not
// exposed — only the values that are tracked by the scheduler.)
//
// Steps and Unwrap return the same slice; Unwrap also makes Workflow
// participate in the Steper unwrapping protocol (see wrap.go), so utilities
// like As[T] / HasStep / String can walk into nested workflows.
func (w *Workflow) Steps() []Steper { return w.Unwrap() }
func (w *Workflow) Unwrap() []Steper {
	if w.Empty() {
		return nil
	}
	return Keys(w.steps)
}

// RootOf returns the root step (the value the scheduler tracks) that contains
// `step`, or nil if no root contains it. A step is its own root when it was
// added directly.
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

// StateOf returns the State for `step` — the per-step bookkeeping (status +
// config). For composite steps, StateOf returns the State of the OWNING
// workflow level (root or sub-workflow), not necessarily this top-level
// workflow's State.
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

// UpstreamOf returns each direct upstream of `step` mapped to that upstream's
// current StepResult. Upstream identities are normalised to their root step
// (i.e. the value the scheduler tracks), so callers see exactly what the
// scheduler sees.
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

// IsTerminated reports whether every step in the workflow has reached a
// terminal status. The tick loop polls this to decide when to exit.
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

// Reset rewinds every Step's status back to Pending so the Workflow can be
// Do()-ed again. Reset rejects with ErrWorkflowIsRunning if a Do call is
// currently in flight.
//
// Reset does NOT modify w.steps (the set of Steps registered via Add) — a
// Workflow built once can be Do()-ed any number of times via Reset/Do
// cycles, with the same DAG each time. To start from an empty set of Steps,
// allocate a new Workflow.
//
// Reset does NOT modify w.Option either. Cross-run accumulation of
// parent-inherited contributions is prevented by the snapshot/restore in
// Do(), not by Reset. Calling Reset between runs is therefore optional from
// an Option-isolation standpoint; its purpose is purely to rewind per-step
// status for re-execution.
func (w *Workflow) Reset() error {
	if !w.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer w.isRunning.Unlock()
	w.reset()
	return nil
}

// reset is the per-Do internal reset: clear all step results back to Pending,
// install a fresh statusChange Cond, and re-allocate the concurrency lease
// bucket sized for Option.MaxConcurrency.
//
// reset does NOT touch w.Option: parent → child Option inheritance is
// preserved by the snapshot/restore in Do() (see Workflow.Do).
func (w *Workflow) reset() {
	for _, state := range w.steps {
		state.SetStepResult(StepResult{Status: Pending})
	}
	w.statusChange = sync.NewCond(&sync.Mutex{})
	if mc := w.maxConcurrency(); mc > 0 {
		w.leaseBucket = make(chan struct{}, mc)
	} else {
		w.leaseBucket = nil
	}
}

// InheritOption implements [WorkflowOptionReceiver]. A parent Workflow calls
// this method on each sub-workflow root step's first receiver (located via
// findOptionReceiver) ONCE in the parent's Do() prologue.
//
// Merge rules:
//   - if w.Option.DontInherit is true, this is a no-op (restore is still
//     non-nil but does nothing);
//   - for each scalar pointer (MaxConcurrency, DontPanic, SkipAsError) and
//     interface/pointer (Clock, StepDefaults) field: if the child's field is
//     nil, the parent's value is copied in; non-nil child fields are
//     preserved;
//   - for each slice (Mutators, StepInterceptors, AttemptInterceptors): a
//     fresh slice equal to parent ++ child replaces the child's field.
//
// The returned restore function rewinds w.Option to its pre-InheritOption
// shape; the parent MUST defer it on every Do() exit path so the child does
// not retain inherited state across runs. restore is always non-nil and is
// safe to call multiple times (idempotent).
func (w *Workflow) InheritOption(parent WorkflowOption) (restore func()) {
	prev := w.Option
	if w.Option.DontInherit {
		return func() { w.Option = prev }
	}
	if w.Option.MaxConcurrency == nil {
		w.Option.MaxConcurrency = parent.MaxConcurrency
	}
	if w.Option.DontPanic == nil {
		w.Option.DontPanic = parent.DontPanic
	}
	if w.Option.SkipAsError == nil {
		w.Option.SkipAsError = parent.SkipAsError
	}
	if w.Option.Clock == nil {
		w.Option.Clock = parent.Clock
	}
	if w.Option.StepDefaults == nil {
		w.Option.StepDefaults = parent.StepDefaults
	}
	w.Option.Mutators = prependSlice(parent.Mutators, w.Option.Mutators)
	w.Option.StepInterceptors = prependSlice(parent.StepInterceptors, w.Option.StepInterceptors)
	w.Option.AttemptInterceptors = prependSlice(parent.AttemptInterceptors, w.Option.AttemptInterceptors)
	return func() { w.Option = prev }
}

// effectiveStepInterceptors returns the chain to invoke for THIS run. With
// parent → child Option inheritance now performed eagerly in Do()'s prologue
// (writing into w.Option.StepInterceptors directly), the effective chain IS
// simply w.Option.StepInterceptors.
func (w *Workflow) effectiveStepInterceptors() []StepInterceptor {
	return w.Option.StepInterceptors
}

// effectiveAttemptInterceptors mirrors effectiveStepInterceptors for AttemptInterceptors.
func (w *Workflow) effectiveAttemptInterceptors() []AttemptInterceptor {
	return w.Option.AttemptInterceptors
}

// Do runs the Workflow synchronously: it spawns a goroutine for every
// runnable step, blocks the calling goroutine on a tick loop until every
// step has reached a terminal status, then returns.
//
// Concurrency: only one Do (or Reset) may be in flight at a time per
// Workflow instance — re-entrant calls return ErrWorkflowIsRunning.
//
// Return value:
//   - nil  if every step finished Succeeded (and, if SkipAsError == false,
//     Skipped also counts as success).
//   - ErrWorkflow (a map of step → StepResult) otherwise. ErrCycleDependency
//     is returned by preflight if the graph isn't a DAG.
func (w *Workflow) Do(ctx context.Context) error {
	// Single-runner guard.
	if !w.isRunning.TryLock() {
		return ErrWorkflowIsRunning
	}
	defer w.isRunning.Unlock()

	// Snapshot Option so any InheritOption writes performed below (and
	// transitively by nested workflows during their own Do() prologue) are
	// reverted at the end of THIS Do() call. The snapshot is a shallow copy;
	// this is correct because:
	//   - InheritOption only overwrites nil pointer fields with the parent's
	//     pointer values (not mutating the parent's targets), and
	//   - prependSlice always allocates a fresh slice, so neither the
	//     snapshot's slice header nor the parent's slice is mutated.
	optSnapshot := w.Option
	defer func() { w.Option = optSnapshot }()

	// Nothing to do.
	if w.Empty() {
		return nil
	}

	w.reset()

	// Reject cycles before launching any work.
	if err := w.preflight(); err != nil {
		return err
	}

	// Propagate w.Option into every sub-workflow root step exactly once,
	// BEFORE the tick loop dispatches anything. Receivers are located via
	// pre-order Unwrap walk so a sub-workflow may be wrapped in a Steper-only
	// wrapper (e.g. NamedStep) without losing inheritance.
	//
	// Each receiver's InheritOption returns a `restore` func that we MUST
	// invoke on every Do() exit path so the child's Option is rewound to
	// its pre-InheritOption shape. Without this, repeated Do() runs of the
	// same parent would accumulate parent contributions on the child.
	var childRestores []func()
	for step := range w.steps {
		if recv := findOptionReceiver(step); recv != nil {
			if r := recv.InheritOption(w.Option); r != nil {
				childRestores = append(childRestores, r)
			}
		}
	}
	defer func() {
		for _, r := range childRestores {
			r()
		}
	}()

	// Tick loop: each time a step terminates it Signal()s the cond, we wake
	// up and tick() again. Inline-settled steps may unblock more steps within
	// the same tick (no signal needed for those — see tick()).
	w.statusChange.L.Lock()
	for {
		if done := w.tick(ctx); done {
			break
		}
		w.statusChange.Wait()
	}
	w.statusChange.L.Unlock()

	// Drain worker goroutines so we don't return while children are still alive.
	w.waitGroup.Wait()

	// Build the per-step error map and decide the overall outcome.
	err := make(ErrWorkflow)
	for step, state := range w.steps {
		err[step] = state.GetStepResult()
	}
	if w.skipAsError() && err.AllSucceeded() {
		return nil
	}
	if !w.skipAsError() && err.AllSucceededOrSkipped() {
		return nil
	}
	return err
}

// scanned is a private status used only by preflight() to mark steps it has
// proven to be reachable in topological order. It is replaced by Pending
// before Do() starts dispatching.
const scanned StepStatus = "scanned"

// stepExecution is the per-step worker context handed to the goroutine that
// runs a single step. attempt is bumped after each completed attempt by the
// retry loop.
type stepExecution struct {
	w       *Workflow
	step    Steper
	state   *State
	attempt uint64
}

// isAllUpstreamScanned reports whether every upstream of a step has been
// proved reachable by preflight (has the private "scanned" status).
func isAllUpstreamScanned(ups map[Steper]StepResult) bool {
	for _, up := range ups {
		if up.Status != scanned {
			return false
		}
	}
	return true
}

// isAnyUpstreamNotTerminated reports whether at least one upstream is still
// running / pending. The tick loop uses this to skip steps whose upstreams
// haven't all settled yet.
func isAnyUpstreamNotTerminated(ups map[Steper]StepResult) bool {
	for _, up := range ups {
		if !up.Status.IsTerminated() {
			return true
		}
	}
	return false
}

// preflight verifies the dependency graph is a DAG. It iteratively marks
// every step whose upstreams are all already marked, until no further
// progress is possible. Anything left unmarked sits in a cycle and is
// reported via ErrCycleDependency.
//
// On success, all step statuses are reset to Pending so the tick loop can
// dispatch them.
func (w *Workflow) preflight() error {
	// Topo-scan: mark Steps whose upstreams are all marked, repeat until fixed point.
	for {
		hasNewScanned := false
		for step, state := range w.steps {
			if state.GetStatus() == scanned {
				continue
			}
			if isAllUpstreamScanned(w.UpstreamOf(step)) {
				hasNewScanned = true
				state.SetStatus(scanned)
			}
		}
		if !hasNewScanned {
			break
		}
	}

	// Anything still unscanned participates in a cycle.
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

	// Reset everyone to Pending for the real run.
	for _, step := range w.steps {
		step.SetStepResult(StepResult{Status: Pending})
	}
	return nil
}

// tick is one round of the scheduler. It is non-blocking — it spawns
// goroutines for every Pending step that is now eligible. Returns true iff
// every step has reached a terminal status.
//
// Why Condition is evaluated HERE (under statusChange.L) rather than inside
// the worker goroutine:
//
//   - Steps whose Condition resolves to a TERMINAL status (Skipped/Canceled)
//     are settled INLINE — no goroutine, no concurrency lease, no
//     interceptor chain. This keeps zero-cost branches truly cheap.
//   - Steps that WILL execute have their status set to Running before the
//     worker is spawned, so a subsequent tick cannot double-spawn them.
//
// Inline-settled steps may unblock downstream steps in the same tick. Because
// no goroutine is spawned for them, no signalStatusChange will fire — so we
// loop within tick() until a single pass produces no inline progress;
// otherwise the main Do() loop would Wait() forever for a signal that never
// comes.
func (w *Workflow) tick(ctx context.Context) bool {
	for {
		if w.IsTerminated() {
			return true
		}
		progressed := false
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

			// Apply Mutators exactly once per step, before reading Option /
			// evaluating Condition / starting the first attempt. This way the
			// Option/Before/After contributions from Mutators are visible to
			// the rest of this iteration. The flag is flipped BEFORE the merge
			// runs so that a panicking mutator (caught by applyMutators's
			// recover when Option.DontPanic is set) does not cause re-entry on the
			// next tick.
			if !state.MutatorsApplied() {
				state.SetMutatorsApplied()
				w.applyMutators(ctx, step, state)
				// If applyMutators caught a panic (DontPanic), the state already
				// holds an error. Settle the step inline as Failed so it never
				// reaches the worker goroutine.
				if err := state.GetError(); err != nil {
					state.SetStepResult(StepResult{
						Status:     Failed,
						Err:        err,
						FinishedAt: w.clock().Now(),
					})
					progressed = true
					continue
				}
			}

			// Evaluate Condition inline. If terminal (Skipped/Canceled), settle
			// the step here — no goroutine, no lease, no interceptor chain.
			cond := DefaultCondition
			if option := state.Option(); option != nil && option.Condition != nil {
				cond = option.Condition
			}
			if nextStatus := cond(ctx, ups); nextStatus.IsTerminated() {
				state.SetStepResult(StepResult{
					Status:     nextStatus,
					FinishedAt: w.clock().Now(),
				})
				progressed = true
				continue
			}

			// Step will execute: take a lease and spawn a worker goroutine.
			// SetStatus(Running) happens here (under statusChange.L) so a
			// subsequent tick won't see it as Pending and double-spawn.
			if w.lease() {
				state.SetStatus(Running)
				w.waitGroup.Add(1)
				ex := &stepExecution{w: w, step: step, state: state}
				go ex.run(ctx)
			}
		}
		// If we settled any step inline this pass, re-iterate to give downstream
		// steps a chance to be picked up without waiting for a signal.
		if !progressed {
			return false
		}
	}
}

// signalStatusChange wakes the tick loop. Called from a worker goroutine
// after the worker has updated its step's status to terminal.
func (w *Workflow) signalStatusChange() {
	w.statusChange.L.Lock()
	defer w.statusChange.L.Unlock()
	w.statusChange.Signal()
}

// run executes one step from start to terminal status: it builds the
// StepInterceptor chain (innermost call is executeWithRetry, which loops over
// attempts), runs it, classifies the result into a StepStatus, records the
// final StepResult, releases the concurrency lease, and signals the scheduler.
func (ex *stepExecution) run(ctx context.Context) {
	defer ex.w.waitGroup.Done()

	// Build the StepInterceptor chain. tick() has already evaluated the
	// Condition (terminal results were settled inline) and set the status to
	// Running, so we can dive straight in.
	//
	// When Option.DontPanic is true, EVERY interceptor invocation is wrapped in
	// catchPanicAsError so a panicking user interceptor cannot crash the
	// process or leave the lease unreleased / status unsignalled.
	stepNext := func(ctx context.Context) error { return ex.executeWithRetry(ctx) }
	stepICs := ex.w.effectiveStepInterceptors()
	for i := len(stepICs) - 1; i >= 0; i-- {
		// `ic` and `nextLocal` are declared inside the loop body with `:=`,
		// so they are fresh on every iteration and the closure below captures
		// each iteration's instance independently. The explicit naming makes
		// the per-iteration scoping obvious.
		ic := stepICs[i]
		nextLocal := stepNext
		stepNext = func(ctx context.Context) error {
			if ex.w.dontPanic() {
				return catchPanicAsError(func() error {
					return ic.InterceptStep(ctx, ex.step, nextLocal)
				})
			}
			return ic.InterceptStep(ctx, ex.step, nextLocal)
		}
	}

	err := stepNext(ctx)

	// Classify the error into a terminal StepStatus. Cancellation errors
	// (context.Canceled / DeadlineExceeded / DefaultIsCanceled-recognised)
	// are reported as Canceled rather than Failed.
	status := StatusFromError(err)
	if status == Failed {
		switch {
		case DefaultIsCanceled(err),
			errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded):
			status = Canceled
		}
	}

	ex.state.SetStepResult(StepResult{
		Status:     status,
		Err:        err,
		FinishedAt: ex.w.clock().Now(),
	})

	// Release the lease BEFORE signalling, so when the tick loop wakes up it
	// can immediately acquire a fresh lease for the next runnable step.
	ex.w.unlease()
	ex.w.signalStatusChange()
}

// executeWithRetry runs a single step's full attempt sequence under the
// configured retry policy and step-level Timeout. Before running, it
// propagates the effective interceptor chain into nested workflows so
// multi-level nesting (grandparent → parent → child) accumulates correctly
// for THIS run, while the user-supplied bases stay untouched.
func (ex *stepExecution) executeWithRetry(ctx context.Context) error {
	option := ex.state.Option()

	attemptChain := ex.buildAttemptChain()

	var notAfter time.Time
	if option != nil && option.Timeout != nil {
		notAfter = ex.w.clock().Now().Add(*option.Timeout)
		var cancel func()
		ctx, cancel = ex.w.clock().WithDeadline(ctx, notAfter)
		defer cancel()
	}

	return ex.w.retry(option.RetryOption)(ctx, attemptChain, notAfter)
}

// buildAttemptChain wraps a single attempt (Before → Do → After) with the
// per-attempt interceptors, returning a function suitable for the retry loop.
// The chain is wrapped one final time in a function that always increments
// ex.attempt after each completed attempt — even when an interceptor
// short-circuits — so the attempt counter remains accurate.
func (ex *stepExecution) buildAttemptChain() func(context.Context) error {
	chain := func(ctx context.Context) error {
		return ex.runAttempt(ctx)
	}
	attemptICs := ex.w.effectiveAttemptInterceptors()
	for i := len(attemptICs) - 1; i >= 0; i-- {
		// Same per-iteration capture pattern as run(); see comment there.
		ic := attemptICs[i]
		nextLocal := chain
		chain = func(ctx context.Context) error {
			if ex.w.dontPanic() {
				return catchPanicAsError(func() error {
					return ic.InterceptAttempt(ctx, ex.step, ex.attempt, nextLocal)
				})
			}
			return ic.InterceptAttempt(ctx, ex.step, ex.attempt, nextLocal)
		}
	}
	inner := chain
	return func(ctx context.Context) error {
		defer func() { ex.attempt++ }()
		return inner(ctx)
	}
}

// runAttempt executes one attempt: Before callbacks → Do → After callbacks.
//
// The `do` wrapper is either a direct invocation, or — when Option.DontPanic is true
// — catchPanicAsError, which converts a panic to an ErrPanic-tagged error.
// The Before chain may swap the context that is threaded into Do (and the
// After chain). After callbacks always run, even if Before or Do failed; they
// receive the latest error and can transform it.
func (ex *stepExecution) runAttempt(ctx context.Context) error {
	do := func(fn func() error) error { return fn() }
	if ex.w.dontPanic() {
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

// lease takes one slot from the concurrency permit pool. Returns true if the
// caller may now run, or false if the pool is full (the tick loop will retry
// on the next signal). When Option.MaxConcurrency is unset (leaseBucket == nil), the
// answer is always true.
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

// unlease returns one slot to the concurrency permit pool, or is a no-op if
// Option.MaxConcurrency is unset.
func (w *Workflow) unlease() {
	if w.leaseBucket != nil {
		<-w.leaseBucket
	}
}

// catchPanicAsError invokes f, recovers any panic, and returns it as an
// ErrPanic carrying a filtered stack trace (only frames inside this module
// are kept, to keep the trace readable).
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

// SubWorkflow makes any user struct behave as a Step that contains a
// Workflow.
//
// Deprecated: Embed [Workflow] directly in your own struct instead.
// SubWorkflow will be removed in the next major version of go-workflow.
// The recommended pattern is:
//
//	type MyStep struct {
//	    flow.Workflow
//	}
//
//	func NewMyStep() *MyStep {
//	    s := &MyStep{}
//	    s.Add(
//	        flow.Step(/* stepX */),
//	    )
//	    return s
//	}
//
// A Workflow embedded this way also satisfies [WorkflowOptionReceiver], so
// parent → child Option inheritance continues to work without any extra
// boilerplate.
type SubWorkflow struct{ w Workflow }

func (s *SubWorkflow) Unwrap() Steper                    { return &s.w }
func (s *SubWorkflow) Add(builders ...Builder) *Workflow { return s.w.Add(builders...) }
func (s *SubWorkflow) Do(ctx context.Context) error      { return s.w.Do(ctx) }

// Reset clears the inner workflow so a subsequent BuildStep() can rebuild
// from scratch.
//
// Deprecated: Reset is only invoked by the deprecated [StepBuilder.BuildStep]
// path. With the [Mutator] mechanism (see [Mutate]) and Do()-time sub-workflow
// construction, Reset is no longer needed and will be removed in the next
// major version of go-workflow.
func (s *SubWorkflow) Reset() { s.w = Workflow{} }

// InheritOption delegates to the inner Workflow's InheritOption so a parent
// workflow's Option propagates into the SubWorkflow's inner Workflow during
// the deprecation window. Implements [WorkflowOptionReceiver].
func (s *SubWorkflow) InheritOption(parent WorkflowOption) (restore func()) {
	return s.w.InheritOption(parent)
}
