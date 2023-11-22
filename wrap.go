package flow

import "fmt"

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
func (st StepTree) IsRoot(step Steper) bool   { return st[step] == step }

// Add a step into the tree:
//   - if step is already in the tree, no-op.
//   - if step is new, it will be a new root.
//     -- all sub-steps wrapped inside will also be updated to have this new root.
//     -- if sub-step is already a root, it will be added to oldRoots.
//     -- if sub-step is not a root, it means there must be another root, so panic.
func (sc StepTree) Add(step Steper) (newRoot Steper, oldRoots set[Steper]) {
	if step == nil {
		return nil, nil
	}
	if root, ok := sc[step]; ok {
		return root, nil
	}
	// now current step becomes new root!
	newRoot = step
	oldRoots = make(set[Steper])
	for {
		if pRoot, ok := sc[step]; ok {
			if pRoot == step { // step is a previous root
				oldRoots.Add(pRoot)
			} else if len(oldRoots) == 0 { // step has another root
				panic(fmt.Errorf("add step %T(%s) failed: inner step %T(%s) already has a root %T(%s)",
					newRoot, newRoot,
					step, step,
					pRoot, pRoot,
				))
			}
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
				if step == nil {
					continue
				}
				root, olds := sc.Add(step)
				if olds == nil { // step already in tree
					if root == step { // step is a previous root
						oldRoots.Add(root)
					} else {
						panic(fmt.Errorf("add step %T(%s) failed: inner step %T(%s) already has a root %T(%s)",
							newRoot, newRoot,
							step, step,
							root, root,
						))
					}
				} else { // step is new
					oldRoots.Union(olds)
				}
				sc[step] = newRoot
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
