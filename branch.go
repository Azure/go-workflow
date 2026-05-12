package flow

import (
	"context"
)

// BranchCheckFunc inspects target after it has run and decides whether the
// branch this check guards should be selected. Returning a non-nil error
// fails the selected branch's step (delivered as the BeforeStep error).
type BranchCheckFunc[T Steper] func(context.Context, T) (bool, error)

// If wires a target step plus a Then/Else branch into the workflow:
//
//	If(target, func(ctx context.Context, target *Target) (bool, error) {
//	    // true  -> run the Then branch.
//	    // false -> run the Else branch.
//	    // err   -> the selected branch step fails with this error.
//	}).
//	    Then(thenStep).
//	    Else(elseStep)
//
// All Then/Else steps depend on target and on the check's outcome — they
// won't be considered until target has terminated.
func If[T Steper](target T, check BranchCheckFunc[T]) *IfBranch[T] {
	return &IfBranch[T]{Target: target, BranchCheck: BranchCheck[T]{Check: check}}
}

// IfBranch is the configurable If(...) builder. It registers:
//
//   - the Target step (with an AfterStep that runs the BranchCheck after
//     Target.Do has completed),
//   - all Then steps, gated by a Condition that fires only when
//     BranchCheck.OK == true,
//   - all Else steps, gated by the inverse Condition,
//   - a shared BeforeStep on every branch step that surfaces a non-nil
//     BranchCheck.Error as the step's failure.
type IfBranch[T Steper] struct {
	Target      T // the step whose result the branch check inspects.
	BranchCheck BranchCheck[T]
	ThenStep    []Steper
	ElseStep    []Steper
	// Cond is layered IN ADDITION to the branch check: it applies to BOTH
	// ThenStep and ElseStep — NOT to Target. Defaults to DefaultCondition.
	Cond Condition
}

// Then appends step(s) to the Then branch.
func (i *IfBranch[T]) Then(th ...Steper) *IfBranch[T] {
	i.ThenStep = append(i.ThenStep, th...)
	return i
}

// Else appends step(s) to the Else branch.
func (i *IfBranch[T]) Else(el ...Steper) *IfBranch[T] {
	i.ElseStep = append(i.ElseStep, el...)
	return i
}

// When sets the upstream-evaluation Condition applied to ALL Then and Else
// steps. It is composed with the branch check (both must allow the step to
// run). Default is DefaultCondition.
//
// NOTE: this does NOT affect the Target step.
func (i *IfBranch[T]) When(cond Condition) *IfBranch[T] {
	i.Cond = cond
	return i
}

// isThen returns the Condition for either the Then branch (isThen=true) or
// the Else branch (isThen=false): first defer to i.Cond on the upstreams,
// then accept only if the BranchCheck's OK matches the requested side.
func (i *IfBranch[T]) isThen(isThen bool) Condition {
	return func(ctx context.Context, ups map[Steper]StepResult) StepStatus {
		if status := ConditionOrDefault(i.Cond)(ctx, ups); status != Running {
			return status
		}
		if i.BranchCheck.OK == isThen {
			return Running
		}
		return Skipped
	}
}

// AddToWorkflow lays the If(...) construct into a {step→config} map. It
// chains: Target (with AfterStep running the branch check), Then branch
// (gated by isThen(true)), Else branch (gated by isThen(false)), and a
// shared BeforeStep on every branch step that propagates BranchCheck.Error
// as the step's failure.
func (i *IfBranch[T]) AddToWorkflow() map[Steper]*StepConfig {
	return Steps().Merge(
		Steps(i.Target).AfterStep(func(ctx context.Context, s Steper, err error) error {
			i.BranchCheck.Do(ctx, i.Target)
			return err
		}),
		Steps(i.ThenStep...).When(i.isThen(true)),
		Steps(i.ElseStep...).When(i.isThen(false)),
		Steps(append(append([]Steper{},
			i.ThenStep...), i.ElseStep...,
		)...).
			DependsOn(i.Target).
			BeforeStep(func(ctx context.Context, s Steper) (context.Context, error) {
				if i.BranchCheck.Error != nil {
					return ctx, i.BranchCheck.Error
				}
				return ctx, nil
			}),
	).AddToWorkflow()
}

// Switch wires a target step plus a Case/Default selection into the workflow:
//
//	Switch(target).
//	    Case(case1, func(ctx context.Context, t *Target) (bool, error) {
//	        // true -> run case1.
//	        // err  -> case1 fails with this error.
//	    }).
//	    Case(case2, func(ctx context.Context, t *Target) (bool, error) { ... }).
//	    Default(defaultStep) // runs only if every Case check returned false.
func Switch[T Steper](target T) *SwitchBranch[T] {
	return &SwitchBranch[T]{Target: target, CasesToCheck: make(map[Steper]*BranchCheck[T])}
}

// SwitchBranch is the configurable Switch(...) builder. It registers:
//
//   - the Target step,
//   - one or more Case steps (each gated by its own BranchCheck),
//   - an optional Default step that runs iff none of the Case checks selected
//     their case (and depends on every Case so the decision is observable).
type SwitchBranch[T Steper] struct {
	Target       T
	CasesToCheck map[Steper]*BranchCheck[T]
	DefaultStep  []Steper
	// Cond is the upstream-evaluation Condition for ALL case steps and the
	// default step — NOT the Target. Defaults to DefaultCondition.
	Cond Condition
}

// BranchCheck is the per-branch state recorded by If/Switch: the check
// function plus its most recent result (OK / Error). The result is set when
// the framework runs Do() during the Target's AfterStep (for If) or when the
// case condition is evaluated (for Switch).
type BranchCheck[T Steper] struct {
	Check BranchCheckFunc[T]
	OK    bool
	Error error
}

// Do invokes the check function and records its result on the BranchCheck.
func (bc *BranchCheck[T]) Do(ctx context.Context, target T) {
	bc.OK, bc.Error = bc.Check(ctx, target)
}

// Case registers a single case step with its branch check.
func (s *SwitchBranch[T]) Case(step Steper, check BranchCheckFunc[T]) *SwitchBranch[T] {
	return s.Cases([]Steper{step}, check)
}

// Cases registers multiple steps that share the same branch check function.
// (The same check is recorded once per step — each gets its own result.)
func (s *SwitchBranch[T]) Cases(steps []Steper, check BranchCheckFunc[T]) *SwitchBranch[T] {
	for _, step := range steps {
		s.CasesToCheck[step] = &BranchCheck[T]{Check: check}
	}
	return s
}

// Default appends fallback step(s) that run when every Case check returns false.
func (s *SwitchBranch[T]) Default(step ...Steper) *SwitchBranch[T] {
	s.DefaultStep = append(s.DefaultStep, step...)
	return s
}

// When sets the upstream-evaluation Condition applied to all Case steps and
// the Default step. NOT applied to the Target.
func (s *SwitchBranch[T]) When(cond Condition) *SwitchBranch[T] {
	s.Cond = cond
	return s
}

// isCase builds the Condition for a specific case step. It first defers to
// s.Cond on the upstreams, then runs that case's branch check and returns
// Running iff the check accepted.
func (s *SwitchBranch[T]) isCase(c Steper) func(ctx context.Context, ups map[Steper]StepResult) StepStatus {
	return func(ctx context.Context, ups map[Steper]StepResult) StepStatus {
		if status := ConditionOrDefault(s.Cond)(ctx, ups); status != Running {
			return status
		}
		if check, ok := s.CasesToCheck[c]; ok {
			check.Do(ctx, s.Target)
			if check.OK {
				return Running
			}
		}
		return Skipped
	}
}

// isDefault is the Default step's Condition: skip if any case selected itself,
// otherwise consult s.Cond (with case steps filtered out of the upstream map
// so their Skipped status doesn't poison conditions like AllSucceeded).
func (s *SwitchBranch[T]) isDefault(ctx context.Context, ups map[Steper]StepResult) StepStatus {
	for _, check := range s.CasesToCheck {
		if check.OK {
			return Skipped
		}
	}
	// Hide the case steps from the user-supplied condition: their Skipped
	// status is intentional and not a sign of upstream failure.
	up := make(map[Steper]StepResult)
	for step, status := range ups {
		if _, isCase := s.CasesToCheck[step]; !isCase {
			up[step] = status
		}
	}
	if status := ConditionOrDefault(s.Cond)(ctx, up); status != Running {
		return status
	}
	return Running
}

// AddToWorkflow lays the Switch(...) construct into a {step→config} map.
// Every Case step depends on Target and is gated by isCase(step). The
// Default step (if any) depends on Target AND every Case (so it observes
// their decisions) and is gated by isDefault.
func (s *SwitchBranch[T]) AddToWorkflow() map[Steper]*StepConfig {
	steps := Steps()
	cases := []Steper{}
	for step := range s.CasesToCheck {
		step := step
		cases = append(cases, step)
		steps.Merge(
			Steps(step).
				DependsOn(s.Target).
				When(s.isCase(step)).
				BeforeStep(func(ctx context.Context, step Steper) (context.Context, error) {
					for c, check := range s.CasesToCheck {
						if HasStep(step, c) && check.Error != nil {
							return ctx, check.Error
						}
					}
					return ctx, nil
				}),
		)
	}
	if s.DefaultStep != nil {
		steps.Merge(
			Steps(s.DefaultStep...).
				DependsOn(s.Target).
				DependsOn(cases...).
				When(s.isDefault),
		)
	}
	return steps.AddToWorkflow()
}
