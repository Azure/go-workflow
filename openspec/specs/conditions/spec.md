## ADDED Requirements

### Requirement: Condition contract

A `Condition` is a function `func(context.Context, map[Steper]StepResult) StepStatus`
called by the Workflow after all upstream Steps have terminated. The return value decides
what happens next:

- Return `Running` → the Step proceeds to execute.
- Return any terminated status (`Skipped`, `Canceled`, …) → the Step is immediately set
  to that status without calling `Do`.

The Condition SHALL only be called when every upstream Step is in a terminated status.

#### Scenario: Condition receives all upstream results
- **WHEN** a Step has upstreams A and B and both are terminated
- **THEN** the Condition is called with a map containing entries for both A and B

#### Scenario: Condition returning Running starts the Step
- **WHEN** a Condition returns `Running`
- **THEN** the Step transitions to `Running` and `Do` is called

#### Scenario: Condition returning a terminal status skips Do
- **WHEN** a Condition returns `Skipped` (or any other terminated status)
- **THEN** the Step is set to that status immediately; `Do` is never called

---

### Requirement: DefaultCondition is AllSucceeded

Unless overridden with `.When(cond)`, every Step uses `AllSucceeded` as its Condition.

#### Scenario: Step runs when all upstreams succeed (default)
- **WHEN** all upstream Steps have status `Succeeded`
  and no explicit Condition is set
- **THEN** the Step runs

#### Scenario: Step is skipped when any upstream did not succeed (default)
- **WHEN** any upstream Step has a status other than `Succeeded`
  and no explicit Condition is set
- **THEN** the Step is set to `Skipped`

---

### Requirement: AllSucceeded condition

`AllSucceeded` runs the Step when **every** upstream terminated with `Succeeded`.
If the context is canceled it returns `Canceled` regardless of upstream statuses.

#### Scenario: All upstreams succeeded
- **WHEN** every upstream Step has status `Succeeded`
- **THEN** `AllSucceeded` returns `Running`

#### Scenario: Any upstream not succeeded
- **WHEN** any upstream Step has status `Failed`, `Skipped`, or `Canceled`
- **THEN** `AllSucceeded` returns `Skipped`

#### Scenario: Context canceled
- **WHEN** the context passed to the Condition is canceled
- **THEN** `AllSucceeded` returns `Canceled`

---

### Requirement: AnySucceeded condition

`AnySucceeded` runs the Step when **at least one** upstream terminated with `Succeeded`.

#### Scenario: One upstream succeeded
- **WHEN** at least one upstream has status `Succeeded`
- **THEN** `AnySucceeded` returns `Running`

#### Scenario: No upstream succeeded
- **WHEN** no upstream has status `Succeeded`
- **THEN** `AnySucceeded` returns `Skipped`

---

### Requirement: AllSucceededOrSkipped condition

`AllSucceededOrSkipped` runs the Step when every upstream terminated with either
`Succeeded` or `Skipped`, treating skipped upstreams as acceptable.

#### Scenario: Upstreams all succeeded or skipped
- **WHEN** every upstream has status `Succeeded` or `Skipped`
- **THEN** `AllSucceededOrSkipped` returns `Running`

#### Scenario: Any upstream failed or canceled
- **WHEN** any upstream has status `Failed` or `Canceled`
- **THEN** `AllSucceededOrSkipped` returns `Skipped`

---

### Requirement: AnyFailed condition

`AnyFailed` runs the Step when **at least one** upstream terminated with `Failed`.
This is useful for cleanup or alerting Steps that should execute on failure.

#### Scenario: One upstream failed
- **WHEN** at least one upstream has status `Failed`
- **THEN** `AnyFailed` returns `Running`

#### Scenario: No upstream failed
- **WHEN** no upstream has status `Failed`
- **THEN** `AnyFailed` returns `Skipped`

---

### Requirement: Always condition

`Always` runs the Step unconditionally as long as all upstreams are terminated,
regardless of their status or the context state.

#### Scenario: Runs after any upstream terminal status
- **WHEN** all upstreams are terminated with any combination of statuses
- **THEN** `Always` returns `Running`

---

### Requirement: BeCanceled condition

`BeCanceled` runs the Step **only** when the context has been canceled.
This enables "on-cancel" cleanup or compensation Steps.

#### Scenario: Context is canceled
- **WHEN** the context passed to the Condition is canceled
- **THEN** `BeCanceled` returns `Running`

#### Scenario: Context is not canceled
- **WHEN** the context is still valid
- **THEN** `BeCanceled` returns `Skipped`

---

### Requirement: Custom Condition via When

Any Step may replace the default Condition with a custom function via `.When(cond)`.
Custom conditions receive the context and upstream results and return a `StepStatus`.
They SHOULD call one of the built-in conditions first to handle the common cases before
applying custom logic.

#### Scenario: Custom condition overrides default
- **WHEN** a Step is declared with `.When(myCondition)`
- **THEN** `myCondition` is called instead of `AllSucceeded`

#### Scenario: Custom condition can compose built-ins
- **WHEN** a custom Condition calls `flow.AllSucceeded(ctx, ups)` and then applies
  additional domain logic
- **THEN** both the built-in check and the domain logic are applied

---

### Requirement: ConditionOr and ConditionOrDefault helpers

`ConditionOr(cond, defaultCond)` returns `cond` if it is non-nil, otherwise `defaultCond`.
`ConditionOrDefault(cond)` is a shorthand that falls back to `DefaultCondition`.

#### Scenario: ConditionOr with nil primary
- **WHEN** `ConditionOr(nil, AllSucceeded)` is evaluated
- **THEN** `AllSucceeded` is used

#### Scenario: ConditionOr with non-nil primary
- **WHEN** `ConditionOr(myCondition, AllSucceeded)` is evaluated
- **THEN** `myCondition` is used
