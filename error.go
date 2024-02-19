package flow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Succeed marks the current step as `Succeeded`, while still reports the error.
func Succeed(err error) ErrSucceed { return ErrSucceed{err} }

// Cancel marks the current step as `Canceled`, and reports the error.
func Cancel(err error) ErrCancel { return ErrCancel{err} }

// Skip marks the current step as `Skipped`, and reports the error.
func Skip(err error) ErrSkip { return ErrSkip{err} }

type ErrSucceed struct{ error }
type ErrCancel struct{ error }
type ErrSkip struct{ error }
type ErrPanic struct{ error }
type ErrInput struct{ error }

func (e ErrSucceed) Unwrap() error { return e.error }
func (e ErrCancel) Unwrap() error  { return e.error }
func (e ErrSkip) Unwrap() error    { return e.error }
func (e ErrPanic) Unwrap() error   { return e.error }
func (e ErrInput) Unwrap() error   { return e.error }

// StatusFromError gets the StepStatus from error.
func StatusFromError(err error) StepStatus {
	if err == nil {
		return Succeeded
	}
	switch typedErr := err.(type) {
	case ErrSucceed:
		return Succeeded
	case ErrCancel:
		return Canceled
	case ErrSkip:
		return Skipped
	case interface{ Unwrap() error }:
		return StatusFromError(typedErr.Unwrap())
	default:
		return Failed
	}
}

// StatusError contains the status and error of a Step.
type StatusError struct {
	Status StepStatus
	Err    error
}

// StatusError will be printed as:
//
//	[Status]
//		error message
func (e StatusError) Error() string {
	rv := fmt.Sprintf("[%s]", e.Status)
	if e.Err != nil {
		rv += "\n\t" + strings.ReplaceAll(e.Err.Error(), "\n", "\n\t")
	}
	return rv
}
func (e StatusError) Unwrap() error { return e.Err }

// MarshalJSON allows us to marshal StatusError to json.
//
//	{
//		"status": "Status",
//		"error": "error message"
//	}
func (e StatusError) MarshalJSON() ([]byte, error) {
	switch e.Err.(type) {
	case interface{ MarshalJSON() ([]byte, error) }:
		// new an anonymous struct to avoid stack overflow
		return json.Marshal(struct {
			Status StepStatus `json:"status"`
			Err    error      `json:"error"`
		}{
			Status: e.Status,
			Err:    e.Err,
		})
	default:
		rv := struct {
			Status StepStatus `json:"status"`
			Err    *string    `json:"error"`
		}{
			Status: e.Status,
		}
		if e.Err != nil {
			err := e.Err.Error()
			rv.Err = &err
		}
		return json.Marshal(rv)
	}
}

// ErrWorkflow contains all errors reported from terminated Steps in Workflow.
//
// Keys are root Steps, values are its status and error.
type ErrWorkflow map[Steper]StatusError

// ErrWorkflow will be printed as:
//
//	Step: [Status]
//		error message
func (e ErrWorkflow) Error() string {
	var builder strings.Builder
	for step, serr := range e {
		builder.WriteString(fmt.Sprintf("%s: ", String(step)))
		builder.WriteString(fmt.Sprintln(serr.Error()))
	}
	return builder.String()
}

// MarshalJSON allows us to marshal ErrWorkflow to json.
//
//	{
//		"Step": {
//			"status": "Status",
//			"error": "error message"
//		}
//	}
func (e ErrWorkflow) MarshalJSON() ([]byte, error) {
	rv := make(map[string]StatusError)
	for step, sErr := range e {
		rv[String(step)] = sErr
	}
	return json.Marshal(rv)
}
func (e ErrWorkflow) Unwrap() []error {
	rv := []error{}
	for _, v := range e {
		rv = append(rv, v.Err)
	}
	return rv
}
func (e ErrWorkflow) AllSucceeded() bool {
	for _, sErr := range e {
		if sErr.Status != Succeeded {
			return false
		}
	}
	return true
}
func (e ErrWorkflow) AllSucceededOrSkipped() bool {
	for _, sErr := range e {
		switch sErr.Status {
		case Succeeded, Skipped: // skipped step can have error to indicate why it's skipped
		default:
			return false
		}
	}
	return true
}

var ErrWorkflowIsRunning = fmt.Errorf("Workflow is running, please wait for it terminated")

// Step status is not Pending when Workflow starts to run.
type ErrUnexpectStepInitStatus map[Steper]StepStatus

func (e ErrUnexpectStepInitStatus) Error() string {
	var builder strings.Builder
	builder.WriteString("Unexpected Step Initial Status:")
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
	var builder strings.Builder
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
