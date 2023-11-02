package workflow

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
func Steps(downs ...Steper) addSteps {
	dep := make(Dependency)
	for _, down := range downs {
		dep[down] = nil
	}
	return addSteps{
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
// with the support of Input and InputDependsOn.
func Step[I any](downs ...Downstream[I]) addStepsIO[I] {
	return addStepsIO[I]{
		addSteps: Steps(ToSteps(downs)...),
		Downs:    downs,
	}
}

type addStepsIO[I any] struct {
	addSteps
	Downs []Downstream[I]
}

type InputFunc[I any] func(context.Context, *I) error

// Input adds Input func for the Step(s).
// If the Input function returns error, Downstream Step will return a ErrFlow.
// Input respects the order in building calls, because it's actually a empty Upstream.
//
//	Step(down).
//		Input(/* this Input will be feeded first */).
//		InputDependsOn(Adapt(up, /* then receive Output from up */)).
//		Input(/* this Input is after up's Output */),
func (as addStepsIO[I]) Input(fns ...InputFunc[I]) addStepsIO[I] {
	for _, down := range as.Downs {
		down := down // capture range variable
		as.Dependency[down] = append(as.Dependency[down], Link{
			Flow: func(ctx context.Context) error {
				for _, fn := range fns {
					if err := fn(ctx, down.Input()); err != nil {
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
// Use Adapt function to convert the Upstream's Output to Downstream's Input if the types are different.
//
//	Step(down).InputDependsOn(
//		up1, // up1's Output == down's Input
//		Adapt(up2, func(_ context.Context, o O, i *I) error {
//			// o: Output of up2
//			// i: Input of down
//			// fill i from o
//		}),
//	)
func (as addStepsIO[I]) InputDependsOn(ups ...Upstream[I]) addStepsIO[I] {
	for _, down := range as.Downs {
		down := down
		for _, up := range ups {
			up := up
			var link Link
			if adapt, ok := up.(*adapt[I]); ok {
				link.Upstream = adapt.Steper
				link.Flow = func(ctx context.Context) error {
					return adapt.Flow(ctx, down.Input())
				}
			} else {
				link.Upstream = up
				link.Flow = func(ctx context.Context) error {
					up.Output(down.Input())
					return nil
				}
			}
			as.Dependency[down] = append(as.Dependency[down], link)
		}
	}
	return as
}

type AdaptFunc[I, O any] func(context.Context, O, *I) error

// Adapt bridges Upstream and Downstream with different I/O types.
func Adapt[I, O any](up Upstream[O], fn AdaptFunc[I, O]) *adapt[I] {
	return &adapt[I]{
		Steper: up,
		Flow: func(ctx context.Context, i *I) error {
			return fn(ctx, GetOutput[O](up), i)
		},
	}
}

type adapt[I any] struct {
	Steper
	Flow func(context.Context, *I) error
}

func (a *adapt[I]) Output(o *I) { /* only implements Upstream[I] interface */ }

type addSteps struct {
	Downs []Steper
	Dependency
}

// DependsOn declares dependency between Steps.
//
// "Upstreams happen before Downstream" is guaranteed.
// Upstream's Output will not be sent to Downstream's Input.
func (as addSteps) DependsOn(ups ...Steper) addSteps {
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
func (as addSteps) Timeout(timeout time.Duration) addSteps {
	for _, step := range as.Downs {
		step.setTimeout(timeout)
	}
	return as
}

// Condition decides whether the Step should be Canceled.
func (as addSteps) Condition(cond Condition) addSteps {
	for _, step := range as.Downs {
		step.setCondition(cond)
	}
	return as
}

// When decides whether the Step should be Skipped.
func (as addSteps) When(when When) addSteps {
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
func (as addSteps) Retry(fns ...func(*RetryOption)) addSteps {
	for _, step := range as.Downs {
		step.setRetry(addRetry(step.GetRetry(), fns...))
	}
	return as
}

func (as addSteps) done() Dependency {
	return as.Dependency
}
