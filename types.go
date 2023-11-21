package flow

import (
	"context"
)

// Steper is basic unit of a Workflow.
//
// Implement this interface to be added into Workflow via Step() or Steps().
//
// Please do not expect the type of Steper, use Is() or As() if you need to check / get the typed step from the step tree.
type Steper interface {
	Do(context.Context) error
}

type set[T comparable] map[T]struct{}

func (s set[T]) Add(vs ...T) {
	for _, v := range vs {
		s[v] = struct{}{}
	}
}
func (s set[T]) Union(sets ...set[T]) {
	for _, set := range sets {
		for v := range set {
			s[v] = struct{}{}
		}
	}
}

// dependency tracks the dependencies between Step(s).
// We say "A depends on B", or "B happened-before A", then A is Downstream, B is Upstream.
//
//	dependency{
//		"A": {"B"},
//	}
//
// The keys are Downstream(s), the values are Upstream(s).
type dependency map[Steper]set[Steper]

func (d dependency) UpstreamOf(step Steper) set[Steper] { return d[step] }

// WARNING: this is expensive
func (d dependency) DownstreamOf(step Steper) set[Steper] {
	downs := make(set[Steper])
	for down, ups := range d {
		for up := range ups {
			if up == step {
				downs.Add(down)
				break
			}
		}
	}
	return downs
}
func (d dependency) Merge(others ...dependency) dependency {
	for _, other := range others {
		for down, ups := range other {
			if _, ok := d[down]; !ok {
				d[down] = make(set[Steper])
			}
			for up := range ups {
				d[down].Add(up)
			}
		}
	}
	return d
}
