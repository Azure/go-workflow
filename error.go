package flow

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Succeed wraps err in an ErrSucceed so that StatusFromError will classify
// the resulting error as Succeeded. Use it when your step has reportable
// information to bubble up but you still want the step counted as a success.
func Succeed(err error) ErrSucceed { return ErrSucceed{err} }

// Cancel wraps err in an ErrCancel so the step is classified as Canceled.
func Cancel(err error) ErrCancel { return ErrCancel{err} }

// Skip wraps err in an ErrSkip so the step is classified as Skipped.
func Skip(err error) ErrSkip { return ErrSkip{err} }

// Status-marker errors. They behave like ordinary error wrappers (Unwrap
// returns the underlying error) but additionally tell StatusFromError which
// terminal StepStatus to assign:
//
//   - ErrSucceed   → Succeeded
//   - ErrCancel    → Canceled
//   - ErrSkip      → Skipped
//   - ErrPanic     → Failed (only ever produced when Workflow.DontPanic is true)
//   - ErrBeforeStep→ Failed (the failure happened in a Before/Input callback,
//     not in Do itself)
type ErrSucceed struct{ error }
type ErrCancel struct{ error }
type ErrSkip struct{ error }
type ErrPanic struct{ error }
type ErrBeforeStep struct{ error }

func (e ErrSucceed) Unwrap() error    { return e.error }
func (e ErrCancel) Unwrap() error     { return e.error }
func (e ErrSkip) Unwrap() error       { return e.error }
func (e ErrPanic) Unwrap() error      { return e.error }
func (e ErrBeforeStep) Unwrap() error { return e.error }

// WithStackTraces returns a wrapper that captures up to `depth` runtime
// frames (skipping the topmost `skip` frames) and attaches them to err as an
// ErrWithStackTraces. Frames matched by any `ignores` predicate are dropped.
//
// catchPanicAsError uses this to enrich panic errors with a filtered stack.
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

// ErrWithStackTraces decorates an error with the runtime frames that were
// active when WithStackTraces was applied. Its Error() formatting is:
//
//	<inner error message>
//
//	Stack Traces:
//	    file:line
//	    file:line
//	    ...
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

// StackTraces renders each captured frame as "file:line".
func (e ErrWithStackTraces) StackTraces() []string {
	stacks := make([]string, 0, len(e.Frames))
	for i := range e.Frames {
		stacks = append(stacks, fmt.Sprintf("%s:%d", e.Frames[i].File, e.Frames[i].Line))
	}
	return stacks
}

// StatusFromError classifies an error into a terminal StepStatus.
//
//   - nil                                              → Succeeded
//   - any error wrapping (via Unwrap) ErrSucceed/Cancel/Skip → that status
//   - anything else                                    → Failed
//
// Note: context.Canceled / context.DeadlineExceeded are NOT translated here —
// the worker in workflow.go applies that policy after consulting
// DefaultIsCanceled, so the per-step Status ends up Canceled for cancellation
// errors even if StatusFromError reported Failed.
func StatusFromError(err error) StepStatus {
	if err == nil {
		return Succeeded
	}
	for {
		switch typedErr := err.(type) {
		case ErrSucceed:
			return Succeeded
		case ErrCancel:
			return Canceled
		case ErrSkip:
			return Skipped
		case interface{ Unwrap() error }:
			err = typedErr.Unwrap()
		default:
			return Failed
		}
	}
}

// StepResult is the public terminal record of a single step's run: its final
// status, the last error observed (may be nil for Succeeded), and the wall
// clock time the step finished. FinishedAt is zero if the step never ran.
type StepResult struct {
	Status     StepStatus
	Err        error
	FinishedAt time.Time
}

// Error renders a StepResult as:
//
//	[Status]
//	    error message
//
// (with the error message indented).
func (e StepResult) Error() string {
	rv := fmt.Sprintf("[%s]", e.Status)
	if e.Err != nil {
		rv += "\n\t" + indent(e.Err.Error())
	}
	return rv
}
func (e StepResult) Unwrap() error { return e.Err }

// indent rewrites any inner newlines so multi-line errors stay aligned under
// the leading status tag.
func indent(s string) string { return strings.ReplaceAll(s, "\n", "\n\t") }

// ErrWorkflow is the error returned by Workflow.Do when one or more steps did
// not finish in a Succeeded (or Skipped, depending on SkipAsError) state. It
// is keyed by ROOT step (composite-step internals are folded into their
// containing root).
type ErrWorkflow map[Steper]StepResult

// sortedSteps orders the keys of an ErrWorkflow for stable rendering:
// finished steps first (oldest FinishedAt first), then never-ran steps; ties
// are broken by String(step). This makes Error() output reproducible across
// runs even though the underlying map iteration order is randomized.
func sortedSteps(e ErrWorkflow) []Steper {
	steps := make([]Steper, 0, len(e))
	for step := range e {
		steps = append(steps, step)
	}
	sort.Slice(steps, func(i, j int) bool {
		ti := e[steps[i]].FinishedAt
		tj := e[steps[j]].FinishedAt
		zeroI := ti.IsZero()
		zeroJ := tj.IsZero()
		if zeroI != zeroJ {
			return !zeroI // non-zero (finished) before zero (never ran)
		}
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return String(steps[i]) < String(steps[j])
	})
	return steps
}

// Unwrap returns the per-step errors in deterministic order so errors.Is /
// errors.As can search through them.
func (e ErrWorkflow) Unwrap() []error {
	steps := sortedSteps(e)
	rv := make([]error, 0, len(e))
	for _, step := range steps {
		if err := e[step].Err; err != nil {
			rv = append(rv, err)
		}
	}
	return rv
}

// Error renders an ErrWorkflow as a deterministic, multi-line dump:
//
//	step1: [Status]
//	    error message
//	step2: [Status]
//	    error message
//	...
func (e ErrWorkflow) Error() string {
	var builder strings.Builder
	for _, step := range sortedSteps(e) {
		fmt.Fprintf(&builder, "%s: ", String(step))
		fmt.Fprintln(&builder, e[step].Error())
	}
	return builder.String()
}

// AllSucceeded reports whether every step ended in Succeeded.
func (e ErrWorkflow) AllSucceeded() bool {
	for _, sErr := range e {
		if sErr.Status != Succeeded {
			return false
		}
	}
	return true
}

// AllSucceededOrSkipped reports whether every step ended in Succeeded or
// Skipped. (Skipped steps may still carry an Err describing why they were
// skipped — this method ignores that.)
func (e ErrWorkflow) AllSucceededOrSkipped() bool {
	for _, sErr := range e {
		switch sErr.Status {
		case Succeeded, Skipped:
		default:
			return false
		}
	}
	return true
}

// ErrWorkflowIsRunning is returned by Workflow.Do (and Workflow.Reset) when
// the workflow is already executing in another goroutine. The workflow is
// single-runner: wait for the in-flight Do to return before invoking again.
var ErrWorkflowIsRunning = fmt.Errorf("Workflow is running, please wait for it terminated")

// ErrCycleDependency is returned by Workflow.Do's preflight check when the
// declared graph isn't acyclic. It maps each step still in a cycle to the
// upstream step(s) that prevented it from being topologically scanned.
type ErrCycleDependency map[Steper][]Steper

// Error renders an ErrCycleDependency as:
//
//	Cycle Dependency Error:
//	    stepA depends on [
//	        stepB
//	    ]
//	    ...
func (e ErrCycleDependency) Error() string {
	depErr := make([]string, 0, len(e))
	for step, ups := range e {
		depsStr := []string{}
		for _, up := range ups {
			depsStr = append(depsStr, String(up))
		}
		depErr = append(depErr, fmt.Sprintf(
			"%s depends on [\n\t%s\n]",
			String(step), indent(strings.Join(depsStr, "\n")),
		))
	}
	return fmt.Sprintf("Cycle Dependency Error:\n\t%s", indent(strings.Join(depErr, "\n")))
}
