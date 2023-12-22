package flow_test

import (
	"context"
	"testing"

	flow "github.com/Azure/go-workflow"
)

func TestNonOpStep(t *testing.T) {
	t.Parallel()
	workflow := new(flow.Workflow)
	nonOpStep := &flow.NonOpStep{Name: "nonopstep"}
	workflow.Add(
		flow.Step(nonOpStep),
	)
	if err := workflow.Do(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestNonOpStepString(t *testing.T) {
	t.Parallel()
	nonOpStep := &flow.NonOpStep{Name: "nonopstep"}
	if nonOpStep.String() != "NonOp(nonopstep)" {
		t.Error("NonOpStep.String() does not match expected value")
	}
}
