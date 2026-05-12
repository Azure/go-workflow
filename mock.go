package flow

import "context"

// Mock returns a Builder that swaps in a fake Do for an existing step. The
// original step value is kept (so identity-based lookups via As[T] / HasStep
// still find it), but its Do is replaced by your function.
//
//	w.Add(
//	    flow.Mock(realStep, func(ctx context.Context) error {
//	        // pretend behaviour for tests
//	        return nil
//	    }),
//	)
//
// Mock is most useful in tests after the workflow is already wired (possibly
// by production code), when you want to substitute behaviour without
// rebuilding the graph.
func Mock[T Steper](step T, do func(context.Context) error) Builder {
	return Step(&MockStep{Step: step, MockDo: do})
}

// MockStep is the wrapper produced by Mock. It exposes the original Step via
// Unwrap (so utilities like As[T] / HasStep / String still see through it)
// while delegating Do to MockDo.
type MockStep struct {
	Step   Steper
	MockDo func(context.Context) error
}

func (m *MockStep) Unwrap() Steper               { return m.Step }
func (m *MockStep) Do(ctx context.Context) error { return m.MockDo(ctx) }
