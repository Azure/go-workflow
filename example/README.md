# Examples

This directory is the **`go-workflow` learning path** in code form. Each
file is a runnable [Go example test](https://pkg.go.dev/testing#hdr-Examples)
focused on one question. Read top to bottom on a first pass; jump around
once you know what you need.

`go test ./example/...` runs everything and verifies the output blocks
stay in sync with the library.

## Path

### Get the mental model (read first)

| File                            | What you'll learn |
|---------------------------------|---|
| [01_quickstart](01_quickstart_test.go)             | A 3-minute end-to-end tour: parallel fetch + merge into a profile. |
| [02_steps_and_deps](02_steps_and_deps_test.go)     | `Step` / `Steps` / `DependsOn` / `Pipe` / `BatchPipe` / `Func` / `Name`. |

### Move data through the graph

| File                            | What you'll learn |
|---------------------------------|---|
| [03_data_flow](03_data_flow_test.go)               | Typed `Input` / `Output` between Steps. `Func` / `FuncIO` / `FuncI` / `FuncO`. |
| [04_callbacks](04_callbacks_test.go)               | `BeforeStep` / `AfterStep` and how they relate to `Input`. |

### Decide what runs (and what doesn't)

| File                            | What you'll learn |
|---------------------------------|---|
| [05_conditions](05_conditions_test.go)             | `Condition`, `When`, `flow.Skip` / `flow.Cancel` from inside `Do`. |
| [06_branching](06_branching_test.go)               | `If` / `Switch` for runtime-data-driven branches. |
| [07_retry_and_timeout](07_retry_and_timeout_test.go) | `Retry`, per-attempt timeout, step timeout, deterministic-clock testing. |

### Build bigger workflows

| File                            | What you'll learn |
|---------------------------------|---|
| [08_workflow_in_workflow](08_workflow_in_workflow_test.go) | Use a `*Workflow` as a Step. Why a "composite Step" struct is an antipattern. |
| [09_workflow_options](09_workflow_options_test.go)         | `MaxConcurrency`, `DontPanic`. |

### Operate, debug, test

| File                            | What you'll learn |
|---------------------------------|---|
| [10_observability](10_observability_test.go)       | `StepInterceptor` / `AttemptInterceptor` for cross-cutting logging, tracing, metrics. |
| [11_debugging](11_debugging_test.go)               | `ErrWorkflow` and `Workflow.StateOf` for post-run inspection. |
| [12_testing_workflows](12_testing_workflows_test.go) | `flow.Mock` to substitute Step behaviour in tests. |

## Conventions used in these examples

- **`flow.Func` / `FuncIO` / `FuncI` / `FuncO`** for inline Steps. Real code
  often defines its own `Steper` types; these helpers keep examples short.
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
