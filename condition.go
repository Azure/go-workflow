package workflow

import (
	"context"
)

// StepStatus describes the status of a Step.
//
// The relations between StepStatus, Condition and When are:
//
//													  /--false-> StepStatusCanceled
//	StepStatusPending -> [When] ---true--> [Condition] --true--> StepStatusRunning --err == nil--> StepStatusSucceeded
//								\--false-> StepStatusSkipped					   \--err != nil--> StepStatusFailed
type StepStatus string

const (
	StepStatusPending   StepStatus = ""
	StepStatusRunning   StepStatus = "Running"
	StepStatusFailed    StepStatus = "Failed"
	StepStatusSucceeded StepStatus = "Succeeded"
	StepStatusCanceled  StepStatus = "Canceled"
	StepStatusSkipped   StepStatus = "Skipped" // Skipped will be propagated to all downstreams
)

func (s StepStatus) IsTerminated() bool {
	return s == StepStatusFailed || s == StepStatusSucceeded || s == StepStatusCanceled || s == StepStatusSkipped
}

func (s StepStatus) String() string {
	switch s {
	case StepStatusPending:
		return "Pending"
	case StepStatusRunning, StepStatusFailed, StepStatusSucceeded, StepStatusCanceled, StepStatusSkipped:
		return string(s)
	default:
		return "Unknown"
	}
}

// Condition is a function to determine whether the Step should be Canceled, false -> Canceled.
// Condition makes the decision based on the status of all the Upstream Steps.
// Condition is only called when all Upstreams are terminated.
type Condition func(ups []StepReader) bool

var DefaultCondition Condition = Succeeded

// Always: as long as all Upstreams are terminated
func Always(deps []StepReader) bool {
	return true
}

// Succeeded: all Upstreams are Succeeded
func Succeeded(ups []StepReader) bool {
	for _, e := range ups {
		switch e.GetStatus() {
		case StepStatusFailed, StepStatusCanceled:
			return false
		}
	}
	return true
}

// Failed: any Upstream is Failed
func Failed(ups []StepReader) bool {
	hasFailed := false
	for _, e := range ups {
		switch e.GetStatus() {
		case StepStatusFailed:
			hasFailed = true
		case StepStatusCanceled:
			return false
		}
	}
	return hasFailed
}

// SucceededOrFailed: all Upstreams are Succeeded or Failed (or Skipped)
func SucceededOrFailed(deps []StepReader) bool {
	for _, dep := range deps {
		switch dep.GetStatus() {
		case StepStatusCanceled:
			return false
		}
	}
	return true
}

// Canceled: any one Upstream is Canceled
func Canceled(ups []StepReader) bool {
	for _, up := range ups {
		switch up.GetStatus() {
		case StepStatusCanceled:
			return true
		}
	}
	return false
}

// When is a function to determine whether the Step should be Skipped.
// When makes the decesion according to the context and environment, so it's an arbitrary function.
// When is called after Condition.
type When func(context.Context) bool

var DefaultWhenFunc = When(func(context.Context) bool {
	return true
})

// Skip: this step will always be Skipped
func Skip(context.Context) bool {
	return false
}
