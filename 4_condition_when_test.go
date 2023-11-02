package workflow_test

import (
	"context"
	"fmt"

	fl "go.goms.io/aks/rp/test/v3/workflow"
)

// Workflow tracks each Step's status, and decides whether should queue Step based on When and Condition.
//
// The relations between StepStatus, When and Condition are:
//
//													  /--false-> StepStatusCanceled
//	StepStatusPending -> [When] ---true--> [Condition] --true--> StepStatusRunning --err == nil--> StepStatusSucceeded
//								\--false-> StepStatusSkipped					   \--err != nil--> StepStatusFailed
//
// In a word,
//   - When:      decides whether the Step should be **Skipped**
//   - Condition: decides whether the Step should be **Canceled**
//
// Canceled vs Skipped
//   - When: will be propagated through dependency to Downstream(s),
//     i.e. A depends on B, if B is Skipped, A will be Skipped.
//   - Condition: will still be evaulated by Downstream's Condition,
//     i.e. A depends on B, if B is Canceled, and A's Condition is 'Canceled', the A will still Run.
//
// Conditions have these predefined ones:
//   - Always:            all Upstreams are *terminated*
//   - Succeeded:         all Upstreams are Succeeded
//   - Failed:            any Upstream is Failed
//   - SucceededOrFailed: all Upstreams are Succeeded or Failed
//   - Canceled:          any Upstream is Canceled
//
// Terminated StepStaus are:
//   - StepStatusFailed
//   - StepStatusSucceeded
//   - StepStatusCanceled
//
// Only succeeded Upstreams will flow Output to Downstreams Input.
type ArbitraryTask struct{ fl.BaseEmptyIO }
type FailedStep struct{ fl.BaseEmptyIO }

func (a *ArbitraryTask) Do(context.Context) error { return nil }
func (a *FailedStep) Do(context.Context) error    { return fmt.Errorf("failed!") }

func ExampleConditionWhen() {
	flow := new(fl.Workflow)

	var (
		skipMe          = new(ArbitraryTask)
		skipMeToo       = new(ArbitraryTask)
		cancelMe        = new(ArbitraryTask)
		runWhenCanceled = new(ArbitraryTask)
		then            = new(ArbitraryTask)
		failed          = new(FailedStep)
	)

	flow.Add(
		fl.Step(skipMe).When(fl.Skip),
		fl.Step(skipMeToo).DependsOn(skipMe),
		fl.Step(cancelMe).Condition(func(ups []fl.StepReader) bool {
			return false // always cancel
		}),
		fl.Step(runWhenCanceled).
			DependsOn(cancelMe).
			Condition(fl.Canceled),
		fl.Step(then).
			DependsOn(failed).
			Condition(fl.SucceededOrFailed),
	)
	_ = flow.Run(context.Background())
	fmt.Println(skipMe.GetStatus())
	fmt.Println(cancelMe.GetStatus())
	fmt.Println(runWhenCanceled.GetStatus())
	fmt.Println(failed.GetStatus())
	fmt.Println(then.GetStatus())
	// Output:
	// Skipped
	// Canceled
	// Succeeded
	// Failed
	// Succeeded
}
