package flow

// Is reports whether the any step in step's chain matches target type.
func Is[T Steper](s Steper) bool { return len(As[T](s)) > 0 }

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

// Add step to tree, returns the new and old roots of the branch(s).
func (sc StepTree) Add(step Steper) (new Steper, olds []Steper) {
	if step == nil {
		return nil, nil
	}
	root, exist := sc[step]
	if exist {
		return root, []Steper{root}
	}
	// current step now becomes new root!
	new = step
	for {
		if root, ok := sc[step]; ok {
			olds = append(olds, root)
		}
		sc[step] = new
		switch u := step.(type) {
		case interface{ Unwrap() Steper }:
			step = u.Unwrap()
			if step == nil {
				return
			}
		case interface{ Unwrap() []Steper }:
			for _, step := range u.Unwrap() {
				if step != nil {
					_, sOlds := sc.Add(step)
					sc[step] = new
					for _, old := range sOlds {
						if old != nil {
							olds = append(olds, old)
						}
					}
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
