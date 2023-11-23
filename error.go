package flow

import (
	"fmt"
	"strings"
)

// Cancel will mark the current step `Canceled`.
func Cancel(err error) ErrCancel { return ErrCancel{Err: err} }

// Skip will mark the current step `Skipped`.
func Skip(err error) ErrSkip { return ErrSkip{Err: err} }

type ErrCancel struct{ Err error }
type ErrSkip struct{ Err error }

func (e ErrCancel) Error() string { return e.Err.Error() }
func (e ErrCancel) Unwrap() error { return e.Err }
func (e ErrSkip) Error() string   { return e.Err.Error() }
func (e ErrSkip) Unwrap() error   { return e.Err }

// ErrInput indicates the error happens in feeding input to a Step.
//
//	Input(func(ctx context.Context, s *SomeStep) error {
//		return err
//	})
//
// or
//
//	Input(func(_ context.Context, _ *SomeStep) error {
//		panic(err)
//	})
type ErrInput struct {
	Err error
	To  Steper
}

func (e ErrInput) Error() string { return fmt.Sprintf("ErrInput(%s): %v", String(e.To), e.Err) }
func (e ErrInput) Unwrap() error { return e.Err }

// StatusError tracks the status and error of a Step.
type StatusError struct {
	Status StepStatus
	Err    error
}

func (e StatusError) Error() string { return fmt.Sprintf("[%s] %v", e.Status, e.Err) }
func (e StatusError) Unwrap() error { return e.Err }

// ErrWorkflow contains all errors reported from terminated Steps.
type ErrWorkflow map[Steper]StatusError

func (e ErrWorkflow) Error() string {
	builder := new(strings.Builder)
	for step, serr := range e {
		builder.WriteString(fmt.Sprintf("%s: ", String(step)))
		builder.WriteString(fmt.Sprintln(serr))
	}
	return builder.String()
}
func (e ErrWorkflow) Unwrap() []error {
	rv := []error{}
	for _, v := range e {
		rv = append(rv, v.Err)
	}
	return rv
}
func (e ErrWorkflow) IsNil() bool {
	for _, sErr := range e {
		if sErr.Err != nil {
			return false
		}
	}
	return true
}

var ErrWorkflowIsRunning = fmt.Errorf("Workflow is running, please wait for it terminated")
var ErrWorkflowHasRun = fmt.Errorf("Workflow has run, check result error via .Err()")

// Step status is not Pending when Workflow starts to run.
type ErrUnexpectStepInitStatus map[Steper]StepStatus

func (e ErrUnexpectStepInitStatus) Error() string {
	builder := new(strings.Builder)
	builder.WriteString("Unexpect Step initial status:")
	for step, status := range e {
		builder.WriteRune('\n')
		builder.WriteString(fmt.Sprintf(
			"%s [%s]",
			String(step), status,
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
			depsStr = append(depsStr, String(up))
		}
		builder.WriteRune('\n')
		builder.WriteString(fmt.Sprintf(
			"%s: [%s]",
			String(step), strings.Join(depsStr, ", "),
		))
	}
	return builder.String()
}
