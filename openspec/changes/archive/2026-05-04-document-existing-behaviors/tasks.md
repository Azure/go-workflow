## 1. Execution Model spec

- [x] 1.1 Review `workflow.go` (tick, preflight, Do, reset) and verify all scenarios in `execution-model/spec.md` match the implementation
- [x] 1.2 Cross-check Step status lifecycle against `condition.go` `StepStatus` constants and `IsTerminated()`
- [x] 1.3 Verify `ErrWorkflow` behavior (AllSucceeded, AllSucceededOrSkipped) in `error.go`

## 2. Step Configuration spec

- [x] 2.1 Verify idempotent `Add` / `StepConfig.Merge` behavior in `step.go` against spec scenarios
- [x] 2.2 Verify `Input` / `Output` callback execution order and per-retry behavior using `example/03_io_data_flow_test.go`
- [x] 2.3 Verify `BeforeStep` context threading and short-circuit behavior
- [x] 2.4 Verify `DefaultOption` prepend-and-override semantics in `workflow.go:Add`

## 3. Conditions spec

- [x] 3.1 Verify each built-in Condition (`AllSucceeded`, `AnySucceeded`, `AnyFailed`, `Always`, `BeCanceled`, `AllSucceededOrSkipped`) against `condition.go`
- [x] 3.2 Verify `ConditionOr` / `ConditionOrDefault` helper behavior
- [x] 3.3 Cross-check condition behavior with `example/04_condition_when_test.go`

## 4. Retry and Timeout spec

- [x] 4.1 Verify `Attempts` semantics (total calls, 0=unlimited) against `retry.go`
- [x] 4.2 Verify Step Timeout vs Per-Try Timeout interaction using `example/07_timeout_test.go`
- [x] 4.3 Verify `NextBackOff` callback receives correct `RetryEvent` fields
- [x] 4.4 Verify context cancellation stops the retry loop

## 5. Branching spec

- [x] 5.1 Verify `If/Then/Else` implicit `DependsOn` wiring in `branch.go:IfBranch.AddToWorkflow`
- [x] 5.2 Verify `Switch` multi-match behavior (non-exclusive) and Default logic in `SwitchBranch.AddToWorkflow`
- [x] 5.3 Verify `BranchCheck` state is stored in AfterStep and read in Condition, not re-evaluated
- [x] 5.4 Cross-check with `example/05_branch_if_switch_test.go`

## 6. Composite Steps spec

- [x] 6.1 Verify `Unwrap()` protocol and root Step replacement logic in `workflow.go:addStep`
- [x] 6.2 Verify `Has`, `As`, `HasStep` traversal in `wrap.go`
- [x] 6.3 Verify `BuildStep` is called exactly once and `Reset` is called before `BuildStep` in `build_step.go`
- [x] 6.4 Verify `SubWorkflow` exposes inner Steps to outer Workflow via `Unwrap`
- [x] 6.5 Cross-check with `example/10_update_workflow_test.go`, `example/11_workflow_in_workflow_test.go`, `example/13_composite_step_test.go`

## 7. Workflow Options spec

- [x] 7.1 Verify `MaxConcurrency` lease bucket behavior in `workflow.go:lease/unlease`
- [x] 7.2 Verify `DontPanic` catch behavior in `catchPanicAsError` and `ErrPanic` wrapping
- [x] 7.3 Verify `SkipAsError` affects `AllSucceededOrSkipped` vs `AllSucceeded` check in `Do`
- [x] 7.4 Verify `Clock` nil-initialization in `reset()` and mock clock scenario
- [x] 7.5 Cross-check with `example/08_workflow_option_test.go`
