package flow

import (
	"context"
)

// NoOpStep is a Step whose Do is a deliberate no-op. Useful as a synthetic
// "join point" — give multiple branches a single downstream to depend on
// without doing any real work.
type NoOpStep struct{ Name string }

// NoOp builds a NoOpStep with the given display name.
//
//	join := flow.NoOp("merge")
//	w.Add(flow.Steps(join).DependsOn(branchA, branchB, branchC))
func NoOp(name string) *NoOpStep { return &NoOpStep{Name: name} }

func (n *NoOpStep) String() string         { return n.Name }
func (*NoOpStep) Do(context.Context) error { return nil }
