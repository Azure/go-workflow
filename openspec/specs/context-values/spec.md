# context-values Specification

## Purpose

This capability defines the contract for flowing typed values through a
`context.Context` across go-workflow and any user / contrib code that
participates in a Workflow. It exists so that:

- Cross-cutting values (logger, identity, tenant, tracer, …) have a single,
  conventional way to be injected by the caller and read by `Steper`
  implementations — without each contrib package inventing its own
  unexported context key.
- The convention is **type-safe** (no `interface{}` casts at the call site)
  and **zero-dependency on go-workflow internals** (a value type can be
  defined in any package and still get a stable key).
- A first-class `*slog.Logger` key is published by go-workflow itself, so
  contrib packages can rely on a single convention for structured logging
  rather than threading a logger through their own constructors.

## Requirements

### Requirement: Generic ContextKey[T] helper

`flow` SHALL expose a generic struct type `ContextKey[T any]` that, when
declared as a package-level variable, acts as a unique, type-safe key for a
value of type `T` in `context.Context`:

```go
type ContextKey[T any] struct{}

func (k ContextKey[T]) With(ctx context.Context, v T) context.Context
func (k ContextKey[T]) From(ctx context.Context) (T, bool)
func (k ContextKey[T]) FromOr(ctx context.Context, def T) T
```

Semantics:

- `With` returns a new `context.Context` carrying `v` under the key
  identified by the `ContextKey[T]` value used as receiver. It SHALL NOT
  mutate the passed `ctx`.
- `From` returns the value stored under the key and `ok=true` if present;
  the zero value of `T` and `ok=false` otherwise.
- `FromOr` returns the stored value, or `def` if no value is present. It
  SHALL NOT panic when the value is absent.

`ContextKey[T]` is a zero-size struct, and the underlying `context` key is
the receiver value itself. This means uniqueness is determined by `T`
(the generic instantiation), **not** by the variable's identity. Packages
that own a value type SHALL declare exactly one canonical `ContextKey[T]`
variable for that type and document it as the conventional key. Two
unrelated packages choosing the same `T` will collide; this is by design
and matches the "one key per type" convention.

#### Scenario: With + From round-trips a value
- **WHEN** `ctx2 := key.With(ctx, v)` is called
- **THEN** `key.From(ctx2)` returns `(v, true)`
- **AND** `key.From(ctx)` (the original context) still returns `(zero, false)`

#### Scenario: FromOr returns default when unset
- **WHEN** `key.FromOr(ctx, def)` is called on a context without the key
- **THEN** the return value equals `def`
- **AND** no panic is raised

#### Scenario: Keys with different T do not collide
- **GIVEN** `var ka = ContextKey[A]{}` and `var kb = ContextKey[B]{}`
  where `A` and `B` are distinct types
- **WHEN** both `ka.With(ctx, a)` and `kb.With(ctx, b)` are applied
- **THEN** `ka.From(ctx)` returns `a` and `kb.From(ctx)` returns `b` —
  neither sees the other's value

#### Scenario: Two ContextKey[T] vars of the same T share the same key
- **GIVEN** `var k1 = ContextKey[string]{}` and `var k2 = ContextKey[string]{}`
- **WHEN** `k1.With(ctx, "x")` is applied
- **THEN** `k2.From(ctx)` returns `("x", true)` — uniqueness is by `T`
  alone; package-level variables of the same `ContextKey[T]` are
  interchangeable keys

---

### Requirement: Canonical Logger key

`flow` SHALL expose a package-level variable:

```go
var Logger = ContextKey[*slog.Logger]{}
```

as the conventional context key for a `*slog.Logger` flowing through a
Workflow. Callers SHALL inject a logger using `flow.Logger.With(ctx, l)`
and `Steper` implementations SHALL read it using
`flow.Logger.FromOr(ctx, slog.Default())`.

go-workflow itself does not require the ctx to carry a logger; the
convention exists so that contrib packages and user steps can agree on a
single key, avoiding a proliferation of per-package logger keys and
constructor parameters.

#### Scenario: Steper reads a logger injected by the caller
- **GIVEN** `ctx = flow.Logger.With(ctx, mySlog)`
- **WHEN** a step's `Do(ctx)` calls `flow.Logger.FromOr(ctx, slog.Default())`
- **THEN** it receives `mySlog`

#### Scenario: Steper falls back to slog.Default() when no logger injected
- **GIVEN** a context with no logger injected
- **WHEN** a step's `Do(ctx)` calls `flow.Logger.FromOr(ctx, slog.Default())`
- **THEN** it receives `slog.Default()` (or whichever default the caller passes)

---

### Requirement: LogStepFields interceptor

`flow` SHALL provide a `StepInterceptor` constructor:

```go
func LogStepFields(extra ...func(context.Context, Steper) []any) StepInterceptor
```

that derives the ctx logger by calling `base.With("step", flow.String(step), <extra...>)`
and re-injects the derived logger via `flow.Logger.With` for the duration
of the wrapped step. The base logger SHALL be `flow.Logger.FromOr(ctx, slog.Default())`.

The `extra` variadic parameter accepts caller-supplied functions, each
returning a slice of slog-style `key, value, key, value, …` `any` pairs to
append to the derived logger's fields. This lets callers tag steps with
business attributes (tenant, region, request ID, …) without writing a
custom interceptor.

The original logger stored in `ctx` (if any) SHALL NOT be mutated; each
step run sees a freshly derived logger built from the base, so step-level
fields do not accumulate across steps within a single workflow run.

#### Scenario: Interceptor binds step=<flow.String(step)> onto the ctx logger
- **GIVEN** `ctx` carries a `*slog.Logger`, and a step `s` whose
  `flow.String(s)` returns `"MyStep"`
- **WHEN** `LogStepFields()` wraps `s.Do(ctx)`
- **THEN** any log emitted via `flow.Logger.FromOr(ctx, ...)` inside `Do`
  carries the attribute `step=MyStep`

#### Scenario: Extra functions append additional fields
- **GIVEN** `LogStepFields(func(_, _) []any { return []any{"tenant", "acme"} })`
- **WHEN** the interceptor wraps a step's execution
- **THEN** logs emitted inside the step carry both `step=...` and
  `tenant=acme`

#### Scenario: Falls back to slog.Default() when ctx has no logger
- **GIVEN** a context with no logger injected
- **WHEN** `LogStepFields()` wraps a step's execution
- **THEN** the derived logger is built on top of `slog.Default()` and is
  re-injected so the step still observes a non-nil logger via
  `flow.Logger.FromOr`

#### Scenario: Original ctx logger is not mutated
- **GIVEN** a base logger `L` injected via `flow.Logger.With(ctx, L)`
- **WHEN** `LogStepFields()` wraps any number of steps
- **THEN** logging via `L` directly produces records without any `step=...`
  attribute attached by the interceptor

#### Scenario: Errors from next propagate unchanged
- **WHEN** the wrapped `next(ctx)` returns an error `err`
- **THEN** `LogStepFields(...).InterceptStep` returns the same error

---

### Requirement: LogAttemptField interceptor

`flow` SHALL provide an `AttemptInterceptor` constructor:

```go
func LogAttemptField() AttemptInterceptor
```

that derives the ctx logger by calling `base.With("attempt", attempt)` and
re-injects the derived logger for the duration of the wrapped attempt. The
base logger SHALL be `flow.Logger.FromOr(ctx, slog.Default())`. Composing
this with `LogStepFields` (registered separately on
`Option.StepInterceptors`) gives logs both `step=...` and `attempt=N`.

#### Scenario: Interceptor binds attempt=N onto the ctx logger
- **GIVEN** an attempt with index `2`
- **WHEN** `LogAttemptField()` wraps the attempt
- **THEN** any log emitted via `flow.Logger.FromOr(ctx, ...)` inside the
  attempt carries the attribute `attempt=2`
