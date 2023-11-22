package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// After a Workflow is constructed, you can still update the Workflow.
// Get the Steps in Workflow, via `workflow.Steps()`, then `Add()` them back to update the Step.
//
//	for _, step := range workflow.Steps() {
//		workflow.Add(
//			flow.Steps(step).Retry(...), // update the Step
//		)
//	}
//
// However, the `step` here has a type of interface `Steper`, we're losing its concrete type information!
// So we cannot use strongly typed `Step()` to update the Step's Input callbacks.
//
// A very common proposal here, is to type assert the `step` to its concrete type.
//
//	for _, step := range workflow.Steps() {
//		switch typedStep := step.(type) {
//		case *SomeStep:
//			workflow.Add(
//				flow.Step(typedStep).Input(...), // update the typed Step
//			)
//		}
//	}
//
// The above proposal works for most cases, but from the perspective of framework, we have to think twice.
// Notice that, `flow` only requires an interface to be acceptable into Workflow, we're not requiring a concrete type.
// Thus when implementing a Step, it's very common to wrap (decorate) a Step to add more features.
//
//	type DecorateStep struct {
//		BaseStep flow.Steper
//	}
//
//	func (d *DecorateStep) Do(ctx context.Context) error {
//		// do something before
//		err := d.BaseStep.Do(ctx)
//		// do something after
//		return err
//	}
//
// Here, you may notice we're having following problem:
//
//	How to retrieve the concrete type from an interface that may be wrapped?
//
// Kindly remind you that in standard package `errors`, you can use `errors.Is` and `errors.As` to check or get the concrete type of error,
// and `Unwrap() error` method to unwrap the error.
//
// In `flow`, we provide similar functions `flow.Is` and `flow.As` to check or get the concrete type of Step,
// and user can implements a `Unwrap() Steper` method for decorating step type to allow `flow` unwraping it.
type WrapStep struct{ flow.Steper }

func (w *WrapStep) Unwrap() flow.Steper { return w.Steper }
func (w *WrapStep) Do(ctx context.Context) error {
	fmt.Println("WRAP: BEFORE")
	err := w.Steper.Do(ctx)
	fmt.Println("WRAP: AFTER")
	return err
}

func ExampleUpdateWorkflow() {
	foo := &Foo{}
	bar := &Bar{}

	workflow := new(flow.Workflow)
	workflow.Add(
		flow.Step(bar).DependsOn(foo),
	)

	// assume since here, we lost the reference of stepA and stepB
	// and we could have additional steps to inject
	sayHello := &SayHello{}
	workflow.Add(flow.Step(sayHello).Input(func(ctx context.Context, sh *SayHello) error {
		sh.Who = "World!"
		return nil
	}))

	// update the original Steps
	for _, step := range workflow.Steps() {
		switch {
		case flow.Is[*Foo](step):
			workflow.Add(flow.Step(step).DependsOn(sayHello))
		case flow.Is[*Bar](step):
			workflow.Add(flow.Step(
				&WrapStep{step},
			))
		}
	}

	_ = workflow.Do(context.Background())
	// Output:
	// Hello World!
	// Foo
	// BEFORE
	// Bar
	// AFTER
}

// Since Go1.21, errors package support unwraping multiple errors, `flow` also supports this feature.
type MultiWrapStep struct{ Steps []flow.Steper }

func (m *MultiWrapStep) Unwrap() []flow.Steper { return m.Steps }
func (m *MultiWrapStep) Do(ctx context.Context) error {
	fmt.Println("MULTI: BEFORE")
	defer fmt.Println("MULTI: AFTER")
	for i, step := range m.Steps {
		fmt.Printf("MULTI: STEP %d\n", i)
		if err := step.Do(ctx); err != nil {
			return err
		}
	}
	return nil
}

func ExampleMultiWrap() {
	step := &MultiWrapStep{
		Steps: []flow.Steper{
			new(Foo),
			new(Bar),
		},
	}
	fmt.Println(flow.Is[*Foo](step)) // true
	fmt.Println(flow.Is[*Bar](step)) // true

	// actually Workflow itself also implements `Unwrap() []Steper` method
	workflow := new(flow.Workflow)
	workflow.Add(flow.Step(step).DependsOn(new(SayHello)))

	for _, sayHello := range flow.As[*SayHello](workflow) {
		workflow.Add(flow.Step(sayHello).Input(func(ctx context.Context, sh *SayHello) error {
			sh.Who = "you can unwrap workflow!"
			return nil
		}))
	}

	_ = workflow.Do(context.Background())
	// Output:
	// true
	// true
	// Hello you can unwrap workflow!
	// MULTI: BEFORE
	// MULTI: STEP 0
	// Foo
	// MULTI: STEP 1
	// Bar
	// MULTI: AFTER
}
