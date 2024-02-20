package flow_test

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
)

// # Condition and When
//
// Workflow tracks and updates each Step's status according to the execution result.
//
// StepStatus and Condition designs are learn from https://docs.github.com/en/actions/learn-github-actions/expressions#status-check-functions
//
// StepStatus are:
//   - Pending
//   - Running
//   - Failed
//   - Succeeded
//   - Canceled
//   - Skipped
//
// Only Pending Step can be queued to be executed.
//
// Before kicking the Step off, Workflow will check current Step's When setting
//
//	// When is a function to determine what's the next status of Step.
//	// When makes the decision based on the status and result of all the Upstream Steps.
//	// When is only called when all Upstreams are terminated.
//	type When func(context.Context, map[Steper]StatusError) StepStatus
//
// After When makes the decision of next status, Workflow will update Step's status accordingly.
//
// If the decision is Running, Workflow starts a goroutine to run the Step.
func ExampleConditionWhen() {
	var (
		succeeded = new(SucceededStep)
		failed    = new(FailedStep)
		canceled  = new(CanceledStep)
		skipped   = new(SkippedStep)

		allSucceeded = flow.Func("allSucceeded", func(context.Context) error {
			fmt.Println("AllSucceeded")
			return nil
		})
		always = flow.Func("always", func(context.Context) error {
			fmt.Println("Always")
			return nil
		})
		anyFailed = flow.Func("anyFailed", func(ctx context.Context) error {
			fmt.Println("AnyFailed")
			return nil
		})
		beCanceled = flow.Func("beCanceled", func(ctx context.Context) error {
			fmt.Println("BeCanceled")
			return nil
		})
	)

	workflow := new(flow.Workflow)
	workflow.Add(
		// AllSucceeded will run when all Upstreams are Succeeded,
		// so allSucceeded will be skipped here.
		flow.Step(allSucceeded).DependsOn(succeeded, failed, canceled, skipped).
			When(flow.AllSucceeded),
		// AnyFailed will run when any Upstream is Failed,
		// so anyFailed will be run.
		flow.Step(anyFailed).DependsOn(succeeded, failed, canceled, skipped).
			When(flow.AnyFailed),
	)
	_ = workflow.Do(context.Background())                   // AnyFailed
	fmt.Println(workflow.StateOf(allSucceeded).GetStatus()) // Skipped
	fmt.Println(workflow.StateOf(skipped).GetStatus())      // Skipped
	fmt.Println(workflow.StateOf(canceled).GetStatus())     // Canceled

	workflow = new(flow.Workflow)
	workflow.Add(
		// Always will run the Step regardlessly
		flow.Step(always).DependsOn(succeeded, failed, canceled, skipped).
			When(flow.Always),
		// BeCanceled will run when the workflow is canceled
		flow.Step(beCanceled).When(flow.BeCanceled).DependsOn(always),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // just cancel this ctx
	_ = workflow.Do(ctx)
	// Always
	// BeCanceled
	fmt.Println(workflow.StateOf(succeeded).GetStatus())  // Canceled
	fmt.Println(workflow.StateOf(always).GetStatus())     // Succeeded
	fmt.Println(workflow.StateOf(beCanceled).GetStatus()) // Succeeded

	// Output:
	// AnyFailed
	// Skipped
	// Skipped
	// Canceled
	// Always
	// BeCanceled
	// Canceled
	// Succeeded
	// Succeeded
}

type SucceededStep struct{}
type FailedStep struct{}
type CanceledStep struct{}
type SkippedStep struct{}

func (s *SucceededStep) Do(context.Context) error { return nil }
func (s *FailedStep) Do(context.Context) error    { return fmt.Errorf("failed!") }
func (s *CanceledStep) Do(context.Context) error  { return flow.Cancel(fmt.Errorf("cancel")) } // notice you can manually cancel a Step
func (s *SkippedStep) Do(context.Context) error   { return flow.Skip(fmt.Errorf("skip")) }     // and Skip also
