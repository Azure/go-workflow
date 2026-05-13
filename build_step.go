package flow

// StepBuilder is the per-Workflow memo that ensures every Step's optional
// BuildStep() hook fires at most once.
//
// A Step type can implement BuildStep() to assemble its internal sub-steps
// lazily — typically the first time it is added to a Workflow:
//
//	type StepImpl struct{}
//	func (s *StepImpl) Unwrap() []flow.Steper { return /* internal steps */ }
//	func (s *StepImpl) Do(ctx context.Context) error { /* ... */ }
//	func (s *StepImpl) BuildStep()                  { /* assemble children */ }
//
//	workflow.Add(
//	    flow.Step(new(StepImpl)), // BuildStep() fires here, exactly once.
//	)
//
// The StepBuilder is embedded in Workflow itself, so Workflow.Add transparently
// invokes BuildStep on every newly seen step.
//
// Deprecated: StepBuilder, the [StepBuilder.BuildStep] method, the user-facing
// `BuildStep()` hook, and [SubWorkflow] are all scheduled for removal together
// in the next major version of go-workflow. Use [Mutate] for cross-cutting
// modification and construct sub-workflows at the call site instead (embed
// [Workflow] directly, or build inside Do() guarded by sync.Once). Behavior
// is unchanged in this release; Workflow still embeds StepBuilder and
// Workflow.Add still calls BuildStep on every newly seen Step.
type StepBuilder struct{ built Set[Steper] }

// BuildStep walks the tree of step (pre-order) and triggers BuildStep() on
// each node that implements it, recording the node so future calls skip it.
//
// Two early-exit rules keep behaviour predictable when composing workflows:
//
//   - If a node implements `BuildStep(Steper)` (the StepBuilder shape itself,
//     i.e. it manages a sub-workflow of its own), descent stops at that node —
//     the inner workflow is responsible for building its own contents.
//   - If a node implements `Reset()`, it is reset before BuildStep() runs, so
//     the build always starts from a clean slate.
//
// In both build cases the walker returns TraverseEndBranch so the parent
// composite's children aren't double-visited from this side.
//
// Deprecated: this lazy-initialization hook will be removed in the next major
// version of go-workflow. Use [Mutate] for cross-cutting modification, and
// construct sub-workflows inside Do() instead. See
// openspec/changes/2026-05-06-step-mutator/design.md.
func (sb *StepBuilder) BuildStep(s Steper) {
	if sb.built == nil {
		sb.built = make(Set[Steper])
	}
	Traverse(s, func(s Steper, walked []Steper) TraverseDecision {
		if sb.built.Has(s) {
			return TraverseEndBranch // already built
		}
		if _, ok := s.(interface{ BuildStep(Steper) }); ok {
			return TraverseEndBranch // it's a sub-workflow, let it manage its own steps
		}
		if b, ok := s.(interface{ BuildStep() }); ok {
			if r, ok := s.(interface{ Reset() }); ok {
				r.Reset() // reset the step before building
			}
			b.BuildStep()
			sb.built.Add(s)
			return TraverseEndBranch // not necessary to go deeper
		}
		return TraverseContinue
	})
}
