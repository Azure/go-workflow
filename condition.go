package flow

import (
	"context"
	"errors"
	"fmt"
)

// StepStatus describes the lifecycle state of a Step inside a Workflow.
//
// A step starts at Pending, is moved to Running by the scheduler when it is
// dispatched to a goroutine, and ends in one of the four terminal states:
// Failed, Succeeded, Canceled or Skipped. Use IsTerminated() to test for
// "done".
//
// Steps that the scheduler decides not to run (Skipped / Canceled, settled
// inline by the Condition check) move from Pending straight to a terminal
// state without ever entering Running.
type StepStatus string

const (
	Pending   StepStatus = ""          // not yet started.
	Running   StepStatus = "Running"   // currently executing in a worker goroutine.
	Failed    StepStatus = "Failed"    // terminal: Do (or a callback) returned a non-nil error.
	Succeeded StepStatus = "Succeeded" // terminal: Do returned nil.
	Canceled  StepStatus = "Canceled"  // terminal: ctx was canceled, or the error wraps context.Canceled / DeadlineExceeded.
	Skipped   StepStatus = "Skipped"   // terminal: the Condition decided not to run the step.
)

// IsTerminated reports whether the status is one of Failed, Succeeded,
// Canceled or Skipped. The Workflow tick loop polls this to decide when
// downstream steps may be considered.
func (s StepStatus) IsTerminated() bool {
	switch s {
	case Failed, Succeeded, Canceled, Skipped:
		return true
	}
	return false
}

// String renders the status for logs / errors. Pending is rendered as
// "Pending" rather than the empty string, and unknown values are tagged so
// they stand out.
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

// Condition decides what should happen to a step once all of its upstreams
// have terminated. It returns the next StepStatus:
//
//   - Running                   → the scheduler will dispatch the step to a worker.
//   - Skipped / Canceled / etc. → the scheduler settles the step inline, with
//     no goroutine, no concurrency lease, and no interceptor chain.
//
// The map passed in keys every direct upstream by its root Steper and exposes
// its terminal StepResult. The condition is invoked with the workflow's
// context, so it can also observe ctx.Err() to react to a top-level cancel.
type Condition func(ctx context.Context, ups map[Steper]StepResult) StepStatus

var (
	// DefaultCondition is the Condition used when a step doesn't set its own
	// via When(). Defaults to AllSucceeded — i.e. a step runs only if every
	// upstream succeeded.
	DefaultCondition Condition = AllSucceeded

	// DefaultIsCanceled classifies an error as a "cancellation" rather than a
	// "failure". The built-in conditions and the worker's terminal-status
	// computation both consult this hook. Override it to recognize your own
	// cancellation sentinels.
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

// Always runs the step regardless of upstream outcomes (as long as every
// upstream is terminated, which the scheduler guarantees before calling).
func Always(context.Context, map[Steper]StepResult) StepStatus {
	return Running
}

// AllSucceeded runs the step only when every upstream succeeded. If any
// upstream is in a non-Succeeded terminal state the step becomes Skipped. If
// the workflow context is already canceled, the step becomes Canceled.
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

// AnySucceeded runs the step as soon as at least one upstream succeeded;
// otherwise it is Skipped. Canceled context still wins.
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

// AllSucceededOrSkipped tolerates Skipped upstreams: the step runs as long as
// no upstream is Failed or Canceled. Canceled context still wins.
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

// BeCanceled inverts the usual "context cancel skips me" rule: this step runs
// only when the workflow context is already canceled, otherwise it is
// Skipped. Useful for cleanup steps that should fire only when the workflow
// is being torn down.
func BeCanceled(ctx context.Context, _ map[Steper]StepResult) StepStatus {
	if DefaultIsCanceled(ctx.Err()) {
		return Running
	}
	return Skipped
}

// AnyFailed runs the step when at least one upstream failed; otherwise it is
// Skipped. Useful for "on failure" branches. Canceled context still wins.
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

// ConditionOr returns cond unchanged, or defaultCond if cond is nil. Lets
// callers compose conditions without a nil check.
func ConditionOr(cond, defaultCond Condition) Condition {
	return func(ctx context.Context, ups map[Steper]StepResult) StepStatus {
		if cond == nil {
			return defaultCond(ctx, ups)
		}
		return cond(ctx, ups)
	}
}

// ConditionOrDefault is ConditionOr with the package-level DefaultCondition.
func ConditionOrDefault(cond Condition) Condition { return ConditionOr(cond, DefaultCondition) }
