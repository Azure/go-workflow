package flow

import (
	"reflect"
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

// As finds the first step in step's chain that matches target type, and if one is found, sets
// target to that step value and returns true. Otherwise, it returns false.
func As(s Steper, target any) bool {
	if s == nil {
		return false
	}
	if target == nil {
		panic("flow: target cannot be nil")
	}
	val := reflect.ValueOf(target)
	typ := val.Type()
	if typ.Kind() != reflect.Ptr || val.IsNil() {
		panic("flow: target must be a non-nil pointer")
	}
	targetType := typ.Elem()
	if targetType.Kind() != reflect.Interface && !targetType.Implements(steperType) {
		panic("flow: *target must be interface or implement error")
	}
	for {
		if reflect.TypeOf(s).AssignableTo(targetType) {
			val.Elem().Set(reflect.ValueOf(s))
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
				if As(s, target) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
}

var steperType = reflect.TypeOf((*Steper)(nil)).Elem()

// NamedStep wraps and changes step's name by override String() method.
type NamedStep struct {
	Name string
	Steper
}

func (ns *NamedStep) String() string { return ns.Name }
func (ns *NamedStep) Unwrap() Steper { return ns.Steper }

// StepTree records trees of steps, steps are chained with Unwrap() method.
//
// i.e.
//
//	StepA -- StepA.Unwrap() --> StepB -- StepB.Unwrap() --> StepC, StepD
//
// Then
//
//	StepTree = map[Steper]Steper{
//		StepA: StepA,
//		StepB: StepA,
//		StepC: StepA,
//		StepD: StepA,
//	}
//
// StepA is so-call "root" Step.
type StepTree map[Steper]Steper

// RootOf returns the root step of step in the tree.
func (st StepTree) RootOf(step Steper) Steper { return st[step] }

// Add adds step to StepTree, and returns the root step of this step in the tree.
func (sc StepTree) Add(step Steper) (root Steper) {
	if step == nil {
		return nil
	}
	pRoot, exist := sc[step]
	if exist {
		return pRoot
	}
	root = step
	for {
		sc[step] = root
		switch u := step.(type) {
		case interface{ Unwrap() Steper }:
			step = u.Unwrap()
			if step == nil {
				return root
			}
		case interface{ Unwrap() []Steper }:
			for _, step := range u.Unwrap() {
				switch sc.Add(step) {
				case nil, root: // do nothing:
				case step:
					sc[step] = root
				}
			}
			return root
		default:
			return root
		}
	}
}

// Roots returns all root steps in the tree.
func (sc StepTree) Roots() set[Steper] {
	rv := make(set[Steper])
	for _, v := range sc {
		rv.Add(v)
	}
	return rv
}
