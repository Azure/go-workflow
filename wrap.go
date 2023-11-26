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
//		Things []*DoSomeThing
//	}
//	func (d *DoManyThings) Do(context.Context) error { /* do many fancy things */ }
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
// Actually, Workflow itself also implements Steper interface,
// meaning you can use Workflow as a Step in another Workflow!

// # How to audit / retrieve / update all steps from the Workflow?
//
//	workflow := func() *Workflow {
//		...
//		workflow.Add(Step(doSomeThing))
//		return workflow
//	}
//	// from now on, we don't have reference to doSomeThing
//	// however, I still want to update doSomeThing,
//	// like modify its input, configuration, or even behavior (by decorator).
//
// # Introduce Unwrap()
//
// Kindly remind that, nesting problem is not a new issue in Go.
// In Go, we have a very common error pattern:
//
//	type MyError struct {
//		Err error
//	}
//	func (e *MyError) Error() string { return fmt.Sprintf("MyError(%v)", e.Err) }
//
// The solution is using Unwrap() method:
//
//	func (e *MyError) Unwrap() error { return e.Err }
//
// Then standard package errors provides Is() and As() functions to help us deal with nested errors.
// We also provides a similar Is() and As() functions for Steper.
//
// Workflow users only need to implement the below methods for your Step implementations:
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
//	decorated := &Docorator{Steper: step}
//	workflow.Add(
//		Step(doSomeThing),
//		Step(decorated),
//	)
//
// StepTree is the solution to the above questions.
//
// # What is StepTree?
//
// StepTree is a data structure that
//   - keys are all Steps in track
//   - values are the root of that Step, or lowest ancestor that branches.
//
// Let's dive into the definitions, if some Step nest another Step, then
//
//	type Parent struct { // the outer one as Parent
//		Child Steper     // the inner one as Child
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
//   - [R]oot: the root of some Nested Step, it doesn't have outer layer.
//   - [L]eaf: the leaf of some Nested Step, it doesn't have inner layer.
//   - [T]runk: the trunk of some Nested Step, it has method Unwrap() Steper
//   - [B]ranch: the branch of some Nested Step, it has method Unwrap() []Steper
//
// Then the StepTree built for the above tree is:
//
//	StepTree{
//		R1: R1, // root's value is itself
//		L1: R1,
//		T2: R2, // trunk is "trasparent"
//		L2: R2,
//		B3: R3,
//		L3: B3, // to the lowest [B]ranch ancestor
//		T3: B3,
//		L4: B3,
//		...
//	}
type StepTree map[Steper]Steper

func (st StepTree) IsRoot(step Steper) bool {
	if step == nil || st[step] == nil {
		return false
	}
	return st[step] == step
}
func (st StepTree) RootOf(step Steper) Steper {
	for !st.IsRoot(st[step]) {
		step = st[step]
	}
	return step
}
func (sc StepTree) Roots() Set[Steper] {
	rv := make(Set[Steper])
	for k, v := range sc {
		if k == v {
			rv.Add(v)
		}
	}
	return rv
}

// Add a step and all it's descendant steps to the tree.
//
// If step is already in the tree, it's no-op.
// If step is new, the step will becomes a new root.
func (sc StepTree) Add(step Steper) (oldRoots Set[Steper]) {
	if step == nil || sc[step] != nil {
		return nil
	}
	return sc.newRoot(step, step)
}
func (sc StepTree) newRoot(root, step Steper) (oldRoots Set[Steper]) {
	oldRoots = make(Set[Steper])
	for {
		pRoot, ok := sc[step]
		switch {
		case !ok: // step is new
			sc[step] = root
		case ok && pRoot == step && pRoot != root: // step is an old root
			sc[step] = root
			oldRoots.Add(pRoot)
		case ok && pRoot != step && oldRoots.Has(pRoot): // step is the child of old root
			sc[step] = root
		case ok && pRoot != step && !oldRoots.Has(pRoot):
			panic(fmt.Errorf("add step %T(%s) failed: inner step %T(%s) already has a root %T(%s)",
				root, root,
				step, step,
				pRoot, pRoot,
			))
		}
		switch u := step.(type) {
		case interface{ Tree() StepTree }:
			// this is a quick path for using Workflow as a Step
			// Workflow implements Tree(), such that we can skip building the sub-tree,
			// instead, just make current step as the root of the previous roots
			// and merge the sub-tree
			subTree := u.Tree()
			for subRoot := range subTree.Roots() {
				subTree[subRoot] = root
			}
			sc.Merge(subTree)
			return
		case interface{ Unwrap() Steper }:
			step = u.Unwrap()
			if step == nil {
				return
			}
		case interface{ Unwrap() []Steper }:
			for _, inner := range u.Unwrap() {
				if inner == nil {
					continue
				}
				// the current step will becomes the root
				// of descendants steps
				oldRoots.Union(sc.newRoot(step, inner))
			}
			return
		default:
			return
		}
	}
}
func (sc StepTree) Merge(other StepTree) {
	for k, v := range other {
		switch {
		case sc[k] == nil:
			sc[k] = v
		case sc[k] != v:
			panic(fmt.Errorf("merge step tree failed: step %T(%s) already has a root %T(%s), but to merge %T(%s)",
				k, k,
				sc[k], sc[k],
				v, v,
			))
		}
	}
}

// WithName gives your step a name by overriding String() method.
func WithName(name string, step Steper) *NamedStep {
	return &NamedStep{Name: name, Steper: step}
}

// NamedStep is a wrapper of Steper, it gives your step a name by overriding String() method.
type NamedStep struct {
	Name string
	Steper
}

func (ns *NamedStep) String() string { return ns.Name }
func (ns *NamedStep) Unwrap() Steper { return ns.Steper }

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
