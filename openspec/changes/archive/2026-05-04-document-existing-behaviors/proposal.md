## Why

`go-workflow` has a well-designed API and rich example suite, but no formal specifications
describing the guaranteed behavior of each subsystem. Contributors and users must read source
code or reverse-engineer behavior from tests. Formalizing these behaviors as specs creates
a stable contract that makes future changes reviewable and prevents accidental regressions.

## What Changes

- Add 7 new spec documents covering every major behavioral subsystem of the library.
- No existing code is modified; this is documentation-only.

## Capabilities

### New Capabilities

- `execution-model`: The DAG execution engine — Step status lifecycle, topological scheduling,
  concurrency, preflight cycle detection, and the tick/signal loop.
- `step-configuration`: The builder API for configuring steps — `DependsOn`, `Input`, `Output`,
  `BeforeStep`, `AfterStep`, idempotent `Add()` merge semantics, and `DefaultOption` propagation.
- `conditions`: All built-in `Condition` functions and the contract for custom conditions —
  `AllSucceeded`, `AnySucceeded`, `AnyFailed`, `Always`, `BeCanceled`, `AllSucceededOrSkipped`.
- `retry-and-timeout`: `RetryOption` field semantics, the Step Timeout vs Per-Try Timeout
  interaction, backoff integration, and how retry interacts with context cancellation.
- `branching`: Runtime behavior of `If/Then/Else` and `Switch/Case/Default` — when branch
  checks run, how `BranchCheck` state is shared, and how branch errors propagate.
- `composite-steps`: The `Unwrap()` protocol, `Has`/`As`/`HasStep` traversal, `SubWorkflow`
  + `BuildStep` pattern, and Workflow-as-a-Step semantics.
- `workflow-options`: `MaxConcurrency`, `DontPanic`, `SkipAsError`, `DefaultOption`, and
  `Clock` — what each option guarantees and how options compose.

### Modified Capabilities

## Impact

- `openspec/specs/` — 7 new spec directories, each with a `spec.md`.
- No source code changes.
