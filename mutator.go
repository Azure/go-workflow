package flow

import "context"

// Mutator represents a type-dispatched, once-per-step contribution of
// configuration to a Step. The interface has a single unexported method, so
// the only producer is the generic constructor [Mutate].
type Mutator interface {
	applyTo(ctx context.Context, step Steper) (matched bool, target Steper, builder Builder)
}

// Mutate constructs a [Mutator] that runs against any step whose concrete type
// matches T anywhere along its Unwrap() chain (within a single workflow's
// boundaries). The first matching layer is passed to fn. fn returns a [Builder]
// whose configuration for the matched step is merged into the step's
// StepConfig at first scheduling. Returning a nil Builder is valid (useful
// when fn only mutates fields on the typed step pointer).
func Mutate[T Steper](fn func(ctx context.Context, step T) Builder) Mutator {
	return mutatorFunc[T](fn)
}

type mutatorFunc[T Steper] func(ctx context.Context, step T) Builder

func (m mutatorFunc[T]) applyTo(ctx context.Context, step Steper) (bool, Steper, Builder) {
	var (
		matched bool
		typed   T
		match   Steper
	)
	Traverse(step, func(s Steper, _ []Steper) TraverseDecision {
		if v, ok := s.(T); ok {
			typed = v
			match = s
			matched = true
			return TraverseStop
		}
		// Stop at workflow boundaries: do NOT descend into a nested workflow's
		// inner steps from here. Inner steps are reached when the inner
		// workflow runs its own Do() prologue — the parent's Mutators have
		// already been merged into the child's Option.Mutators via
		// [WorkflowOptionReceiver.InheritOption].
		if _, isWorkflow := s.(interface {
			StateOf(Steper) *State
		}); isWorkflow {
			return TraverseEndBranch
		}
		return TraverseContinue
	})
	if !matched {
		return false, nil, nil
	}
	return true, match, m(ctx, typed)
}
