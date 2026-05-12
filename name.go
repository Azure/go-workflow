package flow

import "fmt"

// Name attaches a human-readable display name to a Step by wrapping it in a
// NamedStep. The returned Builder can be passed to Workflow.Add directly:
//
//	workflow.Add(
//	    Step(a),
//	    Name(a, "StepA"), // a will now log/print as "StepA"
//	)
//
// Note: Name produces a wrapper Step. The original Step is reachable via
// Unwrap() (so As[T] / HasStep / interceptor inheritance still see through
// the wrapper), but the wrapper itself becomes the value the Workflow tracks.
func Name(step Steper, name string) Builder {
	return Step(&NamedStep{Name: name, Steper: step})
}

// Names attaches display names to many steps at once.
//
//	workflow.Add(
//	    Names(map[Steper]string{
//	        stepA: "A",
//	        stepB: "B",
//	    }),
//	)
func Names(m map[Steper]string) Builder {
	as := AddSteps{}
	for step, name := range m {
		as[&NamedStep{name, step}] = nil
	}
	return as
}

// NameFunc is like Name but the display name is computed every time it is
// requested — useful when the name depends on runtime data.
func NameFunc(step Steper, fn func() string) Builder {
	return NameStringer(step, stringer(fn))
}

// NameStringer is like NameFunc but takes any fmt.Stringer as the name source.
func NameStringer(step Steper, name fmt.Stringer) Builder {
	return Step(&StringerNamedStep{Name: name, Steper: step})
}

// NamedStep wraps a Steper and overrides its String() method with a fixed
// name. It preserves Steper identity through Unwrap.
type NamedStep struct {
	Name string
	Steper
}

func (ns *NamedStep) String() string { return ns.Name }
func (ns *NamedStep) Unwrap() Steper { return ns.Steper }

// stringer adapts a `func() string` to fmt.Stringer for NameFunc.
type stringer func() string

func (s stringer) String() string { return s() }

// StringerNamedStep wraps a Steper and overrides its String() with a
// dynamic, runtime-evaluated name supplied by a fmt.Stringer.
type StringerNamedStep struct {
	Name fmt.Stringer
	Steper
}

func (sns *StringerNamedStep) String() string { return sns.Name.String() }
func (sns *StringerNamedStep) Unwrap() Steper { return sns.Steper }
