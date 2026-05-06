# Tasks: structured-event-sink

## Implementation

- [x] Define public types in `event.go` (`EventType`, `WorkflowEvent`, `StepInterceptor`, `AttemptInterceptor`, `StepInterceptorFunc`, `AttemptInterceptorFunc`, `StepInfo`, `AttemptInfo`, `InterceptorReceiver`, `retryNotifier`)
- [x] Implement `NewStepEventSink` and `NewAttemptEventSink` in `event.go`
- [x] Add `StepInterceptors`/`AttemptInterceptors` fields to `Workflow` struct
- [x] Introduce `stepExecution` struct; simplify `tick()` to only claim step via `scheduled` sentinel
- [x] Implement `stepExecution.run()`, `executeWithRetry()`, `buildAttemptChain()`, `runAttempt()`, `wireNotify()`
- [x] Delete `makeDoForStep()` and `runStep()` from `workflow.go`
- [x] Implement `SubWorkflow.PrependInterceptors` in `wrap.go`

## Tests

- [x] Unit tests for `EventType` constants and `StepInterceptorFunc`/`AttemptInterceptorFunc` adapters
- [x] Unit tests for `NewStepEventSink` (Succeeded, Failed, Skipped, OnRetry)
- [x] Unit tests for `NewAttemptEventSink` (Started event)
- [x] Integration test: basic step success with StepInterceptor
- [x] Integration test: StepInterceptor chain ordering (A→B→B→A)
- [x] Integration test: AttemptInterceptor chain ordering (X→Y→Y→X)
- [x] Integration test: Skipped step enters interceptor chain with TerminalReason
- [x] Integration test: Retrying events with correct attempt numbers
- [x] Integration test: SubWorkflow interceptor propagation
- [x] Integration test: child interceptor preserved alongside parent
- [x] Integration test: `PrependInterceptors` not duplicated on retry (`TestSubWorkflow_InterceptorNotDuplicatedOnRetry`)
- [x] Regression test: zero-interceptor workflow unchanged
- [x] Race detector clean (`go test -race ./...`)

## Bug Fixes (found during review)

- [x] Fix C1: `PrependInterceptors` moved from `runAttempt` (per-attempt) to `executeWithRetry` (once per step)
- [x] Fix wireNotify timing: `Retrying.Attempt` uses `ex.attempt - 1` (defer in `runAttempt` fires before Notify)
- [x] Fix `EventType` to be a distinct named type (`type EventType string`), not a type alias
