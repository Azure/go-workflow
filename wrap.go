package flow

import (
	"fmt"
	"log/slog"
	"strings"
)

// # Composite Steps
//
// A "composite step" is a Step that is implemented by combining (embedding,
// wrapping or aggregating) one or more other Steps. The Workflow only
// schedules its top-level (root) steps; the inner Steps remain the composite
// step's own concern. To let the Workflow see through the composition (so
// utilities like Has[T], As[T], HasStep, dependency wiring and BuildStep can
// reach the inner steps), the composite exposes them via Unwrap().
//
// ## Example: aggregating two steps
//
//	type DoSomeThing struct{}
//	func (d *DoSomeThing) Do(context.Context) error { /* ... */ }
//
//	type DoManyThings struct {
//	    DoSomeThing
//	    DoOtherThing
//	}
//	func (d *DoManyThings) Do(context.Context) error { /* fan out ... */ }
//
// ## Example: wrapping (decorator)
//
//	type Decorator struct{ Steper }
//	func (d *Decorator) Do(ctx context.Context) error {
//	    /* before */
//	    err := d.Steper.Do(ctx)
//	    /* after */
//	    return err
//	}
//
// Workflow itself implements Steper, so any Workflow can be embedded as a
// step inside another Workflow (see SubWorkflow in workflow.go).
//
// ## Reaching into composites
//
// If you no longer hold a direct reference to an inner step but still need
// to inspect or modify it (e.g. to mock it for tests, or attach an
// Input/Output), expose it with one of the two Unwrap shapes recognised by
// Traverse():
//
//	type WrapStep struct{ Steper }
//	func (w *WrapStep) Unwrap() Steper { return w.Steper }
//
//	type WrapSteps struct{ Steps []Steper }
//	func (w *WrapSteps) Unwrap() []Steper { return w.Steps }
//
// Then Has[T], As[T], HasStep and Traverse will all walk through the
// composite to find what you're after — mirroring the standard library's
// errors.Is / errors.As pattern for wrapped errors.

// TraverseDecision is the value a Traverse visitor returns to direct the walk.
type TraverseDecision int

const (
	TraverseContinue  = iota // keep walking into this node's children.
	TraverseStop             // stop the entire traversal immediately.
	TraverseEndBranch        // skip this node's children, continue with siblings.
)

// Traverse performs a pre-order depth-first walk of the Step tree rooted at s.
//
// For each node visited, the callback receives:
//   - the Step itself,
//   - the path of Steps walked to reach it (excluding the current node).
//
// The callback's TraverseDecision controls whether to descend, prune, or
// stop. A nil callback is treated as TraverseStop.
//
// The walk understands two Unwrap shapes:
//   - `Unwrap() Steper`     — single child, descend into it.
//   - `Unwrap() []Steper`   — multiple children, descend into each in order.
//
// Anything else is a leaf.
func Traverse(s Steper, f func(Steper, []Steper) TraverseDecision, walked ...Steper) TraverseDecision {
	if f == nil {
		return TraverseStop
	}
	for {
		if s == nil {
			return TraverseEndBranch
		}
		if dec := f(s, walked); dec != TraverseContinue {
			return dec
		}
		walked = append(walked, s)
		switch u := s.(type) {
		case interface{ Unwrap() Steper }:
			s = u.Unwrap()
		case interface{ Unwrap() []Steper }:
			for _, s := range u.Unwrap() {
				if dec := Traverse(s, f, walked...); dec == TraverseStop {
					return dec
				}
			}
			return TraverseContinue
		default:
			return TraverseContinue
		}
	}
}

// Has reports whether the Step tree rooted at s contains any node assignable
// to T. Mirrors errors.As's "is there a wrapped error of this type?".
func Has[T Steper](s Steper) bool {
	find := false
	Traverse(s, func(s Steper, walked []Steper) TraverseDecision {
		if _, ok := s.(T); ok {
			find = true
			return TraverseStop
		}
		return TraverseContinue
	})
	return find
}

// As collects every node in the Step tree assignable to T, in pre-order.
// Returns nil (zero-length) if there are no matches.
func As[T Steper](s Steper) []T {
	var rv []T
	Traverse(s, func(s Steper, walked []Steper) TraverseDecision {
		if v, ok := s.(T); ok {
			rv = append(rv, v)
		}
		return TraverseContinue
	})
	return rv
}

// HasStep reports whether the Step tree rooted at step contains the exact
// instance target (pointer-equality). Returns false if target is nil.
func HasStep(step, target Steper) bool {
	if target == nil {
		return false
	}
	find := false
	Traverse(step, func(s Steper, walked []Steper) TraverseDecision {
		if s == target {
			find = true
			return TraverseStop
		}
		return TraverseContinue
	})
	return find
}

// String renders a Step (and any composite contents) as a debug-friendly
// multi-line string. The format prefers, in order:
//
//   - the Step's own String() method, if any;
//   - "<Type>(<addr>) { ... }" for single-child wrappers;
//   - "<Type>(<addr>) { each child on its own line }" for multi-child wrappers;
//   - "<Type>(<addr>)" for leaves.
//
// This is also what LogValue uses, so it's the canonical text form of a Step
// across logs, errors and panics.
func String(step Steper) string {
	if step == nil {
		return "<nil>"
	}
	switch u := step.(type) {
	case interface{ String() string }:
		return u.String()
	case interface{ Unwrap() Steper }:
		return fmt.Sprintf("%T(%p) {\n\t%s\n}", u, u, indent(String(u.Unwrap())))
	case interface{ Unwrap() []Steper }:
		stepStrs := []string{}
		for _, step := range u.Unwrap() {
			stepStrs = append(stepStrs, String(step))
		}
		return fmt.Sprintf("%T(%p) {\n\t%s\n}", u, u, indent(strings.Join(stepStrs, "\n")))
	default:
		return fmt.Sprintf("%T(%p)", step, step)
	}
}

// LogValue produces a slog-friendly handle for a Step that defers the
// (potentially expensive) String() call until the slog backend actually
// renders the field:
//
//	logger.With("step", LogValue(step))
//
// If you don't care about laziness, the equivalent eager form is:
//
//	logger.With("step", String(step))
func LogValue(step Steper) logValue { return logValue{Steper: step} }

// logValue carries a Step around with custom String / LogValue / MarshalJSON
// implementations so it serializes via String() for any sink.
type logValue struct{ Steper }

func (lv logValue) String() string       { return String(lv.Steper) }
func (lv logValue) LogValue() slog.Value { return slog.StringValue(String(lv.Steper)) }
func (lv logValue) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", String(lv.Steper))), nil
}
