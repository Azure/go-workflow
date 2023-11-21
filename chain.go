package flow

import (
	"fmt"
	"reflect"
)

type unwrapper interface {
	Unwrap() Steper
}

// Is reports whether the any step in step's chain matches target type.
func Is[T Steper](s Steper) bool {
	if _, ok := s.(T); ok {
		return true
	}
	for {
		if u, ok := s.(unwrapper); ok {
			s = u.Unwrap()
			if s == nil {
				return false
			}
			if _, ok := s.(T); ok {
				return true
			}
		} else {
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
		if u, ok := s.(unwrapper); ok {
			s = u.Unwrap()
			if s == nil {
				return false
			}
		} else {
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

// StepChain records chains of steps, steps are chained with Unwrap() method.
//
// i.e.
//
//	StepA -- StepA.Unwrap() --> StepB -- StepB.Unwrap() --> StepC
//
// Then
//
//	StepChain = map[Steper]Steper{
//		StepA: StepA,
//		StepB: StepA,
//		StepC: StepA,
//	}
//
// StepA is so-call "root" Step.
type StepChain map[Steper]Steper

// RootOf returns the root step of step in the chain.
func (sc StepChain) RootOf(step Steper) Steper { return sc[step] }

// Add adds step to StepChain, and returns the root step of this step in the chain.
//
// Add will panic if some step in the chain has different root.
// i.e.
//
//	StepA -- StepA.Unwrap() --> StepC
//	StepB -- StepB.Unwrap() --> StepC
//
// Then add StepA and StepB will panic.
func (sc StepChain) Add(step Steper) (root Steper) {
	if step == nil {
		return nil
	}
	pRoot, exist := sc[step]
	if exist {
		return pRoot
	}
	root = step
	for {
		pRoot, exist := sc[step]
		if exist {
			panic(fmt.Errorf("flow: step chain has different root for %T, root: %T, previous: %T", step, root, pRoot))
		}
		sc[step] = root
		u, ok := step.(unwrapper)
		if ok {
			step = u.Unwrap()
		} else {
			break
		}
	}
	return root
}

// AllRoots returns all root steps in the chain.
func (sc StepChain) AllRoots() Set[Steper] {
	rv := make(Set[Steper])
	for _, v := range sc {
		rv.Add(v)
	}
	return rv
}
