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
type StepConfig struct {
	Upstreams Set[Steper]                 // Upstreams of the Step, means these Steps should happen-before this Step
	Input     func(context.Context) error // Input callback of the Step, will be called before Do
	Option    func(*StepOption)           // Option customize the Step settings
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
func Steps(steps ...Steper) addSteps {
	rv := make(addSteps)
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
func Step[S Steper](steps ...S) addStep[S] {
	return addStep[S]{
		addSteps: Steps(ToSteps(steps)...),
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
func Pipe(steps ...Steper) addSteps {
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
func (as addSteps) DependsOn(ups ...Steper) addSteps {
	for down := range as {
		as[down].Upstreams.Add(ups...)
	}
	return as
}

// Input adds Input callback for the Step(s).
//
// Input callback will be called before Do,
// and the order will respect the order of declarations.
//
//	Step(a).
//		Input(/* 1. this Input will be called first */).
//		InputDependsOn(Adapt(up, /* 2. then receive Output from up */)).
//		Input(/* 3. this Input is after up's Output */)
//	Step(a).Input(/* 4. this Input is after all */)
func (as addStep[S]) Input(fns ...func(context.Context, S) error) addStep[S] {
	for _, step := range as.Steps {
		step := step // capture range variable
		as.addSteps[step].AddInput(func(ctx context.Context) error {
			for _, fn := range fns {
				if fn != nil {
					if err := fn(ctx, step); err != nil {
						return err
					}
				}
			}
			return nil
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
func (as addStep[S]) InputDependsOn(adapts ...Adapter[S]) addStep[S] {
	for _, step := range as.Steps {
		step := step
		for _, adapt := range adapts {
			adapt := adapt
			as.addSteps[step].Upstreams.Add(adapt.Upstream)
			as.addSteps[step].AddInput(func(ctx context.Context) error {
				return adapt.Flow(ctx, step)
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

// Timeout sets the Step level timeout.
func (as addSteps) Timeout(timeout time.Duration) addSteps {
	for step := range as {
		as[step].AddOption(func(so *StepOption) {
			so.Timeout = &timeout
		})
	}
	return as
}

// When set the Condition for the Step.
func (as addSteps) When(cond Condition) addSteps {
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
func (as addSteps) Retry(opts ...func(*RetryOption)) addSteps {
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

func (as addSteps) Done() map[Steper]*StepConfig { return as } // WorkflowAdder

func (as addStep[S]) DependsOn(ups ...Steper) addStep[S] {
	as.addSteps = as.addSteps.DependsOn(ups...)
	return as
}
func (as addStep[S]) Timeout(timeout time.Duration) addStep[S] {
	as.addSteps = as.addSteps.Timeout(timeout)
	return as
}
func (as addStep[S]) When(when Condition) addStep[S] {
	as.addSteps = as.addSteps.When(when)
	return as
}
func (as addStep[S]) Retry(fns ...func(*RetryOption)) addStep[S] {
	as.addSteps = as.addSteps.Retry(fns...)
	return as
}

type Adapter[S Steper] struct {
	Upstream Steper
	Flow     func(context.Context, S) error
}
type addStep[S Steper] struct {
	addSteps
	Steps []S
}
type addSteps map[Steper]*StepConfig

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
func (sc *StepConfig) AddInput(input func(context.Context) error) {
	switch {
	case input == nil:
		return
	case sc.Input == nil:
		sc.Input = input
	default:
		old := sc.Input
		sc.Input = func(ctx context.Context) error {
			if err := old(ctx); err != nil {
				return err
			}
			return input(ctx)
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
	sc.AddInput(other.Input)
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
