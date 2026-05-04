## ADDED Requirements

### Requirement: MaxConcurrency limits simultaneous running Steps

When `Workflow.MaxConcurrency` is set to a positive integer `N`, the Workflow SHALL run
at most `N` Steps concurrently. Additional runnable Steps wait until a running Step
terminates before starting. A value of `0` (the default) means unlimited concurrency.

The limit is implemented via a buffered channel used as a lease bucket.

#### Scenario: MaxConcurrency=2 allows exactly 2 concurrent Steps
- **WHEN** `MaxConcurrency` is 2 and 3 independent Steps are added
- **THEN** at most 2 Steps run simultaneously; the third starts only after one finishes

#### Scenario: MaxConcurrency=0 imposes no limit
- **WHEN** `MaxConcurrency` is 0 (the default)
- **THEN** all runnable Steps start concurrently without any concurrency bound

---

### Requirement: DontPanic converts panics to errors

When `Workflow.DontPanic` is `true`, any `panic` in a Step's `Do`, `Input`, or
`BeforeStep`/`AfterStep` callbacks is recovered and returned as a `ErrPanic`-wrapped
error, setting the Step to `Failed`. Stack trace information is captured and included
in the error.

When `DontPanic` is `false` (the default), panics propagate normally and crash the
process.

#### Scenario: Panic converted to Failed with DontPanic=true
- **WHEN** `DontPanic` is `true` and a Step panics during `Do`
- **THEN** the Step status is `Failed`; the returned `ErrWorkflow` entry wraps the panic
  value as an `ErrPanic`

#### Scenario: Panic propagates with DontPanic=false
- **WHEN** `DontPanic` is `false` and a Step panics
- **THEN** the panic propagates out of the goroutine (process crash or test failure)

---

### Requirement: SkipAsError controls whether Skipped counts as failure

When `Workflow.SkipAsError` is `false` (the default), Steps that are `Skipped` are
considered acceptable outcomes. `workflow.Do` returns `nil` if all root Steps are
`Succeeded` or `Skipped`.

When `SkipAsError` is `true`, any `Skipped` Step causes `workflow.Do` to return an
`ErrWorkflow` even if no Step actually failed.

#### Scenario: Skipped is acceptable by default
- **WHEN** `SkipAsError` is `false` and all root Steps are either `Succeeded` or `Skipped`
- **THEN** `workflow.Do` returns `nil`

#### Scenario: Skipped counts as error when SkipAsError=true
- **WHEN** `SkipAsError` is `true` and at least one root Step is `Skipped`
- **THEN** `workflow.Do` returns an `ErrWorkflow` containing the skipped Step

---

### Requirement: DefaultOption applies a baseline StepOption to all Steps

`Workflow.DefaultOption` is a `*StepOption` that the Workflow prepends to the Option
slice of every Step added to it. This lets callers set a universal default for all Steps
(e.g., a global timeout) without modifying each Step individually.

Step-level options that are set after the default take precedence because the Option
slice is evaluated left-to-right and later values overwrite earlier ones on the same
`StepOption` struct.

#### Scenario: DefaultOption sets a global timeout
- **WHEN** `Workflow.DefaultOption` has `Timeout` set to 10 minutes
  and a Step is added without its own `Timeout`
- **THEN** the effective timeout for that Step is 10 minutes

#### Scenario: Step-level option overrides the default
- **WHEN** `Workflow.DefaultOption` has `Timeout` of 10 minutes and a Step declares
  `.Timeout(5 * time.Minute)`
- **THEN** the effective timeout for that Step is 5 minutes

---

### Requirement: Clock enables time injection for testing

`Workflow.Clock` is a `clock.Clock` interface (from `github.com/benbjohnson/clock`).
The Workflow uses the Clock for all time-related operations: Step-level timeouts,
per-try timeouts in the retry loop, and backoff waits.

When `Clock` is `nil`, the Workflow uses the real wall clock (`clock.New()`).
Providing a mock clock allows unit tests to control time without real delays.

#### Scenario: Nil Clock uses wall clock
- **WHEN** `Workflow.Clock` is not set
- **THEN** the Workflow automatically initializes it to `clock.New()` at the start of `Do`

#### Scenario: Mock clock controls timeout behavior in tests
- **WHEN** a `clock.Mock` is injected as `Workflow.Clock`
  and the mock is advanced past a Step's `Timeout` duration
- **THEN** the Step's context is canceled and the Step is set to `Canceled`
