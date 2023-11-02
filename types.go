package workflow

import (
	"context"
)

// Steper is basic unit of a Workflow.
type Steper interface {
	stepBase
	Do(context.Context) error
}

// SteperIO[I, O any] is the basic unit of a Workflow,
// with strong typed Input and Output.
type SteperIO[I, O any] interface {
	Steper
	inputer[I]
	outputer[O]
}

type inputer[I any] interface {
	Input() *I
}

type outputer[O any] interface {
	Output(*O)
}

// Downstream is a Steper that depends on Upstream(s).
type Downstream[I any] interface {
	Steper
	inputer[I]
}

// Upstream is a Steper that is depended by Downstream(s).
type Upstream[O any] interface {
	Steper
	outputer[O]
}

// Dependency is a relationship between Downstream(s) and Upstream(s).
// We say "A depends on B", or "B happened-before A", or "B's output is A'input", then A is Downstream, B is Upstream.
type Dependency map[Steper][]Link

// Link represents one connection between a Downstream and a Upstream,
// with the data Flow function.
type Link struct {
	Upstream Steper
	Flow     func(context.Context) error // Flow sends Upstream's Output to Downstream's Input
}

// UpstreamOf returns all Upstream(s) of a Downstream.
func (d Dependency) UpstreamOf(down Steper) []Steper {
	var ups []Steper
	for _, l := range d[down] {
		if l.Upstream != nil {
			ups = append(ups, l.Upstream)
		}
	}
	return ups
}

// DownstreamOf returns all Downstream(s) of a Upstream.
// WARNING: this is expensive
func (d Dependency) DownstreamOf(up Steper) []Steper {
	var downs []Steper
	for r, links := range d {
		for _, l := range links {
			if l.Upstream == up {
				downs = append(downs, r)
				break
			}
		}
	}
	return downs
}

// Merge merges other Dependency into this Dependency.
func (d Dependency) Merge(other Dependency) {
	for r, links := range other {
		d[r] = append(d[r], links...)
		// need to add the Upstream(s) as key(s) also
		for _, l := range links {
			if l.Upstream != nil {
				if _, ok := d[l.Upstream]; !ok {
					d[l.Upstream] = nil
				}
			}
		}
	}
}
