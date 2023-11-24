package flow

import (
	"fmt"
	"log/slog"
)

// Is reports whether the any step in step's chain matches target type.
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

// WithName gives your step a name by overriding String() method.
func WithName(name string, step Steper) *NamedStep {
	return &NamedStep{Name: name, Steper: step}
}

type NamedStep struct {
	Name string
	Steper
}

func (ns *NamedStep) String() string { return ns.Name }
func (ns *NamedStep) Unwrap() Steper { return ns.Steper }

// stepTree records trees of steps, steps are chained with Unwrap() method.
//
// stepTree is actually a disjoint-set, where keys are each step in the chain, and values are the root step.
// i.e.
//
//	StepA -- StepA.Unwrap() --> StepB -- StepB.Unwrap() --> StepC, StepD
//
// Then
//
//	stepTree = map[Steper]Steper{
//		StepA: StepA,
//		StepB: StepA,
//		StepC: StepA,
//		StepD: StepA,
//	}
//
// StepA is so-call "root" Step.
type stepTree map[Steper]Steper

// RootOf returns the root step of step in the tree.
func (st stepTree) RootOf(step Steper) Steper { return st[step] }
func (st stepTree) IsRoot(step Steper) bool   { return st[step] == step }

func (sc stepTree) newRoot(root, step Steper) (oldRoots Set[Steper]) {
	oldRoots = make(Set[Steper])
	for {
		pRoot, ok := sc[step]
		switch {
		case !ok: // step is new
			sc[step] = root
		case ok && pRoot == step: // step is a previous root
			sc[step] = root
			oldRoots.Add(pRoot)
		case ok && pRoot != step && len(oldRoots) == 0: // step has another root
			panic(fmt.Errorf("add step %T(%s) failed: inner step %T(%s) already has a root %T(%s)",
				root, root,
				step, step,
				pRoot, pRoot,
			))
		}
		switch u := step.(type) {
		case interface{ Unwrap() Steper }:
			step = u.Unwrap()
			if step == nil {
				return
			}
		case interface{ Unwrap() []Steper }:
			for _, step := range u.Unwrap() {
				if step == nil {
					continue
				}
				oldRoots.Union(sc.newRoot(root, step))
			}
			return
		default:
			return
		}
	}
}

// Add a step into the tree:
//   - if step is already in the tree, no-op.
//   - if step is new, it will be a new root.
//     -- all sub-steps wrapped inside will also be updated to have this new root.
//     -- if sub-step is already a root, it will be added to oldRoots.
//     -- if sub-step is not a root, it means there must be another root, so panic.
func (sc stepTree) Add(step Steper) (newRoot Steper, oldRoots Set[Steper]) {
	if step == nil {
		return nil, nil
	}
	if root, ok := sc[step]; ok {
		return root, nil
	}
	// now current step becomes new root!
	newRoot = step
	oldRoots = sc.newRoot(newRoot, step)
	return
}

// Roots returns all root steps in the tree.
func (sc stepTree) Roots() Set[Steper] {
	rv := make(Set[Steper])
	for _, v := range sc {
		rv.Add(v)
	}
	return rv
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
		rv := "[ "
		for _, step := range u.Unwrap() {
			rv += String(step) + " "
		}
		return rv + "]"
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
