package flow

import (
	"context"
	"errors"
	"fmt"
)

// StepStatus describes the status of a Step.
type StepStatus string

const (
	Pending   StepStatus = ""
	Running   StepStatus = "Running"
	Failed    StepStatus = "Failed"
	Succeeded StepStatus = "Succeeded"
	Canceled  StepStatus = "Canceled"
	Skipped   StepStatus = "Skipped"
)

func (s StepStatus) IsTerminated() bool {
	switch s {
	case Failed, Succeeded, Canceled, Skipped:
		return true
	}
	return false
}

func (s StepStatus) String() string {
	switch s {
	case Pending:
		return "Pending"
	case Running, Failed, Succeeded, Canceled, Skipped:
		return string(s)
	default:
		return fmt.Sprintf("Unknown(%s)", string(s))
	}
}

// Condition is a function to determine what's the next status of Step.
// Condition makes the decision based on the status and result of all the Upstream Steps.
// Condition is only called when all Upstream Steps are terminated.
type Condition func(ctx context.Context, ups map[Steper]StepResult) StepStatus

var (
	// DefaultCondition used in workflow, defaults to AllSucceeded
	DefaultCondition Condition = AllSucceeded
	// DefaultIsCanceled is used to determine whether an error is being regarded as canceled.
	DefaultIsCanceled = func(err error) bool {
		switch {
		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded),
			StatusFromError(err) == Canceled:
			return true
		}
		return false
	}
)

// Always: as long as all Upstreams are terminated
func Always(context.Context, map[Steper]StepResult) StepStatus {
	return Running
}

// AllSucceeded: all Upstreams are Succeeded
func AllSucceeded(ctx context.Context, ups map[Steper]StepResult) StepStatus {
	if DefaultIsCanceled(ctx.Err()) {
		return Canceled
	}
	for _, up := range ups {
		if up.Status != Succeeded {
			return Skipped
		}
	}
	return Running
}

// AnySucceeded: any Upstream is Succeeded
func AnySucceeded(ctx context.Context, ups map[Steper]StepResult) StepStatus {
	if DefaultIsCanceled(ctx.Err()) {
		return Canceled
	}
	for _, up := range ups {
		if up.Status == Succeeded {
			return Running
		}
	}
	return Skipped
}

// AllSucceededOrSkipped: all Upstreams are Succeeded or Skipped
func AllSucceededOrSkipped(ctx context.Context, ups map[Steper]StepResult) StepStatus {
	if DefaultIsCanceled(ctx.Err()) {
		return Canceled
	}
	for _, up := range ups {
		if up.Status != Succeeded && up.Status != Skipped {
			return Skipped
		}
	}
	return Running
}

// BeCanceled: only run when the workflow is canceled
func BeCanceled(ctx context.Context, ups map[Steper]StepResult) StepStatus {
	if DefaultIsCanceled(ctx.Err()) {
		return Running
	}
	return Skipped
}

// AnyFailed: any Upstream is Failed
func AnyFailed(ctx context.Context, ups map[Steper]StepResult) StepStatus {
	if DefaultIsCanceled(ctx.Err()) {
		return Canceled
	}
	for _, up := range ups {
		if up.Status == Failed {
			return Running
		}
	}
	return Skipped
}

// ConditionOrDefault will use DefaultCondition if cond is nil.
func ConditionOrDefault(cond Condition) func(context.Context, map[Steper]StepResult) StepStatus {
	return func(ctx context.Context, ups map[Steper]StepResult) StepStatus {
		if cond == nil {
			return DefaultCondition(ctx, ups)
		}
		return cond(ctx, ups)
	}
}
