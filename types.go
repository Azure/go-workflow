package flow

import (
	"context"
)

// Steper is basic unit of a Workflow.
type Steper interface {
	Do(context.Context) error
}

type Set[T comparable] map[T]struct{}

func (s Set[T]) Add(vs ...T) {
	for _, v := range vs {
		s[v] = struct{}{}
	}
}
func (s Set[T]) Union(sets ...Set[T]) {
	for _, set := range sets {
		for v := range set {
			s[v] = struct{}{}
		}
	}
}

// Dependency tracks the dependencies between Step(s).
// We say "A depends on B", or "B happened-before A", then A is Downstream, B is Upstream.
//
// The keys are Downstream(s), the values are Upstream(s).
type Dependency map[Steper]Set[Steper]

// UpstreamOf returns all Upstream(s) of a Downstream.
func (d Dependency) UpstreamOf(step Steper) Set[Steper] { return d[step] }

// DownstreamOf returns all Downstream(s) of a Upstream.
// WARNING: this is expensive
func (d Dependency) DownstreamOf(step Steper) Set[Steper] {
	downs := make(Set[Steper])
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

// Merge merges other Dependency into this Dependency.
func (d Dependency) Merge(others ...Dependency) Dependency {
	for _, other := range others {
		for down, ups := range other {
			if _, ok := d[down]; !ok {
				d[down] = make(Set[Steper])
			}
			for up := range ups {
				d[down].Add(up)
			}
		}
	}
	return d
}
