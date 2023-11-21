package flow

import (
	"context"
	"time"
)

// implement this interface to be added into Workflow!
type WorkflowAdd interface {
	Done() map[Steper][]WorkflowAddOption
}
type WorkflowAddOption struct {
	Upstream Steper
	Input    func(context.Context) error
	Option   func(*StepOption)
}

// Steps declares a series of Steps.
// The Steps are mutually independent, and will be executed in parallel.
//
//	Steps(a, b, c) // a, b, c will be executed in parallel
//	Steps(a, b, c).DependsOn(d, e) // d, e will be executed in parallel, then a, b, c in parallel
//
// Steps are weak-typed, use Step if you need add Input or InputDependsOn
func Steps(steps ...Steper) AddSteps {
	rv := make(AddSteps)
	for _, step := range steps {
		rv[step] = nil
	}
	return rv
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

// Input adds Input func for the Step(s).
// If the Input function returns error, will return an ErrInput.
// Input respects the order in building calls.
//
//	Step(down).
//		Input(/* this Input will be feeded first */).
//		InputDependsOn(Adapt(up, /* then receive Output from up */)).
//		Input(/* this Input is after up's Output */),
func (as AddStep[S]) Input(fns ...func(context.Context, S) error) AddStep[S] {
	for _, step := range as.Steps {
		step := step // capture range variable
		as.AddSteps[step] = append(as.AddSteps[step], WorkflowAddOption{
			Input: func(ctx context.Context) error {
				for _, fn := range fns {
					if err := fn(ctx, step); err != nil {
						return err
					}
				}
				return nil
			},
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
func (as AddStep[S]) InputDependsOn(adapts ...Adapter[S]) AddStep[S] {
	for _, step := range as.Steps {
		step := step
		for _, adapt := range adapts {
			adapt := adapt
			as.AddSteps[step] = append(as.AddSteps[step], WorkflowAddOption{
				Upstream: adapt.Upstream,
				Input: func(ctx context.Context) error {
					return adapt.Flow(ctx, step)
				},
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

type AddSteps map[Steper][]WorkflowAddOption

// DependsOn declares dependency between Steps.
//
// "Upstreams happen before Downstream" is guaranteed.
func (as AddSteps) DependsOn(ups ...Steper) AddSteps {
	for down := range as {
		for _, up := range ups {
			as[down] = append(as[down], WorkflowAddOption{
				Upstream: up,
			})
		}
	}
	return as
}

// Timeout sets the Step level timeout.
func (as AddSteps) Timeout(timeout time.Duration) AddSteps {
	for step := range as {
		as[step] = append(as[step], WorkflowAddOption{
			Option: func(ss *StepOption) {
				ss.Timeout = timeout
			},
		})
	}
	return as
}

// When decides whether the Step should be Skipped.
func (as AddSteps) When(when When) AddSteps {
	for step := range as {
		as[step] = append(as[step], WorkflowAddOption{
			Option: func(ss *StepOption) {
				ss.When = when
			},
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
	for step := range as {
		as[step] = append(as[step], WorkflowAddOption{
			Option: func(ss *StepOption) {
				ss.RetryOption = appendRetry(ss.RetryOption, opts...)
			},
		})
	}
	return as
}

func (as AddSteps) Done() map[Steper][]WorkflowAddOption { return as }

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
