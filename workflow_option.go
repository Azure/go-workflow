package flow

import "github.com/benbjohnson/clock"

// WorkflowOption groups all configuration that a Workflow exposes to its
// caller AND inherits from a parent Workflow when used as a sub-workflow step.
//
// Scalar fields are pointers so that "unset" (nil) and "explicit zero value"
// are distinguishable. On parent → child Option inheritance, a nil pointer on
// the child means "inherit from parent (or use the runtime default)"; a
// non-nil pointer is the child's own choice and wins over the parent.
//
// Slice fields are not pointer-typed; on inheritance, the parent's slice is
// prepended to the child's slice (parent contributions run first), preserving
// the existing Mutator and Interceptor propagation semantics.
//
// Mutating a Workflow's Option after Do() has started is undefined behavior.
type WorkflowOption struct {
	// MaxConcurrency caps simultaneously-running Steps. nil or 0 means
	// unlimited; a positive value installs a buffered-channel lease bucket.
	MaxConcurrency *int

	// DontPanic, if non-nil and true, recovers panics in Step Do / Input /
	// BeforeStep / AfterStep callbacks and surfaces them as ErrPanic.
	DontPanic *bool

	// SkipAsError, if non-nil and true, counts Skipped terminal status as a
	// workflow failure (so Do returns ErrWorkflow even if no Step actually
	// failed).
	SkipAsError *bool

	// Clock is the time source used for Step timeouts, per-try timeouts in
	// the retry loop, and backoff waits. nil means real wall clock
	// (clock.New()). Inject a clock.Mock in tests to control time.
	Clock clock.Clock

	// StepDefaults, if non-nil, is prepended as the FIRST option to every
	// Step's Option list as a baseline. Per-step Option calls (Retry,
	// Timeout, When, …) still win over it.
	StepDefaults *StepOption

	// Mutators is the workflow-level list of cross-cutting step Mutators.
	// On inheritance, the parent's Mutators are prepended (parent contributions
	// run first within the child).
	Mutators []Mutator

	// StepInterceptors wraps each Step's full lifetime (across retries).
	// On inheritance, the parent's slice is prepended to the child's.
	StepInterceptors []StepInterceptor

	// AttemptInterceptors wraps each individual attempt (Before → Do → After).
	// On inheritance, the parent's slice is prepended to the child's.
	AttemptInterceptors []AttemptInterceptor

	// DontInherit, when true on a sub-workflow Workflow, makes InheritOption
	// a no-op: nothing flows in from the parent. Replaces the previous
	// IsolateInterceptors flag and now governs the entire WorkflowOption,
	// not just interceptors. Naming aligns with DontPanic.
	DontInherit bool
}

// WorkflowOptionReceiver is implemented by any Step that contains a
// sub-workflow. The parent's Do() prologue locates the nearest receiver in
// each root step's Unwrap chain and calls InheritOption ONCE before any
// scheduling begins, so the child's Do() observes the merged Option.
//
// *Workflow itself implements this interface; users get inheritance for
// free by embedding flow.Workflow in their own Step type.
type WorkflowOptionReceiver interface {
	InheritOption(parent WorkflowOption)
}

// prependSlice returns a fresh slice equal to parent ++ child. It MUST NOT
// mutate either input. The fresh backing array is what allows callers to
// snapshot-and-restore a WorkflowOption with a shallow copy: parent and
// child slice headers retain their original backing arrays.
func prependSlice[T any](parent, child []T) []T {
	if len(parent) == 0 {
		if len(child) == 0 {
			return nil
		}
		out := make([]T, len(child))
		copy(out, child)
		return out
	}
	if len(child) == 0 {
		out := make([]T, len(parent))
		copy(out, parent)
		return out
	}
	out := make([]T, 0, len(parent)+len(child))
	out = append(out, parent...)
	out = append(out, child...)
	return out
}

// findOptionReceiver returns the first WorkflowOptionReceiver in the Step
// tree rooted at s, walking via Unwrap in pre-order. Returns nil if none of
// the unwrapped Steps satisfies WorkflowOptionReceiver.
//
// This lets a sub-workflow be wrapped in a Steper-only wrapper (e.g.
// NamedStep, which embeds the Steper interface and therefore does not
// promote InheritOption) without losing parent-Option inheritance.
func findOptionReceiver(s Steper) WorkflowOptionReceiver {
	var found WorkflowOptionReceiver
	Traverse(s, func(s Steper, _ []Steper) TraverseDecision {
		if r, ok := s.(WorkflowOptionReceiver); ok {
			found = r
			return TraverseStop
		}
		return TraverseContinue
	})
	return found
}
