package flow

// Phase clusters Steps into different execution phases.
//
// Workflow supports three phases: Init, Main and Defer.
// It derives from the below common go pattern:
//
//	func init() {}
//	func main() {
//		defer func() {}
//	}
//
// Only all Steps in previous phase terminated, the next phase will start.
// Even if the steps in previous phase are not successful, the next phase will still start.
type Phase int

const (
	PhaseUnknown Phase = iota
	PhaseInit
	PhaseMain
	PhaseDefer
)

func (w *Workflow) getPhases() []Phase { return []Phase{PhaseInit, PhaseMain, PhaseDefer} }
