## ADDED Requirements

### Requirement: If/Then/Else conditional branch

`If(target, checkFn).Then(thenSteps...).Else(elseSteps...)` adds a conditional branch
to the Workflow. The `target` Step runs first; after it terminates its `AfterStep`
callback evaluates `checkFn`. The result of `checkFn` determines which branch
(`Then` or `Else`) proceeds to `Running` — the other branch is set to `Skipped`.

The check function has signature `func(context.Context, T) (bool, error)` where `T` is
the concrete type of the target Step.

#### Scenario: Then branch runs when check returns true
- **WHEN** `checkFn` returns `(true, nil)` after the target Step terminates
- **THEN** all `Then` Steps proceed to execute; all `Else` Steps are set to `Skipped`

#### Scenario: Else branch runs when check returns false
- **WHEN** `checkFn` returns `(false, nil)` after the target Step terminates
- **THEN** all `Else` Steps proceed to execute; all `Then` Steps are set to `Skipped`

#### Scenario: Check error fails the selected branch
- **WHEN** `checkFn` returns a non-nil error
- **THEN** the branch Steps fail with that error (via a `BeforeStep` that propagates it)

#### Scenario: Branch Steps implicitly depend on the target
- **WHEN** an `IfBranch` is added to a Workflow
- **THEN** all `Then` and `Else` Steps automatically declare `DependsOn(target)`
  without the caller needing to declare it explicitly

#### Scenario: If branch respects an outer When condition
- **WHEN** `.When(cond)` is set on the `IfBranch`
- **THEN** `cond` is evaluated for both `Then` and `Else` Steps after the target terminates;
  if `cond` does not return `Running` the branch is not entered

#### Scenario: If workflow is re-run with different state
- **WHEN** the same Workflow with an `IfBranch` is reset and run again
  and `checkFn` returns a different boolean result
- **THEN** the opposite branch executes on the second run

---

### Requirement: Switch/Case/Default conditional branch

`Switch(target).Case(step, checkFn).Default(defaultSteps...)` adds a multi-way branch.
The `target` Step runs first. Each `Case` check function is evaluated independently
(not short-circuit). A `Default` branch runs only if every `Case` check returned `false`.

#### Scenario: Matched Case runs; unmatched Cases are Skipped
- **WHEN** one or more `Case` check functions return `true`
- **THEN** the Steps for matching Cases execute; Steps for non-matching Cases are `Skipped`

#### Scenario: Multiple Cases can match simultaneously
- **WHEN** two `Case` check functions both return `true`
- **THEN** both matched Case Steps execute (Switch is not exclusive)

#### Scenario: Default runs when no Case matched
- **WHEN** all `Case` check functions return `false`
- **THEN** Default Steps execute

#### Scenario: Default is Skipped when any Case matched
- **WHEN** at least one `Case` check function returns `true`
- **THEN** Default Steps are set to `Skipped`

#### Scenario: All Case and Default Steps depend on the target
- **WHEN** a `SwitchBranch` is added to a Workflow
- **THEN** all Case Steps and Default Steps automatically declare `DependsOn(target)`

#### Scenario: Case check error fails the Case Step
- **WHEN** a `Case` check function returns a non-nil error
- **THEN** the corresponding Case Step fails with that error
  (via a `BeforeStep` that propagates `BranchCheck.Error`)

#### Scenario: Switch branch respects an outer When condition
- **WHEN** `.When(cond)` is set on the `SwitchBranch`
- **THEN** `cond` is evaluated for each Case Step and for the Default Steps;
  if it does not return `Running` the branch is not entered

---

### Requirement: Branch check runs inside target's AfterStep

The check function for both `If` and `Switch` is called inside an `AfterStep` callback
registered on the target Step. This means the check runs after `target.Do` returns but
the result (true/false/error) is stored on the `BranchCheck` struct and read later when
the branch Step's Condition is evaluated.

#### Scenario: Check executes after target Do
- **WHEN** the target Step's `Do` method returns
- **THEN** the branch check function is called before any branch Step starts

#### Scenario: BranchCheck state is used in branch Condition
- **WHEN** a branch Step's Condition is evaluated
- **THEN** it reads the already-computed `BranchCheck.OK` and `BranchCheck.Error`
  rather than calling the check function again
