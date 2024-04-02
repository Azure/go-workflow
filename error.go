package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
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
type ErrBefore struct{ error }

func (e ErrSucceed) Unwrap() error                { return e.error }
func (e ErrCancel) Unwrap() error                 { return e.error }
func (e ErrSkip) Unwrap() error                   { return e.error }
func (e ErrPanic) Unwrap() error                  { return e.error }
func (e ErrBefore) Unwrap() error                 { return e.error }
func (e ErrSucceed) MarshalJSON() ([]byte, error) { return DecorateErrorJSON(e.error, nil) }
func (e ErrCancel) MarshalJSON() ([]byte, error)  { return DecorateErrorJSON(e.error, nil) }
func (e ErrSkip) MarshalJSON() ([]byte, error)    { return DecorateErrorJSON(e.error, nil) }
func (e ErrPanic) MarshalJSON() ([]byte, error)   { return DecorateErrorJSON(e.error, nil) }
func (e ErrBefore) MarshalJSON() ([]byte, error)  { return DecorateErrorJSON(e.error, nil) }

// WithStackTraces saves stack frames into error
func WithStackTraces(skip, depth int, ignores ...func(runtime.Frame) bool) func(error) error {
	return func(err error) error {
		pc := make([]uintptr, depth)
		i := runtime.Callers(skip, pc)
		pc = pc[:i]
		frames := runtime.CallersFrames(pc)
		withStackTraces := ErrWithStackTraces{Err: err}
		for {
			frame, more := frames.Next()
			if !more {
				break
			}
			isIgnored := false
			for _, ignore := range ignores {
				if ignore(frame) {
					isIgnored = true
					break
				}
			}
			if !isIgnored {
				withStackTraces.Frames = append(withStackTraces.Frames, frame)
			}
		}
		return withStackTraces
	}
}

// DecorateErrorJSON decorates error with extra fields in JSON format.
func DecorateErrorJSON(rerr error, marshal func() ([]byte, error)) ([]byte, error) {
	var errBytes []byte
	if errMarshal, ok := rerr.(json.Marshaler); ok {
		var err error
		errBytes, err = errMarshal.MarshalJSON()
		if err != nil {
			return nil, err
		}
	} else {
		errBytes = []byte(fmt.Sprintf(`{"error":"%s"}`, rerr.Error()))
	}
	if marshal == nil {
		return errBytes, nil
	}
	mBytes, err := marshal()
	if err != nil {
		return nil, err
	}
	return mergeJSON(errBytes, mBytes), nil
}

func mergeJSON(objs ...[]byte) []byte {
	objsNoBrace := make([][]byte, 0, len(objs))
	for _, obj := range objs {
		if len(obj) > 1 {
			if objNoBrace := obj[1 : len(obj)-1]; len(objNoBrace) > 0 {
				objsNoBrace = append(objsNoBrace, objNoBrace)
			}
		}
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	buf.Write(bytes.Join(objsNoBrace, []byte{','}))
	buf.WriteByte('}')
	return buf.Bytes()
}

// ErrWithStackTraces saves stack frames into error, and prints error into
//
//	error message
//
//	Stack Traces:
//		file:line
type ErrWithStackTraces struct {
	Err    error
	Frames []runtime.Frame
}

func (e ErrWithStackTraces) Unwrap() error { return e.Err }
func (e ErrWithStackTraces) Error() string {
	if st := e.StackTraces(); len(st) > 0 {
		return fmt.Sprintf("%s\n\nStack Traces:\n\t%s\n", e.Err, strings.Join(st, "\n\t"))
	}
	return e.Err.Error()
}
func (e ErrWithStackTraces) MarshalJSON() ([]byte, error) {
	return DecorateErrorJSON(e.Err, func() ([]byte, error) {
		return json.Marshal(struct {
			StackTraces []string `json:"stack_traces,omitempty"`
		}{
			StackTraces: e.StackTraces(),
		})
	})
}
func (e ErrWithStackTraces) StackTraces() []string {
	stacks := make([]string, 0, len(e.Frames))
	for i := range e.Frames {
		stacks = append(stacks, fmt.Sprintf("%s:%d", e.Frames[i].File, e.Frames[i].Line))
	}
	return stacks
}

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
	Status StepStatus `json:"status"`
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
	return DecorateErrorJSON(e.Err, func() ([]byte, error) {
		return json.Marshal(struct {
			Status StepStatus `json:"status"`
		}{
			Status: e.Status,
		})
	})
}

// ErrWorkflow contains all errors reported from terminated Steps in Workflow.
//
// Keys are root Steps, values are its status and error.
type ErrWorkflow map[Steper]StatusError

func (e ErrWorkflow) Unwrap() []error {
	rv := make([]error, 0, len(e))
	for _, sErr := range e {
		rv = append(rv, sErr.Err)
	}
	return rv
}

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

// ErrCycleDependency means there is a cycle-dependency in your Workflow!!!
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
