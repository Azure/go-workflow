package flow

import (
	"context"
	"iter"
	"maps"
	"slices"
	"time"
)

// Steper is the contract every Step must satisfy: a single Do method.
// Implement it on any comparable type (typically a *struct) and the Workflow
// can orchestrate it.
//
// Why "comparable"? The Workflow stores steps as map keys to track their
// state, so two distinct *Foo instances must be distinguishable. In
// particular, do NOT use the empty `struct{}` as a Step type — every
// `struct{}` value compares equal in Go, and the Workflow would treat them
// all as the same step.
type Steper interface {
	Do(context.Context) error
}

// Builder is the glue between user-facing helpers (Step, Steps, Pipe, If, …)
// and Workflow.Add: anything that can produce a {step → config} map can be
// passed to Add().
type Builder interface {
	AddToWorkflow() map[Steper]*StepConfig
}

// BeforeStep is the signature for a hook that runs before a step's Do, on
// every retry attempt. It may swap the context for the rest of the chain.
// Returning a non-nil error short-circuits the remaining BeforeStep callbacks
// and is reported as ErrBeforeStep to the After hooks.
type BeforeStep func(context.Context, Steper) (context.Context, error)

// AfterStep is the signature for a hook that runs after a step's Do, on every
// retry attempt. It receives — and can transform or swallow — the error
// returned by Do (or by a previous AfterStep). Unlike BeforeStep, AfterStep
// callbacks always all run; an error doesn't short-circuit them.
type AfterStep func(context.Context, Steper, error) error

// StepConfig collects everything Workflow needs to know about a single step
// besides the step itself: who it depends on, what hooks to run around it,
// and how to configure retry/timeout/condition.
type StepConfig struct {
	Upstreams Set[Steper]         // steps that must be terminated before this step is considered.
	Before    []BeforeStep        // hooks run before Do, in declaration order, per attempt.
	After     []AfterStep         // hooks run after  Do, in declaration order, per attempt.
	Option    []func(*StepOption) // option mutators folded together to compute the effective StepOption.
}

// StepOption is the resolved per-step configuration the scheduler consults at
// runtime. Built by folding StepConfig.Option in declaration order — later
// mutators win for fields they touch (so Timeout/When/Retry follow
// "last-one-wins").
type StepOption struct {
	RetryOption *RetryOption   // nil means: no retry, run once.
	Condition   Condition      // nil means: use the package-level DefaultCondition (AllSucceeded).
	Timeout     *time.Duration // nil means: no step-level deadline (the step runs until ctx is done).
}

// Steps registers one or more independent Steps to be added into the Workflow.
//
// Steps in the same Steps(...) call are NOT linked to each other: by default
// they may run concurrently. Use DependsOn / When / Retry / Timeout etc. on
// the returned AddSteps to attach common configuration.
//
//	workflow.Add(
//	    Steps(a, b, c),                 // a, b and c are independent (run in parallel).
//	    Steps(a, b, c).DependsOn(d, e), // d and e first; then a, b, c become eligible.
//	)
//
// Use Step (singular, generic) instead when you need typed Input/Output hooks.
func Steps(steps ...Steper) AddSteps {
	rv := make(AddSteps)
	for _, step := range steps {
		rv[step] = &StepConfig{Upstreams: make(Set[Steper])}
	}
	return rv
}

// Step is the typed counterpart of Steps. Because it is generic over the
// concrete step type S, it can offer Input / Output callbacks that receive
// the step as its real type — no type assertion needed.
//
//	Step(a).Input(func(ctx context.Context, a *A) error {
//	    a.Field = "filled at runtime"
//	    return nil
//	})
//
// All Steps(...) helpers (DependsOn, When, Retry, …) are also available on
// the returned AddStep[S].
func Step[S Steper](steps ...S) AddStep[S] {
	return AddStep[S]{
		AddSteps: Steps(ToSteps(steps)...),
		Steps:    steps,
	}
}

// Pipe wires the given Steps into a strict linear pipeline.
//
//	workflow.Add(
//	    Pipe(a, b, c), // a -> b -> c
//	)
//
// Equivalent to:
//
//	workflow.Add(
//	    Step(b).DependsOn(a),
//	    Step(c).DependsOn(b),
//	)
func Pipe(steps ...Steper) AddSteps {
	as := Steps(steps...)
	for i := 0; i < len(steps)-1; i++ {
		as.Merge(Steps(steps[i+1]).DependsOn(steps[i]))
	}
	return as
}

// BatchPipe wires batches of Steps into a "fully-connected" pipeline: every
// step in batch i+1 depends on every step in batch i. Steps within the same
// batch remain independent of each other.
//
//	workflow.Add(
//	    BatchPipe(
//	        Steps(a, b),
//	        Steps(c, d, e),
//	        Steps(f),
//	    ),
//	)
//
// Equivalent to:
//
//	workflow.Add(
//	    Steps(c, d, e).DependsOn(a, b),
//	    Steps(f).DependsOn(c, d, e),
//	)
func BatchPipe(batch ...AddSteps) AddSteps {
	as := Steps()
	for _, other := range batch {
		as.Merge(other)
	}
	for i := 0; i < len(batch)-1; i++ {
		as.Merge(Steps(Keys(batch[i+1])...).DependsOn(Keys(batch[i])...))
	}
	return as
}

// DependsOn declares that the configured step(s) must run AFTER all of the
// given upstream steps have terminated.
//
//	Step(a).DependsOn(b, c) // b and c happen-before a.
//
// Calling DependsOn multiple times is additive (the upstream sets are unioned).
func (as AddSteps) DependsOn(ups ...Steper) AddSteps {
	for down := range as {
		as[down].Upstreams.Add(ups...)
	}
	return as
}

// Input registers a typed BeforeStep callback. It runs at runtime, BEFORE Do,
// on EVERY retry attempt. Use it to populate fields on the step from data
// that's only available once upstreams have finished.
//
// Input callbacks fire in declaration order — both within a single Input(...)
// call and across multiple Input(...) calls on the same step:
//
//	Step(a).
//	    Input(/* 1. fires first  */).
//	    Input(/* 2. fires second */)
//	Step(a).Input(/* 3. fires last */)
//
// Returning a non-nil error short-circuits the remaining Before chain and is
// surfaced as ErrBeforeStep.
func (as AddStep[S]) Input(fns ...func(context.Context, S) error) AddStep[S] {
	for _, step := range as.Steps {
		step := step // capture range variable
		for _, fn := range fns {
			if fn != nil {
				fn := fn // capture range variable
				as.AddSteps[step].Before = append(as.AddSteps[step].Before, func(ctx context.Context, _ Steper) (context.Context, error) {
					return ctx, fn(ctx, step)
				})
			}
		}
	}
	return as
}

// Output registers a typed AfterStep callback. It is the symmetric companion
// of Input: it runs at runtime, AFTER Do, on every retry attempt — but ONLY
// when Do (and the prior After chain) returned nil. Use it to extract fields
// off a successful step into outer scope.
//
// If you need to observe failures too, use AfterStep instead.
func (as AddStep[S]) Output(fns ...func(context.Context, S) error) AddStep[S] {
	for _, step := range as.Steps {
		step := step // capture range variable
		for _, fn := range fns {
			if fn != nil {
				fn := fn // capture range variable
				as.AddSteps[step].After = append(as.AddSteps[step].After, func(ctx context.Context, _ Steper, err error) error {
					if err == nil {
						return fn(ctx, step)
					}
					return err
				})
			}
		}
	}
	return as
}

// BeforeStep registers an untyped BeforeStep callback.
//
// BeforeStep callbacks run before Do, in declaration order, on every retry
// attempt. They may swap the context.Context that flows into Do (and into
// subsequent BeforeStep callbacks). The first non-nil error short-circuits
// the chain.
func (as AddSteps) BeforeStep(befores ...BeforeStep) AddSteps {
	for step := range as {
		as[step].Before = append(as[step].Before, befores...)
	}
	return as
}

// AfterStep registers an untyped AfterStep callback.
//
// AfterStep callbacks run after Do, in declaration order, on every retry
// attempt. The error from Do (or from a previous AfterStep) is threaded
// through, so each callback can observe, transform or swallow it. ALL
// callbacks always run — an error never short-circuits the After chain.
//
// Tip: when you only care about the success path, remember to forward errors:
//
//	Steps(a).AfterStep(func(ctx context.Context, step Steper, err error) error {
//	    if err != nil {
//	        // handle / log the failure
//	        return err          // typically: forward unchanged
//	    }
//	    // success-only post-processing
//	    return nil
//	})
func (as AddSteps) AfterStep(afters ...AfterStep) AddSteps {
	for step := range as {
		as[step].After = append(as[step].After, afters...)
	}
	return as
}

// Timeout sets a step-level deadline that bounds the entire step lifetime
// (all retry attempts together). Last call wins.
//
// For a per-attempt deadline, set RetryOption.TimeoutPerTry inside Retry().
func (as AddSteps) Timeout(timeout time.Duration) AddSteps {
	for step := range as {
		as[step].Option = append(as[step].Option, func(so *StepOption) {
			so.Timeout = &timeout
		})
	}
	return as
}

// When sets the Condition that decides whether the step actually runs. Last
// call wins.
//
// Tip: when composing with built-in conditions, return early so you don't
// accidentally promote a Skipped/Canceled decision to Running:
//
//	Steps(a).When(func(ctx context.Context, ups map[Steper]StepResult) StepStatus {
//	    if status := flow.AllSucceeded(ctx, ups); status != flow.Running {
//	        return status // upstreams aren't all green — bail with their decision
//	    }
//	    if myExtraCheck() {
//	        return flow.Running
//	    }
//	    return flow.Skipped
//	})
func (as AddSteps) When(cond Condition) AddSteps {
	for step := range as {
		as[step].Option = append(as[step].Option, func(so *StepOption) {
			so.Condition = cond
		})
	}
	return as
}

// Retry configures retry behavior for the step. The mutator(s) are applied to
// a fresh RetryOption seeded from DefaultRetryOption (so calling Retry with
// no mutator — e.g. Retry(nil) — opts in to the default retry policy).
//
// Note: the field is named Attempts (total attempts including the first try),
// not MaxAttempts.
//
//	w.Add(
//	    Step(a),                                      // no retry, run once.
//	    Step(b).Retry(func(opt *RetryOption) {        // up to 3 attempts.
//	        opt.Attempts = 3
//	    }),
//	    Step(c).Retry(nil),                           // use DefaultRetryOption as-is.
//	)
func (as AddSteps) Retry(opts ...func(*RetryOption)) AddSteps {
	for step := range as {
		as[step].Option = append(as[step].Option, func(so *StepOption) {
			if so.RetryOption == nil {
				so.RetryOption = new(RetryOption)
				*so.RetryOption = DefaultRetryOption
				// Deep-copy: Backoff holds a pointer to mutable state.
				// A shallow copy shares the same instance across all steps that
				// call Retry(nil), causing a data race when they retry concurrently.
				// Set to nil so retry() falls back to a freshly allocated instance.
				so.RetryOption.Backoff = nil
			}
			for _, opt := range opts {
				if opt != nil {
					opt(so.RetryOption)
				}
			}
		})
	}
	return as
}

// AddToWorkflow makes AddSteps satisfy Builder so it can be passed to
// Workflow.Add directly.
func (as AddSteps) AddToWorkflow() map[Steper]*StepConfig { return as }

// Merge folds other AddSteps maps into this one, unioning per-step
// configuration (upstreams unioned, callbacks/options concatenated).
func (as AddSteps) Merge(others ...AddSteps) AddSteps {
	for _, other := range others {
		for k, v := range other {
			if as[k] == nil {
				as[k] = new(StepConfig)
			}
			as[k].Merge(v)
		}
	}
	return as
}

// DependsOn — typed shim that forwards to the AddSteps method so chaining on
// Step() returns the typed AddStep[S] (preserving Input/Output access).
func (as AddStep[S]) DependsOn(ups ...Steper) AddStep[S] {
	as.AddSteps = as.AddSteps.DependsOn(ups...)
	return as
}

// BeforeStep — typed shim; see AddSteps.BeforeStep.
func (as AddStep[S]) BeforeStep(befores ...BeforeStep) AddStep[S] {
	as.AddSteps = as.AddSteps.BeforeStep(befores...)
	return as
}

// AfterStep — typed shim; see AddSteps.AfterStep.
func (as AddStep[S]) AfterStep(afters ...AfterStep) AddStep[S] {
	as.AddSteps = as.AddSteps.AfterStep(afters...)
	return as
}

// Timeout — typed shim; see AddSteps.Timeout.
func (as AddStep[S]) Timeout(timeout time.Duration) AddStep[S] {
	as.AddSteps = as.AddSteps.Timeout(timeout)
	return as
}

// When — typed shim; see AddSteps.When.
func (as AddStep[S]) When(when Condition) AddStep[S] {
	as.AddSteps = as.AddSteps.When(when)
	return as
}

// Retry — typed shim; see AddSteps.Retry.
func (as AddStep[S]) Retry(fns ...func(*RetryOption)) AddStep[S] {
	as.AddSteps = as.AddSteps.Retry(fns...)
	return as
}

// AddStep is the typed view returned by Step[S]. It embeds AddSteps so every
// untyped helper (DependsOn, When, Retry, …) is available, and adds the
// typed Input/Output helpers on top.
type AddStep[S Steper] struct {
	AddSteps
	Steps []S
}

// AddSteps is the {step → config} map produced by Steps(...) / Pipe(...) /
// BatchPipe(...). It satisfies Builder via AddToWorkflow.
type AddSteps map[Steper]*StepConfig

// ToSteps widens a typed slice []S to []Steper. Useful when you want to add
// a homogeneous slice of steps without an explicit per-element conversion.
//
//	steps := []*MyStep{ ... }
//	flow.Add(
//	    Steps(ToSteps(steps)...),
//	)
func ToSteps[S Steper](steps []S) []Steper {
	rv := []Steper{}
	for _, s := range steps {
		rv = append(rv, s)
	}
	return rv
}

// Merge folds another StepConfig into this one in place: upstream sets are
// unioned, callbacks and options are concatenated. A nil other is a no-op.
func (sc *StepConfig) Merge(other *StepConfig) {
	if other == nil {
		return
	}
	if sc.Upstreams == nil {
		sc.Upstreams = make(Set[Steper])
	}
	sc.Upstreams.Union(other.Upstreams)
	sc.Before = append(sc.Before, other.Before...)
	sc.After = append(sc.After, other.After...)
	sc.Option = append(sc.Option, other.Option...)
}

// Set is a tiny generic set built on map. Used internally for upstream sets,
// the BuildStep memo, etc.
type Set[T comparable] map[T]struct{}

// Has reports whether v is in the set. Nil-safe.
func (s Set[T]) Has(v T) bool {
	if s == nil {
		return false
	}
	_, ok := s[v]
	return ok
}

// Add inserts the given values into the set, lazily allocating the backing
// map if needed.
func (s *Set[T]) Add(vs ...T) {
	if *s == nil {
		*s = make(Set[T])
	}
	for _, v := range vs {
		(*s)[v] = struct{}{}
	}
}

// Union folds all elements from the given sets into this one.
func (s *Set[T]) Union(sets ...Set[T]) {
	for _, set := range sets {
		s.Add(set.Flatten()...)
	}
}

// Flatten returns the set's elements as a slice in unspecified order.
func (s Set[T]) Flatten() []T {
	r := make([]T, 0, len(s))
	for v := range s {
		r = append(r, v)
	}
	return r
}

// Seq returns an iter.Seq over the set's elements. Order is unspecified.
func (s Set[T]) Seq() iter.Seq[T] {
	return func(yield func(T) bool) {
		for v := range s {
			if !yield(v) {
				return
			}
		}
	}
}

// Keys returns the keys of m in unspecified order.
//
// Deprecated: prefer slices.Collect(maps.Keys(m)) from the standard library.
func Keys[M ~map[K]V, K comparable, V any](m M) []K {
	return slices.Collect(maps.Keys(m))
}

// Values returns the values of m in unspecified order.
//
// Deprecated: prefer slices.Collect(maps.Values(m)) from the standard library.
func Values[M ~map[K]V, K comparable, V any](m M) []V {
	return slices.Collect(maps.Values(m))
}
