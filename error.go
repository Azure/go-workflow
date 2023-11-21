package flow

import (
	"fmt"
	"strings"
)

// Cancel will mark the current step Canceled.
func Cancel(err error) ErrCancel { return ErrCancel{Err: err} }

// Skip will mark the current step Skipped.
func Skip(err error) ErrSkip { return ErrSkip{Err: err} }

type ErrCancel struct{ Err error }
type ErrSkip struct{ Err error }

func (e ErrCancel) Error() string { return e.Err.Error() }
func (e ErrCancel) Unwrap() error { return e.Err }
func (e ErrSkip) Error() string   { return e.Err.Error() }
func (e ErrSkip) Unwrap() error   { return e.Err }

// ErrInput indicates the error happens in passing Upstream's Output to Downstream's Input.
//
//	Input(func(ctx context.Context, s *SomeStep) error {
//		return err
//	})
type ErrInput struct {
	Err error
	To  Steper
}

func (e *ErrInput) Error() string {
	return fmt.Sprintf("ErrInput(%s): %s", e.To, e.Err)
}
func (e *ErrInput) Unwrap() error { return e.Err }

type StatusErr struct {
	Status StepStatus
	Err    error
}

func (e *StatusErr) Error() string { return fmt.Sprintf("[%s] %s", e.Status, e.Err) }
func (e *StatusErr) Unwrap() error { return e.Err }

// ErrWorkflow contains all errors of Steps in a Workflow.
type ErrWorkflow map[Steper]*StatusErr

func (e ErrWorkflow) Error() string {
	builder := new(strings.Builder)
	for step, serr := range e {
		if serr != nil {
			builder.WriteString(fmt.Sprintf("%s [%s]: %s\n", step, serr.Status, serr.Err))
		}
	}
	return builder.String()
}
func (e ErrWorkflow) IsNil() bool {
	for _, sErr := range e {
		if sErr != nil && sErr.Err != nil {
			return false
		}
	}
	return true
}

var ErrWorkflowIsRunning = fmt.Errorf("Workflow is running, please wait for it terminated")
var ErrWorkflowHasRun = fmt.Errorf("Workflow has run, check result error via .Err()")

// Only when the Step status is not StepStautsPending when Workflow starts to run.
type ErrUnexpectStepInitStatus map[Steper]StepStatus

func (e ErrUnexpectStepInitStatus) Error() string {
	builder := new(strings.Builder)
	builder.WriteString("Unexpect Step initial status:")
	for step, status := range e {
		builder.WriteRune('\n')
		builder.WriteString(fmt.Sprintf(
			"%s [%s]",
			step, status,
		))
	}
	return builder.String()
}

// There is a cycle-dependency in your Workflow!!!
type ErrCycleDependency map[Steper][]Steper

func (e ErrCycleDependency) Error() string {
	builder := new(strings.Builder)
	builder.WriteString("Cycle Dependency Error:")
	for step, ups := range e {
		depsStr := []string{}
		for _, up := range ups {
			depsStr = append(depsStr, fmt.Sprintf("%s", up))
		}
		builder.WriteRune('\n')
		builder.WriteString(fmt.Sprintf(
			"%s: [%s]",
			step, strings.Join(depsStr, ", "),
		))
	}
	return builder.String()
}
