# Examples

This directory is the **`go-workflow` learning path** in code form. Each
file is a runnable [Go example test](https://pkg.go.dev/testing#hdr-Examples)
focused on one question.

`go test ./example/...` runs everything and verifies the output blocks
stay in sync with the library.

## How this path is ordered

These files are sorted by **how often you'll reach for the feature**, not
by API surface area or implementation depth. The earlier the file, the
more likely you'll touch it on day one. Read top-to-bottom on a first
pass and you'll build the right mental model in the right order; jump
around once you know what you need.

When adding a new example, place it where its frequency-of-use lands —
even if that pushes everything below it down by one. The numbers are
ordering hints, not stable identifiers.

## Path

### Get the mental model (read first)

| File                            | What you'll learn |
|---------------------------------|---|
| [01_quickstart](01_quickstart_test.go)             | Any struct with a `Do` method is a Step. End-to-end 3-minute tour: parallel fetch + merge into a profile, with data flowing through `Input` callbacks. |
| [02_steps_and_deps](02_steps_and_deps_test.go)     | `Step` / `Steps` / `DependsOn` / `Pipe` / `BatchPipe` / `Name`. |

### Move data into a Step

| File                            | What you'll learn |
|---------------------------------|---|
| [03_data_flow](03_data_flow_test.go)               | The standard `Input` callback pattern (with your structs and with `Func`/`FuncIO`/`FuncI`/`FuncO`) — typed, point-to-point. |
| [04_context_values](04_context_values_test.go)     | `flow.ContextKey[T]` for graph-wide values like a logger, request ID or client; `flow.Logger` and the log interceptors as the canonical case. |
| [05_callbacks](05_callbacks_test.go)               | `BeforeStep` / `AfterStep` and how they relate to `Input`. |

### Decide what runs (and what doesn't)

| File                            | What you'll learn |
|---------------------------------|---|
| [06_conditions](06_conditions_test.go)             | `Condition`, `When`, `flow.Skip` / `flow.Cancel` from inside `Do`. |
| [07_branching](07_branching_test.go)               | `If` / `Switch` for runtime-data-driven branches. |
| [08_retry_and_timeout](08_retry_and_timeout_test.go) | `Retry`, per-attempt timeout, step timeout, deterministic-clock testing. |

### Build bigger workflows

| File                            | What you'll learn |
|---------------------------------|---|
| [09_workflow_in_workflow](09_workflow_in_workflow_test.go) | Use a `*Workflow` as a Step. Why a "composite Step" struct is an antipattern. |
| [10_workflow_options](10_workflow_options_test.go)         | `MaxConcurrency`, `DontPanic`. |

### Operate, debug, test

| File                            | What you'll learn |
|---------------------------------|---|
| [11_observability](11_observability_test.go)       | `StepInterceptor` / `AttemptInterceptor` for cross-cutting logging, tracing, metrics. |
| [12_debugging](12_debugging_test.go)               | `ErrWorkflow` and `Workflow.StateOf` for post-run inspection. |
| [13_testing_workflows](13_testing_workflows_test.go) | `flow.Mock` to substitute Step behaviour in tests. |
| [14_mutators](14_mutators_test.go)                 | `Mutate[T]` to contribute config (defaults, Retry, callbacks) to every Step of a type — even inside sub-Workflows. |

## Conventions used in these examples

- **Production Steps are your own structs.** Anything with a
  `Do(context.Context) error` method satisfies `flow.Steper`. The first
  four files (01–04) all use plain structs to make this concrete.
- **`flow.Func` / `FuncIO` / `FuncI` / `FuncO`** show up from 05 onward
  for inline scaffolding when the focus of an example is something other
  than the Step body itself (a wiring shape, a Condition, a retry policy,
  etc.). They are convenience helpers — not the primary way to define a Step.
- **Sorted output** when a Step inspects map iteration (which is unordered)
  so the `// Output:` block stays stable.
- **`zeroTimer` / `clock.Mock`** in `07_retry_and_timeout` so retry / timeout
  examples don't actually sleep.

## Where to look beyond this directory

- The `Workflow`, `Step`, `Steps`, `Pipe`, `Retry`, `If`, `Switch` etc.
  godoc on [pkg.go.dev](https://pkg.go.dev/github.com/Azure/go-workflow)
  has the full API surface and many small inline examples.
- `openspec/specs/` contains the formal behaviour specs that these
  examples exercise.
