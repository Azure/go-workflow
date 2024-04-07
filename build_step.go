package flow

// StepBuilder allows to build the internal Steps when adding into Workflow.
//
//	type StepImpl struct {}
//	func (s *StepImpl) Unwrap() []flow.Steper { return /* internal steps */ }
//	func (s *StepImpl) Do(ctx context.Context) error { /* ... */ }
//	func (s *StepImpl) BuildStep() { /* build internal steps */ }
//
//	workflow.Add(
//		flow.Step(new(StepImpl)), // here will call StepImpl.BuildStep() once implicitly
//	)
type StepBuilder struct{ built Set[Steper] }

// BuildStep calls BuildStep() method of the Steper if it's implemented,
// and ensure it's called only once for each Steper.
func (sb *StepBuilder) BuildStep(s Steper) {
	if sb.built == nil {
		sb.built = make(Set[Steper])
	}
	Traverse(s, func(s Steper, walked []Steper) TraverseDecision {
		if sb.built.Has(s) {
			return TraverseDecision{Continue: false}
		}
		if _, ok := s.(interface{ BuildStep(Steper) }); ok {
			return TraverseDecision{Continue: false}
		}
		if b, ok := s.(interface{ BuildStep() }); ok {
			b.BuildStep()
			sb.built.Add(s)
		}
		return TraverseDecision{Continue: true}
	})
}
