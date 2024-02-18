package flow

import (
	"context"
	"time"
)

// Steper describes the requirement for a Step, which is basic unit of a Workflow.
//
// Implement this interface to allow the power of Workflow to orchestrate your Steps.
// Notice your implementation should be a pointer type.
type Steper interface {
	Do(context.Context) error
}

// Implement this interface to be added into Workflow!
type WorkflowAdder interface {
	Done() map[Steper]*StepConfig
}

// BeforeStep defines callback being called BEFORE step being executed.
type BeforeStep func(context.Context, Steper) (context.Context, error)

// AfterStep defines callback being called AFTER step being executed.
type AfterStep func(context.Context, Steper, error) error

type StepConfig struct {
	Upstreams Set[Steper]       // Upstreams of the Step, means these Steps should happen-before this Step
	Before    BeforeStep        // Before callbacks of the Step, will be called before Do
	After     AfterStep         // After callbacks of the Step, will be called before Do
	Option    func(*StepOption) // Option customize the Step settings
}
type StepOption struct {
	RetryOption *RetryOption   // RetryOption customize how the Step should be retried, default (nil) means no retry.
	Condition   Condition      // Condition decides whether Workflow should execute the Step, default to DefaultCondition.
	Timeout     *time.Duration // Timeout sets the Step level timeout, default (nil) means no timeout.
}

// Steps declares a series of Steps ready to be added into Workflow.
//
// The Steps declared are mutually independent.
//
//	workflow.Add(
//		Steps(a, b, c),					// a, b, c will be executed in parallel
//		Steps(a, b, c).DependsOn(d, e), // d, e will be executed in parallel, then a, b, c in parallel
//	)
func Steps(steps ...Steper) AddSteps {
	rv := make(AddSteps)
	for _, step := range steps {
		rv[step] = &StepConfig{Upstreams: make(Set[Steper])}
	}
	return rv
}

// Step declares Step ready to be added into Workflow.
//
// The main difference between Step() and Steps() is that,
// Step() allows to add Input and InputDependsOn for the Step.
//
//	Step(a).Input(func(ctx context.Context, a *A) error {
//		// fill a
//	}))
func Step[S Steper](steps ...S) AddStep[S] {
	return AddStep[S]{
		AddSteps: Steps(ToSteps(steps)...),
		Steps:    steps,
	}
}

// Pipe creates a pipeline in Workflow.
//
//	workflow.Add(
//		Pipe(a, b, c), // a -> b -> c
//	)
//
// The above code is equivalent to:
//
//	workflow.Add(
//		Step(b).DependsOn(a),
//		Step(c).DependsOn(b),
//	)
func Pipe(steps ...Steper) AddSteps {
	as := Steps()
	for i := 0; i < len(steps)-1; i++ {
		for k, v := range Steps(steps[i+1]).DependsOn(steps[i]) {
			if as[k] == nil {
				as[k] = &StepConfig{Upstreams: make(Set[Steper])}
			}
			as[k].Merge(v)
		}
	}
	return as
}

// DependsOn declares dependency on the given Steps.
//
//	Step(a).DependsOn(b, c)
//
// Then b, c should happen-before a.
func (as AddSteps) DependsOn(ups ...Steper) AddSteps {
	for down := range as {
		as[down].Upstreams.Add(ups...)
	}
	return as
}

// Input adds Before callback for the Step(s).
//
// Input callbacks will be called before Do,
// and the order will respect the order of declarations.
//
//	Step(a).
//		Input(/* 1. this Input will be called first */).
//		InputDependsOn(Adapt(up, /* 2. then receive Output from up */)).
//		Input(/* 3. this Input is after up's Output */)
//	Step(a).Input(/* 4. this Input is after all */)
func (as AddStep[S]) Input(fns ...func(context.Context, S) error) AddStep[S] {
	for _, step := range as.Steps {
		step := step // capture range variable
		as.AddSteps[step].AddBefore(func(ctx context.Context, _ Steper) (context.Context, error) {
			for _, fn := range fns {
				if fn != nil {
					if err := fn(ctx, step); err != nil {
						return ctx, err
					}
				}
			}
			return ctx, nil
		})
	}
	return as
}

// InputDependsOn declares dependency between Steps, and with feeding data from Upstream to Downstream.
//
// It's useful when the Downstream needs some data from Upstream, and the data is not available until Upstream is done.
// The Input callback will ignore the Upstream's result as long as it's terminated.
//
// Due to limitation of Go's generic type system,
// Use Adapt function to workaround the type check.
//
//	Step(down).InputDependsOn(
//		Adapt(up, func(_ context.Context, u *Up, d *Down) error {
//			// fill Down from Up
//			// here Up is terminated, and Down has not started yet
//		}),
//	)
//
// Deprecated: Use Input with DependsOn instead.
func (as AddStep[S]) InputDependsOn(adapts ...Adapter[S]) AddStep[S] {
	for _, step := range as.Steps {
		step := step
		for _, adapt := range adapts {
			adapt := adapt
			as.AddSteps[step].Upstreams.Add(adapt.Upstream)
			as.AddSteps[step].AddBefore(func(ctx context.Context, _ Steper) (context.Context, error) {
				return ctx, adapt.Flow(ctx, step)
			})
		}
	}
	return as
}

// Adapt bridges Upstream and Downstream with defining how to flow data.
//
// Use it with InputDependsOn.
//
//	Step(down).InputDependsOn(
//		Adapt(up, func(_ context.Context, u *Up, d *Down) error {
//			// fill Down from Up
//			// here Up is terminated, and Down has not started yet
//		}),
//	)
func Adapt[U, D Steper](up U, fn func(context.Context, U, D) error) Adapter[D] {
	return Adapter[D]{
		Upstream: up,
		Flow: func(ctx context.Context, d D) error {
			return fn(ctx, up, d)
		},
	}
}

func (as AddSteps) Before(befores ...BeforeStep) AddSteps {
	for step := range as {
		for _, before := range befores {
			as[step].AddBefore(before)
		}
	}
	return as
}
func (as AddSteps) After(afters ...AfterStep) AddSteps {
	for step := range as {
		for _, after := range afters {
			as[step].AddAfter(after)
		}
	}
	return as
}

// Timeout sets the Step level timeout.
func (as AddSteps) Timeout(timeout time.Duration) AddSteps {
	for step := range as {
		as[step].AddOption(func(so *StepOption) {
			so.Timeout = &timeout
		})
	}
	return as
}

// When set the Condition for the Step.
func (as AddSteps) When(cond Condition) AddSteps {
	for step := range as {
		as[step].AddOption(func(so *StepOption) {
			so.Condition = cond
		})
	}
	return as
}

// Retry customize how the Step should be retried.
//
// If it's never called, the Step will not be retried.
// The RetryOption has a DefaultRetryOption as base to be modified.
func (as AddSteps) Retry(opts ...func(*RetryOption)) AddSteps {
	for step := range as {
		as[step].AddOption(func(so *StepOption) {
			if so.RetryOption == nil {
				so.RetryOption = new(RetryOption)
				*so.RetryOption = DefaultRetryOption
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

func (as AddSteps) Done() map[Steper]*StepConfig { return as } // WorkflowAdder

func (as AddStep[S]) DependsOn(ups ...Steper) AddStep[S] {
	as.AddSteps = as.AddSteps.DependsOn(ups...)
	return as
}
func (as AddStep[S]) Before(befores ...BeforeStep) AddStep[S] {
	as.AddSteps = as.AddSteps.Before(befores...)
	return as
}
func (as AddStep[S]) After(afters ...AfterStep) AddStep[S] {
	as.AddSteps = as.AddSteps.After(afters...)
	return as
}
func (as AddStep[S]) Timeout(timeout time.Duration) AddStep[S] {
	as.AddSteps = as.AddSteps.Timeout(timeout)
	return as
}
func (as AddStep[S]) When(when Condition) AddStep[S] {
	as.AddSteps = as.AddSteps.When(when)
	return as
}
func (as AddStep[S]) Retry(fns ...func(*RetryOption)) AddStep[S] {
	as.AddSteps = as.AddSteps.Retry(fns...)
	return as
}

type Adapter[S Steper] struct {
	Upstream Steper
	Flow     func(context.Context, S) error
}
type AddStep[S Steper] struct {
	AddSteps
	Steps []S
}
type AddSteps map[Steper]*StepConfig

// ToSteps converts []<StepDoer implemention> to []StepDoer.
//
//	steps := []someStepImpl{ ... }
//	flow.Add(
//		Steps(ToSteps(steps)...),
//	)
func ToSteps[S Steper](steps []S) []Steper {
	rv := []Steper{}
	for _, s := range steps {
		rv = append(rv, s)
	}
	return rv
}

func (sc *StepConfig) AddOption(opt func(*StepOption)) {
	switch {
	case opt == nil:
		return
	case sc.Option == nil:
		sc.Option = opt
	default:
		old := sc.Option
		sc.Option = func(so *StepOption) {
			old(so)
			opt(so)
		}
	}
}
func (sc *StepConfig) AddBefore(before BeforeStep) {
	switch {
	case before == nil:
		return
	case sc.Before == nil:
		sc.Before = before
	default:
		old := sc.Before
		sc.Before = func(ctx context.Context, s Steper) (context.Context, error) {
			if newCtx, err := old(ctx, s); err != nil {
				return newCtx, err
			} else {
				return before(newCtx, s)
			}
		}
	}
}
func (sc *StepConfig) AddAfter(after AfterStep) {
	switch {
	case after == nil:
		return
	case sc.After == nil:
		sc.After = after
	default:
		old := sc.After
		sc.After = func(ctx context.Context, s Steper, err error) error {
			return after(ctx, s, old(ctx, s, err))
		}
	}
}
func (sc *StepConfig) Merge(other *StepConfig) {
	if other == nil {
		return
	}
	if sc.Upstreams == nil {
		sc.Upstreams = make(Set[Steper])
	}
	sc.Upstreams.Union(other.Upstreams)
	sc.AddBefore(other.Before)
	sc.AddAfter(other.After)
	sc.AddOption(other.Option)
}

type Set[T comparable] map[T]struct{}

func (s Set[T]) Has(v T) bool {
	_, ok := s[v]
	return ok
}
func (s Set[T]) Add(vs ...T) {
	for _, v := range vs {
		s[v] = struct{}{}
	}
}
func (s Set[T]) Union(sets ...Set[T]) {
	for _, set := range sets {
		for v := range set {
			s[v] = struct{}{}
		}
	}
}
