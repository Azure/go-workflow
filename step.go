package flow

import (
	"context"
	"time"
)

// Steper is basic unit of a Workflow.
//
// Implement this interface to be added into Workflow via Step() or Steps().
//
// Please do not expect the type of Steper, use Is() or As() if you need to check / get the typed step from the step tree.
type Steper interface {
	Do(context.Context) error
}

// implement this interface to be added into Workflow!
type WorkflowAdder interface {
	Done() map[Steper]*StepConfig
}
type StepConfig struct {
	Upstreams Set[Steper]
	Input     func(context.Context) error
	Option    func(*StepOption)
}

// StepOption saves the option for a Step in Workflow,
// including its timeout, retry options, etc.
type StepOption struct {
	RetryOption *RetryOption
	When        When
	Timeout     time.Duration
}

// Steps declares a series of Steps.
// The Steps are mutually independent, and will be executed in parallel.
//
//	Steps(a, b, c) // a, b, c will be executed in parallel
//	Steps(a, b, c).DependsOn(d, e) // d, e will be executed in parallel, then a, b, c in parallel
//
// Steps are weak-typed, use Step if you need add Input or InputDependsOn
func Steps(steps ...Steper) addSteps {
	rv := make(addSteps)
	for _, step := range steps {
		rv[step] = &StepConfig{Upstreams: make(Set[Steper])}
	}
	return rv
}

// Step declares Steps ready for building dependencies to Workflow,
// with the support of Input(...) and InputDependsOn(...).
func Step[S Steper](steps ...S) addStep[S] {
	return addStep[S]{
		addSteps: Steps(ToSteps(steps)...),
		Steps:    steps,
	}
}

// Pipe creates a pipeline in Workflow, the order would be steps[0] -> steps[1] -> steps[2] -> ...
func Pipe(steps ...Steper) addSteps {
	if len(steps) == 0 {
		return Steps()
	}
	as := Steps(steps[0])
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

type addStep[S Steper] struct {
	addSteps
	Steps []S
}

// Input adds Input func for the Step(s).
// If the Input function returns error, will return an ErrInput.
// Input respects the order in building calls.
//
//	Step(down).
//		Input(/* this Input will be feeded first */).
//		InputDependsOn(Adapt(up, /* then receive Output from up */)).
//		Input(/* this Input is after up's Output */),
func (as addStep[S]) Input(fns ...func(context.Context, S) error) addStep[S] {
	for _, step := range as.Steps {
		step := step // capture range variable
		as.addSteps[step].AddInput(func(ctx context.Context) error {
			for _, fn := range fns {
				if err := fn(ctx, step); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return as
}

// InputDependsOn declares dependency between Steps, with flowing data from Upstream to Downstream.
//
// Use Adapt function to flow the data from Upstream to Downstream:
//
//	Step(down).InputDependsOn(
//		Adapt(up, func(_ context.Context, u *Up, d *Down) error {
//			// fill Down from Up
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

type Adapter[S Steper] struct {
	Upstream Steper
	Flow     func(context.Context, S) error // Flow will flow data from Upstream to Downstream
}

// Adapt bridges Upstream and Downstream with defining how to flow data.
func Adapt[U, D Steper](up U, fn func(context.Context, U, D) error) Adapter[D] {
	return Adapter[D]{
		Upstream: up,
		Flow: func(ctx context.Context, d D) error {
			return fn(ctx, up, d)
		},
	}
}

type addSteps map[Steper]*StepConfig

// DependsOn declares dependency between Steps.
//
// "Upstreams happen before Downstream" is guaranteed.
func (as addSteps) DependsOn(ups ...Steper) addSteps {
	for down := range as {
		as[down].Upstreams.Add(ups...)
	}
	return as
}

// Timeout sets the Step level timeout.
func (as addSteps) Timeout(timeout time.Duration) addSteps {
	for step := range as {
		as[step].AddOption(func(so *StepOption) {
			so.Timeout = timeout
		})
	}
	return as
}

// When decides whether the Step should be Skipped.
func (as addSteps) When(when When) addSteps {
	for step := range as {
		as[step].AddOption(func(so *StepOption) {
			so.When = when
		})
	}
	return as
}

func appendRetry(opt *RetryOption, fns ...func(*RetryOption)) *RetryOption {
	if opt == nil {
		opt = new(RetryOption)
		*opt = DefaultRetryOption
	}
	for _, fn := range fns {
		fn(opt)
	}
	return opt
}

// Retry sets the RetryOption for the Step.
func (as addSteps) Retry(opts ...func(*RetryOption)) addSteps {
	for step := range as {
		as[step].AddOption(func(so *StepOption) {
			so.RetryOption = appendRetry(so.RetryOption, opts...)
		})
	}
	return as
}

func (as addSteps) Done() map[Steper]*StepConfig { return as }

func (as addStep[S]) Timeout(timeout time.Duration) addStep[S] {
	as.addSteps = as.addSteps.Timeout(timeout)
	return as
}
func (as addStep[S]) When(when When) addStep[S] {
	as.addSteps = as.addSteps.When(when)
	return as
}
func (as addStep[S]) Retry(fns ...func(*RetryOption)) addStep[S] {
	as.addSteps = as.addSteps.Retry(fns...)
	return as
}

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
