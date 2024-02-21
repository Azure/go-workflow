package flow

import (
	"fmt"
	"log/slog"
	"strings"
)

// # What is a Nested Step?
//
// Consider this case, Alice writes a Step implementation,
//
//	type DoSomeThing struct{}
//	func (d *DoSomeThing) Do(context.Context) error { /* do fancy things */ }
//
// After that, Bob finds the above implementation is useful, but still not enough.
// So Bob combines the above Steps into a new Step,
//
//	type DoManyThings struct {
//		DoSomeThing
//		DoOtherThing
//	}
//	func (d *DoManyThings) Do(context.Context) error { /* do fancy things then other thing */ }
//
// We define the above DoManyThings as a Nested Step, the below Decorator is another example.
//
//	type Decorator struct { Steper }
//	func (d *Decorator) Do(ctx context.Context) error {
//		/* do something before */
//		err := d.Steper.Do(ctx)
//		/* do something after */
//		return err
//	}
//
// Since Workflow only requires a Step to satisfy the below interface:
//
//	type Steper interface {
//		Do(context.Context) error
//	}
//
// It's easy, intuitive, flexible and yet powerful to use Nested Steps.
//
// Actually, Workflow itself also implements Steper interface,
// meaning you can use Workflow as a Step in another Workflow!

// # How to audit / retrieve / update all steps from the Workflow?
//
//	workflow := func() *Workflow {
//		...
//		workflow.Add(Step(doSomeThing))
//		return workflow
//	}
//
//	from now on, we don't have reference to the internal steps, like doSomeThing
//	however, it's totally possible have necessary to update doSomeThing,
//	like modify its input, configuration, or even its behavior (by decorator).
//
// # Introduce Unwrap()
//
// Kindly remind that, nesting problem is not a new issue in Go.
// In Go, we have a very common error pattern:
//
//	type MyError struct { Err error }
//	func (e *MyError) Error() string { return fmt.Sprintf("MyError(%v)", e.Err) }
//
// The solution is using Unwrap() method:
//
//	func (e *MyError) Unwrap() error { return e.Err }
//
// Then standard package errors provides Is() and As() functions to help us deal with nested errors.
// We also provides a similar Is() and As() functions for Steper.
//
// Users only need to implement the below methods for your Step implementations:
//
//	type WrapStep struct { Steper }
//	func (w *WrapStep) Unwrap() Steper { return w.Steper }
//	// or
//	type WrapSteps struct { Steps []Steper }
//	func (w *WrapSteps) Unwrap() []Steper { return w.Steps }
//
// to expose your inner Steps.

// Is reports whether the any step in step's tree matches target type.
func Is[T Steper](s Steper) bool {
	if s == nil {
		return false
	}
	for {
		if _, ok := s.(T); ok {
			return true
		}
		switch u := s.(type) {
		case interface{ Unwrap() Steper }:
			s = u.Unwrap()
			if s == nil {
				return false
			}
		case interface{ Unwrap() []Steper }:
			for _, s := range u.Unwrap() {
				if Is[T](s) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
}

// As finds all steps in the tree of step that matches target type, and returns them.
// The sequence of the returned steps is preorder traversal.
func As[T Steper](s Steper) []T {
	if s == nil {
		return nil
	}
	var rv []T
	for {
		if v, ok := s.(T); ok {
			rv = append(rv, v)
		}
		switch u := s.(type) {
		case interface{ Unwrap() Steper }:
			s = u.Unwrap()
			if s == nil {
				return rv
			}
		case interface{ Unwrap() []Steper }:
			for _, s := range u.Unwrap() {
				rv = append(rv, As[T](s)...)
			}
			return rv
		default:
			return rv
		}
	}
}

// StepTree is a tree data structure of steps, it helps Workflow tracks Nested Steps.
//
// # Why StepTree is needed?
//
// What if someone add a Step and its Nested Step to Workflow?
//
//	doSomeThing := &DoSomeThing{}
//	decorated := &Decorator{Steper: step}
//	workflow.Add(
//		Step(doSomeThing),
//		Step(decorated),
//	)
//
// docorated.Do() will call doSomeThing.Do() internally, and apparently,
// we don't want doSomeThing.Do() being called twice.
//
// StepTree is the solution to the above questions.
//
// # What is StepTree?
//
// Let's dive into the definitions, if some Step wrap another Step, then
//
//	type Parent struct {
//		Child Steper
//	}
//	type Parent struct { // This Parent "branches"
//		Children []Steper
//	}
//
// Then we can draw a tree like:
//
//	┌────┐ ┌────┐    ┌────┐
//	│ R1 │ │ R2 │    │ R3 │
//	└─┬──┘ └─┬──┘    └─┬──┘
//	┌─┴──┐ ┌─┴──┐    ┌─┴──┐
//	│ L1 │ │ T2 │    │ B3 │
//	└────┘ └─┬──┘    └─┬──┘
//	         │      ┌──┴────┐
//	       ┌─┴──┐ ┌─┴──┐  ┌─┴──┐
//	       │ L2 │ │ L3 │  │ T3 │
//	       └────┘ └────┘  └─┬──┘
//	                      ┌─┴──┐
//	                      │ L4 │
//	                      └────┘
//
// Where
//   - [R]oot: the root Step, there isn't other Step wrapping it.
//   - [L]eaf: the leaf Step, it doesn't wrap any Step inside.
//   - [T]runk: the trunk Step, it has method Unwrap() Steper, one Child.
//   - [B]ranch: the branch Step, it has method Unwrap() []Steper, multiple Children.
//
// Then the StepTree built for the above tree is:
//
//	StepTree{
//		R1: R1, // root's value is itself
//		L1: R1,
//		T2: R2,
//		L2: T2,
//		B3: R3,
//		L3: B3,
//		T3: B3,
//		L4: T3,
//		...
//	}
//
// StepTree is a data structure that
//   - keys are all Steps in track
//   - values are the ancestor Steps of the corresponding key
//
// If we consider sub-workflow into the tree, all sub-Workflow are "branch" Steps.
//
// The contract between Nested Step and Workflow is:
//
//	Once a Step "wrap" other Steps, it should have responsibility to orchestrate the inner steps.
//
// So from the Workflow's perspective, it only needs to orchestrate the root Steps,
// to make sure all Steps are executed in the right order.
type StepTree map[Steper]Steper

// IsRoot reports whether the step is a root step.
func (st StepTree) IsRoot(step Steper) bool {
	if st == nil || step == nil || st[step] == nil {
		return false
	}
	return st[step] == step
}

// RootOf returns the root step of the given step.
func (st StepTree) RootOf(step Steper) Steper {
	if st == nil || step == nil || st[step] == nil {
		return nil
	}
	for step != nil && st[step] != step { // traverse to root
		step = st[step]
	}
	return step
}

// Roots returns all root steps.
func (st StepTree) Roots() Set[Steper] {
	rv := make(Set[Steper])
	for k, v := range st {
		if k == v {
			rv.Add(v)
		}
	}
	return rv
}

// Add a step and all it's wrapped steps to the tree.
//
// Return the steps that were roots, but now are wrapped and taken place by the new root step.
//
//   - If step is already in the tree, it's no-op.
//   - If step is new, the step will becomes a new root.
//     and all its inner steps will be added to the tree.
//   - If one of the inner steps is already in tree, panic.
func (st StepTree) Add(step Steper) (oldRoots Set[Steper]) {
	if st == nil || step == nil || st[step] != nil {
		return nil
	}
	return st.traverse(step, step)
}

type ErrWrappedStepAlreadyInTree struct {
	StepAlreadyThere Steper
	NewAncestor      Steper
	OldAncestor      Steper
}

func (e ErrWrappedStepAlreadyInTree) Error() string {
	return fmt.Sprintf("add step %q failed: inner step %q already has an ancestor %q",
		String(e.NewAncestor),
		String(e.StepAlreadyThere),
		String(e.OldAncestor),
	)
}

func (st StepTree) traverse(root, step Steper) (oldRoots Set[Steper]) {
	oldRoots = make(Set[Steper])
	for {
		if step == nil {
			return
		}
		if ancestor, ok := st[step]; ok {
			if st.IsRoot(step) {
				oldRoots.Add(step)
				st[step] = root
				return
			} else {
				panic(ErrWrappedStepAlreadyInTree{
					StepAlreadyThere: step,
					NewAncestor:      root,
					OldAncestor:      ancestor,
				})
			}
		}
		st[step] = root
		switch u := step.(type) {
		case interface{ Unwrap() Steper }:
			root = step // the current step becomes new ancestor
			step = u.Unwrap()
		case interface{ Unwrap() []Steper }:
			for _, inner := range u.Unwrap() {
				oldRoots.Union(st.traverse(step, inner))
			}
			return
		default:
			return
		}
	}
}

// String unwraps step and returns a proper string representation.
func String(step Steper) string {
	if step == nil {
		return "<nil>"
	}
	switch u := step.(type) {
	case interface{ String() string }:
		return u.String()
	case interface{ Unwrap() Steper }:
		return String(u.Unwrap())
	case interface{ Unwrap() []Steper }:
		stepStrs := []string{}
		for _, step := range u.Unwrap() {
			stepStrs = append(stepStrs, String(step))
		}
		return fmt.Sprintf("[%s]", strings.Join(stepStrs, ", "))
	default:
		return fmt.Sprintf("%T(%v)", step, step)
	}
}

// LogValue is used with log/slog, you can use it like:
//
//	logger.With("step", LogValue(step))
//
// To prevent expensive String() calls,
//
//	logger.With("step", String(step))
func LogValue(step Steper) logValue { return logValue{Steper: step} }

type logValue struct{ Steper }

func (lv logValue) LogValue() slog.Value { return slog.StringValue(String(lv.Steper)) }
