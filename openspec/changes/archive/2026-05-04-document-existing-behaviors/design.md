## Context

`go-workflow` is a pure Go library that organizes steps into a DAG and executes them
concurrently. The library has a coherent internal design and a thorough example suite, but
no canonical written specification. This document records the decisions made during the
original implementation that every spec author needs to understand before writing a spec.

## Goals / Non-Goals

**Goals:**
- Establish a shared vocabulary (Step, Steper, Upstream, Downstream, State, Condition, …)
- Record the source of truth for behavioral guarantees so spec authors don't contradict each other
- Identify the 7 capability boundaries that map to individual spec files

**Non-Goals:**
- Proposing any new features or changing any existing behavior
- Documenting the internal tick/signal implementation in exhaustive detail (that lives in `execution-model` spec)

## Decisions

### One spec per behavioral subsystem

Each of the 7 capabilities in the proposal maps to a single concern:

| Spec | Primary source files |
|------|---------------------|
| `execution-model` | `workflow.go` (tick, preflight, Do, reset) |
| `step-configuration` | `step.go` (AddStep, AddSteps, StepConfig, Merge) |
| `conditions` | `condition.go` |
| `retry-and-timeout` | `retry.go`, `StepOption.Timeout` in `step.go` |
| `branching` | `branch.go` |
| `composite-steps` | `wrap.go`, `build_step.go`, `name.go` |
| `workflow-options` | `Workflow` struct fields in `workflow.go` |

This boundary keeps each spec self-contained and reviewable independently.

### Spec language: SHALL / MUST for normative requirements

All observable, testable guarantees use SHALL or MUST. Implementation notes that explain
*why* but are not testable use plain prose outside requirement blocks.

### Scenarios drawn from existing examples and tests

Every scenario in the specs corresponds to a behavior already demonstrated by a file in
`example/` or `*_test.go`. No new behavior is invented; specs only codify what exists.

## Risks / Trade-offs

- [Risk] A spec author misreads the source and writes an inaccurate requirement.
  → Mitigation: each spec references the source file and function/type name.
- [Risk] Future code changes silently invalidate a spec scenario.
  → Mitigation: scenarios are written to be directly translatable into Go test cases.

## Open Questions

None — this is a documentation-only change. All behavioral questions are resolved by
reading the existing source and examples.
