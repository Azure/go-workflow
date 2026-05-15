package flow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	flow "github.com/Azure/go-workflow"
)

// # Context values: ContextKey[T], flow.Logger and the log interceptors
//
// **What you'll learn**
//   - Define your own typed context key with `flow.ContextKey[T]` so any
//     Steper can read a cross-cutting value (identity, tracer, k8s client,
//     ...) without an explicit constructor parameter.
//   - Use the canonical `flow.Logger` key (`ContextKey[*slog.Logger]`) to
//     pass a structured logger through `ctx`.
//   - Layer `flow.LogStepFields` and `flow.LogAttemptField` on
//     `Option.StepInterceptors` / `Option.AttemptInterceptors` so every log
//     line is automatically tagged with `step=<name>` and `attempt=N`.
//
// **The convention in one paragraph**
//
// `flow.ContextKey[T]` is a zero-size, generic struct used as a typed
// context key. Declare exactly one canonical `ContextKey[T]` variable per
// value type — uniqueness is by `T`, so two such variables of the same `T`
// collide on purpose. Callers use `key.With(ctx, v)` to inject and
// `key.From(ctx)` / `key.FromOr(ctx, def)` to read.

// --- shared helpers ---------------------------------------------------------

// Identity is a contrived value type carried through ctx; pretend it holds
// Azure / k8s / tenant credentials.
type Identity struct {
	TenantID string
	SubID    string
}

// IdentityKey is the canonical key for Identity. A package that owns a
// value type should export the key once so every Steper agrees on it.
var IdentityKey = flow.ContextKey[Identity]{}

// captured is a tiny helper that turns slog JSON output into a
// deterministic, alphabetically-keyed string per record so the godoc
// // Output: blocks are stable.
type captured struct{ buf bytes.Buffer }

func (c *captured) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&c.buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Drop time / level so output stays deterministic.
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func (c *captured) print() {
	for _, line := range strings.Split(strings.TrimRight(c.buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		_ = json.Unmarshal([]byte(line), &rec)
		keys := make([]string, 0, len(rec))
		for k := range rec {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, rec[k]))
		}
		fmt.Println(strings.Join(parts, " "))
	}
}

// --- ExampleContextKey ------------------------------------------------------

// ExampleContextKey shows the basic ContextKey[T] mechanism: declare a key
// once, inject from the caller, read inside any Steper.
func ExampleContextKey() {
	ctx := IdentityKey.With(context.Background(), Identity{TenantID: "t-1", SubID: "s-1"})

	useStep := flow.Func("UseIdentity", func(ctx context.Context) error {
		id, ok := IdentityKey.From(ctx)
		fmt.Printf("id=%+v ok=%v\n", id, ok)
		return nil
	})

	w := new(flow.Workflow)
	w.Add(flow.Step(useStep))
	_ = w.Do(ctx)
	// Output:
	// id={TenantID:t-1 SubID:s-1} ok=true
}

// --- ExampleLogger ----------------------------------------------------------

// ExampleLogger shows the canonical flow.Logger key: inject a *slog.Logger
// once, read it (with a default fallback) inside any Steper.
func ExampleLogger() {
	cap := &captured{}
	ctx := flow.Logger.With(context.Background(), cap.logger())

	step := flow.Func("Greet", func(ctx context.Context) error {
		log := flow.Logger.FromOr(ctx, slog.Default())
		log.Info("hello", "who", "world")
		return nil
	})

	w := new(flow.Workflow)
	w.Add(flow.Step(step))
	_ = w.Do(ctx)

	cap.print()
	// Output:
	// msg=hello who=world
}

// --- ExampleLogStepFields ---------------------------------------------------

// ExampleLogStepFields shows the two ready-made interceptors at work:
// LogStepFields binds step=<flow.String(step)> (plus any caller-supplied
// extras) onto the ctx logger, and LogAttemptField binds attempt=N inside
// the retry loop. The step's Do() then logs only business attributes.
func ExampleLogStepFields() {
	cap := &captured{}
	ctx := flow.Logger.With(context.Background(), cap.logger())

	greet := flow.Func("Greet", func(ctx context.Context) error {
		log := flow.Logger.FromOr(ctx, slog.Default())
		log.Info("greeting", "name", "Ada")
		return nil
	})

	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			StepInterceptors: []flow.StepInterceptor{
				flow.LogStepFields(
					func(_ context.Context, _ flow.Steper) []any { return []any{"tenant", "acme"} },
				),
			},
			AttemptInterceptors: []flow.AttemptInterceptor{flow.LogAttemptField()},
		},
	}
	w.Add(flow.Step(greet))
	_ = w.Do(ctx)

	cap.print()
	// Output:
	// attempt=0 msg=greeting name=Ada step=Greet tenant=acme
}
