# go-workflow

[![Go Report Card](https://goreportcard.com/badge/github.com/Azure/go-workflow)](https://goreportcard.com/report/github.com/Azure/go-workflow)
[![Go Test Status](https://github.com/Azure/go-workflow/actions/workflows/go.yml/badge.svg)](https://github.com/Azure/go-workflow/actions/workflows/go.yml)
[![Go Test Coverage](https://raw.githubusercontent.com/Azure/go-workflow/badges/.badges/main/coverage.svg)](/.github/.testcoverage.yml)

> Describe steps and the dependencies between them. We run them as a DAG — concurrently,
> with retry, timeout, conditions and interceptors — and block until everything is done.

```go
// Two steps that pass data through a typed dependency.
type Fetch struct{ URL, Body string }
type Save struct{ Body, Path string }

func (f *Fetch) Do(ctx context.Context) error { f.Body = httpGet(ctx, f.URL); return nil }
func (s *Save) Do(ctx context.Context) error  { return os.WriteFile(s.Path, []byte(s.Body), 0o644) }

func main() {
	fetch := &Fetch{URL: "https://example.com"}
	save := &Save{Path: "page.html"}

	w := new(flow.Workflow)
	w.Add(
		// Retry the fetch up to 3 times, capped at 30s total.
		flow.Step(fetch).
			Retry(func(o *flow.RetryOption) { o.Attempts = 3 }).
			Timeout(30*time.Second),

		// save runs only after fetch succeeds, and reads its output as its input.
		flow.Step(save).DependsOn(fetch).
			Input(func(_ context.Context, s *Save) error {
				s.Body = fetch.Body
				return nil
			}),
	)

	if err := w.Do(context.Background()); err != nil {
		log.Fatal(err) // *flow.ErrWorkflow — one entry per failing step.
	}
}
```

## Why

- **Tiny interface.** A step is anything with `Do(context.Context) error`. No codegen, no DSL.
- **Dependencies as code.** `Step(x).DependsOn(y)`, `Pipe(...)`, `BatchPipe(...)`, `If/Switch`.
- **Concurrent by default.** Each ready step runs in its own goroutine; cap with `Option.MaxConcurrency`.
- **Per-step controls.** Retry with backoff, timeout, conditions, typed `Input`/`Output`, before/after hooks.
- **Composable.** A `Workflow` is itself a `Step`, so workflows nest — interceptors and options
  flow into children automatically.
- **No surprises.** `Workflow.Do` blocks until every goroutine has exited and every step is terminal.

## Install

```bash
go get github.com/Azure/go-workflow
```

Requires Go 1.23+.

## How a step ends up

```
Pending → Running → Succeeded | Failed | Canceled | Skipped
```

`Skipped` and `Canceled` are settled inline by the scheduler when a step's `Condition` decides
it shouldn't run — no goroutine, no concurrency lease, no interceptor chain. A failing step does
**not** abort siblings; only downstream steps see it (and become `Skipped` under the default
`AllSucceeded` condition).

`Workflow.Do` returns `nil` on success, or an `ErrWorkflow` (`map[Steper]StepResult`) you can
range over. `ErrCycleDependency` is returned from preflight if your graph isn't a DAG.

## Wiring the graph

| Helper                                 | Means                                                                          |
|----------------------------------------|--------------------------------------------------------------------------------|
| `flow.Step(s)`                         | Add one typed step (enables typed `Input`/`Output`).                           |
| `flow.Steps(s1, s2, …)`                | Add several independent steps (run in parallel).                               |
| `flow.Pipe(a, b, c)`                   | Linear pipeline `a → b → c`.                                                   |
| `flow.BatchPipe(Steps(a,b), Steps(c))` | Every step in batch _i_ depends on every step in batch _i-1_.                  |
| `flow.If(...)`, `flow.Switch(...)`     | Conditional branches based on the result of a target step.                     |

Common chainables on the result: `DependsOn`, `When(cond)`, `Retry(...)`, `Timeout(d)`,
`Input(fn)`, `Output(fn)`, `BeforeStep(fn)`, `AfterStep(fn)`. `Add(...)` is repeatable —
calling it again merges new config into existing steps.

## Workflow knobs

Workflow-level configuration lives in `flow.Workflow.Option` (type `WorkflowOption`).
Scalar fields are pointer-typed so unset / explicit-zero are distinguishable;
slice fields stay slices. When a Workflow is used as a sub-workflow, the
parent's Option propagates into the child via `WorkflowOptionReceiver` —
nil scalars take the parent's value, slices are parent-prepended. Set
`Option.DontInherit = true` on a child to opt out.

```go
mc := 4
dontPanic := true
w := &flow.Workflow{Option: flow.WorkflowOption{
    MaxConcurrency: &mc,
    DontPanic:      &dontPanic,
}}
```

| Field                          | Effect                                                                       |
|--------------------------------|------------------------------------------------------------------------------|
| `Option.MaxConcurrency`        | Max running steps at once. `nil` or `0` = unlimited.                         |
| `Option.DontPanic`             | Recover panics into `ErrPanic` instead of crashing.                          |
| `Option.SkipAsError`           | Treat `Skipped` as workflow failure (default: skipped is OK).                |
| `Option.StepDefaults`          | Base `*StepOption` applied (then overridable) to every step.                 |
| `Option.StepInterceptors`      | Wrap full step lifetime (across retries).                                    |
| `Option.AttemptInterceptors`   | Wrap each individual attempt (`Before → Do → After`).                        |
| `Option.Mutators`              | Cross-cutting per-type Step contributions (see `flow.Mutate`).               |
| `Option.DontInherit`           | When nested as a child step, don't inherit any of the parent's Option.       |
| `Option.Clock`                 | Inject a clock for deterministic tests.                                      |

### Sub-workflows

Embed `flow.Workflow` directly in your own struct and call `Add` at
construction time — this is the recommended pattern. The embedded
`Workflow` satisfies `WorkflowOptionReceiver`, so the parent's Option
flows in automatically. The legacy `flow.SubWorkflow` type is deprecated
and will be removed in the next major version.

## Learn more

- **[`example/`](./example)** — runnable, narrated examples for every feature, in increasing
  order of complexity (`01_step_do_test.go` → `14_mock_step_test.go`). Best place to start.
- **[`openspec/specs/`](./openspec/specs)** — formal specs for execution model, branching,
  conditions, retry/timeout, composite steps, interceptors and workflow options.
- **DeepWiki:** <https://deepwiki.com/Azure/go-workflow>

## Contrib

Optional, independently-versioned modules under `contrib/`:

- **[`contrib/otel`](./contrib/otel)** — OpenTelemetry tracing integration
  via the existing `StepInterceptor` / `AttemptInterceptor` extension
  points. Released as a separate Go module
  (`github.com/Azure/go-workflow/contrib/otel`) so its OpenTelemetry
  dependency does not enter core's transitive graph.

## Contributing

This project welcomes contributions. Most contributions require you to agree to a Contributor
License Agreement — see <https://cla.opensource.microsoft.com>. The CLA bot will guide you on
your first PR.

This project follows the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
Questions? <opencode@microsoft.com>.

## Trademarks

This project may contain trademarks for Microsoft projects, products, or services. Authorized
use must follow [Microsoft's Trademark & Brand Guidelines](https://www.microsoft.com/en-us/legal/intellectualproperty/trademarks/usage/general).
Third-party trademarks are subject to their own policies.
