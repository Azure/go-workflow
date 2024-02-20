package flow

// Phase groups Steps into different execution phases.
//
// Workflow supports three built-in phases: Init, Main and Defer.
// It derives from the below common go pattern:
//
//	func init() {}
//	func main() {
//		defer func() {}
//	}
//
// - Only all Steps in previous phase terminated, the next phase will start.
// - Even if the steps in previous phase are not successful, the next phase will always start.
// - The order of steps in the same phase is not guaranteed. (defer here is not stack!)
//
// Customized phase can be added to WorkflowPhases.
type Phase string

const (
	PhaseUnknown Phase = ""
	PhaseInit    Phase = "Init"
	PhaseMain    Phase = "Main"
	PhaseDefer   Phase = "Defer"
)

// WorkflowPhases defines the order of phases Workflow executes.
// New phases can be added to this, please support the built-in phases.
//
// i.e.
//
//	var PhaseDebug flow.Phase = "Debug"
//
//	func init() {
//		flow.WorkflowPhases = []flow.Phase{flow.PhaseInit, flow.PhaseMain, PhaseDebug, flow.PhaseDefer}
//	}
//
// Then in your package, workflow will execute the steps in PhaseDebug after PhaseMain, before PhaseDefer.
var WorkflowPhases = []Phase{PhaseInit, PhaseMain, PhaseDefer}
