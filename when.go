package flow

import (
	"context"
	"errors"
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
	return s == Failed || s == Succeeded || s == Canceled || s == Skipped
}

func (s StepStatus) String() string {
	switch s {
	case Pending:
		return "Pending"
	case Running, Failed, Succeeded, Canceled, Skipped:
		return string(s)
	default:
		return "Unknown"
	}
}

// When is a function to determine what's the next status of Step.
// When makes the decision based on the status and result of all the Upstream Steps.
// When is only called when all Upstreams are terminated.
type When func(ctx context.Context, ups map[Steper]StatusErr) StepStatus

var (
	DefaultWhen       When = AllSucceeded
	DefaultIsCanceled      = func(err error) bool {
		switch {
		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded),
			errors.Is(err, ErrCancel{}):
			return true
		}
		return false
	}
)

// Always: as long as all Upstreams are terminated
func Always(context.Context, map[Steper]StatusErr) StepStatus {
	return Running
}

// AllSucceeded: all Upstreams are Succeeded
func AllSucceeded(ctx context.Context, ups map[Steper]StatusErr) StepStatus {
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

// BeCanceled: only run when the workflow is canceled
func BeCanceled(ctx context.Context, ups map[Steper]StatusErr) StepStatus {
	if DefaultIsCanceled(ctx.Err()) {
		return Running
	}
	return Skipped
}

// AnyFailed: any Upstream is Failed
func AnyFailed(ctx context.Context, ups map[Steper]StatusErr) StepStatus {
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
