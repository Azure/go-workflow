package flow

import (
	"context"
	"time"
)

// implement this interface to be added into Workflow
type WorkflowStep interface {
	Dependencies() Dependency
	States() map[Steper][]func(*StepState)
	Inputs() map[Steper][]func(context.Context) error
}

// Steps declares a series of Steps.
// The Steps are mutually independent, and will be executed in parallel.
//
//	Steps(a, b, c) // a, b, c will be executed in parallel
//	Steps(a, b, c).DependsOn(d, e) // d, e will be executed in parallel, then a, b, c in parallel
//
// Steps are weak-typed, use Step if you need add Input or InputDependsOn
func Steps(steps ...Steper) AddSteps {
	dep := make(Dependency)
	state := make(map[Steper][]func(*StepState))
	input := make(map[Steper][]func(context.Context) error)
	for _, step := range steps {
		dep[step] = make(Set[Steper])
		state[step] = nil
		input[step] = nil
	}
	return AddSteps{
		Steps:      steps,
		Dependency: dep,
		State:      state,
		Input:      input,
	}
}

// Step declares Steps ready for building dependencies to Workflow,
// with the support of Input(...) and InputDependsOn(...).
func Step[S Steper](steps ...S) AddStep[S] {
	return AddStep[S]{
		AddSteps: Steps(ToSteps(steps)...),
		Steps:    steps,
	}
}

type AddStep[S Steper] struct {
	AddSteps
	Steps []S
}

type InputFunc[S any] func(context.Context, S) error

// Input adds Input func for the Step(s).
// If the Input function returns error, Downstream Step will return a ErrFlow.
// Input respects the order in building calls, because it's actually a empty Upstream.
//
//	Step(down).
//		Input(/* this Input will be feeded first */).
//		InputDependsOn(Adapt(up, /* then receive Output from up */)).
//		Input(/* this Input is after up's Output */),
func (as AddStep[S]) Input(fns ...InputFunc[S]) AddStep[S] {
	for _, step := range as.Steps {
		step := step // capture range variable
		as.AddSteps.Input[step] = append(as.AddSteps.Input[step], func(ctx context.Context) error {
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

// InputDependsOn declares dependency between Steps,
// with sending Upstream's Output to Downstream's Input.
//
// Use Adapt function to convert the Upstream to Downstream
//
//	Step(down).InputDependsOn(
//		Adapt(up, func(_ context.Context, u *Up, d *Down) error {
//			// fill Down from Up
//		}),
//	)
func (as AddStep[S]) InputDependsOn(adapts ...Adapter[S]) AddStep[S] {
	for _, step := range as.Steps {
		step := step
		for _, adapt := range adapts {
			adapt := adapt
			as.AddSteps.Dependency[step][adapt.Upstream] = struct{}{}
			as.AddSteps.Input[step] = append(as.AddSteps.Input[step], func(ctx context.Context) error {
				return adapt.Flow(ctx, step)
			})
		}
	}
	return as
}

type Adapter[S Steper] struct {
	Upstream Steper
	Flow     func(context.Context, S) error
}

type AdaptFunc[U, D Steper] func(context.Context, U, D) error

// Adapt bridges Upstream and Downstream with defining how to adapt different steps.
func Adapt[U, D Steper](up U, fn AdaptFunc[U, D]) Adapter[D] {
	return Adapter[D]{
		Upstream: up,
		Flow: func(ctx context.Context, d D) error {
			return fn(ctx, up, d)
		},
	}
}

type AddSteps struct {
	Steps      []Steper
	Dependency Dependency
	State      map[Steper][]func(*StepState)
	Input      map[Steper][]func(context.Context) error
}

// DependsOn declares dependency between Steps.
//
// "Upstreams happen before Downstream" is guaranteed.
// Upstream's Output will not be sent to Downstream's Input.
func (as AddSteps) DependsOn(ups ...Steper) AddSteps {
	for _, down := range as.Steps {
		for _, up := range ups {
			as.Dependency[down][up] = struct{}{}
		}
	}
	return as
}

// Timeout sets the Step level timeout.
func (as AddSteps) Timeout(timeout time.Duration) AddSteps {
	for _, step := range as.Steps {
		as.State[step] = append(as.State[step], func(s *StepState) {
			s.Timeout = timeout
		})
	}
	return as
}

// When decides whether the Step should be Skipped.
func (as AddSteps) When(when When) AddSteps {
	for _, step := range as.Steps {
		as.State[step] = append(as.State[step], func(s *StepState) {
			s.When = when
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
func (as AddSteps) Retry(opts ...func(*RetryOption)) AddSteps {
	for _, step := range as.Steps {
		as.State[step] = append(as.State[step], func(s *StepState) {
			s.RetryOption = appendRetry(s.RetryOption, opts...)
		})
	}
	return as
}

func (as AddSteps) Inputs() map[Steper][]func(context.Context) error { return as.Input }
func (as AddSteps) Dependencies() Dependency                         { return as.Dependency }
func (as AddSteps) States() map[Steper][]func(*StepState)            { return as.State }

func (as AddStep[S]) Timeout(timeout time.Duration) AddStep[S] {
	as.AddSteps = as.AddSteps.Timeout(timeout)
	return as
}
func (as AddStep[S]) When(when When) AddStep[S] {
	as.AddSteps = as.AddSteps.When(when)
	return as
}
func (as AddStep[S]) Retry(fns ...func(*RetryOption)) AddStep[S] {
	as.AddSteps = as.AddSteps.Retry(fns...)
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
