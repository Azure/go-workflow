package workflow

import (
	"fmt"
	"strings"
)

// ErrFlow indicates the error happens in passing Upstream's Output to Downstream's Input.
//
//	Input(func(ctx context.Context, s *SomeStep) error {
//		return err
//	})
type ErrFlow struct {
	Err  error
	From StepReader
}

func (e *ErrFlow) Error() string {
	return fmt.Sprintf("ErrFlow(From %s [%s]): %s", e.From, e.From.GetStatus(), e.Err.Error())
}

// ErrWorkflow contains all errors of Steps in a Workflow.
type ErrWorkflow map[StepReader]error

func (e ErrWorkflow) Error() string {
	builder := new(strings.Builder)
	for reporter, err := range e {
		if err != nil {
			builder.WriteString(fmt.Sprintf(
				"%s [%s]: %s\n",
				reporter.String(), reporter.GetStatus().String(), err.Error(),
			))
		}
	}
	return builder.String()
}

func (e ErrWorkflow) IsNil() bool {
	for _, err := range e {
		if err != nil {
			return false
		}
	}
	return true
}

var ErrWorkflowIsRunning = fmt.Errorf("Workflow is running, please wait for it terminated")
var ErrWorkflowHasRun = fmt.Errorf("Workflow has run, check result error via .Err()")

// Only when the Step status is not StepStautsPending when Workflow starts to run.
type ErrUnexpectStepInitStatus []StepReader

func (e ErrUnexpectStepInitStatus) Error() string {
	builder := new(strings.Builder)
	builder.WriteString("Unexpect Step initial status:")
	for _, j := range e {
		builder.WriteRune('\n')
		builder.WriteString(fmt.Sprintf(
			"%s [%s]",
			j, j.GetStatus(),
		))
	}
	return builder.String()
}

// There is a cycle-dependency in your Workflow!!!
type ErrCycleDependency map[StepReader][]StepReader

func (e ErrCycleDependency) Error() string {
	builder := new(strings.Builder)
	builder.WriteString("Cycle Dependency Error:")
	for j, deps := range e {
		depsStr := []string{}
		for _, dep := range deps {
			depsStr = append(depsStr, dep.String())
		}
		builder.WriteRune('\n')
		builder.WriteString(fmt.Sprintf(
			"%s: [%s]",
			j, strings.Join(depsStr, ", "),
		))
	}
	return builder.String()
}
