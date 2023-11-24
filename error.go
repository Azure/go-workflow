package flow

import (
	"encoding/json"
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

// StatusError tracks the status and error of a Step.
type StatusError struct {
	Status StepStatus `json:"status"`
	Err    error      `json:"error"`
}

func (e StatusError) Error() string {
	rv := fmt.Sprintf("[%s]", e.Status)
	if e.Err != nil {
		rv += "\n\t" + strings.ReplaceAll(e.Err.Error(), "\n", "\n\t")
	} else {
		rv += " <nil>"
	}
	return rv
}
func (e StatusError) Unwrap() error { return e.Err }
func (e StatusError) MarshalJSON() ([]byte, error) {
	switch e.Err.(type) {
	case interface{ MarshalJSON() ([]byte, error) }:
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

// ErrWorkflow contains all errors reported from terminated Steps.
type ErrWorkflow map[Steper]StatusError

func (e ErrWorkflow) Error() string {
	builder := new(strings.Builder)
	for step, serr := range e {
		builder.WriteString(fmt.Sprintf("%s: ", String(step)))
		builder.WriteString(fmt.Sprintln(serr.Error()))
	}
	return builder.String()
}
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
