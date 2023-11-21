package flow

import (
	"fmt"
	"reflect"
)

type unwrapper interface {
	Unwrap() Steper
}

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

type NamedStep struct {
	Name string
	Steper
}

func (ns *NamedStep) String() string { return ns.Name }
func (ns *NamedStep) Unwrap() Steper { return ns.Steper }

// StepChain records chains of Step(s), Step(s) are chained with Unwrap() method.
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
func (sc StepChain) AllRoots() Set[Steper] {
	rv := make(Set[Steper])
	for _, v := range sc {
		rv.Add(v)
	}
	return rv
}
