package flow

import (
	"context"
	"time"
)

// implement this interface to be used in Workflow.Add()
type WorkflowStep interface {
	done() Dependency
}

// Steps declares a series of Steps.
// The Steps are mutually independent, and will be executed in parallel.
//
//	Steps(a, b, c) // a, b, c will be executed in parallel
//	Steps(a, b, c).DependsOn(d, e) // d, e will be executed in parallel, then a, b, c in parallel
//
// Steps are weak-typed, use Step if you need add Input or InputDependsOn
func Steps(downs ...Steper) AddSteps {
	dep := make(Dependency)
	for _, down := range downs {
		dep[down] = nil
	}
	return AddSteps{
		Downs:      downs,
		Dependency: dep,
	}
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

// Step declares Steps ready for building dependencies to Workflow,
// with the support of Input(...) and InputDependsOn(...).
func Step[S Steper](downs ...S) AddStep[S] {
	return AddStep[S]{
		AddSteps: Steps(ToSteps(downs)...),
		Downs:    downs,
	}
}

type AddStep[S Steper] struct {
	AddSteps
	Downs []S
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
	for _, down := range as.Downs {
		down := down // capture range variable
		as.Dependency[down] = append(as.Dependency[down], Link{
			Flow: func(ctx context.Context) error {
				for _, fn := range fns {
					if err := fn(ctx, down); err != nil {
						return err
					}
				}
				return nil
			},
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
	for _, down := range as.Downs {
		down := down
		for _, adapt := range adapts {
			adapt := adapt
			as.Dependency[down] = append(as.Dependency[down], Link{
				Upstream: adapt.Upstream,
				Flow: func(ctx context.Context) error {
					return adapt.Flow(ctx, down)
				},
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
	Downs []Steper
	Dependency
}

// DependsOn declares dependency between Steps.
//
// "Upstreams happen before Downstream" is guaranteed.
// Upstream's Output will not be sent to Downstream's Input.
func (as AddSteps) DependsOn(ups ...Steper) AddSteps {
	links := []Link{}
	for _, up := range ups {
		links = append(links, Link{Upstream: up})
	}
	for down := range as.Dependency {
		as.Dependency[down] = append(as.Dependency[down], links...)
	}
	return as
}

// Timeout sets the Step level timeout.
func (as AddSteps) Timeout(timeout time.Duration) AddSteps {
	for _, step := range as.Downs {
		step.setTimeout(timeout)
	}
	return as
}

// Condition decides whether the Step should be Canceled.
func (as AddSteps) Condition(cond Condition) AddSteps {
	for _, step := range as.Downs {
		step.setCondition(cond)
	}
	return as
}

// When decides whether the Step should be Skipped.
func (as AddSteps) When(when When) AddSteps {
	for _, step := range as.Downs {
		step.setWhen(when)
	}
	return as
}

func addRetry(opt *RetryOption, fns ...func(*RetryOption)) *RetryOption {
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
func (as AddSteps) Retry(fns ...func(*RetryOption)) AddSteps {
	for _, step := range as.Downs {
		step.setRetry(addRetry(step.GetRetry(), fns...))
	}
	return as
}

func (as AddSteps) done() Dependency { return as.Dependency }
