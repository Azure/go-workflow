package flow

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

// NamedStep wraps and changes step's name by override String() method.
type NamedStep struct {
	Name string
	Steper
}

func (ns *NamedStep) String() string { return ns.Name }
func (ns *NamedStep) Unwrap() Steper { return ns.Steper }

// StepTree records trees of steps, steps are chained with Unwrap() method.
//
// StepTree is actually a disjoint-set, where keys are each step in the chain, and values are the root step.
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

func (sc StepTree) Add(step Steper) (newRoot Steper, oldRoots set[Steper]) {
	if step == nil {
		return nil, nil
	}
	oldRoots = make(set[Steper])
	if root, ok := sc[step]; ok {
		newRoot = root
		oldRoots.Add(root)
		return
	}
	// now current step becomes new root!
	newRoot = step
	for {
		if pRoot, ok := sc[step]; ok {
			oldRoots.Add(pRoot)
		}
		sc[step] = newRoot
		switch u := step.(type) {
		case interface{ Unwrap() Steper }:
			step = u.Unwrap()
			if step == nil {
				return
			}
		case interface{ Unwrap() []Steper }:
			for _, step := range u.Unwrap() {
				if step != nil {
					_, olds := sc.Add(step)
					sc[step] = newRoot
					oldRoots.Union(olds)
				}
			}
			return
		default:
			return
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
